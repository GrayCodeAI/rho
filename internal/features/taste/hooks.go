package taste

import (
	"crypto/sha256"
	"fmt"
	"sync"
)

// Hooks provides integration points for the engine/REPL loop to feed signals
// to the taste collector.
type Hooks struct {
	mu        sync.Mutex
	collector *Collector
	profile   *Profile
	store     *Store
	projectID string
}

// NewHooks creates a taste hooks instance bound to a project.
func NewHooks(projectID string, store *Store) (*Hooks, error) {
	profile, err := store.Load(projectID)
	if err != nil {
		profile = NewProfile(projectID)
	}

	return &Hooks{
		collector: NewCollector(profile),
		profile:   profile,
		store:     store,
		projectID: projectID,
	}, nil
}

// Profile returns the current taste profile.
func (h *Hooks) Profile() *Profile {
	return h.profile
}

// Collector returns the underlying collector.
func (h *Hooks) Collector() *Collector {
	return h.collector
}

// OnCodeProposed records a new code proposal from the agent.
func (h *Hooks) OnCodeProposed(sessionID, proposedCode string) string {
	id := generateProposalID(sessionID, proposedCode)
	h.collector.RecordProposal(id, proposedCode)
	return id
}

// OnCodeAccepted records that the user accepted the proposed code without edits.
func (h *Hooks) OnCodeAccepted(sessionID, proposedCode string) {
	id := generateProposalID(sessionID, proposedCode)
	h.collector.RecordProposal(id, proposedCode)
	h.collector.RecordOutcome(id, OutcomeAccept)
	h.persist()
}

// OnCodeEdited records that the user modified the proposed code.
func (h *Hooks) OnCodeEdited(sessionID, proposedCode, finalCode string) {
	id := generateProposalID(sessionID, proposedCode)
	h.collector.RecordProposal(id, proposedCode)
	h.collector.RecordEdit(id, finalCode)
	h.persist()
}

// OnCodeRejected records that the user rejected the proposed code entirely.
func (h *Hooks) OnCodeRejected(sessionID, proposedCode string) {
	id := generateProposalID(sessionID, proposedCode)
	h.collector.RecordProposal(id, proposedCode)
	h.collector.RecordOutcome(id, OutcomeReject)
	h.persist()
}

// PromptContext returns the current taste profile as a system prompt fragment.
func (h *Hooks) PromptContext() string {
	return h.profile.ToPromptContext()
}

// persist saves the profile to disk.
func (h *Hooks) persist() {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Best-effort save — don't block the user on persistence failures.
	_ = h.store.Save(h.projectID, h.profile)
}

// generateProposalID creates a deterministic ID from session and code content.
func generateProposalID(sessionID, code string) string {
	h := sha256.New()
	h.Write([]byte(sessionID))
	h.Write([]byte(code[:min(len(code), 500)]))
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
