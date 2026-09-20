package fingerprint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot returns the rho repo root (great-grandparent of internal/features/fingerprint).
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(wd)))
}

func TestGenerate_RhoRepo(t *testing.T) {
	root := repoRoot(t)
	fp, err := Generate(root)
	if err != nil {
		t.Fatalf("Generate(%q): %v", root, err)
	}

	// Should detect Go as a language.
	foundGo := false
	for _, l := range fp.Languages {
		if l.Name == "Go" {
			foundGo = true
			break
		}
	}
	if !foundGo {
		t.Error("expected Go to be detected as a language")
	}

	// Should have a reasonable number of files.
	if fp.TotalFiles < 100 {
		t.Errorf("expected >100 files, got %d", fp.TotalFiles)
	}

	// Should detect go mod.
	if fp.PackageManager != "go mod" {
		t.Errorf("expected package manager 'go mod', got %q", fp.PackageManager)
	}

	// Should have dependencies.
	if fp.Dependencies == 0 {
		t.Error("expected non-zero dependency count")
	}

	// Should have tests.
	if !fp.HasTests {
		t.Error("expected HasTests to be true")
	}

	// Name should be the directory name. The repo may be checked out under a
	// pre-rename directory (e.g. `hawk`) during the Rho migration, so compare
	// against the actual root basename instead of hardcoding the product name.
	if want := filepath.Base(root); fp.Name != want {
		t.Errorf("expected Name=%q, got %q", want, fp.Name)
	}

	t.Logf("Fingerprint: files=%d lines=%d langs=%d deps=%d pm=%s",
		fp.TotalFiles, fp.TotalLines, len(fp.Languages), fp.Dependencies, fp.PackageManager)
}

func TestLanguageDetection(t *testing.T) {
	// Create a temp directory with known files.
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n")
	writeFile(t, filepath.Join(dir, "util.go"), "package main\n\nfunc add(a, b int) int {\n\treturn a + b\n}\n")
	writeFile(t, filepath.Join(dir, "app.js"), "const x = 1;\nconsole.log(x);\n")
	writeFile(t, filepath.Join(dir, "style.css"), "body {\n  color: red;\n}\n")

	fp, err := Generate(dir)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if fp.TotalFiles != 4 {
		t.Errorf("expected 4 files, got %d", fp.TotalFiles)
	}

	// Go should be the top language (10 lines vs 2 + 3).
	if len(fp.Languages) == 0 {
		t.Fatal("expected at least one language")
	}
	if fp.Languages[0].Name != "Go" {
		t.Errorf("expected Go as top language, got %q", fp.Languages[0].Name)
	}

	// Check that all expected languages are present.
	langNames := make(map[string]bool)
	for _, l := range fp.Languages {
		langNames[l.Name] = true
	}
	for _, want := range []string{"Go", "JavaScript", "CSS"} {
		if !langNames[want] {
			t.Errorf("expected language %q to be detected", want)
		}
	}
}

func TestFormat_Concise(t *testing.T) {
	fp := &Fingerprint{
		Name: "test-repo",
		Languages: []LanguageInfo{
			{Name: "Go", Percentage: 80.5, Files: 50, Lines: 10000},
			{Name: "Shell", Percentage: 10.2, Files: 5, Lines: 1200},
			{Name: "YAML", Percentage: 9.3, Files: 8, Lines: 1100},
		},
		TotalFiles:     63,
		TotalLines:     12300,
		Dependencies:   15,
		HasTests:       true,
		HasCI:          true,
		License:        "MIT",
		PackageManager: "go mod",
		GitInfo: &GitInfo{
			Branch:       "main",
			CommitCount:  142,
			LastCommit:   "fix: handle edge case in parser",
			Contributors: 3,
		},
	}

	out := fp.Format()
	t.Logf("Format output:\n%s", out)

	// Should be concise.
	if len(out) > 2000 {
		t.Errorf("Format() output too long: %d chars (want <2000 for ~500 tokens)", len(out))
	}

	// Should contain key info.
	checks := []string{
		"test-repo",
		"Go 80.5%",
		"Files: 63",
		"Lines: 12300",
		"go mod",
		"15 deps",
		"tests",
		"CI",
		"MIT",
		"main",
		"142",
		"fix: handle edge case in parser",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("Format() missing %q", want)
		}
	}
}

