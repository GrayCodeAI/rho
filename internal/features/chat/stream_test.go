package chat

import (
	"testing"

	"github.com/GrayCodeAI/rho/internal/engine"
)

func TestPumpStreamEventsCoalescesContentAndPreservesTerminalEvent(t *testing.T) {
	input := make(chan engine.StreamEvent, 4)
	input <- engine.StreamEvent{Type: "content", Content: "first"}
	input <- engine.StreamEvent{Type: "content", Content: " second"}
	input <- engine.StreamEvent{Type: "tool_use", ToolName: "search"}
	close(input)

	var got []engine.StreamEvent
	PumpStreamEvents(input, func(event engine.StreamEvent) { got = append(got, event) })
	if len(got) != 4 {
		t.Fatalf("got %d events, want 4: %#v", len(got), got)
	}
	if got[0].Content != "first" || got[1].Content != " second" || got[2].Type != "tool_use" || got[3].Type != "done" {
		t.Fatalf("unexpected events: %#v", got)
	}
}

func TestPumpStreamEventsStopsAtError(t *testing.T) {
	input := make(chan engine.StreamEvent, 2)
	input <- engine.StreamEvent{Type: "error", Content: "failed"}
	input <- engine.StreamEvent{Type: "content", Content: "late"}
	close(input)

	var got []engine.StreamEvent
	PumpStreamEvents(input, func(event engine.StreamEvent) { got = append(got, event) })
	if len(got) != 1 || got[0].Type != "error" {
		t.Fatalf("unexpected post-error events: %#v", got)
	}
}

func TestShouldFlushStreamChunkBuffer(t *testing.T) {
	if ShouldFlushStreamChunkBuffer("short") || !ShouldFlushStreamChunkBuffer("sentence.") || !ShouldFlushStreamChunkBuffer("line\n") {
		t.Fatal("unexpected punctuation flush behavior")
	}
	if !ShouldFlushStreamChunkBuffer(string(make([]byte, 512))) {
		t.Fatal("expected size flush")
	}
}
