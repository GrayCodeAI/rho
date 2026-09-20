package stableid

import (
	"testing"
)

func mustKey(t *testing.T, kind Kind, canonical string) RuleKey {
	t.Helper()
	k, ok := NewKey(kind, canonical)
	if !ok {
		t.Fatalf("NewKey(%d,%q) rejected", kind, canonical)
	}
	return k
}

func TestCanonicalIdentityNamespacesKinds(t *testing.T) {
	command := CanonicalIdentity(KindCommand, "README.md")
	file := CanonicalIdentity(KindFileMutation, "README.md")
	if command == file {
		t.Fatalf("canonical identities collided: %q", command)
	}
	if command != "command\x00README.md" || file != "file_mutation\x00README.md" {
		t.Fatalf("unexpected canonical identities: %q and %q", command, file)
	}
}

// fx tests: set inserts a stable nonzero id.
func TestSetCreatesStableNonzeroID(t *testing.T) {
	original := NewState()
	key := mustKey(t, KindCommand, "command\x00git status")
	next, status := ApplySet(original, SetEvent{
		Key: key, DisplayIdentity: "git status in /workspace", Decision: Deny,
	})
	if status != Applied {
		t.Fatalf("expected applied, got %s", status)
	}
	if len(original.Rules) != 0 {
		t.Fatal("original state must remain unchanged")
	}
	if len(next.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(next.Rules))
	}
	r := next.Rules[0]
	if r.ID == 0 {
		t.Fatal("rule id must be nonzero")
	}
	if got, ok := RuleForID(next, r.ID); !ok || got.ID != r.ID {
		t.Fatal("ruleForId must return the inserted rule")
	}
}

// fx tests: replacing a rule preserves its id, changes decision/generation.
func TestSetReplacementPreservesID(t *testing.T) {
	state := NewState()
	key := mustKey(t, KindCommand, "command\x00git push")
	state, st := ApplySet(state, SetEvent{Key: key, DisplayIdentity: "push", Decision: Deny})
	if st != Applied {
		t.Fatalf("first set: %s", st)
	}
	origID := state.Rules[0].ID

	gen := state.Rules[0].Generation
	state, st = ApplySet(state, SetEvent{
		Key: key, DisplayIdentity: "push now", Decision: Allow,
		ExpectedGeneration: &gen,
	})
	if st != Applied {
		t.Fatalf("replacement: %s", st)
	}
	if len(state.Rules) != 1 {
		t.Fatalf("replacement must not add a second rule, got %d", len(state.Rules))
	}
	if state.Rules[0].ID != origID {
		t.Fatalf("replacement must preserve id: got %d want %d", state.Rules[0].ID, origID)
	}
	if state.Rules[0].Decision != Allow {
		t.Fatalf("decision must update to allow")
	}
	if state.Rules[0].Generation <= gen {
		t.Fatal("generation must advance on replacement")
	}
}

// Stale set: expected_generation mismatch on an existing key is stale.
func TestSetStaleOnGenerationMismatch(t *testing.T) {
	state := NewState()
	key := mustKey(t, KindCommand, "command\x00go test")
	state, _ = ApplySet(state, SetEvent{Key: key, DisplayIdentity: "t", Decision: Deny})

	wrong := state.Rules[0].Generation + 1
	if _, st := ApplySet(state, SetEvent{Key: key, DisplayIdentity: "x", Decision: Allow, ExpectedGeneration: &wrong}); st != Stale {
		t.Fatalf("wrong generation must be stale, got %s", st)
	}
	// A new rule with expected_generation set (must-not-exist) is stale if it exists.
	if _, st := ApplySet(state, SetEvent{Key: key, DisplayIdentity: "y", Decision: Deny}); st != Stale {
		t.Fatalf("insert of existing key without expected must be stale, got %s", st)
	}
}

// Nonzero and monotonic: successive inserts get distinct ascending ids.
func TestIDsAreMonotonicDistinct(t *testing.T) {
	state := NewState()
	ids := []uint64{}
	for i := 0; i < 5; i++ {
		k := mustKey(t, KindCommand, "command\x00cmd"+string(rune('a'+i)))
		var st Status
		state, st = ApplySet(state, SetEvent{Key: k, DisplayIdentity: "d", Decision: Deny})
		if st != Applied {
			t.Fatalf("insert %d: %s", i, st)
		}
		ids = append(ids, state.Rules[len(state.Rules)-1].ID)
	}
	seen := map[uint64]bool{}
	prev := uint64(0)
	for _, id := range ids {
		if seen[id] || id <= prev {
			t.Fatalf("ids must be distinct and ascending: %v", ids)
		}
		seen[id] = true
		prev = id
	}
}

