package engine

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/GrayCodeAI/rho/internal/permissions"
	"github.com/GrayCodeAI/rho/internal/permissions/stableid"
)

func newExactStore(t *testing.T) *permissions.StableRuleStore {
	t.Helper()
	s := permissions.NewStableRuleStore(filepath.Join(t.TempDir(), "stable-rules.json"))
	if err := s.Load(); err != nil {
		t.Fatalf("load store: %v", err)
	}
	return s
}

// RememberExact persists a stable-id rule and RevokeExact removes it by id.
func TestPermissionServiceExactRememberRevoke(t *testing.T) {
	svc := NewPermissionService(nil)
	store := newExactStore(t)
	svc.SetExactRuleStore(store)

	id, ok := svc.RememberExact(stableid.KindCommand, "command\x00git push", "git push", stableid.Allow)
	if !ok || id == 0 {
		t.Fatalf("remember failed: id=%d ok=%v", id, ok)
	}
	if d, ok := store.Resolve(stableid.KindCommand, "command\x00git push"); !ok || d != stableid.Allow {
		t.Fatalf("expected resolved allow, got %v ok=%v", d, ok)
	}
	if len(svc.ListExact()) != 1 {
		t.Fatalf("expected 1 exact rule, got %d", len(svc.ListExact()))
	}
	if !svc.RevokeExact(id) {
		t.Fatal("revoke by stable id failed")
	}
	if len(svc.ListExact()) != 0 {
		t.Fatal("expected empty list after revoke")
	}
	if svc.RevokeExact(id) {
		t.Fatal("revoking a gone id must fail")
	}
}

// Upserting the same exact rule preserves its stable id.
func TestPermissionServiceExactUpsertPreservesID(t *testing.T) {
	svc := NewPermissionService(nil)
	store := newExactStore(t)
	svc.SetExactRuleStore(store)

	id1, _ := svc.RememberExact(stableid.KindCommand, "command\x00go test", "v1", stableid.Deny)
	id2, _ := svc.RememberExact(stableid.KindCommand, "command\x00go test", "v2", stableid.Allow)
	if id1 != id2 {
		t.Fatalf("upsert must preserve stable id: %d != %d", id1, id2)
	}
	if len(svc.ListExact()) != 1 {
		t.Fatalf("upsert must not duplicate, got %d", len(svc.ListExact()))
	}
	if d, _ := store.Resolve(stableid.KindCommand, "command\x00go test"); d != stableid.Allow {
		t.Fatal("decision must update to allow")
	}
}

// Without a configured store, remember/revoke are no-ops (nil-by-default).
func TestPermissionServiceExactNilByDefault(t *testing.T) {
	svc := NewPermissionService(nil)
	if id, ok := svc.RememberExact(stableid.KindCommand, "command\x00x", "x", stableid.Allow); ok || id != 0 {
		t.Fatalf("remember on nil store must fail, got id=%d ok=%v", id, ok)
	}
	if svc.RevokeExact(1) {
		t.Fatal("revoke on nil store must fail")
	}
	if svc.ListExact() != nil {
		t.Fatal("list on nil store must be nil")
	}
}

func TestPermissionServiceExactConcurrentAccess(t *testing.T) {
	svc := NewPermissionService(nil)
	svc.SetExactRuleStore(newExactStore(t))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			identity := "command\x00git status " + string(rune('a'+i))
			_, _ = svc.RememberExact(stableid.KindCommand, identity, identity, stableid.Allow)
		}(i)
		go func() {
			defer wg.Done()
			_ = svc.PolicyView().ExactRules
			_ = svc.ListExact()
		}()
	}
	wg.Wait()
	if got := len(svc.ListExact()); got != 50 {
		t.Fatalf("concurrent exact rule count = %d, want 50", got)
	}
}
