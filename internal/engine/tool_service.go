package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/GrayCodeAI/rho/internal/engine/safety"

	"github.com/GrayCodeAI/rho/internal/engine/token"

	"github.com/GrayCodeAI/rho/internal/engine/planning"
	"github.com/GrayCodeAI/rho/internal/hooks"
	"github.com/GrayCodeAI/rho/internal/intelligence/repomap"
	"github.com/GrayCodeAI/rho/internal/observability/metrics"
	"github.com/GrayCodeAI/rho/internal/observability/oteltrace"
	"github.com/GrayCodeAI/rho/internal/prompts"
	"github.com/GrayCodeAI/rho/internal/securitylog"
	"github.com/GrayCodeAI/rho/internal/tool"
	"github.com/GrayCodeAI/rho/internal/types"
)

// ToolService is the Session's view of the tool execution surface:
// the tool registry, the post-call pipeline, blast-radius estimation,
// and the per-tool timeout. Extracted from Session in Phase 6 of the
// god-object decomposition (see docs/session-decomposition.md).
type ToolService struct {
	registry          *tool.Registry
	tracer            *oteltrace.Tracer
	agentSpawn        tool.AgentSpawnFn
	snapshots         SnapshotTracker
	bgMu              sync.Mutex
	executionConfigMu sync.RWMutex
	workingDir        string
	readOnlyBash      bool
	autoCommit        bool
	bgManager         *tool.BackgroundAgentManager
	deps              toolExecutionDeps
	metrics           *metrics.Registry
	auditLog          *securitylog.Log
	pipeline          *tool.Pipeline

	// semanticMu guards semanticIdx, the lazily-built local code-search index.
	semanticMu  sync.Mutex
	semanticIdx *repomap.SemanticIndex
}

// semanticIndex returns the cached TF-IDF code-search index, building it from
// the working directory on first use. It is safe for concurrent callers.
func (s *ToolService) semanticIndex() (*repomap.SemanticIndex, error) {
	s.semanticMu.Lock()
	defer s.semanticMu.Unlock()
	if s.semanticIdx != nil {
		return s.semanticIdx, nil
	}
	dir := s.WorkingDir()
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if dir == "" {
		return nil, fmt.Errorf("code search unavailable: no working directory")
	}
	idx, err := repomap.BuildSemanticIndex(dir, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("code search index: %w", err)
	}
	s.semanticIdx = idx
	return idx, nil
}

// RefreshCodeIndex drops the cached semantic index so the next search rebuilds
// it from disk.
func (s *ToolService) RefreshCodeIndex() error {
	if s == nil {
		return nil
	}
	s.semanticMu.Lock()
	s.semanticIdx = nil
	s.semanticMu.Unlock()
	_, err := s.semanticIndex()
	return err
}

func (s *ToolService) SetAgentSpawnFn(fn tool.AgentSpawnFn) {
	if s != nil {
		s.agentSpawn = fn
	}
}

func (s *ToolService) AgentSpawnFn() tool.AgentSpawnFn {
	if s == nil {
		return nil
	}
	return s.agentSpawn
}

// toolExecutionDeps contains the service-owned collaborators needed for one
// raw tool invocation. Keeping these dependencies on ToolService removes the
// permission, approval, tracing, timeout, and retry boundary from Session;
// post-call product hooks remain in Session until the next migration slice.
type toolExecutionDeps struct {
	permissions        *PermissionService
	chat               *ChatService
	memory             *MemoryService
	agentSpawn         tool.AgentSpawnFn
	askUser            func(string) (string, error)
	workingDir         string
	checkApproval      func(context.Context, string, map[string]interface{}) (bool, string)
	recordPolicy       func(types.ToolCall, string, bool, string)
	recordVerification func(types.ToolCall, string, bool)
	redactOutput       func(string) string
	lifecycle          *LifecycleService
	appendSystem       func(string)
	taskExec           tool.TaskExecutorFunc
}

// NewToolService constructs a ToolService with the given registry.
func NewToolService(registry *tool.Registry) *ToolService {
	return &ToolService{registry: registry}
}

// Pipeline returns the tool interception pipeline (waterfall over the pre/post stages).
// Never nil: it is materialized on first access so callers can register before the
// service graph is fully wired. An empty pipeline is a strict pass-through.
func (s *ToolService) Pipeline() *tool.Pipeline {
	if s == nil {
		return nil
	}
	if s.pipeline == nil {
		s.pipeline = tool.NewPipeline()
	}
	return s.pipeline
}

