package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pathBoundedContext returns a context whose ToolContext restricts writes to the
// supplied allowed directory (plus the CWD "."). With no ToolContext at all,
// validatePathAllowed no-ops — so tests MUST attach one to exercise the guard.
func pathBoundedContext(t *testing.T, allowed string) context.Context {
	t.Helper()
	return WithToolContext(context.Background(), &ToolContext{
		AllowedDirectories: []string{allowed},
	})
}

// outsidePath is an absolute path that is never within a tempdir workspace.
const outsidePath = "/etc/hosts-tool-guard-test"

func TestStructuredEditTool_RejectsPathOutsideAllowedDirectory(t *testing.T) {
	t.Parallel()
	tool := StructuredEditTool{}
	ctx := pathBoundedContext(t, t.TempDir())

	input, _ := json.Marshal(map[string]any{
		"path": outsidePath,
		"blocks": []map[string]any{
			{"search": "x", "replace": "y"},
		},
	})
	_, err := tool.Execute(ctx, input)
	if err == nil {
		t.Fatal("expected StructuredEditTool to reject a path outside the allowed directory")
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Fatalf("expected an out-of-bounds error, got: %v", err)
	}
}

// A path inside the allowed directory must still be accepted by StructuredEdit.
func TestStructuredEditTool_AcceptsPathInsideAllowedDirectory(t *testing.T) {
	t.Parallel()
	tool := StructuredEditTool{}
	allowed := t.TempDir()
	target := filepath.Join(allowed, "file.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := pathBoundedContext(t, allowed)

	input, _ := json.Marshal(map[string]any{
		"path": target,
		"blocks": []map[string]any{
			{"search": "package main", "replace": "package renamed"},
		},
	})
	_, err := tool.Execute(ctx, input)
	if err != nil {
		t.Fatalf("expected in-bounds path to succeed, got: %v", err)
	}
	data, rerr := os.ReadFile(target)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "package renamed") {
		t.Fatalf("expected the edit to apply, file content: %q", string(data))
	}
}

func TestSmartCreateTool_RejectsPathOutsideAllowedDirectory(t *testing.T) {
	t.Parallel()
	tool := &SmartCreateTool{Creator: NewSmartCreator(t.TempDir())}
	ctx := pathBoundedContext(t, t.TempDir())

	input, _ := json.Marshal(map[string]any{"path": outsidePath + ".go"})
	_, err := tool.Execute(ctx, input)
	if err == nil {
		t.Fatal("expected SmartCreateTool to reject a path outside the allowed directory")
	}
}

// A path inside the allowed directory must still be accepted by SmartCreate.
func TestSmartCreateTool_AcceptsPathInsideAllowedDirectory(t *testing.T) {
	t.Parallel()
	tool := &SmartCreateTool{Creator: NewSmartCreator(t.TempDir())}
	allowed := t.TempDir()
	target := filepath.Join(allowed, "new.txt")
	ctx := pathBoundedContext(t, allowed)

	input, _ := json.Marshal(map[string]any{"path": target})
	res, err := tool.Execute(ctx, input)
	if err != nil {
		t.Fatalf("expected in-bounds create to succeed, got: %v", err)
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Fatalf("expected file to be created at %s: %v", target, statErr)
	}
	if !strings.Contains(res, target) {
		t.Fatalf("expected result to mention the new file, got: %q", res)
	}
}

// PatchTool applies to files named inside the patch body, so a patch that
// targets an out-of-workspace file must be rejected before any write happens.
func TestPatchTool_RejectsPatchTargetingPathOutsideAllowedDirectory(t *testing.T) {
	t.Parallel()
	tool := PatchTool{}
	allowed := t.TempDir()
	// Seed the in-bounds reference file so the "update" path's contents resolve;
	// the guard must still reject it on path grounds before reading.
	if err := os.WriteFile(filepath.Join(allowed, "ok.txt"), []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := pathBoundedContext(t, allowed)

	patch := "*** Begin Patch\n*** Update File: " + outsidePath + "\n@@@ @@@\n-removed\n+added\n*** End Patch\n"
	input, _ := json.Marshal(map[string]any{"patch": patch})
	_, err := tool.Execute(ctx, input)
	if err == nil {
		t.Fatal("expected PatchTool to reject a patch targeting a path outside the allowed directory")
	}
}

// A patch targeting an in-bounds file must still apply.
func TestPatchTool_AcceptsPatchTargetingPathInsideAllowedDirectory(t *testing.T) {
	t.Parallel()
	tool := PatchTool{}
	allowed := t.TempDir()
	target := filepath.Join(allowed, "a.txt")
	if err := os.WriteFile(target, []byte("func main() {\n\toriginal\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := pathBoundedContext(t, allowed)

	patch := "*** Begin Patch\n" +
		"*** Update File: " + target + "\n" +
		"@@@ func main() {@@@\n" +
		"- original\n" +
		"+ newline\n" +
		"*** End Patch"
	input, _ := json.Marshal(map[string]any{"patch": patch})
	res, err := tool.Execute(ctx, input)
	if err != nil {
		t.Fatalf("expected in-bounds patch to succeed, got: %v", err)
	}
	if !strings.Contains(res, "patched") && !strings.Contains(res, "file") {
		t.Fatalf("expected a successful patch result, got: %q", res)
	}
}
