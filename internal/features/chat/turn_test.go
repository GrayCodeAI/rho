package chat

import "testing"

func TestTurnStateReasoningOnly(t *testing.T) {
	var state TurnState
	state.ObserveThinking()
	if !state.ReasoningOnly() {
		t.Fatal("thinking without output or tools should be reasoning-only")
	}
	state.ObserveContent()
	if state.ReasoningOnly() {
		t.Fatal("content should clear reasoning-only classification")
	}
	state.Reset()
	state.ObserveThinking()
	state.ObserveTool()
	if state.ReasoningOnly() {
		t.Fatal("tool activity should clear reasoning-only classification")
	}
}