// SetPipeline replaces the tool interception pipeline. New code should prefer
// s.Pipeline().Register(...) so registrations never clobber each other; SetPipeline
// exists for tests and for composition roots that build the pipeline wholesale.
func (s *ToolService) SetPipeline(p *tool.Pipeline) {
	s.executionConfigMu.Lock()
	s.pipeline = p
	s.executionConfigMu.Unlock()
}

// WithExecutionDeps binds the extracted service graph used by ExecuteOne.
func (s *ToolService) WithExecutionDeps(deps toolExecutionDeps) *ToolService {
	s.executionConfigMu.Lock()
	defer s.executionConfigMu.Unlock()
	s.deps = deps
	s.workingDir = deps.workingDir
	return s
}

// SetWorkingDir configures the preferred working directory for tool execution
// and graph observations.
func (s *ToolService) SetWorkingDir(dir string) {
	if s == nil {
		return
	}
	s.executionConfigMu.Lock()
	defer s.executionConfigMu.Unlock()
	s.workingDir = dir
	s.deps.workingDir = dir
}

// WorkingDir returns the preferred working directory for tool execution.
func (s *ToolService) WorkingDir() string {
	if s == nil {
		return ""
	}
	s.executionConfigMu.RLock()
	defer s.executionConfigMu.RUnlock()
	return s.workingDir
}

// SetReadOnlyBash enables the explore/plan Bash allowlist for this tool
// service and all subsequent tool contexts.
func (s *ToolService) SetReadOnlyBash(enabled bool) {
	if s == nil {
		return
	}
	s.executionConfigMu.Lock()
	defer s.executionConfigMu.Unlock()
	s.readOnlyBash = enabled
}

// ReadOnlyBash reports whether Bash is restricted to the explore/plan
// allowlist.
func (s *ToolService) ReadOnlyBash() bool {
	if s == nil {
		return false
	}
	s.executionConfigMu.RLock()
	defer s.executionConfigMu.RUnlock()
	return s.readOnlyBash
}

// SetAutoCommit enables git auto-commit after successful Write/Edit/StructuredEdit.
func (s *ToolService) SetAutoCommit(enabled bool) {
	if s == nil {
		return
	}
	s.executionConfigMu.Lock()
	defer s.executionConfigMu.Unlock()
	s.autoCommit = enabled
}

// AutoCommit reports whether write tools should auto-commit.
func (s *ToolService) AutoCommit() bool {
	if s == nil {
		return false
	}
	s.executionConfigMu.RLock()
	defer s.executionConfigMu.RUnlock()
	return s.autoCommit
}

// WithMetrics attaches the registry used for tool execution counters.
func (s *ToolService) WithMetrics(registry *metrics.Registry) *ToolService {
	s.metrics = registry
	return s
}

// WithTracer configures the OTel tracer.
func (s *ToolService) WithTracer(t *oteltrace.Tracer) *ToolService {
	s.tracer = t
	return s
}

// WithAuditLog configures the tamper-evident security event log.
// When set, every tool execution is recorded as a security event.
func (s *ToolService) WithAuditLog(l *securitylog.Log) *ToolService {
	s.auditLog = l
	return s
}

// Tracer returns the tool/runtime tracer shared by session loop spans.
func (s *ToolService) Tracer() *oteltrace.Tracer {
	if s == nil {
		return nil
	}
	return s.tracer
}

// WithSnapshots configures the snapshot tracker.
func (s *ToolService) WithSnapshots(snap SnapshotTracker) *ToolService {
	s.snapshots = snap
	return s
}

// WithBackgroundManager configures the background sub-agent manager.
func (s *ToolService) WithBackgroundManager(bm *tool.BackgroundAgentManager) *ToolService {
	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	s.bgManager = bm
	return s
}

// EnsureBackgroundManager returns the configured background manager, creating
// one exactly once when the session has not supplied one. Tool execution may
// initialize this lazily from concurrent read-only calls, so the operation
// must be atomic at the service boundary.
func (s *ToolService) EnsureBackgroundManager() *tool.BackgroundAgentManager {
	if s == nil {
		return nil
	}
	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	if s.bgManager == nil {
		s.bgManager = tool.NewBackgroundAgentManager()
	}
	return s.bgManager
}

