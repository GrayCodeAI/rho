package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine/safety"

	"github.com/GrayCodeAI/rho/internal/engine/cost"

	"github.com/GrayCodeAI/rho/internal/engine/streaming"
	"github.com/GrayCodeAI/rho/internal/engine/validation"

	agentcontracts "github.com/GrayCodeAI/rho/internal/contracts/agent"
	"github.com/GrayCodeAI/rho/internal/conversationarc"
	"github.com/GrayCodeAI/rho/internal/engine/planning"
	"github.com/GrayCodeAI/rho/internal/eventlog"
	"github.com/GrayCodeAI/rho/internal/observability/logger"
	"github.com/GrayCodeAI/rho/internal/observability/metrics"
	"github.com/GrayCodeAI/rho/internal/observability/oteltrace"
	"github.com/GrayCodeAI/rho/internal/plugin"
	"github.com/GrayCodeAI/rho/internal/prompts"
	"github.com/GrayCodeAI/rho/internal/provider/gateway"
	"github.com/GrayCodeAI/rho/internal/resilience/ratelimit"
	"github.com/GrayCodeAI/rho/internal/schedule"
	"github.com/GrayCodeAI/rho/internal/session"
	"github.com/GrayCodeAI/rho/internal/snapshot"
	"github.com/GrayCodeAI/rho/internal/tool"
	"github.com/GrayCodeAI/rho/internal/types"
)

// MemoryRecaller abstracts memory recall/remember so engine avoids importing memory directly.
type MemoryRecaller interface {
	Recall(query string, tokenBudget int) (string, error)
	// Remember persists a content+category pair. The ctx lets background
	// callers bound the call so a slow/hung memory backend cannot leak a
	// goroutine (the HarrierBridge path honors it for network cancellation).
	Remember(ctx context.Context, content, category string) error
}

// SnapshotTracker abstracts the snapshot system so engine doesn't import snapshot directly.
type SnapshotTracker interface {
	Track(message string) (string, error)
	TrackCtx(ctx context.Context, message string) (string, error)
}

// Session manages a conversation with an LLM via flux.
// The mu RWMutex protects the remaining session metadata for concurrent
// access. Transcript and system-context state are owned by PersistenceService.
//
// Phases 1-7 of the god-object decomposition (see
// docs/session-decomposition.md) have extracted the 35-collaborator
// god object into 7 cohesive sub-services. Session is now a thin
// orchestrator that delegates to:
//
//	llm            *ChatService        (Phase 1: LLM transport)
//	perms          *PermissionService  (Phase 2: safety/approval)
//	life           *LifecycleService   (Phase 3: self-improvement loop)
//	memory         *MemoryService      (Phase 4: memory layer)
//	persist        *PersistenceService (Phase 5: conversation store)
//	tools          *ToolService        (Phase 6: tool execution)
//
// Session retains only orchestration state and integrations that do not yet
// have a dedicated service. Permission, tool execution, transcript, memory,
// and lifecycle state are owned by the corresponding services below.
type Session struct {
	mu   sync.RWMutex
	Cost cost.Cost

	// llm is the LLM transport service (Phase 1 extraction). All new
	// code should go through s.llm.* rather than duplicating transport state.
	// Named lowercase (unexported) to avoid colliding with the public
	// Session.Chat() method used by Reflector and SelfReview.
	llm *ChatService
	// perms (Phase 2), life (Phase 3), memory (Phase 4), persist
	// (Phase 5), tools (Phase 6) are the remaining 5 sub-services.
	// All optional; nil is the default and the agent loop preserves
	// its `if s.X != nil` branching.
	perms   *PermissionService
	life    *LifecycleService
	memory  *MemoryService
	persist *PersistenceService
	goals   *planning.GoalTracker // optional goal tracker; emits goal.change lifecycle events
	tools   *ToolService
	// arc is the optional durable conversation-arc sidecar (goals/decisions/
	// milestones/phase) loaded per session. See conversationarc package.
	arc *conversationarc.Arc
	// incremental is the opt-in incremental system-context reconciler for
	// dynamic sections (e.g. memories). Nil unless RHO_INCREMENTAL_CONTEXT=1.
	// See incremental.go.
	incremental *memoryIncremental
	// learnFn persists structured lessons produced by failure reflection to a
	// cross-session store (e.g. the chat client's SelfImprover). It is a
	// callback so the engine stays decoupled from storage; nil disables it.
	learnFn func(what, why, lesson, category string)
	// sec is the session's tamper-evident security event log, opened lazily on
	// the first recorded event (see security_events.go).
	sec *securityLog
	// GLMThinkingEnabled toggles GLM/Z.ai extended reasoning on outgoing requests
	// (applied only when provider is zai_payg or zai_coding). nil leaves the model default.

	// Advanced features
	//
	// Deprecated: most of these have been folded into sub-services;
	// a few remain as legacy fields without a sub-service accessor
	// (Trajectory, LintLoop, TestLoop, FileMentions, Files, Snapshots).
	// For those, keep reading the legacy field — they're
	// populated at session construction and don't have a setter.
	//   Autonomy       -> s.PermSvc().RuntimeState().Autonomy
	//   Plan           -> s.Plan (legacy field; not yet on a sub-service)
	//   Beliefs        -> s.LifecycleSvc().Beliefs()
	//   Critic         -> s.LifecycleSvc().Critic()
	//   Backtrack      -> s.LifecycleSvc().Backtrack()
	//   Limits         -> s.LifecycleSvc().Limits()
	//   Trajectory     -> legacy field; not yet on a sub-service
	//   Shadow         -> s.LifecycleSvc().Shadow()
	//   ConversationGraph -> s.Persistence().Graph()
	//   Sleeptime      -> s.MemorySvc().Sleeptime()
	//   Activity       -> s.MemorySvc().Activity()
	//   SkillDistiller -> s.MemorySvc().SkillDistiller()
	//   AgentsAccum    -> s.LifecycleSvc().AgentsAccum()
	//   FewShotStore   -> s.LifecycleSvc().FewShotStore()
	//   AdaptivePrompt -> s.LifecycleSvc().AdaptivePrompt()
	//   LintLoop       -> legacy field; not yet on a sub-service
	//   TestLoop       -> legacy field; not yet on a sub-service
	//   FileMentions   -> legacy field; not yet on a sub-service
	//   ResponseCache  -> s.LifecycleSvc().ResponseCache()
	//   Pipeline       -> s.LifecycleSvc().Pipeline()
	//   Files          -> legacy field; not yet on Persistence
	//   Steering       -> s.Persistence().Steering()
	//   Snapshots      -> legacy field; not yet on Persistence
	//   Tracer         -> legacy field; oteltrace.NewTracer() for new code
	// Backtrack and limits are owned by LifecycleService.

	// lastCompactionMsgDelta captures the message count pruned by the most
	// recent compaction pass, emitted as compaction.prune in recordCompaction.
	// Set by callers that know the before/after bounds (e.g. smartCompact).
	lastCompactionMsgDelta int
	// lastCompactionSummary captures the summary text produced by the most
	// recent LLM-based compaction pass, emitted as compaction.summary.
	lastCompactionSummary string

	// lastSkillCatalogDigest tracks the most recent skill catalog digest
	// emitted into the transcript (DSH tool-skill catalog digest).
	lastSkillCatalogDigest string

	// scheduleManager coordinates session-log-backed schedule timers.
	scheduleManager *schedule.Manager

	// promptQueue manages FIFO priority turns and steering turns.
	promptQueue *PromptQueue

	// announcements manages in-session broadcasts and notices.
	announcements *AnnouncementFeed

	// Control plane (product modes) — orthogonal to SpecStage and shellmode.
	workMode WorkMode
}

