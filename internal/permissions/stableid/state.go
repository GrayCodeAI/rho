// Package stableid ports fx's session permission state
// (vercel-labs/fx, src/core/permissions/session_permission_state.zig):
// session-scoped, exact permission rules each carrying a stable, monotonically
// increasing id and generation. Operations are pure: ApplySet/ApplyRevoke
// return a new immutable State plus a status, so concurrent updates can be
// merged optimistically with an expected_generation check (fx's
// applied/stale/full/invalid outcomes).
//
// The invariant this preserves over rho's glob-based remembered rules is
// that a rule is addressable by a stable id for the lifetime of the session —
// it survives workspace changes — and can be listed and revoked by that id
// without re-deriving the rule from the current workspace.
package stableid

import (
	"crypto/sha256"
	"fmt"
	"sort"
)

// maxRules and maxIdentityBytes bound the state, matching fx.
const (
	maxRules         = 1024
	maxIdentityBytes = 4096
)

// Kind classifies the identity a rule is keyed on.
type Kind int

const (
	KindCommand Kind = iota
	KindFileMutation
	KindStructuredTool
)

func (k Kind) String() string {
	switch k {
	case KindCommand:
		return "command"
	case KindFileMutation:
		return "file_mutation"
	case KindStructuredTool:
		return "structured_tool"
	}
	return "unknown"
}

// CanonicalIdentity namespaces an exact identity by its rule kind. The kind
// separator is part of the stable-key contract so identical text used by two
// different rule kinds can never collide.
func CanonicalIdentity(kind Kind, identity string) string {
	return kind.String() + "\x00" + identity
}

// Decision is the action a rule prescribes.
type Decision int

const (
	Deny Decision = iota
	Allow
)

func (d Decision) String() string {
	if d == Allow {
		return "allow"
	}
	return "deny"
}

// RuleKey is the exact identity of a rule: kind + sha256 digest of the
// canonical identity string.
type RuleKey struct {
	Kind      Kind
	Digest    [32]byte
	Canonical string
}

// NewKey builds a RuleKey from a kind and canonical identity string,
// returning ok=false when the identity is empty or exceeds the bound.
func NewKey(kind Kind, canonical string) (RuleKey, bool) {
	if len(canonical) == 0 || len(canonical) > maxIdentityBytes {
		return RuleKey{}, false
	}
	var digest [32]byte
	h := sha256.Sum256([]byte(canonical))
	copy(digest[:], h[:])
	return RuleKey{Kind: kind, Digest: digest, Canonical: canonical}, true
}

// Equal reports whether two keys reference the same rule.
func (k RuleKey) Equal(o RuleKey) bool {
	return k.Kind == o.Kind && k.Digest == o.Digest
}

// Rule is a single exact permission rule with a stable id and generation.
type Rule struct {
	ID              uint64
	Key             RuleKey
	DisplayIdentity string
	Decision        Decision
	Generation      uint64
}

// RuleSnap is a copyable view of a rule.
type RuleSnap struct {
	ID              uint64
	Key             RuleKey
	DisplayIdentity string
	Decision        Decision
	Generation      uint64
}

// State is an immutable snapshot of the session's exact rules.
type State struct {
	NextGeneration uint64
	Rules          []Rule
}

// NewState returns an empty, valid state.
func NewState() State {
	return State{NextGeneration: 1}
}

// SetEvent requests an upsert of an exact rule.
type SetEvent struct {
	Key                RuleKey
	DisplayIdentity    string
	Decision           Decision
	ExpectedGeneration *uint64 // nil = must not already exist
}

// RevokeEvent requests removal of the rule with the given stable id.
type RevokeEvent struct {
	ID                 uint64
	ExpectedGeneration uint64
}

// Status is the outcome of applying an event.
type Status int

const (
	Applied Status = iota
	Stale
	Full
	Invalid
)

func (s Status) String() string {
	switch s {
	case Applied:
		return "applied"
	case Stale:
		return "stale"
	case Full:
		return "full"
	case Invalid:
		return "invalid"
	}
	return "unknown"
}