// Registry returns the tool registry.
func (s *ToolService) Registry() *tool.Registry { return s.registry }

// Classify splits tool calls into concurrent (read-only) and
// sequential (write) batches.
func (s *ToolService) Classify(calls []types.ToolCall) (concurrent, sequential []types.ToolCall) {
	for _, tc := range calls {
		if tool.IsReadOnly(tc.Name) {
			concurrent = append(concurrent, tc)
		} else {
			sequential = append(sequential, tc)
		}
	}
	return
}

// emitEvent sends an event to the stream channel, abandoning the send if the
// context is cancelled or the channel is nil. Without this, a consumer that
// stops draining (TUI quit, daemon client disconnect) blocks tool goroutines
// forever and ExecuteAll's wg.Wait() never returns.
func emitEvent(ctx context.Context, ch chan<- StreamEvent, ev StreamEvent) {
	if ch == nil {
		return
	}
	select {
	case ch <- ev:
	case <-ctx.Done():
	}
}

// ExecuteAll runs the complete tool batch pipeline. The service owns the
// public operation and callers no longer need to reach into Session's
// unexported execution method. An unconfigured service produces deterministic
// errors instead of panicking.
func (s *ToolService) ExecuteAll(ctx context.Context, calls []types.ToolCall, ch chan<- StreamEvent, turn int, intent string) []toolExecResult {
	if s == nil || s.deps.permissions == nil {
		results := make([]toolExecResult, len(calls))
		for i, call := range calls {
			msg := "tool execution service is unavailable"
			results[i] = toolExecResult{tc: call, output: msg, isErr: true}
			emitEvent(ctx, ch, StreamEvent{Type: "tool_result", ToolName: call.Name, Content: msg})
		}
		return results
	}
	plannedCalls := make([]planning.PlannedCall, len(calls))
	concurrentCalls := make([]indexedToolCall, 0, len(calls))
	sequentialCalls := make([]indexedToolCall, 0, len(calls))
	for i, call := range calls {
		targets := s.ExtractTargets(call)
		plannedCalls[i] = planning.PlannedCall{ToolName: call.Name, Args: call.Arguments, Targets: targets}
		item := indexedToolCall{index: i, tc: call}
		if tool.IsReadOnly(call.Name) {
			concurrentCalls = append(concurrentCalls, item)
		} else {
			sequentialCalls = append(sequentialCalls, item)
		}
	}
	if report := planning.EstimateBlastRadius(plannedCalls); report.Radius.NeedsConfirmation() && ch != nil {
		emitEvent(ctx, ch, StreamEvent{Type: "blast_radius", Content: report.Message})
	}

	results := make([]toolExecResult, len(calls))
	readOnlySem := make(chan struct{}, maxConcurrentReadOnlyToolCalls)
	networkSem := make(chan struct{}, maxConcurrentNetworkReadOnlyToolCalls)
	var wg sync.WaitGroup
	for _, item := range concurrentCalls {
		wg.Add(1)
		go func(item indexedToolCall) {
			defer wg.Done()
			readOnlySem <- struct{}{}
			defer func() { <-readOnlySem }()
			if isNetworkReadOnlyTool(item.tc.Name) {
				networkSem <- struct{}{}
				defer func() { <-networkSem }()
			}
			results[item.index] = s.ExecuteOne(ctx, item.tc, nil, ch, turn, intent)
		}(item)
	}
	wg.Wait()
	for _, item := range sequentialCalls {
		results[item.index] = s.ExecuteOne(ctx, item.tc, nil, ch, turn, intent)
	}
	return results
}

// runPreStage executes the StagePreExecute interceptors. It returns nil when the
// pipeline passes or is empty, and the interceptor's error when it short-circuits.
func (s *ToolService) runPreStage(ctx context.Context, tc types.ToolCall, override tool.Tool) error {
	if s == nil || s.pipeline == nil {
		return nil
	}
	return s.pipeline.Run(tool.StagePreExecute, ctx, tool.ToolRequest{Call: tc, Tool: override}, nil)
}

func bool2tag(isErr bool) string {
	if isErr {
		return "pipeline_error"
	}
	return "pipeline_stop"
}

