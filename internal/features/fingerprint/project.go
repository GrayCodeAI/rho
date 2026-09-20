package fingerprint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProjectFingerprint holds a comprehensive analysis of a project's type,
// tech stack, conventions, and recommended configuration.
type ProjectFingerprint struct {
	Language        string            // primary language
	Languages       []ProjectLangInfo // all detected languages
	Framework       string            // e.g., "chi", "gin", "express", "django", "next.js"
	BuildSystem     string            // "go modules", "npm", "cargo", "gradle", "maven"
	TestFramework   string            // "go test", "jest", "pytest", "cargo test"
	LintTools       []string
	PackageManager  string
	CI              string // "github-actions", "gitlab-ci", "circleci"
	Docker          bool
	Monorepo        bool
	ProjectSize     string // "tiny" (<10 files), "small", "medium", "large" (>1000)
	Conventions     []Convention
	Recommendations []string

	// Internal tracking fields (unexported).
	totalFiles int
}

// ProjectLangInfo holds detection results for a single language in the project scan.
type ProjectLangInfo struct {
	Name       string
	FileCount  int
	Percentage float64
}

// Convention describes a detected coding convention.
type Convention struct {
	Name        string
	Description string
	Confidence  float64
}

// Scan performs a comprehensive analysis of the project directory, detecting
// languages, frameworks, build systems, CI, conventions, and generating
// recommendations.
func Scan(projectDir string) (*ProjectFingerprint, error) {
	absDir, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, fmt.Errorf("fingerprint: resolve path: %w", err)
	}

	// Check that the directory exists.
	info, err := os.Stat(absDir)
	if err != nil {
		return nil, fmt.Errorf("fingerprint: stat directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("fingerprint: %s is not a directory", absDir)
	}

	fp := &ProjectFingerprint{}

	// Detect languages.
	fp.Languages = detectLanguages(absDir)
	if len(fp.Languages) > 0 {
		fp.Language = fp.Languages[0].Name
	}

	// Calculate total files.
	for _, l := range fp.Languages {
		fp.totalFiles += l.FileCount
	}

	// Detect project size.
	fp.ProjectSize = classifyProjectSize(fp.totalFiles)

	// Detect framework.
	fp.Framework = detectFramework(absDir, fp.Language)

	// Detect build system.
	fp.BuildSystem = detectBuildSystem(absDir)

	// Detect package manager.
	fp.PackageManager = detectProjectPackageManager(absDir)

	// Detect test framework.
	fp.TestFramework = detectTestFramework(absDir, fp.Language)

	// Detect lint tools.
	fp.LintTools = detectLintTools(absDir)

	// Detect CI system.
	fp.CI = detectCISystem(absDir)

	// Detect Docker.
	fp.Docker = detectDocker(absDir)

	// Detect monorepo.
	fp.Monorepo = detectMonorepo(absDir)

	// Detect conventions.
	fp.Conventions = detectConventions(absDir, fp.Language)

	// Generate recommendations.
	fp.Recommendations = generateRecommendations(fp)

	return fp, nil
}

// generateRecommendations produces rho configuration suggestions based on the
// detected project fingerprint.
func generateRecommendations(fp *ProjectFingerprint) []string {
	var recs []string

	// Language-specific recommendations.
	switch fp.Language {
	case "Go":
		if fp.TestFramework == "go test" || fp.TestFramework == "go test + testify" {
			recs = append(recs, "Add `go test ./...` as your test command")
		}
		if !containsString(fp.LintTools, "golangci-lint") {
			recs = append(recs, "Consider adding .golangci.yml for consistent linting")
		}
		if fp.Framework == "chi" {
			recs = append(recs, "Your project uses chi router — rho can help with middleware patterns")
		}
		if fp.Framework == "gin" {
			recs = append(recs, "Your project uses gin — rho can help with handler patterns")
		}
		if fp.Framework == "echo" {
			recs = append(recs, "Your project uses echo — rho can help with middleware and routing")
		}

	case "JavaScript", "TypeScript":
		if fp.TestFramework == "jest" {
			recs = append(recs, "Add `npm test` or `npx jest` as your test command")
		}
		if fp.TestFramework == "vitest" {
			recs = append(recs, "Add `npx vitest` as your test command")
		}
		if !containsString(fp.LintTools, "eslint") && !containsString(fp.LintTools, "biome") {
			recs = append(recs, "Consider adding ESLint or Biome for consistent linting")
		}
		if fp.Framework == "next.js" {
			recs = append(recs, "Your project uses Next.js — rho can help with App Router patterns")
		}

	case "Python":
		if fp.TestFramework == "pytest" {
			recs = append(recs, "Add `pytest` as your test command")
		}
		if !containsString(fp.LintTools, "ruff") && !containsString(fp.LintTools, "flake8") {
			recs = append(recs, "Consider adding ruff or flake8 for Python linting")
		}

	case "Rust":
		if fp.TestFramework == "cargo test" {
			recs = append(recs, "Add `cargo test` as your test command")
		}
		if !containsString(fp.LintTools, "clippy") {
			recs = append(recs, "Consider adding clippy for Rust linting")
		}
	}

	// CI recommendations.
	if fp.CI == "" {
		recs = append(recs, "No CI detected — consider adding GitHub Actions for automated testing")
	}

	// Docker recommendations.
	if !fp.Docker && fp.ProjectSize != "tiny" {
		recs = append(recs, "Consider adding a Dockerfile for reproducible builds")
	}

	// Monorepo recommendations.
	if fp.Monorepo {
		recs = append(recs, "Monorepo detected — configure rho to scope analysis to relevant packages")
	}

	// Convention-based recommendations.
	hasEditorConfig := false
	for _, c := range fp.Conventions {
		if c.Name == "indentation" && c.Confidence < 0.8 {
			recs = append(recs, "Inconsistent indentation detected — consider adding .editorconfig")
		}
	}
	for _, tool := range fp.LintTools {
		if tool == "editorconfig" {
			hasEditorConfig = true
		}
	}
	_ = hasEditorConfig

	return recs
}

