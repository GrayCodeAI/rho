package cmd

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/GrayCodeAI/rho/internal/fsutil"
	"github.com/GrayCodeAI/rho/internal/intelligence/repomap"
	"github.com/GrayCodeAI/rho/internal/tool"
)

// ExportContext generates a comprehensive context document about the current project.
// Output is optimized for pasting into any LLM chat.
func ExportContext(dir string, focus string) (string, error) {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("context export: get working dir: %w", err)
		}
	}

	var b strings.Builder

	b.WriteString("# Project Context\n\n")

	// Project type and language
	projectType, language := detectProjectType(dir)
	b.WriteString(fmt.Sprintf("**Type:** %s\n", projectType))
	b.WriteString(fmt.Sprintf("**Language:** %s\n", language))
	b.WriteString(fmt.Sprintf("**Directory:** %s\n\n", dir))

	// Directory structure (top 2 levels)
	b.WriteString("## Directory Structure\n\n```\n")
	tree := dirTree(dir, 2)
	b.WriteString(tree)
	b.WriteString("```\n\n")

	// Key files
	b.WriteString("## Key Files\n\n")
	for _, name := range keyFiles(dir) {
		content, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- name comes from internal keyFiles() list
		if err != nil {
			continue
		}
		ext := filepath.Ext(name)
		lang := langFromExt(ext)
		b.WriteString(fmt.Sprintf("### %s\n\n```%s\n%s\n```\n\n", name, lang, strings.TrimSpace(string(content))))
	}

	// Git status
	gitInfo := gitContextInfo(dir)
	if gitInfo != "" {
		b.WriteString("## Git Status\n\n")
		b.WriteString(gitInfo)
		b.WriteString("\n\n")
	}

	// AGENTS.md / project instructions
	for _, instrFile := range []string{"AGENTS.md", ".rho.md"} {
		data, err := os.ReadFile(filepath.Join(dir, instrFile)) // #nosec G304 -- instrFile is one of a fixed set of well-known project instruction filenames
		if err == nil && len(data) > 0 {
			b.WriteString(fmt.Sprintf("## Project Instructions (%s)\n\n%s\n\n", instrFile, strings.TrimSpace(string(data))))
			break
		}
	}

	// Focus area files
	if focus != "" {
		b.WriteString(fmt.Sprintf("## Focus Area: %s\n\n", focus))
		focusFiles := findFocusFiles(dir, focus)
		for _, fp := range focusFiles {
			content, err := os.ReadFile(fp) // #nosec G304 -- fp comes from internal findFocusFiles enumeration
			if err != nil {
				continue
			}
			rel, _ := filepath.Rel(dir, fp)
			ext := filepath.Ext(fp)
			lang := langFromExt(ext)
			b.WriteString(fmt.Sprintf("### %s\n\n```%s\n%s\n```\n\n", rel, lang, strings.TrimSpace(string(content))))
		}
	}

	return b.String(), nil
}

// ExportContextToFile writes context to a .md file.
func ExportContextToFile(dir, focus, outputPath string) error {
	content, err := ExportContext(dir, focus)
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, []byte(content), 0o600)
}

// detectProjectType returns the project type and primary language based on
// marker files in the directory.
func detectProjectType(dir string) (string, string) {
	markers := []struct {
		file     string
		projType string
		lang     string
	}{
		{"go.mod", "Go module", "Go"},
		{"Cargo.toml", "Rust crate", "Rust"},
		{"package.json", "Node.js project", "JavaScript/TypeScript"},
		{"pyproject.toml", "Python project", "Python"},
		{"requirements.txt", "Python project", "Python"},
		{"pom.xml", "Maven project", "Java"},
		{"build.gradle", "Gradle project", "Java/Kotlin"},
		{"Gemfile", "Ruby project", "Ruby"},
		{"mix.exs", "Elixir project", "Elixir"},
	}
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(dir, m.file)); err == nil {
			return m.projType, m.lang
		}
	}
	return "unknown", "unknown"
}

// dirTree returns a tree-like string of the directory up to maxDepth levels.
func dirTree(dir string, maxDepth int) string {
	var b strings.Builder
	dirTreeRecurse(&b, dir, "", 0, maxDepth)
	return b.String()
}