// NewSession creates a conversation session through Flux's engine facade.
func NewSession(provider, model, systemPrompt string, registry *tool.Registry) *Session {
	return NewRhoSession(context.Background(), gateway.Selection{
		Provider: provider,
		Model:    model,
	}, provider, model, systemPrompt, registry)
}

// NewSessionWithClient constructs a session with an explicit LLM client (e.g. deployment router).
func NewSessionWithClient(chat ChatClient, provider, model, systemPrompt string, registry *tool.Registry, deploymentRouting bool) *Session {
	if provider == "" || model == "" {
		slog.Debug("NewSessionWithClient called with empty provider or model", "provider", provider, "model", model)
	}
	log := logger.Default()
	s := &Session{}
	rateLimiter := ratelimit.PerSecond(10)
	s.Cost.SetModel(model)
	s.refreshContextWindowCache()

	// Initialize agents accumulator for project learnings.
	cwd, _ := os.Getwd()
	agentsAccum := prompts.NewAgentsAccumulator(cwd)

	// -----------------------------------------------------------------------
	// Wire the 6 sub-services extracted in Phases 1-6 of the god-object
	// decomposition (see docs/session-decomposition.md). New code should
	// prefer the sub-service getters (s.ChatLLM(), s.PermSvc(), etc.).
	// -----------------------------------------------------------------------
	s.llm = NewChatService(chat, ChatServiceConfig{
		Provider:          provider,
		Model:             model,
		DeploymentRouting: deploymentRouting,
		RateLimiter:       rateLimiter,
		Metrics:           metrics.NewRegistry(),
	})
	s.perms = NewPermissionService(log)
	if registry != nil {
		registered := registry.PrimaryTools()
		known := make([]string, 0, len(registered))
		untrusted := make([]string, 0)
		for _, registeredTool := range registered {
			if registeredTool != nil {
				known = append(known, registeredTool.Name())
				isUntrusted := false
				if external, ok := registeredTool.(tool.UntrustedTool); ok {
					isUntrusted = external.Untrusted()
				}
				if isUntrusted {
					untrusted = append(untrusted, registeredTool.Name())
				}
				if aliased, ok := registeredTool.(tool.AliasedTool); ok {
					known = append(known, aliased.Aliases()...)
					if isUntrusted {
						untrusted = append(untrusted, aliased.Aliases()...)
					}
				}
			}
		}
		s.perms.SetKnownTools(known)
		s.perms.SetUntrustedTools(untrusted)
	}
	s.life = NewLifecycleService(log)
	s.memory = NewMemoryService(log)
	s.persist = NewPersistenceService(log)
	s.persist.SetJournal(eventlog.New(nil))
	s.perms.SetJournal(s.persist.Journal())
	s.life.Pipeline().SetJournal(s.persist.Journal())
	// Wire goal.Change events: the planning GoalTracker emits goal.change
	// lifecycle events when goals are added/completed/failed (DSH goal.change seam).
	goalTracker := planning.NewGoalTracker()
	goalTracker.SetJournal(s.persist.Journal())
	s.goals = goalTracker
	// Emit the initial request header so the event spine records the durable
	// system prompt + tool surface (DSH request.header seam).
	if j := s.persist.Journal(); j != nil {
		toolNames := []string{}
		if registry != nil {
			toolNames = registry.ModelVisibleNames()
		}
		j.AppendRequestHeader(eventlog.RequestHeaderFact{
			System: systemPrompt,
			Tools:  toolNames,
			Reason: eventlog.RequestHeaderInitial,
		})
	}
	s.persist.SetAutoCompactThresholdPct(DefaultAutoCompactThresholdPct)
	s.persist.SetSystem(systemPrompt)
	s.tools = NewToolService(registry).WithMetrics(s.llm.Metrics()).WithTracer(oteltrace.NewTracer())
	s.tools.SetPipeline(DefaultToolPipeline())
	s.tools.WithExecutionDeps(toolExecutionDeps{
		permissions: s.perms,
		chat:        s.llm,
		memory:      s.memory,
		agentSpawn: func(ctx context.Context, req agentcontracts.SpawnRequest) (agentcontracts.SpawnResult, error) {
			if s.tools.AgentSpawnFn() == nil {
				return agentcontracts.SpawnResult{Status: agentcontracts.StatusFailed, Error: "agent spawning is unavailable"}, fmt.Errorf("agent spawning is unavailable")
			}
			return s.tools.AgentSpawnFn()(ctx, req)
		},
		askUser: func(question string) (string, error) {
			if s.perms.AskUserFn() == nil {
				return "", fmt.Errorf("ask-user callback is unavailable")
			}
			return s.perms.AskUserFn()(question)
		},
		checkApproval:      s.CheckApproval,
		recordPolicy:       s.recordPolicyObservation,
		recordVerification: s.recordVerificationObservation,
		redactOutput:       s.redactToolResult,
		lifecycle:          s.life,
		appendSystem:       s.AppendSystemContext,
		taskExec:           s.taskExecFromAgentSpawn(),
	})
	s.refreshContextWindowCache()
	s.life.SetAgentsAccumulator(agentsAccum)
	s.life.SetLintLoop(validation.NewLintLoop())
	s.life.SetTestLoop(validation.NewTestLoop())
	s.promptQueue = NewPromptQueue()
	s.announcements = NewAnnouncementFeed()
	return s
}

