package fingerprint

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// langStat tracks file and line counts for a detected language.
type langStat struct {
	files int
	lines int
}

// extToLang maps file extensions to language names.
var extToLang = map[string]string{
	".go":     "Go",
	".js":     "JavaScript",
	".jsx":    "JavaScript",
	".ts":     "TypeScript",
	".tsx":    "TypeScript",
	".py":     "Python",
	".rb":     "Ruby",
	".rs":     "Rust",
	".java":   "Java",
	".kt":     "Kotlin",
	".kts":    "Kotlin",
	".c":      "C",
	".h":      "C",
	".cpp":    "C++",
	".cc":     "C++",
	".cxx":    "C++",
	".hpp":    "C++",
	".cs":     "C#",
	".swift":  "Swift",
	".m":      "Objective-C",
	".mm":     "Objective-C",
	".php":    "PHP",
	".lua":    "Lua",
	".r":      "R",
	".R":      "R",
	".scala":  "Scala",
	".clj":    "Clojure",
	".ex":     "Elixir",
	".exs":    "Elixir",
	".erl":    "Erlang",
	".hs":     "Haskell",
	".dart":   "Dart",
	".pl":     "Perl",
	".pm":     "Perl",
	".sh":     "Shell",
	".bash":   "Shell",
	".zsh":    "Shell",
	".fish":   "Shell",
	".html":   "HTML",
	".htm":    "HTML",
	".css":    "CSS",
	".scss":   "SCSS",
	".sass":   "SCSS",
	".less":   "Less",
	".vue":    "Vue",
	".svelte": "Svelte",
	".sql":    "SQL",
	".proto":  "Protobuf",
	".yaml":   "YAML",
	".yml":    "YAML",
	".json":   "JSON",
	".xml":    "XML",
	".toml":   "TOML",
	".md":     "Markdown",
	".rst":    "reStructuredText",
	".tex":    "LaTeX",
	".zig":    "Zig",
	".nim":    "Nim",
	".v":      "V",
	".nix":    "Nix",
	".tf":     "Terraform",
	".sol":    "Solidity",
}

// skipDirs is the set of directory names to skip during the walk.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".gomodcache":  true,
	"__pycache__":  true,
	".tox":         true,
	".venv":        true,
	"venv":         true,
	"dist":         true,
	"build":        true,
	".next":        true,
	".cache":       true,
	"target":       true, // Rust/Maven
}

// walkDir performs a single directory walk, counting files and lines by language.
// Returns the per-language stats map and total line count.
func walkDir(root string) (map[string]*langStat, int, error) {
	stats := make(map[string]*langStat)
	totalLines := 0

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible entries
		}

		if d.IsDir() {
			name := d.Name()
			if skipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process regular files.
		if !d.Type().IsRegular() {
			return nil
		}

		ext := filepath.Ext(path)
		lang, ok := extToLang[ext]
		if !ok {
			return nil
		}

		lines := countLines(path)
		if stats[lang] == nil {
			stats[lang] = &langStat{}
		}
		stats[lang].files++
		stats[lang].lines += lines
		totalLines += lines

		return nil
	})

	return stats, totalLines, err
}

// countLines counts newline characters in a file. Fast, buffer-based.
func countLines(path string) int {
	f, err := os.Open(path) // #nosec G304 -- path comes from filepath.WalkDir over a project directory being scanned by this dev tool
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()

	count := 0
	buf := make([]byte, 32*1024)
	for {
		n, err := f.Read(buf)
		for i := 0; i < n; i++ {
			if buf[i] == '\n' {
				count++
			}
		}
		if err != nil {
			break
		}
	}
	return count
}

// packageManagerInfo maps a manifest filename to its package manager name.
var packageManagerInfo = []struct {
	file    string
	manager string
}{
	{"go.mod", "go mod"},
	{"package.json", "npm"},
	{"Cargo.toml", "cargo"},
	{"requirements.txt", "pip"},
	{"Pipfile", "pipenv"},
	{"pyproject.toml", "pyproject"},
	{"Gemfile", "bundler"},
	{"composer.json", "composer"},
	{"pom.xml", "maven"},
	{"build.gradle", "gradle"},
	{"build.gradle.kts", "gradle"},
	{"Package.swift", "swift package manager"},
	{"pubspec.yaml", "pub"},
	{"mix.exs", "mix"},
	{"Makefile", "make"},
}

