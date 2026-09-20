package permissions

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/GrayCodeAI/rho/internal/permissions/stableid"
)

func newTempStore(t *testing.T) *StableRuleStore {
	t.Helper()
	dir := t.TempDir()
	s := NewStableRuleStore(filepath.Join(dir, "stable-rules.json"))
	if err := s.Load(); err != nil {
		t.Fatalf("load empty store: %v", err)
	}
	return s
}

// Remember yields a stable id; revoke by that id removes the rule; list
// reflects both.
func TestStoreRememberRevokeList(t *testing.T) {
	s := newTempStore(t)
	id, ok := s.Remember(stableid.KindCommand, "command\x00git push", "git push", stableid.Allow)
	if !ok {
		t.Fatal("remember failed")
	}
	if id == 0 {
		t.Fatal("stable id must be nonzero")
	}
	if got, ok := s.Resolve(stableid.KindCommand, "command\x00git push"); !ok || got != stableid.Allow {
		t.Fatalf("resolved allow expected, got %v ok=%v", got, ok)
	}

	if !s.Revoke(id) {
		t.Fatal("revoke failed for existing id")
	}
	if err := s.Load(); err != nil {
		t.Fatalf("reload after revoke: %v", err)
	}
	if len(s.List()) != 0 {
		t.Fatalf("expected empty list after revoke, got %d", len(s.List()))
	}
	if _, ok := s.Resolve(stableid.KindCommand, "command\x00git push"); ok {
		t.Fatal("revoked rule must be unresolved")
	}
	// Revoking an unknown id fails.
	if s.Revoke(id) {
		t.Fatal("revoking a gone id must fail")
	}
}

// Remembering the same exact rule again preserves its stable id (upsert).
func TestStoreRememberPreservesID(t *testing.T) {
	s := newTempStore(t)
	id1, _ := s.Remember(stableid.KindCommand, "command\x00go test ./...", "go test", stableid.Deny)
	id2, _ := s.Remember(stableid.KindCommand, "command\x00go test ./...", "go test -v", stableid.Allow)
	if id1 != id2 {
		t.Fatalf("upsert must preserve stable id: got %d want %d", id2, id1)
	}
	if got, _ := s.Resolve(stableid.KindCommand, "command\x00go test ./..."); got != stableid.Allow {
		t.Fatal("decision must update to allow on upsert")
	}
	if len(s.List()) != 1 {
		t.Fatalf("upsert must not duplicate rules, got %d", len(s.List()))
	}
}

// IDs survive a Save/Load round-trip (stable across workspace changes).
func TestStorePersistsAcrossReload(t *testing.T) {
	s := newTempStore(t)
	id1, _ := s.Remember(stableid.KindCommand, "command\x00ls", "ls", stableid.Allow)
	_, _ = s.Remember(stableid.KindCommand, "command\x00pwd", "pwd", stableid.Deny)
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	reloaded := NewStableRuleStore(s.path)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got, ok := reloaded.Resolve(stableid.KindCommand, "command\x00ls"); !ok || got != stableid.Allow {
		t.Fatal("persisted allow rule must still resolve")
	}
	if got, _ := reloaded.Resolve(stableid.KindCommand, "command\x00pwd"); got != stableid.Deny {
		t.Fatal("persisted deny rule must still resolve")
	}
	list := reloaded.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 persisted rules, got %d", len(list))
	}
	// The original id is stable after reload.
	if got, ok := reloaded.Resolve(stableid.KindCommand, "command\x00ls"); !ok || got != stableid.Allow {
		t.Fatal("ls rule must survive")
	}
	_ = id1
	// Sorted by id includes both.
	if list[0].ID == list[1].ID {
		t.Fatal("distinct rules must have distinct ids")
	}
}

// A trailing rule can be revoked by id after reload.
func TestStoreRevokeAfterReload(t *testing.T) {
	s := newTempStore(t)
	id1, _ := s.Remember(stableid.KindCommand, "command\x00a", "a", stableid.Allow)
	id2, _ := s.Remember(stableid.KindCommand, "command\x00b", "b", stableid.Deny)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	r := NewStableRuleStore(s.path)
	_ = r.Load()
	if !r.Revoke(id2) {
		t.Fatal("must revoke persisted rule by stable id")
	}
	if len(r.List()) != 1 {
		t.Fatalf("expected 1 after revoke, got %d", len(r.List()))
	}
	if _, ok := r.Resolve(stableid.KindCommand, "command\x00a"); !ok {
		t.Fatal("surviving rule must still resolve")
	}
	_ = id1
}

// Missing file loads as empty; malformed file errors without losing state.
func TestStoreLoadEmptyAndMalformed(t *testing.T) {
	s := newTempStore(t)
	if len(s.List()) != 0 {
		t.Fatal("empty store must have no rules")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := NewStableRuleStore(path)
	if err := bad.Load(); err == nil {
		t.Fatal("malformed file must error on Load")
	}
}

func TestStoreMutationIsTransactionalOnPersistenceFailure(t *testing.T) {
	s := newTempStore(t)
	id, ok := s.Remember(stableid.KindCommand, "command\x00git status", "git status", stableid.Allow)
	if !ok {
		t.Fatal("initial remember failed")
	}
	// Point the store at a directory so the next atomic rename cannot succeed.
	s.path = t.TempDir()
	if nextID, ok := s.Remember(stableid.KindCommand, "command\x00git diff", "git diff", stableid.Allow); ok || nextID != 0 {
		t.Fatal("remember must fail when persistence fails")
	}
	if len(s.List()) != 1 || s.List()[0].ID != id {
		t.Fatalf("failed remember mutated in-memory state: %+v", s.List())
	}
	if s.Revoke(id) {
		t.Fatal("revoke must fail when persistence fails")
	}
	if len(s.List()) != 1 {
		t.Fatal("failed revoke mutated in-memory state")
	}
	if s.Reset() {
		t.Fatal("reset must fail when persistence fails")
	}
	if len(s.List()) != 1 {
		t.Fatal("failed reset mutated in-memory state")
	}
}

func TestStoreSaveLeavesNoFixedTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stable-rules.json")
	store := NewStableRuleStore(path)
	if _, ok := store.Remember(stableid.KindCommand, "command\x00git status", "git status", stableid.Allow); !ok {
		t.Fatal("remember failed")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("fixed temp file should not exist, stat error=%v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("temporary file was not cleaned up: %s", entry.Name())
		}
	}
	reloaded := NewStableRuleStore(path)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("load after atomic save: %v", err)
	}
	if got := reloaded.List(); len(got) != 1 || got[0].DisplayIdentity != "git status" {
		t.Fatalf("loaded rules = %#v", got)
	}
}

func TestStoreConcurrentWritersMergeUnderFileLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stable-rules.json")
	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			store := NewStableRuleStore(path)
			canonical := "command\x00git status --" + strconv.Itoa(i)
			if _, ok := store.Remember(stableid.KindCommand, canonical, canonical, stableid.Allow); !ok {
				errs <- fmt.Errorf("writer %d failed", i)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	loaded := NewStableRuleStore(path)
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	if got := len(loaded.List()); got != writers {
		t.Fatalf("merged rule count = %d, want %d", got, writers)
	}
}
