package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdditionalDirContextLoadsInstructions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("extra instructions"), 0o644); err != nil {
		t.Fatal(err)
	}

	abs, block, err := AdditionalDirContext(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(abs) || !strings.Contains(block, "Additional directory: "+abs) || !strings.Contains(block, "extra instructions") {
		t.Fatalf("unexpected workspace context: path=%q block=%q", abs, block)
	}
}

func TestAdditionalDirContextRejectsFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := AdditionalDirContext(file); err == nil {
		t.Fatal("expected file path to be rejected")
	}
}
