package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A model-facing path check with no ToolContext attached must fail closed
// rather than silently skipping the allowed-directory policy.
func TestValidatePathAllowedFailsClosedWithoutToolContext(t *testing.T) {
	if err := validatePathAllowed(context.Background(), "somefile.txt"); err == nil {
		t.Fatal("validatePathAllowed must fail closed when no ToolContext is attached")
	}
}

func TestGuardedRootPathRejectsSymlinkEscape(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(allowed, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	path := filepath.Join(link, "secret.txt")
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := pathBoundedContext(t, allowed)
	if _, err := readGuardedFile(ctx, path); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("read through symlink = %v, want an out-of-bounds error", err)
	}
}

func TestGuardedRootPathAllowsRegularFile(t *testing.T) {
	allowed := t.TempDir()
	path := filepath.Join(allowed, "file.txt")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := pathBoundedContext(t, allowed)
	if err := writeGuardedFile(ctx, path, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := readPinnedFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "after" {
		t.Fatalf("content = %q, want after", data)
	}
}
