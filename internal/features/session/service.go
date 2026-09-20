// Package session owns the application-level session persistence seam.
//
// The lower-level session package owns the file format and recovery
// algorithms. This package owns the boundary between a live runtime
// conversation and the durable session record, including the conversation-arc
// sidecar. CLI and TUI code should depend on this package instead of building
// persistence records itself.
package session

import (
	"path/filepath"
	"time"

	"github.com/GrayCodeAI/rho/internal/conversationarc"
	store "github.com/GrayCodeAI/rho/internal/session"
	"github.com/GrayCodeAI/rho/internal/storage"
	"github.com/GrayCodeAI/rho/internal/types"
)

// Snapshot is the persistence boundary for a live conversation.
type Snapshot struct {
	ID       string
	Model    string
	Provider string
	Messages []types.FluxMessage
	Created  time.Time
	Arc      *conversationarc.Arc
}

// Persistent converts a live snapshot into the durable session representation.
func (s Snapshot) Persistent() *store.Session {
	created := s.Created
	if created.IsZero() {
		created = time.Now()
	}
	return &store.Session{
		ID:        s.ID,
		Model:     s.Model,
		Provider:  s.Provider,
		Messages:  store.FromRuntimeMessages(s.Messages),
		CreatedAt: created,
	}
}

// Save writes the durable session atomically and persists its arc sidecar on a
// best-effort basis, matching the existing durability contract. The session
// record is authoritative; a sidecar failure must not turn a successful
// transcript save into a failed save.
func Save(s Snapshot) error {
	if err := store.Save(s.Persistent()); err != nil {
		return err
	}
	if s.Arc != nil && !s.Arc.IsEmpty() {
		_ = s.Arc.Save(filepath.Join(storage.SessionsDir(), s.ID))
	}
	return nil
}

// LoadArc loads the durable conversation arc for a session. A missing sidecar
// is returned as an error so callers can explicitly choose their new-arc
// fallback.
func LoadArc(id string) (*conversationarc.Arc, error) {
	return conversationarc.Load(filepath.Join(storage.SessionsDir(), id))
}

// NewArc creates the initial conversation arc for a session.
func NewArc() *conversationarc.Arc {
	return conversationarc.New()
}