// PromptQueue returns the session's prompt turn queue.
func (s *Session) PromptQueue() *PromptQueue {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.promptQueue == nil {
		return nil
	}
	return s.promptQueue
}

// Announcements returns the session's announcement feed.
func (s *Session) Announcements() *AnnouncementFeed {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.announcements == nil {
		return nil
	}
	return s.announcements
}

// ReattachTransport swaps the LLM client after deployment routing or provider.json changes.
// Also reattaches the ChatService so the agent loop's `s.ChatLLM().Stream`
// call site picks up the new client (Phase 7 migration).
func (s *Session) ReattachTransport(chat ChatClient, provider string, deploymentRouting bool) {
	if chat == nil {
		return
	}
	if llm := s.ChatLLM(); llm != nil {
		llm.Reattach(chat, strings.TrimSpace(provider))
	}
	// deploymentRouting is now read through ChatService; the ChatService
	// constructed at session creation already holds the value. If a
	// future path needs to toggle it post-construction, extend
	// ChatService with a setter.
	_ = deploymentRouting
}

// taskExecFromAgentSpawn returns the TaskRun executor: it spawns a general
// sub-agent to perform a stored task, feeding the task's description, active
// form, and checkpoint as the prompt. Nil agent-spawn capability disables the
// executor (the TaskRun tool then reports that no executor is configured).
func (s *Session) taskExecFromAgentSpawn() tool.TaskExecutorFunc {
	return func(ctx context.Context, t *tool.Task) (string, error) {
		if s == nil || s.tools == nil || s.tools.AgentSpawnFn() == nil {
			return "", fmt.Errorf("task execution unavailable: no agent spawn capability")
		}
		prompt := "Execute the following task and report results.\n\nSubject: " + t.Subject +
			"\n\nDescription: " + t.Description
		if t.ActiveForm != "" {
			prompt += "\n\n(You are working on: " + t.ActiveForm + ")"
		}
		if len(t.Checkpoint) > 0 {
			b, _ := json.Marshal(t.Checkpoint)
			prompt += "\n\nPrior progress (checkpoint): " + string(b)
		}
		res, err := s.tools.AgentSpawnFn()(ctx, agentcontracts.SpawnRequest{
			Prompt:       prompt,
			Description:  "Execute task " + t.ID,
			SubagentType: "general",
		})
		if err != nil {
			return "", err
		}
		if res.Status == agentcontracts.StatusFailed {
			if res.Output != "" {
				return "", fmt.Errorf("%s", res.Output)
			}
			return "", fmt.Errorf("task agent reported failure")
		}
		out := res.Output
		if res.Summary != "" {
			if out != "" {
				out += "\n"
			}
			out += res.Summary
		}
		return out, nil
	}
}

