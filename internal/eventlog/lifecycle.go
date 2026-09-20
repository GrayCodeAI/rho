// Lifecycle event payloads and append helpers.
//
// These types port the DeepSeek Harness event vocabulary beyond Rho's original
// core spine. Each event is log-only (does not participate in ProjectMessages)
// unless noted. See docs/plans/dsh-harness-port-plan.md for the upstream parity
// matrix and mapping table.
//
// DSH source: packages/core/session/src/types.ts (SessionEventMap),
// packages/core/session/src/known-event-types.ts,
// plus plugin packages: compaction, hooks, feedback, goal, subagent, shell, preset.
package eventlog

import "time"

// --- Compaction lifecycle ---
// DeepSeek Harness models compaction as a four-stage lifecycle:
// start → prune → end → summary. Rho previously recorded only a single
// session.compacted event; the lifecycle types preserve the full durable
// trail so resume can replay the exact compaction sequence.

// CompactionStartFact records the start of a compaction pass, matching DSH's
// compaction/start event. Includes the compaction ID, optional source command,
// and owning turn number (null for standalone manual transactions between turns).
type CompactionStartFact struct {
	Strategy        string `json:"strategy,omitempty"`
	CompactionID    string `json:"compaction_id,omitempty"`
	SourceCommandID string `json:"source_command_id,omitempty"`
	Turn            *int   `json:"turn,omitempty"` // null = standalone between turns
}

// CompactionPruneFact records a single pruning action within a compaction pass,
// matching DSH's compaction/prune event. The shadowedRange/shadowedSeqs fields
// document which surface nodes were removed; shadowedTokenCount is the heuristic
// price so a pure consumer can subtract it without retaining per-node prices.
type CompactionPruneFact struct {
	Strategy string `json:"strategy,omitempty"`
	// Messages is the count of messages removed from the projection (Rho's
	// simplified invariant; replaces shadowedSeqs for simple pruners).
	Messages int `json:"messages,omitempty"`
	// DSH parity fields (all optional; Rho's simple pruner populates Messages
	// instead of shadowedSeqs):
	ShadowedRangeStart uint64   `json:"shadowed_range_start,omitempty"`
	ShadowedRangeEnd   uint64   `json:"shadowed_range_end,omitempty"`
	ShadowedSeqs       []uint64 `json:"shadowed_seqs,omitempty"`
	ShadowedTokenCount int      `json:"shadowed_token_count,omitempty"`
}

// CompactionEndFact records the completion of a compaction pass, matching
// DSH's compaction/end event. Error is non-empty if the attempt failed.
type CompactionEndFact struct {
	Strategy     string `json:"strategy,omitempty"`
	TokensBefore int    `json:"tokens_before,omitempty"`
	TokensAfter  int    `json:"tokens_after,omitempty"`
	// DSH parity fields:
	CompactionID    string `json:"compaction_id,omitempty"`
	SourceCommandID string `json:"source_command_id,omitempty"`
	Turn            *int   `json:"turn,omitempty"`
	Error           string `json:"error,omitempty"`
}

// CompactionSummaryFact records the summary content produced by a compaction pass,
// matching DSH's compaction/summary event. The summary replaces the shadowed
// range in the model-visible surface (the replacement user/message that follows
// is priced by this metering event).
type CompactionSummaryFact struct {
	Summary string `json:"summary,omitempty"`
	// DSH parity fields:
	CompactionID          string   `json:"compaction_id,omitempty"`
	SourceCommandID       string   `json:"source_command_id,omitempty"`
	ShadowedRangeStart    uint64   `json:"shadowed_range_start,omitempty"`
	ShadowedRangeEnd      uint64   `json:"shadowed_range_end,omitempty"`
	ShadowedSeqs          []uint64 `json:"shadowed_seqs,omitempty"`
	ShadowedTokenCount    int      `json:"shadowed_token_count,omitempty"`
	Provider              string   `json:"provider,omitempty"`
	Model                 string   `json:"model,omitempty"`
	MaxTokens             *int     `json:"max_tokens,omitempty"`
	UsagePromptTokens     int      `json:"usage_prompt_tokens,omitempty"`
	UsageCompletionTokens int      `json:"usage_completion_tokens,omitempty"`
}

// AppendCompactionStart records the start of a compaction pass.
func (l *Log) AppendCompactionStart(strategy string) {
	if l == nil {
		return
	}
	l.Append(CompactionStart, CompactionStartFact{Strategy: strategy})
}