// ExecuteOne performs the service-owned tool invocation: event
// emission, permission/approval, tracing, tool context, lookup, timeout,
// retry, and raw execution. PostProcess and CompleteResult
// own the remaining result lifecycle.
func (s *ToolService) ExecuteOne(ctx context.Context, tc types.ToolCall, override tool.Tool, ch chan<- StreamEvent, turn int, intent string) toolExecResult {
	result := toolExecResult{tc: tc, state: ToolStateValidating}
	emitEvent(ctx, ch, StreamEvent{Type: "tool_use", ToolName: tc.Name, ToolID: tc.ID, ToolState: ToolStateValidating})
	var span *oteltrace.Span
	if s.tracer != nil {
		_, span = oteltrace.StartToolSpan(ctx, s.tracer, tc.Name, tc.ID)
	}
	finishDenied := func(tag string, msg string) toolExecResult {
		emitEvent(ctx, ch, StreamEvent{Type: "tool_result", ToolName: tc.Name, Content: msg, ToolState: ToolStateFailed, ToolReason: result.reason})
		if span != nil {
			span.SetTag(tag, "true")
			span.Finish()
		}
		result.output, result.isErr, result.err, result.span = msg, true, fmt.Errorf("%s", msg), nil
		result.state = ToolStateFailed
		switch tag {
		case "denied":
			result.reason = ToolReasonPermission
		case "approval_denied":
			result.reason = ToolReasonApproval
		case "error":
			result.reason = ToolReasonUnknown
		case "pipeline_error":
			result.reason = ToolReasonPipelineFailure
		default:
			result.reason = ToolReasonExecutionError
		}
		return result
	}
	if s.deps.permissions == nil {
		return finishDenied("denied", "permission service is unavailable")
	}
	// Dynamic tools may be registered after the session's initial permission
	// snapshot. Observe the concrete implementation immediately before policy
	// evaluation so external MCP/plugin tools cannot inherit registry trust.
	candidate := override
	if candidate == nil && s.registry != nil {
		candidate, _ = s.registry.Get(tc.Name)
	}
	if external, ok := candidate.(tool.UntrustedTool); ok && external.Untrusted() {
		s.deps.permissions.MarkToolUntrusted(tc.Name)
	}
	result.state = ToolStatePermissionWait
	granted, denyMsg := s.deps.permissions.CheckTool(ctx, safety.ToolCallInfo{Name: tc.Name, ID: tc.ID, Args: tc.Arguments})
	if s.deps.recordPolicy != nil {
		s.deps.recordPolicy(tc, "permission", granted, denyMsg)
	}
	if !granted {
		return finishDenied("denied", denyMsg)
	}
	// Tool pipeline (stage pre): the deepseek-harness tools/pre-execute
	// waterfall. An empty pipeline is a strict pass-through; once registered,
	// the first interceptor to short-circuit stops the call before approval and
	// execution. Runs after the permission engine, which can only loosen — never
	// gate — an instrumented interceptor decision, preserving fail-closed ordering.
	if err := s.runPreStage(ctx, tc, override); err != nil {
		var sc *tool.ShortCircuit
		if errors.As(err, &sc) {
			msg, isErr := sc.ToolError()
			if msg != "" {
				return finishDenied(bool2tag(isErr), msg)
			}
			// Silent short-circuit: no model-visible message. Stop the call
			// without surfacing an error.
		}
		return finishDenied("pipeline_error", err.Error())
	}
	if s.auditLog != nil {
		_, _ = s.auditLog.Append(
			securitylog.SeverityInfo,
			"tool_exec",
			fmt.Sprintf("tool=%s session=%s", tc.Name, tc.ID),
			tc.Name, tc.ID,
		)
	}
	if s.metrics != nil {
		s.metrics.Counter("tool_exec_total").Inc()
	}
	approved, approvalDeny := true, ""
	if s.deps.checkApproval != nil {
		approved, approvalDeny = s.deps.checkApproval(ctx, tc.Name, tc.Arguments)
	}
	if s.deps.permissions.ApprovalEnabled() && s.deps.recordPolicy != nil {
		s.deps.recordPolicy(tc, "approval", approved, approvalDeny)
	}
	if !approved {
		return finishDenied("approval_denied", approvalDeny)
	}
	hooks.ExecuteAsync(ctx, hooks.EventPreTool, map[string]interface{}{"tool": tc.Name, "args": tc.Arguments})
	inputJSON, _ := json.Marshal(tc.Arguments)
	var commitChat func(context.Context, string) (string, error)
	if s.deps.chat != nil {
		commitChat = func(chatCtx context.Context, prompt string) (string, error) {
			resp, err := s.deps.chat.Chat(chatCtx, []types.FluxMessage{{Role: "user", Content: prompt}}, types.ChatOptions{Provider: s.deps.chat.Provider(), Model: s.deps.chat.Model(), MaxTokens: 256})
			if err != nil {
				return "", err
			}
			if resp == nil {
				return "", fmt.Errorf("commit message model returned no response")
			}
			return resp.Content, nil
		}
	}
	var available []tool.Tool
	if s.registry != nil {
		// Full primary set so ToolSearch can discover lazy/optional tools.
		available = s.registry.PrimaryTools()
	}
	toolCtx := tool.WithToolContext(ctx, &tool.ToolContext{
		AgentSpawnFn:        s.deps.agentSpawn,
		AskUserFn:           s.deps.askUser,
		CommitMessageChatFn: commitChat,
		SpecSlugGet:         func() string { return s.deps.permissions.SpecSlug() },
		SpecSlugSet:         func(slug string) { s.deps.permissions.SetSpecSlug(slug) },
		AllowedDirectories:  s.deps.permissions.AllowedDirs(),
		BackgroundManager:   s.EnsureBackgroundManager(),
		ReadOnlyBash:        s.ReadOnlyBash(),
		WorkingDir:          s.WorkingDir(),
		AvailableTools:      available,
		Registry:            s.registry,
		AutoCommit:          s.AutoCommit(),
		TaskExecutor:        s.deps.taskExec,
		// Semantic code search backed by the local TF-IDF index. The index is
		// built lazily from the working directory and cached on the service.
		CodeSearchFn: func(cctx context.Context, query string, limit int) ([]tool.CodeSearchResult, error) {
			idx, err := s.semanticIndex()
			if err != nil {
				return nil, err
			}
			chunks := idx.Search(query, limit)
			out := make([]tool.CodeSearchResult, 0, len(chunks))
			for _, c := range chunks {
				out = append(out, tool.CodeSearchResult{
					Path:      c.Path,
					StartLine: c.StartLine,
					EndLine:   c.EndLine,
					Content:   c.Content,
					Language:  tool.LanguageForFile(c.Path),
				})
			}
			return out, nil
		},
		RefreshCodeIndexFn: func(cctx context.Context) error {
			s.semanticMu.Lock()
			s.semanticIdx = nil
			s.semanticMu.Unlock()
			_, err := s.semanticIndex()
			return err
		},
	})
	t := override
	if t == nil && s.registry != nil {
		var ok bool
		t, ok = s.registry.Get(tc.Name)
		if !ok {
			return finishDenied("error", fmt.Sprintf("Error: unknown tool: %s", tc.Name))
		}
	}
	if t == nil {
		return finishDenied("error", "Error: tool is unavailable")
	}
	result.state = ToolStateExecuting
	// Tool-declared timeout policy (DSH tool-declared-budget parity): the
	// tool's own declared budget wins; tools that don't declare one keep the
	// name-based fallback. Zero-config — declaring tools opt in by
	// implementing tool.TimeoutProvider.
	timeout := toolTimeout(tc.Name)
	if declared := tool.TimeoutOf(t); declared > 0 {
		timeout = declared
	}
	toolCtx, cancel := context.WithTimeout(toolCtx, timeout)
	var output string
	var execErr error
	if rpp, ok := t.(tool.RetryPolicyProvider); ok {
		output, execErr = tool.RetryExecutor(toolCtx, t, inputJSON, rpp.RetryPolicy())
	} else {
		output, execErr = tool.RetryExecutor(toolCtx, t, inputJSON, tool.DefaultRetryPolicy())
	}
	// Only a deadline this call imposed on the execution context is labelled
	// TOOL_TIMEOUT; a DeadlineExceeded the tool produced on its own (while the
	// outer budget was still live) keeps the generic error vocabulary.
	timedOut := errors.Is(execErr, context.DeadlineExceeded) && toolCtx.Err() == context.DeadlineExceeded
	cancel()
	result.output, result.err, result.isErr, result.span = output, execErr, execErr != nil, span
	result.state = ToolStateCompleted
	result.reason = ToolReasonCompleted
	if result.isErr {
		result.state = ToolStateFailed
		result.reason = ToolReasonExecutionError
		if timedOut {
			result.state = ToolStateTimedOut
			result.reason = ToolReasonTimeout
		} else if errors.Is(execErr, context.Canceled) {
			result.state = ToolStateCancelled
			result.reason = ToolReasonCancelled
		}
		// Preserve any partial output the tool produced before failing so the
		// LLM can see what happened, then append the error. A deadline that
		// won surfaces the structured TOOL_TIMEOUT vocabulary so the model sees
		// how long it waited instead of a raw "context deadline exceeded".
		if timedOut {
			result.output = output + "\n\nError: tool call timed out after " + timeout.String() + " (code TOOL_TIMEOUT)"
		} else {
			result.output = output + "\n\nError: " + execErr.Error()
		}
	}
	return result
}