// SubSession clones transport and routing mode for explore/general sub-agents.
func (s *Session) SubSession(model, systemPrompt string, registry *tool.Registry) *Session {
	if registry == nil {
		if tools := s.Tools(); tools != nil {
			registry = tools.Registry()
		}
	}
	var chat ChatClient
	provider := ""
	deploymentRouting := false
	if llm := s.ChatLLM(); llm != nil {
		chat = llm.Client()
		provider = llm.Provider()
		deploymentRouting = llm.DeploymentRouting()
	}
	sub := NewSessionWithClient(chat, provider, model, systemPrompt, registry, deploymentRouting)
	// Propagate the lesson callback so sub-agent failures also persist
	// cross-session lessons.
	s.mu.RLock()
	sub.learnFn = s.learnFn
	s.mu.RUnlock()
	// WireSubagent: propagate journal to sub-session's GoalTracker so
	// goal.change events on sub-agent forks also record into the spine.
	if j := s.persist.Journal(); j != nil && sub.goals != nil {
		sub.goals.SetJournal(j)
	}
	return sub
}

func (s *Session) Model() string {
	if llm := s.ChatLLM(); llm != nil {
		return llm.Model()
	}
	return ""
}

func (s *Session) Provider() string {
	if llm := s.ChatLLM(); llm != nil {
		return llm.Provider()
	}
	return ""
}

func (s *Session) Metrics() *metrics.Registry {
	if s == nil || s.ChatLLM() == nil {
		return nil
	}
	return s.ChatLLM().Metrics()
}

// Logger returns the shared session logger through the observability boundary.
func (s *Session) Logger() *logger.Logger {
	if s == nil {
		return nil
	}
	if s.life != nil && s.life.Logger() != nil {
		return s.life.Logger()
	}
	if s.perms != nil && s.perms.Logger() != nil {
		return s.perms.Logger()
	}
	return logger.Default()
}

// TracerValue returns the session tracer through the observability boundary.
func (s *Session) TracerValue() *oteltrace.Tracer {
	if s == nil || s.tools == nil {
		return nil
	}
	return s.tools.Tracer()
}

// ChatLLM returns the extracted ChatService (Phase 1 of the god-object
// decomposition). New code should prefer this over the legacy Client /
// Provider / Model / Router fields. Returns nil only if the
// session was constructed without going through NewSessionWithClient,
// which should not happen in production.
func (s *Session) ChatLLM() *ChatService { return s.llm }

// PermSvc returns the extracted PermissionService (Phase 2). Returns
// nil only if the session was constructed without
// NewSessionWithClient, which should not happen in production.
func (s *Session) PermSvc() *PermissionService { return s.perms }

// LifecycleSvc returns the extracted LifecycleService (Phase 3).
func (s *Session) LifecycleSvc() *LifecycleService { return s.life }

// SetLearnFn installs the callback that persists structured lessons produced
// by failure reflection. Set it to a SelfImprover.Learn-compatible function
// to close the loop: reflections then survive the session.
func (s *Session) SetLearnFn(fn func(what, why, lesson, category string)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.learnFn = fn
	s.mu.Unlock()
}

// SetArc attaches the session's durable conversation-arc sidecar.
func (s *Session) SetArc(a *conversationarc.Arc) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.arc = a
	s.mu.Unlock()
}

// Arc returns the attached conversation arc, or nil when none is set.
func (s *Session) Arc() *conversationarc.Arc {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	a := s.arc
	s.mu.RUnlock()
	return a
}

// Learn persists a lesson through the configured callback. Safe to call with
// nil session or no callback installed.
func (s *Session) Learn(what, why, lesson, category string) {
	if s == nil {
		return
	}
	s.mu.RLock()
	fn := s.learnFn
	s.mu.RUnlock()
	if fn != nil {
		fn(what, why, lesson, category)
	}
}

// MemorySvc returns the extracted MemoryService (Phase 4).
func (s *Session) MemorySvc() *MemoryService { return s.memory }

