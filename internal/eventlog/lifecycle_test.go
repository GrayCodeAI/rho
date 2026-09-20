package eventlog

import (
	"testing"
	"time"
)

// TestNewEventTypesKnown verifies all ported DeepSeek Harness event types are
// recognized by Known().
func TestNewEventTypesKnown(t *testing.T) {
	newTypes := []Type{
		CompactionStart, CompactionPrune, CompactionEnd, CompactionSummary,
		SessionEndSeed, TodoWrite, RequestHeader, HookInvoked, HookResult,
		FeedbackRecord, GoalChange, PermissionPreset,
		ScheduleChange, SessionTitle, SessionTitleLLMRequest, SubagentDescriptor,
		AgentPresetSelected, AgentInboxSpliced, CommandRun, CommandDone,
		ToolWorkflowAgentStart, ToolWorkflowAgentEnd,
		ToolCodeDispatch, ToolCodeDispatchStart,
		WebDeepSeekSearch,
	}
	for _, ty := range newTypes {
		if !ty.Known() {
			t.Errorf("Type(%q) should be known", ty)
		}
	}

	// Verify string-to-type round-trips.
	cases := map[string]Type{
		"compaction.start":                CompactionStart,
		"compaction.prune":                CompactionPrune,
		"compaction.end":                  CompactionEnd,
		"compaction.summary":              CompactionSummary,
		"session.end-seed":                SessionEndSeed,
		"todo.write":                      TodoWrite,
		"request.header":                  RequestHeader,
		"hook.invoked":                    HookInvoked,
		"hook.result":                     HookResult,
		"feedback.record":                 FeedbackRecord,
		"goal.change":                     GoalChange,
		"permission.preset":               PermissionPreset,
		"schedule.change":                 ScheduleChange,
		"schedule.create":                 ScheduleCreate,
		"schedule.update":                 ScheduleUpdate,
		"schedule.delete":                 ScheduleDelete,
		"schedule.due":                    ScheduleDue,
		"session.title":                   SessionTitle,
		"session.title-llm-request":       SessionTitleLLMRequest,
		"subagent.descriptor":             SubagentDescriptor,
		"agent-preset.selected":           AgentPresetSelected,
		"agent.inbox.spliced":             AgentInboxSpliced,
		"command.run":                     CommandRun,
		"command.done":                    CommandDone,
		"tool-workflow.agent-start":       ToolWorkflowAgentStart,
		"tool-workflow.agent-end":         ToolWorkflowAgentEnd,
		"tool.code-dispatch":              ToolCodeDispatch,
		"tool.code-dispatch-start":        ToolCodeDispatchStart,
		"web.deepseek-search-llm-request": WebDeepSeekSearch,
	}
	for s, ty := range cases {
		if Type(s) != ty {
			t.Errorf("Type(%q) = %q, want %q", s, Type(s), ty)
		}
		if !Type(s).Known() {
			t.Errorf("Type(%q) should be Known", s)
		}
	}
}

