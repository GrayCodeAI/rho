package fingerprint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScan_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	fp, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan(%q): %v", dir, err)
	}

	if fp.Language != "" {
		t.Errorf("expected empty language, got %q", fp.Language)
	}
	if len(fp.Languages) != 0 {
		t.Errorf("expected no languages, got %d", len(fp.Languages))
	}
	if fp.ProjectSize != "tiny" {
		t.Errorf("expected 'tiny' project size, got %q", fp.ProjectSize)
	}
	if fp.Framework != "" {
		t.Errorf("expected empty framework, got %q", fp.Framework)
	}
	if fp.Docker {
		t.Error("expected Docker=false for empty dir")
	}
	if fp.Monorepo {
		t.Error("expected Monorepo=false for empty dir")
	}
}

func TestScan_InvalidPath(t *testing.T) {
	_, err := Scan("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestGenerateRecommendations_Go(t *testing.T) {
	fp := &ProjectFingerprint{
		Language:      "Go",
		TestFramework: "go test",
		Framework:     "chi",
		LintTools:     nil,
		CI:            "github-actions",
		ProjectSize:   "medium",
	}

	recs := generateRecommendations(fp)

	if len(recs) == 0 {
		t.Fatal("expected at least one recommendation")
	}

	// Should recommend test command.
	foundTestRec := false
	for _, r := range recs {
		if strings.Contains(r, "go test") {
			foundTestRec = true
		}
	}
	if !foundTestRec {
		t.Error("expected recommendation about 'go test ./...'")
	}

	// Should recommend golangci-lint.
	foundLintRec := false
	for _, r := range recs {
		if strings.Contains(r, "golangci") {
			foundLintRec = true
		}
	}
	if !foundLintRec {
		t.Error("expected recommendation about golangci-lint")
	}

	// Should mention chi.
	foundChiRec := false
	for _, r := range recs {
		if strings.Contains(r, "chi") {
			foundChiRec = true
		}
	}
	if !foundChiRec {
		t.Error("expected recommendation mentioning chi router")
	}
}

func TestGenerateRecommendations_NoCI(t *testing.T) {
	fp := &ProjectFingerprint{
		Language:    "Go",
		CI:          "",
		ProjectSize: "small",
	}

	recs := generateRecommendations(fp)

	foundCIRec := false
	for _, r := range recs {
		if strings.Contains(r, "CI") || strings.Contains(r, "GitHub Actions") {
			foundCIRec = true
		}
	}
	if !foundCIRec {
		t.Error("expected CI recommendation when no CI is detected")
	}
}

func TestGenerateRecommendations_JS(t *testing.T) {
	fp := &ProjectFingerprint{
		Language:      "JavaScript",
		TestFramework: "jest",
		Framework:     "next.js",
		LintTools:     []string{"eslint"},
		CI:            "github-actions",
		Docker:        true,
		ProjectSize:   "medium",
	}

	recs := generateRecommendations(fp)

	// Should recommend jest test command.
	foundTestRec := false
	for _, r := range recs {
		if strings.Contains(r, "jest") || strings.Contains(r, "npm test") {
			foundTestRec = true
		}
	}
	if !foundTestRec {
		t.Error("expected recommendation about jest/npm test")
	}

	// Should mention Next.js.
	foundNextRec := false
	for _, r := range recs {
		if strings.Contains(r, "Next.js") {
			foundNextRec = true
		}
	}
	if !foundNextRec {
		t.Error("expected recommendation mentioning Next.js")
	}
}

func TestGenerateRecommendations_Python(t *testing.T) {
	fp := &ProjectFingerprint{
		Language:      "Python",
		TestFramework: "pytest",
		LintTools:     nil,
		CI:            "github-actions",
		ProjectSize:   "medium",
	}

	recs := generateRecommendations(fp)

	// Should recommend pytest command.
	foundTestRec := false
	for _, r := range recs {
		if strings.Contains(r, "pytest") {
			foundTestRec = true
		}
	}
	if !foundTestRec {
		t.Error("expected recommendation about pytest")
	}

	// Should recommend linting tool.
	foundLintRec := false
	for _, r := range recs {
		if strings.Contains(r, "ruff") || strings.Contains(r, "flake8") {
			foundLintRec = true
		}
	}
	if !foundLintRec {
		t.Error("expected recommendation about Python linting")
	}
}

func TestGenerateRecommendations_Monorepo(t *testing.T) {
	fp := &ProjectFingerprint{
		Language:    "TypeScript",
		Monorepo:    true,
		CI:          "github-actions",
		ProjectSize: "large",
		Docker:      true,
	}

	recs := generateRecommendations(fp)

	foundMonoRec := false
	for _, r := range recs {
		if strings.Contains(r, "Monorepo") || strings.Contains(r, "monorepo") {
			foundMonoRec = true
		}
	}
	if !foundMonoRec {
		t.Error("expected monorepo recommendation")
	}
}

func TestFormatSummary_Complete(t *testing.T) {
	fp := &ProjectFingerprint{
		Language: "Go",
		Languages: []ProjectLangInfo{
			{Name: "Go", FileCount: 140, Percentage: 97.0},
			{Name: "Shell", FileCount: 4, Percentage: 3.0},
		},
		Framework:      "chi",
		BuildSystem:    "go modules",
		TestFramework:  "go test",
		LintTools:      []string{"golangci-lint"},
		PackageManager: "go modules",
		CI:             "github-actions",
		Docker:         true,
		Monorepo:       false,
		ProjectSize:    "medium",
		Conventions: []Convention{
			{Name: "indentation", Description: "Tabs for indentation", Confidence: 1.0},
			{Name: "error-handling", Description: "Error wrapping with %w", Confidence: 0.85},
		},
		Recommendations: []string{
			"Add `go test ./...` as your test command",
			"Your project uses chi router — rho can help with middleware patterns",
		},
	}

	summary := FormatSummary(fp)

	// Check that key information is present.
	checks := []string{
		"Go (97%)",
		"Shell (3%)",
		"chi",
		"go modules",
		"go test",
		"GitHub Actions",
		"medium",
		"Docker: yes",
		"golangci-lint",
		"Tabs for indentation",
		"100% confidence",
		"Error wrapping with %w",
		"85% confidence",
		"go test ./...",
		"chi router",
	}

	for _, check := range checks {
		if !strings.Contains(summary, check) {
			t.Errorf("FormatSummary missing %q\nGot:\n%s", check, summary)
		}
	}

	t.Logf("FormatSummary output:\n%s", summary)
}

func TestFormatSummary_Minimal(t *testing.T) {
	fp := &ProjectFingerprint{
		Language: "Python",
		Languages: []ProjectLangInfo{
			{Name: "Python", FileCount: 5, Percentage: 100.0},
		},
		ProjectSize: "tiny",
	}

	summary := FormatSummary(fp)

	if !strings.Contains(summary, "Python (100%)") {
		t.Errorf("expected 'Python (100%%)', got:\n%s", summary)
	}
	if !strings.Contains(summary, "tiny") {
		t.Errorf("expected 'tiny' size, got:\n%s", summary)
	}
	// Should not contain sections that are empty.
	if strings.Contains(summary, "Framework:") {
		t.Error("should not show Framework when empty")
	}
	if strings.Contains(summary, "Docker:") {
		t.Error("should not show Docker when false")
	}
	if strings.Contains(summary, "Monorepo:") {
		t.Error("should not show Monorepo when false")
	}
}

func TestFormatSummary_MultipleLanguages(t *testing.T) {
	fp := &ProjectFingerprint{
		Language: "TypeScript",
		Languages: []ProjectLangInfo{
			{Name: "TypeScript", FileCount: 50, Percentage: 60.0},
			{Name: "JavaScript", FileCount: 20, Percentage: 24.0},
			{Name: "CSS", FileCount: 10, Percentage: 12.0},
			{Name: "HTML", FileCount: 3, Percentage: 4.0},
		},
		Framework:   "next.js",
		BuildSystem: "npm",
		ProjectSize: "small",
	}

	summary := FormatSummary(fp)

	if !strings.Contains(summary, "TypeScript (60%)") {
		t.Errorf("expected TypeScript (60%%), got:\n%s", summary)
	}
	if !strings.Contains(summary, "JavaScript (24%)") {
		t.Errorf("expected JavaScript (24%%), got:\n%s", summary)
	}
}

func TestProjectSize_Classification(t *testing.T) {
	tests := []struct {
		files    int
		expected string
	}{
		{0, "tiny"},
		{5, "tiny"},
		{9, "tiny"},
		{10, "small"},
		{50, "small"},
		{99, "small"},
		{100, "medium"},
		{500, "medium"},
		{1000, "medium"},
		{1001, "large"},
		{5000, "large"},
	}

	for _, tt := range tests {
		result := classifyProjectSize(tt.files)
		if result != tt.expected {
			t.Errorf("classifyProjectSize(%d) = %q, want %q", tt.files, result, tt.expected)
		}
	}
}

func TestDetectLintTools(t *testing.T) {
	t.Run("golangci-lint", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, ".golangci.yml"), "linters:\n  enable:\n    - errcheck\n")

		tools := detectLintTools(dir)
		if !containsString(tools, "golangci-lint") {
			t.Errorf("expected golangci-lint, got %v", tools)
		}
	})

	t.Run("eslint config", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, ".eslintrc.json"), `{"extends": "eslint:recommended"}`)

		tools := detectLintTools(dir)
		if !containsString(tools, "eslint") {
			t.Errorf("expected eslint, got %v", tools)
		}
	})

	t.Run("prettier", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, ".prettierrc"), `{"semi": true}`)

		tools := detectLintTools(dir)
		if !containsString(tools, "prettier") {
			t.Errorf("expected prettier, got %v", tools)
		}
	})

	t.Run("multiple tools", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, ".eslintrc.json"), `{}`)
		writeTestFile(t, filepath.Join(dir, ".prettierrc"), `{}`)
		writeTestFile(t, filepath.Join(dir, ".editorconfig"), "root = true\n")

		tools := detectLintTools(dir)
		if len(tools) < 3 {
			t.Errorf("expected at least 3 tools, got %v", tools)
		}
	})

	t.Run("no lint tools", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "main.go"), "package main\n")

		tools := detectLintTools(dir)
		if len(tools) != 0 {
			t.Errorf("expected no lint tools, got %v", tools)
		}
	})
}