// AppendCompactionStartFull records the start with full DSH parity fields.
func (l *Log) AppendCompactionStartFull(f CompactionStartFact) {
	if l == nil {
		return
	}
	l.Append(CompactionStart, f)
}

// AppendCompactionPrune records a pruning action within a compaction pass.
func (l *Log) AppendCompactionPrune(strategy string, messages int) {
	if l == nil {
		return
	}
	l.Append(CompactionPrune, CompactionPruneFact{Strategy: strategy, Messages: messages})
}

// AppendCompactionPruneFull records a pruning action with full DSH parity fields.
func (l *Log) AppendCompactionPruneFull(f CompactionPruneFact) {
	if l == nil {
		return
	}
	l.Append(CompactionPrune, f)
}

// AppendCompactionEnd records the end of a compaction pass.
func (l *Log) AppendCompactionEnd(f CompactionEndFact) {
	if l == nil {
		return
	}
	l.Append(CompactionEnd, f)
}

// AppendCompactionSummary records summary content from a compaction pass.
func (l *Log) AppendCompactionSummary(summary string) {
	if l == nil {
		return
	}
	l.Append(CompactionSummary, CompactionSummaryFact{Summary: summary})
}

// AppendCompactionSummaryFull records summary content with full DSH parity fields.
func (l *Log) AppendCompactionSummaryFull(f CompactionSummaryFact) {
	if l == nil {
		return
	}
	l.Append(CompactionSummary, f)
}

// --- Session seed/fork ---

// AppendSessionEndSeed marks the end of a constructor seed boundary. Events
// before this marker came from the seed (resume/fork); events after are live.
func (l *Log) AppendSessionEndSeed() {
	if l == nil {
		return
	}
	l.Append(SessionEndSeed, nil)
}

// --- Todos ---

// TodoItem is one entry in a todo list snapshot.
type TodoItem struct {
	Content string `json:"content"`
	Status  string `json:"status"` // "pending", "in_progress", "completed"
}

// TodoWriteFact is the payload for todo.write — a whole-list snapshot.
type TodoWriteFact struct {
	Todos []TodoItem `json:"todos,omitempty"`
}

// AppendTodoWrite records a whole-list todo snapshot (last-write-wins on replay).
func (l *Log) AppendTodoWrite(todos []TodoItem) {
	if l == nil {
		return
	}
	l.Append(TodoWrite, TodoWriteFact{Todos: todos})
}

// --- Context injection ---

// ContextInjectedFact records context that was injected into the model-visible
// surface (e.g., file summaries, memory recall, project context). It is
// reconstructible — the projection renders it as a system message.
type ContextInjectedFact struct {
	Content string `json:"content,omitempty"`
}

// AppendContextInjected records context injected into the model surface.
func (l *Log) AppendContextInjected(content string) {
	if l == nil {
		return
	}
	l.Append(ContextInjected, ContextInjectedFact{Content: content})
}

// RequestHeaderReason distinguishes the reason a request/header event was appended.
type RequestHeaderReason string

const (
	// RequestHeaderInitial marks the first header in a new conversation.
	RequestHeaderInitial RequestHeaderReason = "initial"
	// RequestHeaderResume marks the first header over a resumed log.
	RequestHeaderResume RequestHeaderReason = "resume"
	// RequestHeaderChange marks a header that differs from the prior snapshot.
	RequestHeaderChange RequestHeaderReason = "change"
)

// RequestHeaderFact records the durable request configuration: system prompt,
// tools, and the reason the snapshot was appended. The call config (provider,
// model, sampling scalars) is captured separately in the session metadata /
// request.context event.
type RequestHeaderFact struct {
	System string              `json:"system,omitempty"`
	Tools  []string            `json:"tools,omitempty"`
	Reason RequestHeaderReason `json:"reason,omitempty"`
}

// AppendRequestHeader records a request header snapshot.
func (l *Log) AppendRequestHeader(f RequestHeaderFact) {
	if l == nil {
		return
	}
	l.Append(RequestHeader, f)
}

// --- Hooks ---

// HookInvokedFact records that a hook was invoked.
type HookInvokedFact struct {
	Name string `json:"name,omitempty"`
}

// HookResultFact records the outcome of a hook invocation.
type HookResultFact struct {
	Name    string `json:"name,omitempty"`
	Error   string `json:"error,omitempty"`
	Success bool   `json:"success"`
}