// Validate checks all fx state invariants.
func Validate(state State) error {
	if state.NextGeneration == 0 || len(state.Rules) > maxRules {
		return fmt.Errorf("stableid: invalid generation or rule count")
	}
	seenID := map[uint64]bool{}
	seenKey := map[RuleKey]bool{}
	for _, r := range state.Rules {
		if r.ID == 0 || r.Generation == 0 ||
			r.ID > r.Generation || r.ID >= state.NextGeneration ||
			r.Generation >= state.NextGeneration ||
			len(r.Key.Canonical) == 0 || len(r.Key.Canonical) > maxIdentityBytes ||
			len(r.DisplayIdentity) == 0 {
			return fmt.Errorf("stableid: invalid rule %+v", r)
		}
		k := r.Key
		var digest [32]byte
		h := sha256.Sum256([]byte(k.Canonical))
		copy(digest[:], h[:])
		if k.Digest != digest {
			return fmt.Errorf("stableid: digest mismatch for rule %d", r.ID)
		}
		if seenID[r.ID] {
			return fmt.Errorf("stableid: duplicate rule id %d", r.ID)
		}
		if seenKey[k] {
			return fmt.Errorf("stableid: duplicate rule key")
		}
		seenID[r.ID] = true
		seenKey[k] = true
	}
	return nil
}

// cloneState copies the rules slice so returned states are independent.
func cloneState(state State) State {
	cp := make([]Rule, len(state.Rules))
	copy(cp, state.Rules)
	state.Rules = cp
	return state
}

// keyIndex returns the index of the rule matching key, or -1.
func keyIndex(state State, key RuleKey) int {
	for i := range state.Rules {
		if state.Rules[i].Key.Equal(key) {
			return i
		}
	}
	return -1
}

// ApplySet upserts an exact rule and returns the new state and status.
func ApplySet(state State, ev SetEvent) (State, Status) {
	if state.NextGeneration == 0 ||
		len(ev.DisplayIdentity) == 0 ||
		len(ev.Key.Canonical) == 0 || len(ev.Key.Canonical) > maxIdentityBytes {
		return state, Invalid
	}
	nextGen := state.NextGeneration + 1
	if nextGen == 0 { // u64 overflow — matching fx's invalid path
		return state, Invalid
	}
	if idx := keyIndex(state, ev.Key); idx >= 0 {
		if ev.ExpectedGeneration == nil || *ev.ExpectedGeneration != state.Rules[idx].Generation {
			return state, Stale
		}
		next := cloneState(state)
		next.Rules[idx].DisplayIdentity = ev.DisplayIdentity
		next.Rules[idx].Decision = ev.Decision
		next.Rules[idx].Generation = state.NextGeneration
		next.NextGeneration = nextGen
		return next, Applied
	}
	if ev.ExpectedGeneration != nil {
		return state, Stale
	}
	if len(state.Rules) >= maxRules {
		return state, Full
	}
	next := cloneState(state)
	next.Rules = append(next.Rules, Rule{
		ID:              state.NextGeneration,
		Key:             ev.Key,
		DisplayIdentity: ev.DisplayIdentity,
		Decision:        ev.Decision,
		Generation:      state.NextGeneration,
	})
	next.NextGeneration = nextGen
	return next, Applied
}

// ApplyRevoke removes the rule with the given stable id and returns the new
// state and status.
func ApplyRevoke(state State, ev RevokeEvent) (State, Status) {
	idx := -1
	for i := range state.Rules {
		if state.Rules[i].ID == ev.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return state, Stale
	}
	if state.Rules[idx].Generation != ev.ExpectedGeneration {
		return state, Stale
	}
	nextGen := state.NextGeneration + 1
	if nextGen == 0 {
		return state, Invalid
	}
	next := cloneState(state)
	next.Rules = append(next.Rules[:idx], next.Rules[idx+1:]...)
	next.NextGeneration = nextGen
	return next, Applied
}

// RuleForID returns the live rule with the given stable id.
func RuleForID(state State, id uint64) (Rule, bool) {
	for _, r := range state.Rules {
		if r.ID == id {
			return r, true
		}
	}
	return Rule{}, false
}

// RuleForKey returns the live rule keyed by the exact identity.
func RuleForKey(state State, key RuleKey) (Rule, bool) {
	idx := keyIndex(state, key)
	if idx < 0 {
		return Rule{}, false
	}
	return state.Rules[idx], true
}

// Decide resolves the decision for an exact key. ok=false when no exact rule
// exists (fx's unresolved outcome) — never conflated with an explicit deny.
func Decide(state State, key RuleKey) (Decision, bool) {
	idx := keyIndex(state, key)
	if idx < 0 {
		return Deny, false
	}
	return state.Rules[idx].Decision, true
}

// Sorted returns a copy of the rules ordered by stable id, for stable listing.
func Sorted(state State) []RuleSnap {
	out := make([]RuleSnap, 0, len(state.Rules))
	for _, r := range state.Rules {
		out = append(out, RuleSnap(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
