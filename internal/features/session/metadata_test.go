package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenameInMovesSessionFile(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.jsonl")
	if err := os.WriteFile(oldPath, []byte("session"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RenameIn(dir, "old", "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.jsonl")); err != nil {
		t.Fatalf("renamed session missing: %v", err)
	}
}

func TestRenameInRejectsInvalidID(t *testing.T) {
	if err := RenameIn(t.TempDir(), "old", "../escape"); err == nil {
		t.Fatal("expected invalid session name")
	}
}

func TestAddTagInAppendsLabels(t *testing.T) {
	dir := t.TempDir()
	if err := AddTagIn(dir, "session", "important"); err != nil {
		t.Fatal(err)
	}
	if err := AddTagIn(dir, "session", "follow-up"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "session.tags"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "important\nfollow-up\n" {
		t.Fatalf("tags = %q", data)
	}
}