// TestLifecycleEventsRoundTrip verifies that all new event types survive a
// MarshalWire → DecodeWire round-trip with their typed payloads intact.
func TestLifecycleEventsRoundTrip(t *testing.T) {
	l := New(nil)

	l.AppendCompactionStart("sliding-context-window")
	l.AppendCompactionPrune("sliding-context-window", 3)
	l.AppendCompactionEnd(CompactionEndFact{
		Strategy:     "sliding-context-window",
		TokensBefore: 8000,
		TokensAfter:  4000,
	})
	l.AppendCompactionSummary("Summarized early conversation turns.")
	l.AppendSessionEndSeed()
	l.AppendTodoWrite([]TodoItem{
		{Content: "Implement chunk packing", Status: "completed"},
		{Content: "Write tests", Status: "in_progress"},
	})
	l.AppendRequestHeader(RequestHeaderFact{
		System: "You are a coding agent.",
		Tools:  []string{"read", "write", "bash"},
		Reason: RequestHeaderChange,
	})
	l.AppendHookInvoked("post-response")
	l.AppendHookResult("post-response", "")
	l.AppendHookResult("pre-tool", "timeout")
	l.AppendFeedbackRecord(FeedbackFact{
		Category: "negative",
		Detail:   "Wrong refactor suggestion",
		Thumb:    "down",
	})
	l.AppendGoalChange("Refactor the auth module", true)
	l.AppendPermissionPreset("standard_exec", true)
	l.AppendScheduleChange("0 8 * * MON-FRI")
	l.AppendSessionTitle("Refactor auth module")
	l.AppendSessionTitleLLMRequest("deepseek-chat")
	l.AppendSubagentDescriptor(SubagentDescriptorFact{
		Name:  "reviewer-1",
		Agent: "code-reviewer",
		Depth: 1,
	})
	l.AppendAgentPresetSelected("default-agent")
	l.AppendAgentInboxSpliced(1, 0)
	l.AppendCommandRun("go test ./internal/eventlog/")
	l.AppendCommandDone("go test ./internal/eventlog/", 0, "")
	l.AppendToolWorkflowAgentStart("research-agent")
	l.AppendToolWorkflowAgentEnd("research-agent")
	l.AppendToolCodeDispatch("go")
	l.AppendToolCodeDispatchStart("python")
	l.AppendWebDeepSeekSearch("how to port TypeScript to Go")

	events := l.Snapshot()
	if len(events) != 26 {
		t.Fatalf("expected 26 events, got %d", len(events))
	}

	wire, err := MarshalWire(events)
	if err != nil {
		t.Fatalf("MarshalWire: %v", err)
	}
	decoded, err := DecodeWire(wire)
	if err != nil {
		t.Fatalf("DecodeWire: %v", err)
	}
	if len(decoded) != 26 {
		t.Fatalf("decoded %d events, want 26", len(decoded))
	}

	// Spot-check a few payloads.
	if f, ok := decoded[0].Data.(CompactionStartFact); !ok || f.Strategy != "sliding-context-window" {
		t.Fatalf("CompactionStart payload = %T %+v", decoded[0].Data, decoded[0].Data)
	}
	if f, ok := decoded[1].Data.(CompactionPruneFact); !ok || f.Messages != 3 {
		t.Fatalf("CompactionPrune payload = %T %+v", decoded[1].Data, decoded[1].Data)
	}
	if f, ok := decoded[3].Data.(CompactionSummaryFact); !ok || f.Summary != "Summarized early conversation turns." {
		t.Fatalf("CompactionSummary payload = %T %+v", decoded[3].Data, decoded[3].Data)
	}

	// SessionEndSeed has nil data (no payload).
	if decoded[4].Data != nil {
		t.Fatalf("SessionEndSeed payload should be nil, got %T", decoded[4].Data)
	}

	if f, ok := decoded[5].Data.(TodoWriteFact); !ok || len(f.Todos) != 2 {
		t.Fatalf("TodoWrite payload = %T %+v", decoded[5].Data, decoded[5].Data)
	}
	if f, ok := decoded[6].Data.(RequestHeaderFact); !ok || f.System != "You are a coding agent." {
		t.Fatalf("RequestHeader payload = %T %+v", decoded[6].Data, decoded[6].Data)
	}

	// HookResult with error should show Success=false.
	if f, ok := decoded[9].Data.(HookResultFact); !ok || f.Success {
		t.Fatalf("HookResult with error should be non-success: %+v", decoded[9].Data)
	}
	if f, ok := decoded[8].Data.(HookResultFact); !ok || !f.Success {
		t.Fatalf("HookResult without error should be success: %+v", decoded[8].Data)
	}

	if f, ok := decoded[12].Data.(PermissionPresetFact); !ok || !f.Covered {
		t.Fatalf("PermissionPreset payload = %T %+v", decoded[12].Data, decoded[12].Data)
	}

	if f, ok := decoded[23].Data.(CodeDispatchFact); !ok || f.Language != "go" {
		t.Fatalf("ToolCodeDispatch payload = %T %+v", decoded[23].Data, decoded[23].Data)
	}
}

// TestNewEventTypesValidate verifies the full Validate path accepts logs
// containing the new event types.
func TestNewEventTypesValidate(t *testing.T) {
	events := []Event{
		{Type: CompactionStart, Seq: 1, At: time.Now(), Data: CompactionStartFact{Strategy: "lru"}},
		{Type: TodoWrite, Seq: 2, At: time.Now(), Data: TodoWriteFact{Todos: []TodoItem{{Content: "test", Status: "pending"}}}},
		{Type: SessionEndSeed, Seq: 3, At: time.Now(), Data: nil},
		{Type: FeedbackRecord, Seq: 4, At: time.Now(), Data: FeedbackFact{Category: "positive", Thumb: "up"}},
	}
	if err := Validate(events); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
