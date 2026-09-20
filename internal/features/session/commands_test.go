package session

import (
	"strings"
	"testing"
	"time"

	store "github.com/GrayCodeAI/rho/internal/session"
)

func TestFormatHistory(t *testing.T) {
	got := formatHistory([]store.Entry{{
		ID: "session-1", UpdatedAt: time.Date(2026, 9, 19, 22, 10, 0, 0, time.UTC), Preview: "hello",
	}})
	if want := "  session-1  Sep 19 22:10  hello\n"; got != want {
		t.Fatalf("formatHistory = %q, want %q", got, want)
	}
}

func TestFormatSearch(t *testing.T) {
	got := formatSearch("hello", []store.SearchResult{{SessionID: "session-1", MsgIndex: 2, Role: "user", Preview: "hello world"}})
	if !strings.Contains(got, "Search results for \"hello\":\n") || !strings.Contains(got, "[session-1] msg 2 (user): hello world") {
		t.Fatalf("formatSearch = %q", got)
	}
}