// FormatSummary produces a human-readable summary of the project fingerprint.
func FormatSummary(fp *ProjectFingerprint) string {
	var b strings.Builder

	// Project language line.
	if len(fp.Languages) > 0 {
		b.WriteString("Project: ")
		parts := make([]string, 0, len(fp.Languages))
		for _, l := range fp.Languages {
			parts = append(parts, fmt.Sprintf("%s (%.0f%%)", l.Name, l.Percentage))
		}
		// Show top languages (limit to 5).
		limit := len(parts)
		if limit > 5 {
			limit = 5
		}
		if limit == 1 {
			b.WriteString(parts[0])
		} else {
			b.WriteString(parts[0])
			for i := 1; i < limit; i++ {
				b.WriteString(" with some ")
				b.WriteString(parts[i])
			}
		}
		b.WriteByte('\n')
	}

	// Framework.
	if fp.Framework != "" {
		b.WriteString(fmt.Sprintf("Framework: %s\n", fp.Framework))
	}

	// Build system.
	if fp.BuildSystem != "" {
		b.WriteString(fmt.Sprintf("Build: %s\n", fp.BuildSystem))
	}

	// Tests.
	if fp.TestFramework != "" {
		b.WriteString(fmt.Sprintf("Tests: %s\n", fp.TestFramework))
	}

	// CI.
	if fp.CI != "" {
		// Format CI name nicely.
		ciName := fp.CI
		switch ciName {
		case "github-actions":
			ciName = "GitHub Actions"
		case "gitlab-ci":
			ciName = "GitLab CI"
		case "circleci":
			ciName = "CircleCI"
		case "jenkins":
			ciName = "Jenkins"
		case "travis-ci":
			ciName = "Travis CI"
		}
		b.WriteString(fmt.Sprintf("CI: %s\n", ciName))
	}

	// Size.
	if fp.ProjectSize != "" {
		totalFiles := 0
		for _, l := range fp.Languages {
			totalFiles += l.FileCount
		}
		b.WriteString(fmt.Sprintf("Size: %s (%d files)\n", fp.ProjectSize, totalFiles))
	}

	// Docker.
	if fp.Docker {
		b.WriteString("Docker: yes\n")
	}

	// Monorepo.
	if fp.Monorepo {
		b.WriteString("Monorepo: yes\n")
	}

	// Package manager.
	if fp.PackageManager != "" {
		b.WriteString(fmt.Sprintf("Package Manager: %s\n", fp.PackageManager))
	}

	// Lint tools.
	if len(fp.LintTools) > 0 {
		b.WriteString(fmt.Sprintf("Lint: %s\n", strings.Join(fp.LintTools, ", ")))
	}

	// Conventions.
	if len(fp.Conventions) > 0 {
		b.WriteString("\nConventions:\n")
		for _, c := range fp.Conventions {
			b.WriteString(fmt.Sprintf("  - %s (%.0f%% confidence)\n", c.Description, c.Confidence*100))
		}
	}

	// Recommendations.
	if len(fp.Recommendations) > 0 {
		b.WriteString("\nRecommendations:\n")
		for _, r := range fp.Recommendations {
			b.WriteString(fmt.Sprintf("  - %s\n", r))
		}
	}

	return b.String()
}

// containsString checks if a slice contains a given string.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
