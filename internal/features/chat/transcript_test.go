package chat

import "testing"

func TestPlainTranscriptOmitsPresentationMessages(t *testing.T) {
	got := PlainTranscript([]Message{
		{Role: "welcome", Content: "welcome"},
		{Role: "user", Content: " hello "},
		{Role: "assistant", Content: "world"},
		{Role: "usage", Content: "tokens"},
	}, "partial")
	want := "You: hello\n\nrho: world\n\nrho: partial"
	if got != want {
		t.Fatalf("PlainTranscript() = %q, want %q", got, want)
	}
}

func TestPlainTranscriptLineLabelsOperationalMessages(t *testing.T) {
	tests := []struct {
		role string
		want string
	}{
		{role: "error", want: "Error: failed"},
		{role: "thinking", want: "thinking: considering"},
		{role: "tool_use", want: "tool: search"},
		{role: "permission", want: "permission: allow"},
	}
	for _, test := range tests {
		got, ok := PlainTranscriptLine(Message{Role: test.role, Content: stringsForRole(test.role)})
		if !ok || got != test.want {
			t.Errorf("PlainTranscriptLine(%q) = %q, %v; want %q, true", test.role, got, ok, test.want)
		}
	}
}

func TestTurnThinkingPolicy(t *testing.T) {
	messages := []Message{{Role: "user", Content: "question"}, {Role: "thinking", Content: "hidden"}}
	if !HadThinkingOnly(messages) {
		t.Fatal("expected thinking-only turn")
	}
	clean := StripCurrentTurnThinking(messages)
	if len(clean) != 1 || clean[0].Role != "user" {
		t.Fatalf("clean transcript = %#v", clean)
	}
	messages = append(messages, Message{Role: "tool_use", Content: "search"})
	if HadThinkingOnly(messages) {
		t.Fatal("tool activity should prevent thinking-only classification")
	}
}

func TestTrimMessagesPreservesWelcomeAndReportsRemovedCount(t *testing.T) {
	messages := []Message{{Role: "welcome", Content: "welcome"}}
	for i := 0; i < 5; i++ {
		messages = append(messages, Message{Role: "user", Content: string(rune('a' + i))})
	}
	trimmed, removed := TrimMessages(messages, 5, 4, "welcome")
	if removed != 2 || len(trimmed) != 5 {
		t.Fatalf("TrimMessages() removed=%d len=%d, want 2 and 5", removed, len(trimmed))
	}
	if trimmed[0].Role != "welcome" || trimmed[1].Role != "system" || trimmed[2].Content != "c" {
		t.Fatalf("unexpected trimmed transcript: %#v", trimmed)
	}
}

func stringsForRole(role string) string {
	switch role {
	case "error":
		return "failed"
	case "thinking":
		return "considering"
	case "tool_use":
		return "search"
	default:
		return "allow"
	}
}