func dirTreeRecurse(b *strings.Builder, dir, prefix string, depth, maxDepth int) {
	if depth > maxDepth {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	// Filter out hidden directories and common noise
	var visible []os.DirEntry
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__" {
			continue
		}
		visible = append(visible, e)
	}

	for i, e := range visible {
		isLast := i == len(visible)-1
		connector := "├── "
		if isLast {
			connector = "└── "
		}
		b.WriteString(prefix + connector + e.Name())
		if e.IsDir() {
			b.WriteString("/")
		}
		b.WriteString("\n")

		if e.IsDir() {
			childPrefix := prefix + "│   "
			if isLast {
				childPrefix = prefix + "    "
			}
			dirTreeRecurse(b, filepath.Join(dir, e.Name()), childPrefix, depth+1, maxDepth)
		}
	}
}

// keyFiles returns the names of key project files that exist in the directory.
func keyFiles(dir string) []string {
	candidates := []string{
		"README.md", "go.mod", "package.json", "Cargo.toml",
		"pyproject.toml", "main.go", "main.py", "src/main.rs",
		"index.ts", "index.js", "app.py",
	}
	var found []string
	for _, name := range candidates {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			found = append(found, name)
		}
	}
	return found
}

// gitContextInfo returns git branch, recent commits, and diff summary.
func gitContextInfo(dir string) string {
	var b strings.Builder

	// Branch
	if out, err := runGit(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		b.WriteString(fmt.Sprintf("**Branch:** %s\n", strings.TrimSpace(out)))
	}

	// Recent commits
	if out, err := runGit(dir, "log", "--oneline", "-5"); err == nil && strings.TrimSpace(out) != "" {
		b.WriteString("\n**Recent commits:**\n```\n")
		b.WriteString(strings.TrimSpace(out))
		b.WriteString("\n```\n")
	}

	// Diff summary
	if out, err := runGit(dir, "diff", "--stat"); err == nil && strings.TrimSpace(out) != "" {
		b.WriteString("\n**Current diff:**\n```\n")
		b.WriteString(strings.TrimSpace(out))
		b.WriteString("\n```\n")
	}

	return b.String()
}

// runGit executes a git command in the given directory.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", args...) // #nosec G204 -- fixed git executable
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// findFocusFiles finds files matching the focus string in the directory.
func findFocusFiles(dir, focus string) []string {
	var result []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		// Match focus against directory path or file name
		rel, _ := filepath.Rel(dir, path)
		if strings.Contains(rel, focus) {
			result = append(result, path)
		}
		return nil
	})
	// Limit to 20 files to avoid massive output
	if len(result) > 20 {
		result = result[:20]
	}
	return result
}

// renderCXML renders a directory as CXML (Karpathy's rendergit pattern).
// Returns the CXML string and a stats summary.
func renderCXML(dir string) (string, string, error) {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", "", err
		}
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}

	gi := repomap.LoadGitignoreRules(dir)

	binaryExts := map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true, ".ico": true, ".webp": true,
		".pdf": true, ".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true, ".7z": true,
		".exe": true, ".dll": true, ".so": true, ".dylib": true, ".o": true, ".a": true,
		".wasm": true, ".pyc": true, ".class": true, ".jar": true,
		".mp3": true, ".mp4": true, ".wav": true, ".avi": true, ".mov": true, ".mkv": true,
		".ttf": true, ".otf": true, ".woff": true, ".woff2": true, ".eot": true,
		".sqlite": true, ".db": true, ".DS_Store": true,
	}

	const maxFileSize = 50 * 1024

	var files []struct{ rel, content string }
	var scanned, skipped int

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if gi.ShouldIgnore(rel) {
			skipped++
			return nil
		}
		scanned++
		if binaryExts[strings.ToLower(filepath.Ext(name))] {
			skipped++
			return nil
		}
		fi, fiErr := d.Info()
		if fiErr != nil {
			return fiErr
		}
		if fi.Size() > maxFileSize {
			skipped++
			return nil
		}
		data, err := fsutil.ReadPinnedFile(path)
		if err != nil {
			skipped++
			return nil
		}
		if tool.IsBinaryContent(data) {
			skipped++
			return nil
		}
		files = append(files, struct{ rel, content string }{rel, string(data)})
		return nil
	})

	var b strings.Builder
	b.WriteString("<documents>\n")

	// Document 1: directory tree
	b.WriteString("<document index=\"1\">\n<source>directory_tree</source>\n<document_content>\n")
	b.WriteString(dirTree(dir, 3))
	b.WriteString("</document_content>\n</document>\n")

	for i, f := range files {
		b.WriteString(fmt.Sprintf("<document index=\"%d\">\n<source>%s</source>\n<document_content>\n%s\n</document_content>\n</document>\n", i+2, f.rel, f.content))
	}
	b.WriteString("</documents>\n")

	stats := fmt.Sprintf("Render complete: %d files scanned, %d included, %d skipped", scanned, len(files), skipped)
	return b.String(), stats, nil
}