// NormalizeOutput applies the deterministic context-safety policy to a tool
// result before it is persisted or sent to the model. Keeping this in the
// tool service makes output limits consistent for agent-loop and slash-command
// execution paths.
func (s *ToolService) NormalizeOutput(output, canonicalTool, toolID string, contextWindow int) string {
	maxChars := 50000
	if contextWindow > 0 {
		dynamic := contextWindow * 20 / 100 * 4
		if dynamic < 5000 {
			dynamic = 5000
		}
		if dynamic < maxChars {
			maxChars = dynamic
		}
	}
	compressBudget := maxChars / 2
	if len(output) > compressBudget {
		compressed, tokens := token.CompressForContext(output, compressBudget/4)
		if tokens > 0 && tokens < token.CountTokensFast(output) {
			output = compressed
		}
	}
	if len(output) > maxChars {
		output = truncateOutputStructurally(output, maxChars)
	}
	return maybeSpillToolOutput(output, canonicalTool, toolID)
}

// truncateOutputStructurally trims oversized tool output at a structural
// boundary instead of a raw byte cut, so JSON-ish results keep whole lines
// (or whole array elements) rather than being chopped mid-object (Phase 3).
func truncateOutputStructurally(output string, maxChars int) string {
	trimmed := strings.TrimLeft(output, " \t\r\n")
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		// Pretty-printed: prefer the last newline before the cap.
		if cut := strings.LastIndex(output[:maxChars], "\n"); cut >= 0 {
			return appendElisionMarker(output[:cut], output[cut:])
		}
		// Single-line JSON: splice at the last TOP-LEVEL element separator so
		// each kept record stays complete (a naive comma search can land
		// inside an object, orphaning half a record on both sides).
		if cut := lastTopLevelComma(output, maxChars); cut > 0 {
			return appendElisionMarker(output[:cut+1], output[cut+1:])
		}
		// No safe splice: fall back to the byte cap.
		return appendElisionMarker(output[:maxChars], output[maxChars:])
	}
	// Plain text: cut at the last line boundary to keep whole lines.
	if cut := strings.LastIndex(output[:maxChars], "\n"); cut > 0 {
		return appendElisionMarker(output[:cut], output[cut:])
	}
	return appendElisionMarker(output[:maxChars], output[maxChars:])
}

