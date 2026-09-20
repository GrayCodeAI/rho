package session

import store "github.com/GrayCodeAI/rho/internal/session"

// Load returns a durable session for an explicit resume operation.
func Load(id string) (*store.Session, error) {
	return store.Load(id)
}

// RecoveryCandidates returns sessions with recoverable interrupted work.
func RecoveryCandidates() []store.RecoveryCandidate {
	return store.ScanForRecovery()
}

// Resume recovers a session from its durable record or WAL and returns the
// recovery note for the UI.
func Resume(id string) (*store.Session, string, error) {
	return store.ResumeSession(id)
}

// ForkAtMessage creates a legacy message-index fork. Conversation-graph forks
// remain in the engine session until that graph becomes a feature-owned seam.
func ForkAtMessage(id string, index int) (*store.Session, error) {
	return store.Fork(id, index)
}