// Revoke by id with matching generation removes exactly that rule.
func TestRevokeByStableID(t *testing.T) {
	state := NewState()
	for i := 0; i < 3; i++ {
		k := mustKey(t, KindCommand, "command\x00c"+string(rune('a'+i)))
		state, _ = ApplySet(state, SetEvent{Key: k, DisplayIdentity: "d", Decision: Deny})
	}
	target := state.Rules[1]
	gen := target.Generation
	state, st := ApplyRevoke(state, RevokeEvent{ID: target.ID, ExpectedGeneration: gen})
	if st != Applied {
		t.Fatalf("revoke: %s", st)
	}
	if _, ok := RuleForID(state, target.ID); ok {
		t.Fatal("revoked rule must be gone")
	}
	if len(state.Rules) != 2 {
		t.Fatalf("expected 2 rules after revoke, got %d", len(state.Rules))
	}
	// Other rules survive with their ids intact.
	for _, r := range state.Rules {
		if r.ID == target.ID {
			t.Fatal("target id must not remain")
		}
		if _, ok := RuleForID(state, r.ID); !ok {
			t.Fatalf("survivor rule %d must still resolve", r.ID)
		}
	}
}

// Revoke without a matching generation is stale (lost-update protection).
func TestRevokeStaleOnGenerationMismatch(t *testing.T) {
	state := NewState()
	k := mustKey(t, KindCommand, "command\x00x")
	state, _ = ApplySet(state, SetEvent{Key: k, DisplayIdentity: "d", Decision: Deny})
	id := state.Rules[0].ID
	if _, st := ApplyRevoke(state, RevokeEvent{ID: id, ExpectedGeneration: 999}); st != Stale {
		t.Fatalf("mismatched generation must be stale, got %s", st)
	}
	if _, st := ApplyRevoke(state, RevokeEvent{ID: 424242, ExpectedGeneration: 1}); st != Stale {
		t.Fatalf("unknown id must be stale, got %s", st)
	}
}

// Decide resolves allow/deny for an exact key and reports unresolved otherwise.
func TestDecideResolvesExactKey(t *testing.T) {
	state := NewState()
	k := mustKey(t, KindCommand, "command\x00rm -rf /")
	state, _ = ApplySet(state, SetEvent{Key: k, DisplayIdentity: "danger", Decision: Deny})

	if d, ok := Decide(state, k); !ok || d != Deny {
		t.Fatalf("expected resolved deny, got %v ok=%v", d, ok)
	}
	other := mustKey(t, KindCommand, "command\x00ls")
	if _, ok := Decide(state, other); ok {
		t.Fatal("absent key must be unresolved, not a deny")
	}
}

// Validate rejects invariant violations: duplicate id, digest mismatch.
func TestValidateInvariants(t *testing.T) {
	state := NewState()
	k := mustKey(t, KindCommand, "command\x00v")
	state, _ = ApplySet(state, SetEvent{Key: k, DisplayIdentity: "d", Decision: Allow})
	if err := Validate(state); err != nil {
		t.Fatalf("valid state must pass: %v", err)
	}

	// Duplicate id.
	dup := cloneState(state)
	dup.Rules[0].ID = 7
	dup.Rules = append(dup.Rules, Rule{ID: 7, Key: mustKey(t, KindCommand, "command\x00other"), DisplayIdentity: "x", Decision: Deny, Generation: 2})
	dup.NextGeneration = 3
	if err := Validate(dup); err == nil {
		t.Fatal("duplicate id must be invalid")
	}

	// Digest mismatch.
	bad := cloneState(state)
	bad.Rules[0].Key.Digest[0] ^= 0xff
	if err := Validate(bad); err == nil {
		t.Fatal("digest mismatch must be invalid")
	}
}

// Sorted yields rules ordered by stable id.
func TestSortedByStableID(t *testing.T) {
	state := NewState()
	for i := 0; i < 5; i++ {
		k := mustKey(t, KindCommand, "command\x00s"+string(rune('a'+i)))
		state, _ = ApplySet(state, SetEvent{Key: k, DisplayIdentity: "d", Decision: Deny})
	}
	sorted := Sorted(state)
	if len(sorted) != 5 {
		t.Fatalf("expected 5, got %d", len(sorted))
	}
	for i := 1; i < len(sorted); i++ {
		if sorted[i].ID <= sorted[i-1].ID {
			t.Fatalf("not sorted by id: %v", sorted)
		}
	}
}

// Full: exceeding maxRules returns Full.
func TestSetFull(t *testing.T) {
	state := NewState()
	for i := 0; i < maxRules; i++ {
		k := mustKey(t, KindCommand, "command\x00f"+string(rune('a'+i%26))+string(rune('0'+i/26)))
		var st Status
		state, st = ApplySet(state, SetEvent{Key: k, DisplayIdentity: "d", Decision: Deny})
		if st != Applied {
			t.Fatalf("insert %d: %s", i, st)
		}
	}
	k := mustKey(t, KindStructuredTool, "tool\x00overflow")
	if _, st := ApplySet(state, SetEvent{Key: k, DisplayIdentity: "d", Decision: Deny}); st != Full {
		t.Fatalf("over-capacity insert must be full, got %s", st)
	}
}