// lastTopLevelComma returns the index of the last comma at bracket depth 1
// within s[:limit] of an outer array/object, or -1.
func lastTopLevelComma(s string, limit int) int {
	depth := 0
	inStr := false
	esc := false
	last := -1
	n := limit
	if n > len(s) {
		n = len(s)
	}
	for i := 0; i < n; i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		case ',':
			if depth == 1 {
				last = i
			}
		}
	}
	return last
}

// PostProcess applies the domain mutation/validation hooks that follow a raw
// tool invocation. It is intentionally separate from CompleteResult so the
// final event contract remains uniform even when a hook changes the output or
// converts a successful mutation into an error.
func (s *ToolService) PostProcess(ctx context.Context, result toolExecResult, turn int, intent string, contextWindow int) toolExecResult {
	output, isErr := result.output, result.isErr
	canonical := canonicalToolName(result.tc.Name)
	life := s.deps.lifecycle
	if life != nil && life.Limits() != nil {
		life.Limits().RecordToolCall(result.tc.Name)
	}
	if life != nil && life.Beliefs() != nil && (canonical == "Read" || canonical == "Grep" || canonical == "Glob" || canonical == "LS") {
		subject := result.tc.Name
		if p, ok := pathArgument(result.tc.Arguments); ok {
			subject = p
		}
		contentSummary := output
		if len(contentSummary) > 200 {
			contentSummary = contentSummary[:200]
		}
		life.Beliefs().Record("file_purpose", subject, contentSummary, turn)
	}
	if s.deps.memory != nil && s.deps.memory.Enhanced() != nil && (canonical == "Read" || canonical == "Edit" || canonical == "Write") {
		if p, ok := pathArgument(result.tc.Arguments); ok && p != "" {
			if proactiveCtx := s.deps.memory.Enhanced().ProactiveContextForFile(p); proactiveCtx != "" && s.deps.appendSystem != nil {
				s.deps.appendSystem(proactiveCtx)
			}
		}
	}
	if life != nil && life.Beliefs() != nil && (canonical == "Write" || canonical == "Edit") {
		if p, ok := pathArgument(result.tc.Arguments); ok {
			life.Beliefs().Invalidate(p)
		}
	}
	if life != nil && life.AgentsAccum() != nil && !isErr && (canonical == "Write" || canonical == "Edit") {
		if p, ok := pathArgument(result.tc.Arguments); ok && p != "" {
			pattern := prompts.ExtractPattern(result.tc.Name, p, output)
			life.AgentsAccum().Record(intent, pattern, []string{p})
			if err := life.AgentsAccum().Flush(); err != nil {
				slog.Warn("failed to flush agents accumulator", "error", err)
			}
		}
	}
	if life != nil && life.Critic() != nil && !isErr && (canonical == "Write" || canonical == "Edit") {
		if p, ok := pathArgument(result.tc.Arguments); ok {
			origContent := ""
			if data, readErr := readFileContent(p); readErr == nil {
				origContent = data
			}
			verdict := life.Critic().PreScreenPatch(origContent, output, intent)
			if life.Critic().ShouldBlock(verdict) {
				output = fmt.Sprintf("Patch rejected by validator: %s. Try again.", strings.Join(verdict.Issues, "; "))
				isErr = true
			}
		}
	}
	if life != nil && life.Shadow() != nil && !isErr && (canonical == "Write" || canonical == "Edit") {
		if p, ok := pathArgument(result.tc.Arguments); ok {
			validationErrs := life.Shadow().ValidateEdit(p, output)
			if len(validationErrs) > 0 {
				warnings := make([]string, 0, len(validationErrs))
				for _, ve := range validationErrs {
					warnings = append(warnings, ve.Message)
				}
				output += fmt.Sprintf("\n\nValidation warnings: %s", strings.Join(warnings, "; "))
			}
		}
	}
	if life != nil && life.LintLoop() != nil && life.LintLoop().Enabled && !isErr && (canonical == "Write" || canonical == "Edit") {
		if p, ok := pathArgument(result.tc.Arguments); ok {
			count := life.LintLoop().ReflectionCount(p)
			if life.LintLoop().ShouldRetry(count) {
				if lintResult, lintErr := life.LintLoop().RunLint(p); lintErr == nil && lintResult != nil {
					if reflected := life.LintLoop().BuildReflectedMessage(lintResult); reflected != "" {
						life.LintLoop().RecordReflection(p)
						output += "\n\n" + reflected
					}
				}
			}
		}
	}
	output = s.NormalizeOutput(output, canonical, result.tc.ID, contextWindow)
	// Tool pipeline (stage post): the deepseek-harness tools/post-execute
	// waterfall. Registered interceptors observe and may replace the normalized
	// result before the lifecycle hooks below. An empty pipeline is a strict
	// pass-through.
	if s.pipeline != nil {
		post := &tool.ToolResult{
			Request: tool.ToolRequest{Call: result.tc, Tool: result.tool},
			Output:  output,
			IsError: isErr,
		}
		if runErr := s.pipeline.Run(tool.StagePostExecute, ctx, post.Request, post); runErr != nil {
			var sc *tool.ShortCircuit
			if errors.As(runErr, &sc) {
				if msg, scErr := sc.ToolError(); msg != "" {
					output, isErr = msg, scErr
				}
			}
		} else {
			output, isErr = post.Output, post.IsError
		}
	}
	if life != nil && life.Pipeline() != nil {
		var execErr error
		if isErr {
			execErr = fmt.Errorf("%s", output)
		}
		if toolResult := life.Pipeline().PostToolExecution(result.tc.Name, result.tc.Arguments, output, execErr); toolResult != nil {
			if toolResult.StallWarning != "" {
				output += "\n\n" + toolResult.StallWarning
			}
			if toolResult.LintErrors != "" {
				output += "\n\nLint: " + toolResult.LintErrors
			}
			if toolResult.RecoveryAction != "" && toolResult.ShouldRetry {
				output += "\n\nRecovery suggestion: " + toolResult.RecoveryAction
			}
		}
	}
	result.output, result.isErr = output, isErr
	if isErr && result.reason == ToolReasonCompleted {
		result.state = ToolStateFailed
		result.reason = ToolReasonExecutionError
	}
	return result
}

