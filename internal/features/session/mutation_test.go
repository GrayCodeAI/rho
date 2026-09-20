package session

import (
	"testing"

	"github.com/GrayCodeAI/rho/internal/types"
)

func TestRemoveLastExchangesSkipsToolMessages(t *testing.T) {
	messages := []types.FluxMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "two"},
		{Role: "tool_result", Content: "three"},
	}
	got, removed := RemoveLastExchanges(messages, 1)
	if removed != 1 || len(got) != 1 || got[0].Role != "system" {
		t.Fatalf("got messages=%#v removed=%d", got, removed)
	}
}

func TestRemoveLastExchangesDoesNotRemoveIncompletePair(t *testing.T) {
	messages := []types.FluxMessage{{Role: "system"}, {Role: "user", Content: "only user"}}
	got, removed := RemoveLastExchanges(messages, 1)
	if removed != 0 || len(got) != len(messages) {
		t.Fatalf("got messages=%#v removed=%d", got, removed)
	}
}