// Persistence returns the extracted PersistenceService (Phase 5).
// Provides the messages slice and system prompt (read/write) with
// the underlying RWMutex.
func (s *Session) Persistence() *PersistenceService {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	persist := s.persist
	s.mu.RUnlock()
	if persist != nil {
		return persist
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.persist != nil {
		return s.persist
	}
	// A zero-value Session can still be used by narrow UI/test adapters. Keep
	// lazy service materialization for that compatibility case, but there is no
	// second transcript or system-prompt state to import.
	s.persist = NewPersistenceService(s.Logger())
	s.persist.SetJournal(eventlog.New(nil))
	return s.persist
}

// Tools returns the extracted ToolService (Phase 6).
func (s *Session) Tools() *ToolService { return s.tools }

// DeploymentRouting reports whether the chat client is catalog-backed
// (e.g. DeploymentRouter). Read through to the ChatService so there is
// no separate stored field to drift out of sync on ReattachTransport.
func (s *Session) DeploymentRouting() bool {
	if s.llm != nil {
		return s.llm.DeploymentRouting()
	}
	return false
}

// SubServices is the composed view of the 6 sub-services extracted
// in Phases 1-6 of the god-object decomposition. New code should
// prefer the SubServices() accessor over direct Session state.
// Existing code (cmd/, daemon/, multiagent/, …) continues to use
// the legacy fields until they're migrated.
//
// SubServices is a struct (not an interface) because all 6
// sub-services are concrete types; this keeps the API discoverable
// via godoc and avoids the indirection cost of interface dispatch
// on the agent-loop hot path.
type SubServices struct {
	LLM         *ChatService
	Perms       *PermissionService
	Life        *LifecycleService
	Memory      *MemoryService
	Persistence *PersistenceService
	Tools       *ToolService
}

// SubServices returns the 6 new sub-services. All sub-services are
// non-nil for a session constructed via NewSessionWithClient (the
// only production constructor); the nil cases are reachable only
// via direct struct literal construction in tests.
func (s *Session) SubServices() SubServices {
	if s == nil {
		return SubServices{}
	}
	return SubServices{
		LLM:         s.llm,
		Perms:       s.perms,
		Life:        s.life,
		Memory:      s.memory,
		Persistence: s.persist,
		Tools:       s.tools,
	}
}

// Goals returns the session's embedded GoalTracker for lifecycle goal management.
// Returns nil if no goal tracker is configured.
func (s *Session) Goals() *planning.GoalTracker {
	if s == nil {
		return nil
	}
	return s.goals
}

// SetModel updates the active model for subsequent requests.
func (s *Session) SetModel(model string) {
	m := strings.TrimSpace(model)
	s.Cost.SetModel(m)
	if s.llm != nil {
		s.llm.SetModel(m)
	}
	s.syncCascadeDefaultModel()
	s.refreshContextWindowCache()
}

// syncCascadeDefaultModel keeps the cascade router aligned after /config model picks.
func (s *Session) syncCascadeDefaultModel() {
	if s == nil || s.LifecycleSvc() == nil || s.LifecycleSvc().Cascade() == nil {
		return
	}
	if m := strings.TrimSpace(s.Model()); m != "" {
		cascade := s.LifecycleSvc().Cascade()
		cascade.DefaultModel = m
	}
}

// SetProvider updates the active provider for subsequent requests.
func (s *Session) SetProvider(provider string) {
	p := strings.TrimSpace(provider)
	if llm := s.ChatLLM(); llm != nil {
		llm.SetProvider(p)
	}
}

func (s *Session) AddUser(content string) {
	if p := s.Persistence(); p != nil {
		p.AddUser(content)
		if graph := p.Graph(); graph != nil {
			parentID := ""
			if head, err := graph.Head(); err == nil && head != nil {
				parentID = head.ID
			}
			_, _ = graph.Append(parentID, "user", content)
		}
	}
	if memSvc := s.MemorySvc(); memSvc != nil {
		if mem := memSvc.Memory(); mem != nil && strings.Contains(strings.ToLower(content), "remember") {
			go func(c string) {
				// Bound the call so a slow/hung memory backend cannot leak
				// this goroutine; the ctx now propagates to the backend.
				rCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := mem.Remember(rCtx, c, "user_explicit"); err != nil {
					slog.Warn("background memory remember failed", "error", err)
				}
			}(content)
		}
	}
}

// AddUserWithImage adds a user message with an attached image (base64-encoded).
// The imageType should be "image/png", "image/jpeg", etc.
func (s *Session) AddUserWithImage(content string, imageBase64 string, imageType string) {
	if p := s.Persistence(); p != nil {
		p.AddUserWithImage(content, imageBase64, imageType)
		if graph := p.Graph(); graph != nil {
			parentID := ""
			if head, err := graph.Head(); err == nil && head != nil {
				parentID = head.ID
			}
			_, _ = graph.Append(parentID, "user", content+" [image attached]")
		}
	}
}

func (s *Session) AddAssistant(content string) {
	if p := s.Persistence(); p != nil {
		p.AddAssistant(content)
		if graph := p.Graph(); graph != nil {
			parentID := ""
			if head, err := graph.Head(); err == nil && head != nil {
				parentID = head.ID
			}
			_, _ = graph.Append(parentID, "assistant", content)
		}
	}
}

// ForkConversation creates a new branch from a specific point in history.
// Returns the fork node ID and the messages up to that point.
func (s *Session) ForkConversation(nodeID string) (string, error) {
	p := s.Persistence()
	if p == nil {
		return "", nil
	}
	graph := p.Graph()
	if graph == nil {
		return "", nil
	}
	fork, err := graph.Fork(nodeID)
	if err != nil {
		return "", err
	}
	// Rebuild messages from the forked branch.
	history, err := graph.History(fork.ID)
	if err != nil {
		return "", err
	}
	msgs := make([]types.FluxMessage, 0, len(history))
	for _, node := range history {
		if node.Role == "user" || node.Role == "assistant" {
			msgs = append(msgs, types.FluxMessage{Role: node.Role, Content: node.Content})
		}
	}
	p.SetRawMessages(msgs)
	return fork.ID, nil
}

// SwitchBranch navigates to a different branch point and rebuilds messages.
func (s *Session) SwitchBranch(nodeID string) error {
	p := s.Persistence()
	if p == nil {
		return nil
	}
	graph := p.Graph()
	if graph == nil {
		return nil
	}
	if err := graph.SetHead(nodeID); err != nil {
		return err
	}
	history, err := graph.History(nodeID)
	if err != nil {
		return err
	}
	msgs := make([]types.FluxMessage, 0, len(history))
	for _, node := range history {
		if node.Role == "user" || node.Role == "assistant" {
			msgs = append(msgs, types.FluxMessage{Role: node.Role, Content: node.Content})
		}
	}
	p.SetRawMessages(msgs)
	return nil
}

// ListBranches returns child nodes (alternative branches) from a given node.
func (s *Session) ListBranches(nodeID string) ([]*session.ConversationNode, error) {
	p := s.Persistence()
	if p == nil {
		return nil, nil
	}
	graph := p.Graph()
	if graph == nil {
		return nil, nil
	}
	return graph.Branches(nodeID)
}

// ConvoHead returns the current conversation head node ID.
func (s *Session) ConvoHead() string {
	p := s.Persistence()
	if p == nil {
		return ""
	}
	graph := p.Graph()
	if graph == nil {
		return ""
	}
	if head, err := graph.Head(); err == nil && head != nil {
		return head.ID
	}
	return ""
}

// AppendSystemContext adds runtime context, such as /add-dir, to future model calls.
func (s *Session) AppendSystemContext(content string) {
	if p := s.Persistence(); p != nil {
		p.AppendSystemContext(content)
	}
}

// ReplaceSystemContextSection replaces the content of a system prompt section identified by its header.
// If the header is not found, appends the content as a new section.
func (s *Session) ReplaceSystemContextSection(header, content string) {
	if p := s.Persistence(); p != nil {
		p.ReplaceSystemContextSection(header, content)
	}
}

// SetLogger replaces the session logger.
func (s *Session) SetLogger(l *logger.Logger) {
	if l == nil {
		l = logger.Default()
	}
	if s.perms != nil {
		s.perms.SetLogger(l)
	}
	if s.life != nil {
		s.life.SetLogger(l)
	}
	if s.memory != nil {
		s.memory.SetLogger(l)
	}
	if s.persist != nil {
		s.persist.SetLogger(l)
	}
}

// SetAllowedDirs sets directories that file tools are allowed to access.
func (s *Session) SetAllowedDirs(dirs []string) {
	if s.perms != nil {
		s.perms.SetAllowedDirs(dirs)
	}
}

// SetAutoCompactThresholdPct sets the auto-compact threshold.
func (s *Session) SetAutoCompactThresholdPct(pct int) {
	if s.persist != nil {
		s.persist.SetAutoCompactThresholdPct(pct)
	}
}

// SetPinnedMessages sets the number of recent messages protected from compaction.
func (s *Session) SetPinnedMessages(n int) {
	if s.persist != nil {
		s.persist.SetPinnedMessages(n)
	}
}

// SetThinkingEnabled sets the generic host thinking/reasoning toggle on
// the ChatService (the source of truth).
func (s *Session) SetThinkingEnabled(v *bool) {
	if s.llm != nil {
		s.llm.SetThinkingEnabled(v)
	}
}

// SetGLMThinkingEnabled is a deprecated alias of SetThinkingEnabled.
func (s *Session) SetGLMThinkingEnabled(v *bool) {
	s.SetThinkingEnabled(v)
}

// SetSnapshots attaches the snapshot tracker. New code should call
// this instead of writing to the legacy s.Snapshots field directly.
func (s *Session) SetSnapshots(snap *snapshot.Tracker) {
	if s.tools != nil {
		s.tools.WithSnapshots(snap)
	}
}

// SetAutoCommit enables git auto-commit after successful Write/Edit tools.
func (s *Session) SetAutoCommit(enabled bool) {
	if s != nil && s.tools != nil {
		s.tools.SetAutoCommit(enabled)
	}
}

// AutoCommit reports whether write tools auto-commit.
func (s *Session) AutoCommit() bool {
	if s == nil || s.tools == nil {
		return false
	}
	return s.tools.AutoCommit()
}

// SetAskUserFn sets the user-prompt callback. New code should
// call this instead of writing to the legacy s.AskUserFn field.
func (s *Session) SetAskUserFn(fn func(question string) (string, error)) {
	if s.perms != nil {
		s.perms.SetAskUserFn(fn)
	}
}

// SetPermissionFn configures the permission callback on PermissionService.
func (s *Session) SetPermissionFn(fn func(safety.PermissionRequest)) {
	if s.perms != nil {
		s.perms.SetPermissionFn(fn)
	}
}

// SetApproval sets the high-risk action gate on PermissionService.
func (s *Session) SetApproval(a *ApprovalGate) {
	if s.perms != nil {
		s.perms.SetApproval(a)
	}
}

// EnableTurnRecovery activates the opaque request-token escalation layer on
// the permission service (see PermissionService.EnableTurnRecovery).
func (s *Session) EnableTurnRecovery() {
	if s.perms != nil {
		s.perms.EnableTurnRecovery()
	}
}

// EscalatePermission re-opens a previously denied high-risk action by
// presenting the exact opaque permission_request_id (single-use). Delegate to
// the permission service.
func (s *Session) EscalatePermission(requestID string) bool {
	if s.perms == nil {
		return false
	}
	return s.perms.EscalatePermission(requestID)
}

// SetConversationGraph attaches Rho's product-owned conversation graph and
// seeds it from an already-resumed linear transcript when the graph is new.
func (s *Session) SetConversationGraph(graph *session.ConversationGraph) {
	if s.persist != nil {
		s.persist.SetGraph(graph)
		if graph != nil && graph.Empty() {
			parentID := ""
			for _, message := range s.persist.RawMessages() {
				if message.Role != "user" && message.Role != "assistant" {
					continue
				}
				node, err := graph.Append(parentID, message.Role, message.Content)
				if err != nil {
					break
				}
				parentID = node.ID
			}
		}
	}
}

// SetContextWindowCached sets the catalog context window.
func (s *Session) SetContextWindowCached(n int) {
	if s.persist != nil {
		s.persist.SetContextWindowCached(n)
	}
}

// ContextWindowCachedValue returns the cached context window size.
func (s *Session) ContextWindowCachedValue() int {
	if s.persist != nil {
		return s.persist.ContextWindowCached()
	}
	return 0
}

// JournalTitle returns the deterministic, provider-free session title derived from the
// journal's model-visible messages. Callers that persist a session can store it on
// session.Session.Name when no explicit human-provided title exists (Phase 3 title seam).
func (s *Session) JournalTitle() string {
	if s == nil || s.Persistence() == nil {
		return ""
	}
	return s.Persistence().JournalTitle()
}

// JournalWire exports the session's append-only event spine for durable persistence.
// It returns nil when no journal is attached, so callers that persist the session
// can write the version-0 messages-only shape with no changes.
func (s *Session) JournalWire() []eventlog.WireEvent {
	if s == nil {
		return nil
	}
	p := s.Persistence()
	if p == nil || p.Journal() == nil {
		return nil
	}
	wire, err := eventlog.MarshalWire(p.Journal().Snapshot())
	if err != nil {
		slog.Warn("marshal event journal", "error", err)
		return nil
	}
	return wire
}

// ReplayJournal rebuilds the append-only event spine from a persisted wire record and
// attaches it to persistence. The live transcript is not touched: this only restores
// the log so future appends continue the sequence and new projections stay faithful.
// A record that fails validation is returned as an error and leaves the journal
// untouched. A nil or empty record is a no-op (version-0 sessions).
func (s *Session) ReplayJournal(wire []eventlog.WireEvent) error {
	if s == nil || len(wire) == 0 {
		return nil
	}
	log, err := eventlog.Rehydrate(wire, nil)
	if err != nil {
		return err
	}
	p := s.Persistence()
	if p == nil {
		return fmt.Errorf("session: cannot attach journal without persistence")
	}
	p.SetJournal(log)
	if s.perms != nil {
		s.perms.SetJournal(log)
	}
	return nil
}

// Journal returns the session's event log journal, or nil.
func (s *Session) Journal() *eventlog.Log {
	if s == nil || s.Persistence() == nil {
		return nil
	}
	return s.Persistence().Journal()
}

// WorkingDir returns the session's working directory.
func (s *Session) WorkingDir() string {
	if s == nil || s.tools == nil {
		return ""
	}
	return s.tools.WorkingDir()
}

// Cwd returns the session's working directory.
func (s *Session) Cwd() string {
	return s.WorkingDir()
}

// EnsureSkillCatalogStatement computes the current digest over model-invocable skills
// and emits a durable catalog message (or tombstone) on digest change. Unchanged requests add nothing.
func (s *Session) EnsureSkillCatalogStatement() string {
	if s == nil || s.tools == nil || s.tools.Registry() == nil {
		return ""
	}
	_, hasSkillTool := s.tools.Registry().Get("Skill")
	if !hasSkillTool {
		_, hasSkillTool = s.tools.Registry().Get("skill")
	}
	if !hasSkillTool {
		return ""
	}

	cwd := s.WorkingDir()
	allSkills, err := plugin.DefaultRegistry.List(context.Background(), cwd)
	if err != nil {
		return ""
	}
	var invocable []plugin.SkillEntry
	for _, sk := range allSkills {
		if sk.Invocation.IsModelInvocable() {
			invocable = append(invocable, sk)
		}
	}

	digest := plugin.ComputeSkillDigest(invocable)

	s.mu.Lock()
	last := s.lastSkillCatalogDigest
	s.mu.Unlock()

	// Initial scan of raw messages to recover last digest if recovering session
	if last == "" {
		if p := s.Persistence(); p != nil {
			for _, m := range p.RawMessages() {
				if strings.HasPrefix(m.Content, "Available skills (digest: ") {
					if idx := strings.Index(m.Content, "):"); idx > 0 {
						prefix := "Available skills (digest: "
						last = m.Content[len(prefix):idx]
						s.mu.Lock()
						s.lastSkillCatalogDigest = last
						s.mu.Unlock()
					}
				}
			}
		}
	}

	if digest == "empty" && last == "" {
		return ""
	}

	if digest != last {
		msg := plugin.RenderSkillCatalogMessage(invocable, digest)
		if p := s.Persistence(); p != nil {
			p.AppendUserJournaled(types.FluxMessage{Role: "user", Content: msg})
		}
		s.mu.Lock()
		s.lastSkillCatalogDigest = digest
		s.mu.Unlock()
		return msg
	}
	return ""
}

// CostValue returns the session's cost accumulator (a pointer
// to a value type, so its methods can be called). New code
// should call this instead of reading s.Cost directly.
func (s *Session) CostValue() *cost.Cost {
	return &s.Cost
}

func (s *Session) LoadMessages(msgs []types.FluxMessage) {
	s.Persistence().SetRawMessages(msgs)
}

func (s *Session) MessageCount() int {
	return len(s.Persistence().RawMessages())
}

// RawMessages returns the conversation messages for persistence.
//
// PersistenceService is the single source of truth for the live transcript:
// AddUser/AddAssistant and the agent loop (stream.go) all write through it,
// and compaction/governor paths read it. Delegating here means TUI/CLI
// consumers — notably saveSession — see the real, populated transcript.
func (s *Session) RawMessages() []types.FluxMessage {
	if p := s.Persistence(); p != nil {
		return p.RawMessages()
	}
	return nil
}

// Chat implements the LLMClient interface by delegating to the underlying client.
// This allows Session to be passed to components that need LLM access (e.g. Reflector, SelfReview).
func (s *Session) Chat(ctx context.Context, msgs []types.FluxMessage, opts types.ChatOptions) (*types.FluxResponse, error) {
	if s.ChatLLM() == nil {
		return nil, fmt.Errorf("session: no LLM client configured")
	}
	return s.ChatLLM().Chat(ctx, msgs, opts)
}

// Schedule returns the session's in-conversation schedule manager.
func (s *Session) Schedule() *schedule.Manager {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scheduleManager == nil {
		s.scheduleManager = schedule.NewManager()
		var j *eventlog.Log
		if p := s.Persistence(); p != nil {
			j = p.Journal()
		}
		s.scheduleManager.Attach(j, func(item schedule.Item) error {
			content := fmt.Sprintf("[Scheduled Reminder: %s]\n%s", item.ID, item.Prompt)
			if p := s.Persistence(); p != nil {
				if sq := p.Steering(); sq != nil {
					sq.Enqueue(streaming.SteeringMessage{
						Content:  content,
						Priority: 1,
					})
				} else {
					p.AppendUserJournaled(types.FluxMessage{
						Role:    "user",
						Content: content,
					})
				}
			}
			return nil
		})
	}
	return s.scheduleManager
}

// RemoveLastExchange removes the last user+assistant message pair.
func (s *Session) RemoveLastExchange() {
	msgs := s.Persistence().RawMessages()
	if len(msgs) < 2 {
		return
	}
	// Remove from the end until we've removed one user and one assistant message
	removed := 0
	for i := len(msgs) - 1; i >= 0 && removed < 2; i-- {
		role := msgs[i].Role
		if role == "user" || role == "assistant" {
			removed++
			msgs = msgs[:i]
		}
	}
	s.Persistence().SetRawMessages(msgs)
}

// StreamEvent is sent from the engine to the TUI.
type StreamEvent struct {
	Type     string // content, thinking, tool_use, tool_result, usage, compact, done, error
	Content  string
	ToolName string
	ToolID   string
	// ToolState and ToolReason are populated for tool lifecycle events.
	ToolState  ToolState
	ToolReason ToolTerminalReason
	Usage      *StreamUsage // usage data for this event
	// Compaction metadata (Type == "compact")
	TokensBefore int
	TokensAfter  int
}

// StreamUsage tracks token usage for a single stream event.
type StreamUsage struct {
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	CacheReadTokens  int    `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int    `json:"cache_write_tokens,omitempty"`
	Provider         string `json:"provider,omitempty"`
	Model            string `json:"model,omitempty"`
}