// CompleteResult owns the service-level completion contract after Session's
// domain-specific post-processing has finished.
func (s *ToolService) CompleteResult(ctx context.Context, result toolExecResult, ch chan<- StreamEvent) toolExecResult {
	output, isErr := result.output, result.isErr
	if !isErr && s.deps.permissions != nil {
		switch canonicalToolName(result.tc.Name) {
		case "Specify", "Plan", "Tasks", "SpecReset":
			s.deps.permissions.AdvanceSpecStage(result.tc.Name)
		case "ApproveImplementation":
			s.deps.permissions.AdvanceSpecStage(result.tc.Name)
			output = "Spec approved — switched to implementation. You may now make changes."
		}
	}
	if s.metrics != nil {
		s.metrics.Counter("tools.executed").Inc()
		if isErr {
			s.metrics.Counter("tools.errors").Inc()
		}
	}
	if s.deps.memory != nil && s.deps.memory.Enhanced() != nil {
		s.deps.memory.Enhanced().OnToolResult(result.tc.Name, result.tc.Arguments, output, isErr)
	}
	hooks.ExecuteAsync(ctx, hooks.EventPostTool, map[string]interface{}{
		"tool": result.tc.Name, "output": output, "is_err": isErr,
	})
	if s.deps.recordVerification != nil {
		s.deps.recordVerification(result.tc, output, isErr)
	}
	// Redact tool output before it reaches the user-facing stream event so
	// secrets never appear on screen (the model copy is redacted separately
	// in Session). Falls back to unchanged output when no redactor is wired.
	if s.deps.redactOutput != nil {
		output = s.deps.redactOutput(output)
	}
	emitEvent(ctx, ch, StreamEvent{
		Type:       "tool_result",
		ToolName:   result.tc.Name,
		Content:    output,
		ToolState:  result.state,
		ToolReason: result.reason,
	})
	if result.span != nil {
		if isErr {
			result.span.SetTag("error", "true")
		}
		result.span.Finish()
	}
	result.output = output
	return result
}

