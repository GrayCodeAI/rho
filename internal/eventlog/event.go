// Package eventlog is Rho's append-only session event spine.
//
// It ports the durable-design idea from DeepSeek Harness: the session log is the
// single source of truth, and the messages the model actually sees are a
// projection of that log (see docs/plans/dsh-harness-port-plan.md). Keeping
// the vocabulary free of Rho product orchestration lets the log stay a neutral,
// testable record that consumers project through without importing the engine.
//
// Port note: DeepSeek Harness grows its event vocabulary through TypeScript
// declaration merging. Go cannot merge declarations, so the vocabulary is a plain
// const set plus per-kind payloads. New model-visible event kinds are additive
// but must stay reconstructible by DeriveMessages or the record is incomplete.
package eventlog

import (
	"fmt"
	"time"
)

// Type identifies the kind of a durable session event.
type Type string

// The durable event vocabulary. Do not reuse a removed Type; the record refuses
// to load a log containing an event kind the accountable build does not know.
const (
	// SessionMeta is the one-per-log metadata line.
	SessionMeta Type = "session.meta"
	// UserMessage is a user turn appended to the model surface.
	UserMessage Type = "message.user"
	// AssistantMsg is an assistant reply appended to the model surface.
	AssistantMsg Type = "message.assistant"
	// AssistantChunk is a partial assistant stream delta. It preserves replay
	// and UI fidelity but does not itself project into the model surface;
	// the final AssistantMsg event is the reconstructed message. The Kind
	// field discriminates chunk variants: "" / "text" (content delta),
	// "thinking" (reasoning delta), "tool-call" (incremental tool-call arg),
	// "block-start" (response block begins), "block-end" (response block ends),
	// and "finish" (stream ended with a stop reason).
	AssistantChunk Type = "assistant.chunk"
	// ToolCall is a tool invocation the model requested.
	ToolCall Type = "tool.call"
	// ToolResult is the outcome of one ToolCall.
	ToolResult Type = "tool.result"
	// ContextInjected is ephemeral context literally shown to the model. It must
	// be reconstructible, so it lives on the log even though it is not a
	// conversation turn.
	ContextInjected Type = "context.injected"
	// SessionCompacted marks a compaction pass. Compaction mutates the projected
	// surface, so it is a first-class durable fact.
	SessionCompacted Type = "session.compacted"
	// SpecState records the active spec workflow stage as a durable fact.
	SpecState Type = "spec.state"
	// TurnStart / TurnEnd bracket one agent turn.
	TurnStart Type = "turn.start"
	TurnEnd   Type = "turn.end"
	// StepStart / StepEnd bracket one model step inside a turn.
	StepStart Type = "step.start"
	StepEnd   Type = "step.end"
	// PermissionChange records a durable approval/permission decision.
	PermissionChange Type = "permission.change"
	// ApprovalAsked records that a gated action reached the human approval gate.
	ApprovalAsked Type = "approval.asked"
	// ApprovalDecided records the outcome of a human approval gate decision.
	ApprovalDecided Type = "approval.decided"
	// ApprovalPolicy records whether a category is covered by the active policy.
	ApprovalPolicy Type = "approval.policy"
	// ToolWorkflowStart / ToolWorkflowEnd bracket one tool-execution run.
	ToolWorkflowStart Type = "tool-workflow.run-start"
	ToolWorkflowEnd   Type = "tool-workflow.run-end"
	// LLMRetry / LLMRetryStarted record a stream retry decision.
	LLMRetry        Type = "llm.retry"
	LLMRetryStarted Type = "llm.retry-started"
	// RequestContext records the durable model-visible surface at a request boundary.
	RequestContext Type = "request.context"
	// CompactionStart marks the beginning of a compaction pass, recording which
	// strategy engaged. Ported from DeepSeek Harness compaction/start.
	CompactionStart Type = "compaction.start"
	// CompactionPrune records a pruning action within a compaction pass — which
	// messages were dropped or summarized.
	CompactionPrune Type = "compaction.prune"
	// CompactionEnd marks the completion of a compaction pass.
	CompactionEnd Type = "compaction.end"
	// CompactionSummary records the summary content produced by a compaction pass.
	CompactionSummary Type = "compaction.summary"
	// SessionEndSeed marks the end of a constructor/seed replay boundary. Events
	// before it came from the seed (resume, fork); events after are live. Ported
	// from DeepSeek Harness session/end-seed.
	SessionEndSeed Type = "session.end-seed"
	// TodoWrite records a whole-list todo snapshot (last-write-wins). Ported
	// from DeepSeek Harness todo/write.
	TodoWrite Type = "todo.write"
	// RequestHeader records the full request header (config, system prompt, tools).
	// Ported from DeepSeek Harness request/header.
	RequestHeader Type = "request.header"
	// HookInvoked records that a hook was invoked. Ported from DeepSeek Harness
	// hook/invoked.
	HookInvoked Type = "hook.invoked"
	// HookResult records the outcome of a hook invocation. Ported from DeepSeek
	// Harness hook/result.
	HookResult Type = "hook.result"
	// FeedbackRecord records user feedback on a session. Ported from DeepSeek
	// Harness feedback/record.
	FeedbackRecord Type = "feedback.record"
	// GoalChange records a change to the active goal set. Ported from DeepSeek
	// Harness goal/change.
	GoalChange Type = "goal.change"
	// PermissionPreset records whether a category is covered by the active
	// policy preset. Ported from DeepSeek Harness permission/preset.
	PermissionPreset Type = "permission.preset"
	// ScheduleChange records a schedule configuration change. Ported from
	// DeepSeek Harness schedule/change.
	ScheduleChange Type = "schedule.change"
	// ScheduleCreate records an in-conversation schedule creation.
	ScheduleCreate Type = "schedule.create"
	// ScheduleUpdate records an in-conversation schedule update.
	ScheduleUpdate Type = "schedule.update"
	// ScheduleDelete records an in-conversation schedule deletion.
	ScheduleDelete Type = "schedule.delete"
	// ScheduleDue records that an in-conversation schedule reminder was triggered.
	ScheduleDue Type = "schedule.due"
	// SessionTitle records a log-backed session title. Ported from DeepSeek
	// Harness session/title.
	SessionTitle Type = "session.title"
	// SessionTitleLLMRequest records an LLM-driven title generation attempt.
	// Ported from DeepSeek Harness session/title-llm-request.
	SessionTitleLLMRequest Type = "session.title-llm-request"
	// SubagentDescriptor records subagent lifecycle metadata. Ported from
	// DeepSeek Harness subagent/descriptor.
	SubagentDescriptor Type = "subagent.descriptor"
	// AgentPresetSelected records the selected agent preset. Ported from
	// DeepSeek Harness agent-preset/selected.
	AgentPresetSelected Type = "agent-preset.selected"
	// AgentInboxSpliced records a splice into the agent inbox. Ported from
	// DeepSeek Harness agent/inbox/spliced.
	AgentInboxSpliced Type = "agent.inbox.spliced"
	// CommandRun records a command execution start. Ported from DeepSeek
	// Harness command/run.
	CommandRun Type = "command.run"
	// CommandDone records a command execution completion. Ported from DeepSeek
	// Harness command/done.
	CommandDone Type = "command.done"
	// ToolWorkflowAgentStart records agent lifecycle entry in a tool workflow.
	// Ported from DeepSeek Harness tool-workflow/agent-start.
	ToolWorkflowAgentStart Type = "tool-workflow.agent-start"
	// ToolWorkflowAgentEnd records agent lifecycle exit in a tool workflow.
	// Ported from DeepSeek Harness tool-workflow/agent-end.
	ToolWorkflowAgentEnd Type = "tool-workflow.agent-end"
	// ToolCodeDispatch records a code-dispatch tool execution. Ported from
	// DeepSeek Harness tool/code-dispatch.
	ToolCodeDispatch Type = "tool.code-dispatch"
	// ToolCodeDispatchStart records a code-dispatch tool execution start.
	// Ported from DeepSeek Harness tool/code-dispatch-start.
	ToolCodeDispatchStart Type = "tool.code-dispatch-start"
	// WebDeepSeekSearch records a web/deepseek-search LLM request. Ported from
	// DeepSeek Harness web/deepseek-search-llm-request.
	WebDeepSeekSearch Type = "web.deepseek-search-llm-request"

	// PlanMode records plan-mode activation/deactivation, matching DSH's
	// plan/mode event. The last event wins; a log with none folds to inactive.
	PlanMode Type = "plan.mode"
)

