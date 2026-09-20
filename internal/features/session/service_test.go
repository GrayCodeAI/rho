package session

import (
	"testing"
	"time"

	"github.com/GrayCodeAI/rho/internal/types"
)

func TestSnapshotPersistentConvertsRuntimeMessages(t *testing.T) {
	created := time.Unix(123, 0)
	s := Snapshot{
		ID:       "session-1",
		Model:    "model-1",
		Provider: "provider-1",
		Messages: []types.FluxMessage{{Role: "user", Content: "hello"}},
		Created:  created,
	}

	got := s.Persistent()
	if got.ID != s.ID || got.Model != s.Model || got.Provider != s.Provider {
		t.Fatalf("metadata mismatch: %#v", got)
	}
	if !got.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, created)
	}
	if len(got.Messages) != 1 || got.Messages[0].Role != "user" || got.Messages[0].Content != "hello" {
		t.Fatalf("messages = %#v", got.Messages)
	}
}

func TestSnapshotPersistentDefaultsCreatedAt(t *testing.T) {
	before := time.Now()
	got := (Snapshot{ID: "session-1"}).Persistent()
	after := time.Now()
	if got.CreatedAt.Before(before) || got.CreatedAt.After(after) {
		t.Fatalf("CreatedAt = %v, outside [%v, %v]", got.CreatedAt, before, after)
	}
}