// ExtractTargets returns the file targets for a tool call.
func (s *ToolService) ExtractTargets(tc types.ToolCall) []string {
	if s == nil || s.registry == nil {
		return extractTargets(tc)
	}
	if t, ok := s.registry.Get(tc.Name); ok {
		return ExtractTargetsFromSchema(t, tc)
	}
	return extractTargets(tc)
}

// EstimateBlastRadius returns a blast-radius report for a set of
// planned tool calls. Drives the "needs confirmation" prompt.
func (s *ToolService) EstimateBlastRadius(planned []planning.PlannedCall) *planning.BlastRadiusReport {
	return planning.EstimateBlastRadius(planned)
}

// ExecuteRegistered is the compatibility entry point for callers that still
// use the legacy API. Delegate to the canonical ExecuteOne/CompleteResult
// pipeline so permission, approval, context, timeout, hooks, and redaction
// cannot be bypassed.
func (s *ToolService) ExecuteRegistered(ctx context.Context, tc types.ToolCall, ch chan<- StreamEvent) (string, bool) {
	result := s.ExecuteOne(ctx, tc, nil, ch, 0, "")
	result = s.CompleteResult(ctx, result, ch)
	return result.output, result.isErr
}

// BackgroundManager returns the background sub-agent manager, or nil
// if background mode is not available.
func (s *ToolService) BackgroundManager() *tool.BackgroundAgentManager {
	if s == nil {
		return nil
	}
	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	return s.bgManager
}

// Snapshots returns the configured automatic snapshot tracker.
func (s *ToolService) Snapshots() SnapshotTracker { return s.snapshots }