// detectPackageManager checks for known manifest files and returns the manager
// name and dependency count.
func detectPackageManager(dir string) (string, int) {
	for _, pm := range packageManagerInfo {
		path := filepath.Join(dir, pm.file)
		if _, err := os.Stat(path); err == nil {
			deps := countDependencies(path, pm.manager)
			return pm.manager, deps
		}
	}
	return "", 0
}

// countDependencies parses common manifest formats to count dependency entries.
func countDependencies(path, manager string) int {
	switch manager {
	case "go mod":
		return countGoModDeps(path)
	case "npm":
		return countNPMDeps(path)
	case "cargo":
		return countCargoDeps(path)
	case "pip":
		return countLineBasedDeps(path)
	case "bundler":
		return countGemfileDeps(path)
	default:
		return 0
	}
}

// countGoModDeps counts require directives in a go.mod file.
func countGoModDeps(path string) int {
	data, err := os.ReadFile(path) // #nosec G304 -- path is a manifest file located via detectPackageManager's fixed filename list joined with a scanned project directory
	if err != nil {
		return 0
	}

	count := 0
	inRequire := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "require (") || strings.HasPrefix(line, "require(") {
			inRequire = true
			continue
		}
		if inRequire {
			if line == ")" {
				inRequire = false
				continue
			}
			if line != "" && !strings.HasPrefix(line, "//") {
				count++
			}
		}
		// Single-line require.
		if strings.HasPrefix(line, "require ") && !strings.Contains(line, "(") {
			count++
		}
	}
	return count
}