// AppendHookInvoked records a hook invocation.
func (l *Log) AppendHookInvoked(name string) {
	if l == nil {
		return
	}
	l.Append(HookInvoked, HookInvokedFact{Name: name})
}

// AppendHookResult records a hook result.
func (l *Log) AppendHookResult(name string, err string) {
	if l == nil {
		return
	}
	success := err == ""
	l.Append(HookResult, HookResultFact{Name: name, Error: err, Success: success})
}

// --- Feedback ---

// FeedbackFact records user feedback on a session.
type FeedbackFact struct {
	// Category is "positive", "negative", or a free-form label.
	Category string `json:"category,omitempty"`
	Detail   string `json:"detail,omitempty"`
	// Thumb is "up" or "down" for binary feedback.
	Thumb string `json:"thumb,omitempty"`
}

// AppendFeedbackRecord records user feedback.
func (l *Log) AppendFeedbackRecord(f FeedbackFact) {
	if l == nil {
		return
	}
	l.Append(FeedbackRecord, f)
}

// --- Goals ---

// GoalChangeFact records a change to the active goal set.
type GoalChangeFact struct {
	Goal    string `json:"goal,omitempty"`
	Added   bool   `json:"added"`
	Removed bool   `json:"removed"`
}

// AppendGoalChange records a goal addition or removal.
func (l *Log) AppendGoalChange(goal string, added bool) {
	if l == nil {
		return
	}
	l.Append(GoalChange, GoalChangeFact{Goal: goal, Added: added, Removed: !added})
}

// --- Permission preset ---

// PermissionPresetFact records whether a category is covered by the active
// permission policy preset, matching DSH's permission/preset event.
type PermissionPresetFact struct {
	Category string `json:"category,omitempty"`
	Covered  bool   `json:"covered"`
	// DSH parity: the preset name, matching DSH's { preset: string } payload.
	PresetName string `json:"preset,omitempty"`
}

// AppendPermissionPreset records whether a category is covered by the policy.
func (l *Log) AppendPermissionPreset(category string, covered bool) {
	if l == nil {
		return
	}
	l.Append(PermissionPreset, PermissionPresetFact{Category: category, Covered: covered})
}

// --- Schedule change ---

// ScheduleChangeFact records a schedule configuration change.
type ScheduleChangeFact struct {
	Cron string `json:"cron,omitempty"`
}

// AppendScheduleChange records a schedule configuration change.
// Marked ignorable — operational metadata, not model-visible.
func (l *Log) AppendScheduleChange(cron string) {
	if l == nil {
		return
	}
	l.AppendIgnorable(ScheduleChange, ScheduleChangeFact{Cron: cron})
}

// --- Session titles ---

// SessionTitleFact records a log-backed session title.
type SessionTitleFact struct {
	Title string `json:"title,omitempty"`
}

// AppendSessionTitle records a session title.
func (l *Log) AppendSessionTitle(title string) {
	if l == nil {
		return
	}
	l.Append(SessionTitle, SessionTitleFact{Title: title})
}

// SessionTitleLLMRequestFact records an LLM-driven title generation attempt.
type SessionTitleLLMRequestFact struct {
	Model string `json:"model,omitempty"`
}

// AppendSessionTitleLLMRequest records an LLM title generation attempt.
// Marked ignorable — the title itself lives in session.title; this event
// is trace-only data about the generation attempt.
func (l *Log) AppendSessionTitleLLMRequest(model string) {
	if l == nil {
		return
	}
	l.AppendIgnorable(SessionTitleLLMRequest, SessionTitleLLMRequestFact{Model: model})
}

// --- Subagent ---

// SubagentDescriptorVersion is the durable subagent-descriptor format version.
// Must match DSH's SUBAGENT_DESCRIPTOR_VERSION=2 for cross-platform log
// compatibility. A mismatched version means the child cannot be classified by
// this runtime and is treated as opaque.
const SubagentDescriptorVersion = 2

