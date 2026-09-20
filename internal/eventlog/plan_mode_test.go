package eventlog

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPlanModeFactRoundTrip(t *testing.T) {
	log := New(nil)
	log.AppendPlanMode(true)
	log.AppendPlanMode(false)

	events := log.Snapshot()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != PlanMode {
		t.Errorf("expected PlanMode type, got %s", events[0].Type)
	}
	f0, ok := events[0].Data.(PlanModeFact)
	if !ok {
		t.Fatal("expected PlanModeFact data")
	}
	if !f0.Active {
		t.Errorf("expected active=true")
	}
	f1, _ := events[1].Data.(PlanModeFact)
	if f1.Active {
		t.Errorf("expected active=false on second event")
	}
}

func TestPlanModeKnown(t *testing.T) {
	if !PlanMode.Known() {
		t.Fatal("PlanMode should be known")
	}
}

func TestPlanModeWireDecode(t *testing.T) {
	wire := []WireEvent{
		{
			Type: PlanMode, Seq: 1, At: time.Now().UTC(),
			Data: mustJSONRaw(t, PlanModeFact{Active: true}),
		},
	}
	events, err := DecodeWire(wire)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatal("expected 1 event")
	}
	f, ok := events[0].Data.(PlanModeFact)
	if !ok {
		t.Fatal("expected PlanModeFact")
	}
	if !f.Active {
		t.Error("expected active=true")
	}
}

func TestProjectSessionStats(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	events := []Event{
		{Type: TurnStart, Seq: 1, At: now, Data: BoundaryFact{Turn: 1}},
		{Type: StepStart, Seq: 2, At: now.Add(1 * time.Millisecond), Data: BoundaryFact{Turn: 1, Step: 1}},
		{
			Type: AssistantChunk, Seq: 3, At: now.Add(5 * time.Millisecond),
			Data: ChunkFact{Turn: 1, Step: 1, Chunk: "Hello", Kind: ""},
		},
		{
			Type: AssistantMsg, Seq: 4, At: now.Add(10 * time.Millisecond),
			Data: Message{Role: "assistant", Content: "Hello"},
		},
		{Type: StepEnd, Seq: 5, At: now.Add(10 * time.Millisecond), Data: BoundaryFact{Turn: 1, Step: 1}},
		{Type: TurnEnd, Seq: 6, At: now.Add(11 * time.Millisecond), Data: TurnEndFact{Turn: 1, Reason: "completed"}},
	}

	stats := ProjectSessionStats(events)
	if stats.Steps != 1 {
		t.Errorf("expected 1 step, got %d", stats.Steps)
	}
	if stats.Turns != 1 {
		t.Errorf("expected 1 turn, got %d", stats.Turns)
	}
	if stats.TTFPSteps != 1 {
		t.Errorf("expected 1 ttfp step, got %d", stats.TTFPSteps)
	}
	if stats.LLMMs != 9 {
		t.Errorf("expected ~9ms LLM time, got %d", stats.LLMMs)
	}
}

func TestProjectSessionStatsEmpty(t *testing.T) {
	stats := ProjectSessionStats(nil)
	if stats.Turns != 0 || stats.Steps != 0 {
		t.Errorf("expected zero stats for empty events")
	}
}

func TestProjectPermissionsFromPresetAndPolicy(t *testing.T) {
	now := time.Now().UTC()
	events := []Event{
		{Type: PermissionPreset, Seq: 1, At: now, Data: PermissionPresetFact{PresetName: "strict"}},
		{Type: ApprovalPolicy, Seq: 2, At: now, Data: ApprovalPolicyFact{Policy: "never"}},
	}
	ps := ProjectPermissions(events)
	if ps.CurrentValue != "strict" {
		t.Errorf("expected preset value 'strict', got %q", ps.CurrentValue)
	}
}

func TestProjectPermissionsDefaultPolicy(t *testing.T) {
	events := []Event{
		{Type: ApprovalPolicy, Seq: 1, At: time.Now().UTC(), Data: ApprovalPolicyFact{Policy: "never"}},
	}
	ps := ProjectPermissions(events)
	if ps.CurrentValue != "never" {
		t.Errorf("expected 'never', got %q", ps.CurrentValue)
	}
}

func TestProjectPermissionsEmpty(t *testing.T) {
	ps := ProjectPermissions(nil)
	// DSH default: effectiveApprovalPolicy defaults to 'ask' (APPROVAL_POLICIES[0])
	if ps.CurrentValue != "ask" {
		t.Errorf("expected default 'ask', got %q", ps.CurrentValue)
	}
}

func mustJSONRaw(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