func TestDetectPackageManager(t *testing.T) {
	t.Run("pnpm", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "pnpm-lock.yaml"), "lockfileVersion: 5.4\n")
		writeTestFile(t, filepath.Join(dir, "package.json"), `{"name": "app"}`)

		result := detectProjectPackageManager(dir)
		if result != "pnpm" {
			t.Errorf("expected 'pnpm', got %q", result)
		}
	})

	t.Run("yarn", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "yarn.lock"), "# yarn lock\n")
		writeTestFile(t, filepath.Join(dir, "package.json"), `{"name": "app"}`)

		result := detectProjectPackageManager(dir)
		if result != "yarn" {
			t.Errorf("expected 'yarn', got %q", result)
		}
	})

	t.Run("go modules", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "go.sum"), "github.com/foo/bar v1.0.0 h1:abc=\n")

		result := detectProjectPackageManager(dir)
		if result != "go modules" {
			t.Errorf("expected 'go modules', got %q", result)
		}
	})
}

func TestScan_FullGoProject(t *testing.T) {
	dir := t.TempDir()

	// Set up a realistic Go project.
	writeTestFile(t, filepath.Join(dir, "go.mod"), `module example.com/myapp

go 1.21

require (
	github.com/go-chi/chi/v5 v5.0.10
	github.com/stretchr/testify v1.8.0
)
`)
	writeTestFile(t, filepath.Join(dir, "go.sum"), "github.com/go-chi/chi/v5 v5.0.10 h1:abc=\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n")
	writeTestFile(t, filepath.Join(dir, "handler.go"), "package main\n\nimport \"net/http\"\n\nfunc handler(w http.ResponseWriter, r *http.Request) {}\n")
	writeTestFile(t, filepath.Join(dir, "main_test.go"), "package main\n\nimport \"testing\"\n\nfunc TestMain(t *testing.T) {}\n")

	os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755)
	writeTestFile(t, filepath.Join(dir, ".github", "workflows", "ci.yml"), "name: CI\non: push\n")
	writeTestFile(t, filepath.Join(dir, "Dockerfile"), "FROM golang:1.21\nCOPY . .\nRUN go build .\n")
	writeTestFile(t, filepath.Join(dir, ".golangci.yml"), "linters:\n  enable:\n    - errcheck\n")

	fp, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if fp.Language != "Go" {
		t.Errorf("expected language 'Go', got %q", fp.Language)
	}
	if fp.Framework != "chi" {
		t.Errorf("expected framework 'chi', got %q", fp.Framework)
	}
	if fp.BuildSystem != "go modules" {
		t.Errorf("expected build system 'go modules', got %q", fp.BuildSystem)
	}
	if fp.TestFramework != "go test + testify" {
		t.Errorf("expected test framework 'go test + testify', got %q", fp.TestFramework)
	}
	if fp.CI != "github-actions" {
		t.Errorf("expected CI 'github-actions', got %q", fp.CI)
	}
	if !fp.Docker {
		t.Error("expected Docker=true")
	}
	if fp.Monorepo {
		t.Error("expected Monorepo=false")
	}
	if !containsString(fp.LintTools, "golangci-lint") {
		t.Errorf("expected golangci-lint in LintTools, got %v", fp.LintTools)
	}
	if fp.ProjectSize != "tiny" {
		t.Errorf("expected 'tiny' project size (few files), got %q", fp.ProjectSize)
	}
	if len(fp.Recommendations) == 0 {
		t.Error("expected at least one recommendation")
	}

	// Check FormatSummary doesn't panic.
	summary := FormatSummary(fp)
	if summary == "" {
		t.Error("FormatSummary returned empty string")
	}
	t.Logf("Full project summary:\n%s", summary)
}