// SubagentDescriptorFact records the durable identity and lifecycle mode of a
// session-backed subagent child, matching DSH's subagent/descriptor event.
// The descriptor is log-only: it carries no surfaceOp and never enters the
// model-visible surface. It is appended once by the establishing provider
// inside the child's initial turn, before its first request.
type SubagentDescriptorFact struct {
	// Rho compat fields (legacy, still populated on append):
	Name  string `json:"name,omitempty"`
	Agent string `json:"agent,omitempty"`
	Depth int    `json:"depth,omitempty"`

	// DSH parity fields (all optional for backward compatibility):
	// Version is the descriptor format version (must be SubagentDescriptorVersion).
	Version int `json:"version,omitempty"`
	// Mode is "one-shot" or "continuable".
	Mode string `json:"mode,omitempty"`
	// Provider is the ctx.subagents provider name that established the child.
	Provider string `json:"provider,omitempty"`
	// Label is the initial delegation's short description (required for continuable).
	Label string `json:"label,omitempty"`
	// AgentProvider is the resolved child agentOptions.provider (continuable only).
	AgentProvider string `json:"agent_provider,omitempty"`
	// AgentModel is the resolved child agentOptions.model (continuable only).
	AgentModel string `json:"agent_model,omitempty"`
	// Persona is a per-child persona that shadows the deployment persona (continuable only).
	Persona string `json:"persona,omitempty"`
	// ToolFilterAllow is the allow-list for child tool scoping (continuable only).
	ToolFilterAllow []string `json:"tool_filter_allow,omitempty"`
	// ToolFilterDeny is the deny-list for child tool scoping (continuable only).
	ToolFilterDeny []string `json:"tool_filter_deny,omitempty"`
}

// AppendSubagentDescriptor records subagent metadata with a simplified payload.
func (l *Log) AppendSubagentDescriptor(f SubagentDescriptorFact) {
	if l == nil {
		return
	}
	l.Append(SubagentDescriptor, f)
}

// AppendSubagentDescriptorFull records subagent metadata with full DSH parity.
// The Version field defaults to SubagentDescriptorVersion when zero.
func (l *Log) AppendSubagentDescriptorFull(f SubagentDescriptorFact) {
	if l == nil {
		return
	}
	if f.Version == 0 {
		f.Version = SubagentDescriptorVersion
	}
	l.Append(SubagentDescriptor, f)
}

// --- Agent preset ---

// AgentPresetFact records the selected agent preset.
type AgentPresetFact struct {
	Preset string `json:"preset,omitempty"`
}

// AppendAgentPresetSelected records the selected agent preset.
func (l *Log) AppendAgentPresetSelected(preset string) {
	if l == nil {
		return
	}
	l.Append(AgentPresetSelected, AgentPresetFact{Preset: preset})
}

// --- Agent inbox splice ---

// AgentInboxSpliceFact records a splice into the agent inbox.
type AgentInboxSpliceFact struct {
	Count    int `json:"count,omitempty"`
	Position int `json:"position,omitempty"`
}

// AppendAgentInboxSpliced records an inbox splice.
func (l *Log) AppendAgentInboxSpliced(count, position int) {
	if l == nil {
		return
	}
	l.Append(AgentInboxSpliced, AgentInboxSpliceFact{Count: count, Position: position})
}

// --- Command lifecycle ---

// CommandRunFact records a command execution start.
type CommandRunFact struct {
	Command string `json:"command,omitempty"`
	// DSH parity fields:
	CommandID string         `json:"command_id,omitempty"` // paired with command/done
	Name      string         `json:"name,omitempty"`       // resolved command name
	Args      string         `json:"args,omitempty"`       // raw input arguments
	Source    map[string]any `json:"source,omitempty"`     // who issued the command (e.g. {"kind":"user"})
}