func TestFormatMarkdown(t *testing.T) {
	fp := &Fingerprint{
		Name: "my-project",
		Languages: []LanguageInfo{
			{Name: "Python", Percentage: 70.0, Files: 30, Lines: 5000},
			{Name: "YAML", Percentage: 30.0, Files: 10, Lines: 2100},
		},
		TotalFiles:     40,
		TotalLines:     7100,
		Dependencies:   8,
		HasTests:       true,
		HasCI:          false,
		License:        "Apache-2.0",
		PackageManager: "pip",
		GitInfo: &GitInfo{
			Branch:       "develop",
			CommitCount:  50,
			LastCommit:   "add new feature",
			Contributors: 2,
		},
	}

	out := fp.FormatMarkdown()
	t.Logf("Markdown output:\n%s", out)

	// Should be valid markdown with tables.
	if !strings.Contains(out, "# my-project") {
		t.Error("missing markdown title")
	}
	if !strings.Contains(out, "| Files | 40 |") {
		t.Error("missing files row in table")
	}
	if !strings.Contains(out, "| Python |") {
		t.Error("missing Python language row")
	}
	if !strings.Contains(out, "**Branch:** develop") {
		t.Error("missing git branch info")
	}
	if !strings.Contains(out, "## Languages") {
		t.Error("missing Languages section")
	}
	if !strings.Contains(out, "## Git") {
		t.Error("missing Git section")
	}
}

func TestDetectTests_TempDir(t *testing.T) {
	// Dir with test file.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "foo_test.go"), "package foo\n")
	if !detectTests(dir) {
		t.Error("expected detectTests to return true for _test.go file")
	}

	// Dir with test directory.
	dir2 := t.TempDir()
	os.Mkdir(filepath.Join(dir2, "tests"), 0o755)
	if !detectTests(dir2) {
		t.Error("expected detectTests to return true for tests/ directory")
	}

	// Empty dir.
	dir3 := t.TempDir()
	if detectTests(dir3) {
		t.Error("expected detectTests to return false for empty directory")
	}
}

func TestDetectCI(t *testing.T) {
	// With .github/workflows.
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755)
	if !detectCI(dir) {
		t.Error("expected detectCI to detect .github/workflows")
	}

	// Empty dir.
	dir2 := t.TempDir()
	if detectCI(dir2) {
		t.Error("expected detectCI to return false for empty dir")
	}
}

func TestDetectLicense(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "LICENSE"), "MIT License\n\nCopyright (c) 2026\n")
	lic := detectLicense(dir)
	if lic != "MIT" {
		t.Errorf("expected MIT license, got %q", lic)
	}
}

func TestCountGoModDeps(t *testing.T) {
	dir := t.TempDir()
	content := `module example.com/test

go 1.21

require (
	github.com/foo/bar v1.0.0
	github.com/baz/qux v2.0.0
)

require (
	github.com/indirect/one v0.1.0 // indirect
)
`
	path := filepath.Join(dir, "go.mod")
	writeFile(t, path, content)
	count := countGoModDeps(path)
	if count != 3 {
		t.Errorf("expected 3 deps, got %d", count)
	}
}

func TestSkipDirs(t *testing.T) {
	dir := t.TempDir()
	// Create a file in node_modules that should be skipped.
	os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755)
	writeFile(t, filepath.Join(dir, "node_modules", "pkg", "index.js"), "module.exports = {};\n")
	// Create a normal file.
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n")

	fp, err := Generate(dir)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Should only see the main.go file, not the one inside node_modules.
	if fp.TotalFiles != 1 {
		t.Errorf("expected 1 file (skipping node_modules), got %d", fp.TotalFiles)
	}
}

func TestGenerate_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	fp, err := Generate(dir)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if fp.TotalFiles != 0 {
		t.Errorf("expected 0 files, got %d", fp.TotalFiles)
	}
	if fp.TotalLines != 0 {
		t.Errorf("expected 0 lines, got %d", fp.TotalLines)
	}
}

func TestFormat_NoGitInfo(t *testing.T) {
	fp := &Fingerprint{
		Name:       "simple",
		TotalFiles: 5,
		TotalLines: 100,
	}
	out := fp.Format()
	if strings.Contains(out, "Git:") {
		t.Error("should not contain Git: section when GitInfo is nil")
	}
	if !strings.Contains(out, "simple") {
		t.Error("missing repo name")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