func TestScan_FullJSProject(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, filepath.Join(dir, "package.json"), `{
  "name": "my-nextjs-app",
  "dependencies": {
    "next": "^13.0.0",
    "react": "^18.0.0",
    "react-dom": "^18.0.0"
  },
  "devDependencies": {
    "jest": "^29.0.0",
    "eslint": "^8.0.0",
    "@types/react": "^18.0.0"
  }
}`)
	writeTestFile(t, filepath.Join(dir, "package-lock.json"), `{"lockfileVersion": 3}`)
	writeTestFile(t, filepath.Join(dir, ".eslintrc.json"), `{"extends": "next/core-web-vitals"}`)

	os.MkdirAll(filepath.Join(dir, "pages"), 0o755)
	writeTestFile(t, filepath.Join(dir, "pages", "index.tsx"), "export default function Home() { return <div>Hello</div>; }\n")
	writeTestFile(t, filepath.Join(dir, "pages", "about.tsx"), "export default function About() { return <div>About</div>; }\n")
	writeTestFile(t, filepath.Join(dir, "pages", "contact.tsx"), "export default function Contact() { return <div>Contact</div>; }\n")
	writeTestFile(t, filepath.Join(dir, "pages", "blog.tsx"), "export default function Blog() { return <div>Blog</div>; }\n")
	writeTestFile(t, filepath.Join(dir, "lib", "utils.ts"), "export function cn(...args: string[]) { return args.join(' '); }\n")

	os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755)
	writeTestFile(t, filepath.Join(dir, ".github", "workflows", "ci.yml"), "name: CI\n")

	fp, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	// Primary language should be TypeScript (from .tsx/.ts files).
	if fp.Language != "TypeScript" {
		t.Errorf("expected language 'TypeScript', got %q", fp.Language)
	}
	if fp.Framework != "next.js" {
		t.Errorf("expected framework 'next.js', got %q", fp.Framework)
	}
	if fp.TestFramework != "jest" {
		t.Errorf("expected test framework 'jest', got %q", fp.TestFramework)
	}
	if fp.CI != "github-actions" {
		t.Errorf("expected CI 'github-actions', got %q", fp.CI)
	}
	if !containsString(fp.LintTools, "eslint") {
		t.Errorf("expected eslint in lint tools, got %v", fp.LintTools)
	}
}

// writeTestFile is a helper for creating test files.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeTestFile2 is a non-testing.T helper used in table-driven test setup functions.
func writeTestFile2(path, content string) {
	dir := filepath.Dir(path)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(path, []byte(content), 0o644)
}