// Known reports whether t is part of the current vocabulary.
func (t Type) Known() bool {
	switch t {
	case SessionMeta, UserMessage, AssistantMsg, AssistantChunk, ToolCall, ToolResult,
		ContextInjected, SessionCompacted, SpecState,
		TurnStart, TurnEnd, StepStart, StepEnd, PermissionChange,
		ApprovalAsked, ApprovalDecided, ApprovalPolicy,
		ToolWorkflowStart, ToolWorkflowEnd, LLMRetry, LLMRetryStarted, RequestContext,
		CompactionStart, CompactionPrune, CompactionEnd, CompactionSummary,
		SessionEndSeed, TodoWrite, RequestHeader, HookInvoked, HookResult,
		FeedbackRecord, GoalChange, PermissionPreset, ScheduleChange,
		ScheduleCreate, ScheduleUpdate, ScheduleDelete, ScheduleDue,
		SessionTitle, SessionTitleLLMRequest, SubagentDescriptor, AgentPresetSelected,
		AgentInboxSpliced, CommandRun, CommandDone,
		ToolWorkflowAgentStart, ToolWorkflowAgentEnd,
		ToolCodeDispatch, ToolCodeDispatchStart,
		WebDeepSeekSearch, PlanMode:
		return true
	default:
		return false
	}
}