// countNPMDeps counts dependencies + devDependencies in package.json.
func countNPMDeps(path string) int {
	data, err := os.ReadFile(path) // #nosec G304 -- path is a manifest file located via detectPackageManager's fixed filename list joined with a scanned project directory
	if err != nil {
		return 0
	}

	var pkg struct {
		Dependencies    map[string]interface{} `json:"dependencies"`
		DevDependencies map[string]interface{} `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return 0
	}
	return len(pkg.Dependencies) + len(pkg.DevDependencies)
}

// countCargoDeps counts [dependencies] entries in Cargo.toml (simple heuristic).
func countCargoDeps(path string) int {
	data, err := os.ReadFile(path) // #nosec G304 -- path is a manifest file located via detectPackageManager's fixed filename list joined with a scanned project directory
	if err != nil {
		return 0
	}

	count := 0
	inDeps := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			inDeps = strings.Contains(line, "dependencies")
			continue
		}
		if inDeps && line != "" && !strings.HasPrefix(line, "#") && strings.Contains(line, "=") {
			count++
		}
	}
	return count
}

// countLineBasedDeps counts non-empty, non-comment lines (for requirements.txt, etc.).
func countLineBasedDeps(path string) int {
	data, err := os.ReadFile(path) // #nosec G304 -- path is a manifest file located via detectPackageManager's fixed filename list joined with a scanned project directory
	if err != nil {
		return 0
	}

	count := 0
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "-") {
			count++
		}
	}
	return count
}

// countGemfileDeps counts gem lines in a Gemfile.
func countGemfileDeps(path string) int {
	data, err := os.ReadFile(path) // #nosec G304 -- path is a manifest file located via detectPackageManager's fixed filename list joined with a scanned project directory
	if err != nil {
		return 0
	}

	count := 0
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "gem ") || strings.HasPrefix(line, "gem(") {
			count++
		}
	}
	return count
}

// detectTests checks for the presence of test files or test directories.
func detectTests(dir string) bool {
	// Check common test directories.
	testDirs := []string{"test", "tests", "spec", "specs", "__tests__", "testing"}
	for _, td := range testDirs {
		if info, err := os.Stat(filepath.Join(dir, td)); err == nil && info.IsDir() {
			return true
		}
	}

	// Walk top two levels looking for test files.
	found := false
	depth := 0
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipDir
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			// Calculate depth relative to root.
			rel, _ := filepath.Rel(dir, path)
			depth = strings.Count(rel, string(filepath.Separator))
			if depth > 3 {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(name, "_test.go") ||
			strings.HasPrefix(name, "test_") ||
			strings.HasSuffix(name, "_test.py") ||
			strings.HasSuffix(name, ".test.js") ||
			strings.HasSuffix(name, ".test.ts") ||
			strings.HasSuffix(name, ".test.tsx") ||
			strings.HasSuffix(name, ".spec.js") ||
			strings.HasSuffix(name, ".spec.ts") ||
			strings.HasSuffix(name, "_spec.rb") ||
			strings.HasSuffix(name, "Test.java") ||
			strings.HasSuffix(name, "_test.rs") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})

	return found
}

// detectCI checks for CI configuration files.
func detectCI(dir string) bool {
	ciPaths := []string{
		filepath.Join(".github", "workflows"),
		".gitlab-ci.yml",
		"Jenkinsfile",
		".circleci",
		".travis.yml",
		"azure-pipelines.yml",
		"bitbucket-pipelines.yml",
		".drone.yml",
		"cloudbuild.yaml",
		"cloudbuild.yml",
		".buildkite",
	}

	for _, p := range ciPaths {
		full := filepath.Join(dir, p)
		if _, err := os.Stat(full); err == nil {
			return true
		}
	}

	return false
}

// detectLicense reads a LICENSE file and tries to identify the license type from
// its first few lines.
func detectLicense(dir string) string {
	names := []string{"LICENSE", "LICENSE.md", "LICENSE.txt", "LICENCE", "LICENCE.md", "COPYING"}

	for _, name := range names {
		path := filepath.Join(dir, name)
		f, err := os.Open(path) // #nosec G304 -- path joins a fixed license filename with a project directory being scanned by this dev tool
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(f)
		// Read up to the first 5 lines to identify the license.
		var lines []string
		for i := 0; i < 5 && scanner.Scan(); i++ {
			lines = append(lines, scanner.Text())
		}
		// Close eagerly rather than deferring: deferring inside the loop would
		// hold every candidate file open until detectLicense returns.
		_ = f.Close()
		text := strings.Join(lines, " ")
		upper := strings.ToUpper(text)

		switch {
		case strings.Contains(upper, "MIT LICENSE") || strings.Contains(upper, "MIT"):
			return "MIT"
		case strings.Contains(upper, "APACHE LICENSE") || strings.Contains(upper, "APACHE-2"):
			return "Apache-2.0"
		case strings.Contains(upper, "GNU GENERAL PUBLIC LICENSE"):
			if strings.Contains(upper, "VERSION 3") {
				return "GPL-3.0"
			}
			if strings.Contains(upper, "VERSION 2") {
				return "GPL-2.0"
			}
			return "GPL"
		case strings.Contains(upper, "BSD 2-CLAUSE") || strings.Contains(upper, "SIMPLIFIED BSD"):
			return "BSD-2-Clause"
		case strings.Contains(upper, "BSD 3-CLAUSE") || strings.Contains(upper, "REVISED BSD"):
			return "BSD-3-Clause"
		case strings.Contains(upper, "BSD"):
			return "BSD"
		case strings.Contains(upper, "ISC LICENSE") || strings.Contains(upper, "ISC"):
			return "ISC"
		case strings.Contains(upper, "MOZILLA PUBLIC LICENSE"):
			return "MPL-2.0"
		case strings.Contains(upper, "UNLICENSE") || strings.Contains(upper, "UNLICENSED"):
			return "Unlicense"
		case strings.Contains(upper, "CREATIVE COMMONS"):
			return "CC"
		case strings.Contains(upper, "PARITY"):
			return "Parity"
		default:
			// We found a license file but couldn't identify the type.
			return "Unknown"
		}
	}

	return ""
}

// collectGitInfo runs git commands to gather repo metadata. Returns nil if
// the directory is not a git repository.
func collectGitInfo(dir string) *GitInfo {
	// Check if it's a git repo.
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	if out, err := cmd.Output(); err != nil || strings.TrimSpace(string(out)) != "true" {
		return nil
	}

	info := &GitInfo{}

	// Current branch.
	if out, err := gitCmd(dir, "branch", "--show-current"); err == nil {
		info.Branch = strings.TrimSpace(out)
	}
	if info.Branch == "" {
		// Detached HEAD: try to get short ref.
		if out, err := gitCmd(dir, "rev-parse", "--short", "HEAD"); err == nil {
			info.Branch = strings.TrimSpace(out)
		}
	}

	// Commit count.
	if out, err := gitCmd(dir, "rev-list", "--count", "HEAD"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(out)); err == nil {
			info.CommitCount = n
		}
	}

	// Last commit message (first line).
	if out, err := gitCmd(dir, "log", "-1", "--format=%s"); err == nil {
		info.LastCommit = strings.TrimSpace(out)
	}

	// Contributor count (unique authors).
	if out, err := gitCmd(dir, "shortlog", "-sn", "--all", "--no-merges"); err == nil {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		count := 0
		for _, l := range lines {
			if strings.TrimSpace(l) != "" {
				count++
			}
		}
		info.Contributors = count
	}

	return info
}

// gitCmd runs a git command in the given directory and returns its stdout.
func gitCmd(dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", args...) // #nosec G204 -- fixed git executable
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String(), nil
}
