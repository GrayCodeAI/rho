package session

import (
	"testing"

	store "github.com/GrayCodeAI/rho/internal/session"
)

func TestHydrateKeepsRuntimeAndVisibleDisplayProjections(t *testing.T) {
	saved := &store.Session{Messages: []store.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "hello"},
		{Role: "tool", Content: "tool output"},
		{Role: "assistant", Content: "hi"},
	}}
	got := Hydrate(saved)
	if len(got.Runtime) != 4 || len(got.Display) != 2 || got.Display[0].Role != "user" || got.Display[1].Content != "hi" {
		t.Fatalf("Hydrate = %#v", got)
	}
}

func TestHydrateNil(t *testing.T) {
	got := Hydrate(nil)
	if got.Runtime != nil || got.Display != nil {
		t.Fatalf("Hydrate(nil) = %#v", got)
	}
}