// Event is one durable fact. Seq is assigned by the Log at append time and is
// what makes the record order reconstructible.
type Event struct {
	Type Type      `json:"type"`
	Seq  uint64    `json:"seq"`
	At   time.Time `json:"at"`
	Data any       `json:"data,omitempty"`
	// Ignorable marks an event a reader may safely skip when it does not
	// recognize `type`. Matches DSH's `ignorable` invariant.
	Ignorable bool `json:"ignorable,omitempty"`
	// SurfaceOp records how this event entered the model-visible surface.
	// Only present on surface-eligible types (UserMessage, AssistantMsg,
	// ToolResult). Ported from DSH's surfaceOp invariant.
	SurfaceOp *SurfaceOp `json:"surface_op,omitempty"`
	// SourceEventSeqs records earlier event seqs this event cites as sources.
	// Optional — used for replacement provenance. Ported from DSH.
	SourceEventSeqs []uint64 `json:"source_event_seqs,omitempty"`
}

// TurnEndFact records how a turn ended, matching DSH's TurnEndReasonMap:
// completed, aborted, blocked, error, max-tokens, interrupted.
type TurnEndFact struct {
	Turn   int         `json:"turn,omitempty"`
	Reason string      `json:"reason,omitempty"` // "completed", "aborted", "blocked", "error", "max-tokens", "interrupted"
	Error  *LlmFailure `json:"error,omitempty"`  // populated when reason == "error"
}

// LlmFailure is the structured LLM error carried on TurnEnd when
// reason == "error", matching DSH's LlmFailure.
type LlmFailure struct {
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

// SessionFormatVersion is the eventlog format revision. DSH's session JSONL
// backend stamps this into every SessionHeader and refuses logs carrying a
// foreign version on load. Rho mirrors this: the eventlog package defines the
// constant so both write sites and load-time checks read the same source, and
// Validate refuses unknown versions before any event is projected.
const SessionFormatVersion = 1

// ErrForeignFormatVersion is returned when a loaded log carries a format
// version this build does not recognize. Fail-loud stance: a near-identity
// upgrade step is cheap; silently reading a future log wrong is not.
var ErrForeignFormatVersion = fmt.Errorf("eventlog: foreign format version")

// Meta is the SessionMeta payload carried on the first line of the log.
// Ported from DSH's SessionHeader, including fork lineage and agent preset metadata.
type Meta struct {
	ID       string `json:"id,omitempty"`
	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"`
	CWD      string `json:"cwd,omitempty"`
	Agent    string `json:"agent,omitempty"`
	// FormatVersion records the eventlog format revision, independent of the
	// surrounding session JSONL schema so the log can version itself.
	FormatVersion int `json:"format_version"`

	// DSH SessionHeader parity (all optional/omitable):
	ParentSession   string `json:"parent_session,omitempty"`
	SeedLength      uint64 `json:"seed_length,omitempty"` // number of leading seeded events
	Origin          string `json:"origin,omitempty"`      // "subagent" when forked as child
	DelegationDepth int    `json:"delegation_depth,omitempty"`
	AgentPreset     string `json:"agent_preset,omitempty"`
}

// Message is the model-surface payload for User and Assistant events. It mirrors
// the wire message shape without importing the product layer.
type Message struct {
	Role         string               `json:"role,omitempty"`
	Content      string               `json:"content,omitempty"`
	Thinking     string               `json:"thinking,omitempty"`
	Images       []string             `json:"images,omitempty"`
	ContentParts []ContentPartPayload `json:"content_parts,omitempty"`
	ToolUse      []ToolCallPayload    `json:"tool_use,omitempty"`
	ToolResults  []ToolResultPayload  `json:"tool_results,omitempty"`
	// Turn and Step record the turn/step coordinates of the producing step.
	// Present on assistant/message (DSH parity); absent on user/message.
	Turn int `json:"turn,omitempty"`
	Step int `json:"step,omitempty"`
}

// ContentPartPayload records one multimodal part of a Message.
type ContentPartPayload struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
	ImageDetail string `json:"image_detail,omitempty"`
	AudioData   string `json:"audio_data,omitempty"`
	AudioFormat string `json:"audio_format,omitempty"`
}

// ToolCallPayload records the invocation facts needed to reconstruct a ToolCall.
type ToolCallPayload struct {
	Turn      int            `json:"turn,omitempty"`
	Step      int            `json:"step,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ToolResultPayload records one tool result fact.
type ToolResultPayload struct {
	Turn      int    `json:"turn,omitempty"`
	Step      int    `json:"step,omitempty"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

// SpecFact is the payload for the durable spec-state event. The stage string is
// stored as a plain value (not an enum) so the eventlog vocabulary stays
// decoupled from the owning product package that defines the lifecycle.
type SpecFact struct {
	Stage string `json:"stage"`
	Slug  string `json:"slug,omitempty"`
}