// CommandDoneFact records a command execution completion.
type CommandDoneFact struct {
	Command  string `json:"command,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	Error    string `json:"error,omitempty"`
	// DSH parity fields:
	CommandID      string `json:"command_id,omitempty"`       // paired with command/run
	Kind           string `json:"kind,omitempty"`             // "success" or "error"
	Text           string `json:"text,omitempty"`             // rendered outcome text
	SourceEventSeq uint64 `json:"source_event_seq,omitempty"` // paired domain event seq
}

// AppendCommandRun records a command execution start.
func (l *Log) AppendCommandRun(command string) {
	if l == nil {
		return
	}
	l.Append(CommandRun, CommandRunFact{Command: command})
}

// AppendCommandDone records a command execution completion.
func (l *Log) AppendCommandDone(command string, exitCode int, err string) {
	if l == nil {
		return
	}
	l.Append(CommandDone, CommandDoneFact{Command: command, ExitCode: exitCode, Error: err})
}

// --- Tool workflow agent lifecycle ---

// ToolWorkflowAgentFact records agent lifecycle within a tool workflow.
type ToolWorkflowAgentFact struct {
	Agent string `json:"agent,omitempty"`
}

// AppendToolWorkflowAgentStart records agent lifecycle entry in a tool workflow.
func (l *Log) AppendToolWorkflowAgentStart(agent string) {
	if l == nil {
		return
	}
	l.Append(ToolWorkflowAgentStart, ToolWorkflowAgentFact{Agent: agent})
}

// AppendToolWorkflowAgentEnd records agent lifecycle exit in a tool workflow.
func (l *Log) AppendToolWorkflowAgentEnd(agent string) {
	if l == nil {
		return
	}
	l.Append(ToolWorkflowAgentEnd, ToolWorkflowAgentFact{Agent: agent})
}

// --- Code dispatch ---

// CodeDispatchFact records a code-dispatch tool execution.
type CodeDispatchFact struct {
	Language string `json:"language,omitempty"`
}

// AppendToolCodeDispatch records a code dispatch tool execution.
func (l *Log) AppendToolCodeDispatch(language string) {
	if l == nil {
		return
	}
	l.Append(ToolCodeDispatch, CodeDispatchFact{Language: language})
}

// AppendToolCodeDispatchStart records a code dispatch tool execution start.
func (l *Log) AppendToolCodeDispatchStart(language string) {
	if l == nil {
		return
	}
	l.Append(ToolCodeDispatchStart, CodeDispatchFact{Language: language})
}

// --- Web search ---

// WebSearchFact records a web/deepseek-search LLM request.
type WebSearchFact struct {
	Query string `json:"query,omitempty"`
}

// AppendWebDeepSeekSearch records a web/deepseek-search LLM request.
func (l *Log) AppendWebDeepSeekSearch(query string) {
	if l == nil {
		return
	}
	l.Append(WebDeepSeekSearch, WebSearchFact{Query: query})
}

// --- Plan mode ---

// PlanModeFact records plan-mode activation/deactivation, matching DSH's
// plan/mode event. The last appended event wins (last-write-wins on replay).
type PlanModeFact struct {
	Active bool `json:"active"`
}

// AppendPlanMode records plan-mode activation or deactivation.
func (l *Log) AppendPlanMode(active bool) {
	if l == nil {
		return
	}
	l.Append(PlanMode, PlanModeFact{Active: active})
}

// --- In-conversation schedule ---

// ScheduleCreateFact records an in-conversation schedule creation.
type ScheduleCreateFact struct {
	ID        string    `json:"id"`
	Prompt    string    `json:"prompt"`
	DueAt     time.Time `json:"due_at"`
	Interval  string    `json:"interval,omitempty"`
	Recurring bool      `json:"recurring,omitempty"`
}

// AppendScheduleCreate records schedule creation.
func (l *Log) AppendScheduleCreate(fact ScheduleCreateFact) {
	if l == nil {
		return
	}
	l.Append(ScheduleCreate, fact)
}

// ScheduleUpdateFact records an in-conversation schedule update.
type ScheduleUpdateFact struct {
	ID     string     `json:"id"`
	Prompt *string    `json:"prompt,omitempty"`
	DueAt  *time.Time `json:"due_at,omitempty"`
}

// AppendScheduleUpdate records schedule update.
func (l *Log) AppendScheduleUpdate(fact ScheduleUpdateFact) {
	if l == nil {
		return
	}
	l.Append(ScheduleUpdate, fact)
}

// ScheduleDeleteFact records an in-conversation schedule deletion.
type ScheduleDeleteFact struct {
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
}

// AppendScheduleDelete records schedule deletion.
func (l *Log) AppendScheduleDelete(id, reason string) {
	if l == nil {
		return
	}
	l.Append(ScheduleDelete, ScheduleDeleteFact{ID: id, Reason: reason})
}

// ScheduleDueFact records that an in-conversation schedule became due and was delivered.
type ScheduleDueFact struct {
	ID          string     `json:"id"`
	DeliveredAt time.Time  `json:"delivered_at"`
	NextDueAt   *time.Time `json:"next_due_at,omitempty"`
}

// AppendScheduleDue records schedule reminder delivery.
func (l *Log) AppendScheduleDue(fact ScheduleDueFact) {
	if l == nil {
		return
	}
	l.Append(ScheduleDue, fact)
}
