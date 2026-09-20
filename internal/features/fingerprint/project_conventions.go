package fingerprint

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Package-level compiled patterns (M14): the convention detectors run per
// scanned repo (filepath.WalkDir over every matching file); regexp.MustCompile
// per call wasted CPU and allocation.
var (
	pythonSnakeRe  = regexp.MustCompile(`\bdef ([a-z][a-z0-9]*_[a-z0-9_]+)\b`)
	pythonCamelRe  = regexp.MustCompile(`\bdef ([a-z][a-zA-Z0-9]+[A-Z][a-zA-Z0-9]*)\b`)
	goWrapErrRe    = regexp.MustCompile(`fmt\.Errorf\([^)]*%w`)
	goBareErrRe    = regexp.MustCompile(`return\s+err\b`)
	goTableTestRe  = regexp.MustCompile(`(tests|cases|testCases|tt)\s*:?=\s*\[\]struct`)
	goSimpleTestRe = regexp.MustCompile(`func Test[A-Z]\w+\(t \*testing\.T\)`)
	conventionalRe = regexp.MustCompile(`^(feat|fix|chore|docs|style|refactor|perf|test|build|ci|revert)(\(.+\))?:`)
)

// This file holds the coding-convention detectors used by Scan (indentation,
// naming, error handling, import organization, test style, commit style). The
// language/build detectors live in project_detect.go.

// detectConventions analyzes the project to identify coding conventions.
func detectConventions(dir string, lang string) []Convention {
	var conventions []Convention

	// Detect indentation from .editorconfig.
	if conv := detectIndentationConvention(dir); conv != nil {
		conventions = append(conventions, *conv)
	}

	// Detect naming convention by sampling source files.
	if conv := detectNamingConvention(dir, lang); conv != nil {
		conventions = append(conventions, *conv)
	}

	// Detect error handling style (Go-specific).
	if lang == "Go" {
		if conv := detectGoErrorHandling(dir); conv != nil {
			conventions = append(conventions, *conv)
		}
	}

	// Detect import organization.
	if conv := detectImportOrganization(dir, lang); conv != nil {
		conventions = append(conventions, *conv)
	}

	// Detect test naming convention.
	if conv := detectTestNaming(dir, lang); conv != nil {
		conventions = append(conventions, *conv)
	}

	// Detect commit message style.
	if conv := detectCommitStyle(dir); conv != nil {
		conventions = append(conventions, *conv)
	}

	return conventions
}

// detectIndentationConvention reads .editorconfig or samples files.
func detectIndentationConvention(dir string) *Convention {
	// Check .editorconfig first.
	editorConfigPath := filepath.Join(dir, ".editorconfig")
	// #nosec G304 -- editorConfigPath joins a fixed config filename with a project directory being scanned by this dev tool
	if data, err := os.ReadFile(editorConfigPath); err == nil {
		content := strings.ToLower(string(data))
		if strings.Contains(content, "indent_style = tab") {
			return &Convention{
				Name:        "indentation",
				Description: "Tabs for indentation",
				Confidence:  1.0,
			}
		}
		if strings.Contains(content, "indent_style = space") {
			// Try to find indent_size.
			size := "unknown"
			lines := strings.Split(content, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "indent_size") {
					parts := strings.SplitN(line, "=", 2)
					if len(parts) == 2 {
						size = strings.TrimSpace(parts[1])
					}
				}
			}
			desc := "Spaces for indentation"
			if size != "unknown" {
				desc = fmt.Sprintf("%s-space indentation", size)
			}
			return &Convention{
				Name:        "indentation",
				Description: desc,
				Confidence:  1.0,
			}
		}
	}

	// Sample source files to detect indentation.
	tabCount := 0
	spaceCount := 0
	sampled := 0
	maxSamples := 20

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || sampled >= maxSamples {
			return filepath.SkipAll
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(path)
		if _, ok := extToLang[ext]; !ok {
			return nil
		}

		f, err := os.Open(path) // #nosec G304,G122 -- read-only convention scan
		if err != nil {
			return nil
		}
		defer func() { _ = f.Close() }()

		scanner := bufio.NewScanner(f)
		lineCount := 0
		for scanner.Scan() && lineCount < 50 {
			line := scanner.Text()
			if len(line) > 0 {
				if line[0] == '\t' {
					tabCount++
				} else if line[0] == ' ' && len(line) > 1 && line[1] == ' ' {
					spaceCount++
				}
			}
			lineCount++
		}
		sampled++
		return nil
	})

	total := tabCount + spaceCount
	if total == 0 {
		return nil
	}

	if tabCount > spaceCount {
		confidence := float64(tabCount) / float64(total)
		return &Convention{
			Name:        "indentation",
			Description: "Tabs for indentation",
			Confidence:  confidence,
		}
	}
	confidence := float64(spaceCount) / float64(total)
	return &Convention{
		Name:        "indentation",
		Description: "Spaces for indentation",
		Confidence:  confidence,
	}
}

// detectNamingConvention samples identifiers to determine naming style.
func detectNamingConvention(dir string, lang string) *Convention {
	// For Go, the convention is well-known: exported = PascalCase, local = camelCase.
	if lang == "Go" {
		return &Convention{
			Name:        "naming",
			Description: "camelCase/PascalCase (Go standard)",
			Confidence:  1.0,
		}
	}

	// For Python, sample for snake_case vs camelCase.
	if lang == "Python" {
		snakeCount := 0
		camelCount := 0
		sampled := 0

		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || sampled >= 10 {
				return filepath.SkipAll
			}
			if d.IsDir() && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			if d.IsDir() || filepath.Ext(path) != ".py" {
				return nil
			}

			data, err := os.ReadFile(path) // #nosec G304,G122 -- read-only convention scan
			if err != nil {
				return nil
			}
			content := string(data)
			snakeCount += len(pythonSnakeRe.FindAllString(content, -1))
			camelCount += len(pythonCamelRe.FindAllString(content, -1))
			sampled++
			return nil
		})

		total := snakeCount + camelCount
		if total == 0 {
			return nil
		}
		if snakeCount > camelCount {
			return &Convention{
				Name:        "naming",
				Description: "snake_case (Python standard)",
				Confidence:  float64(snakeCount) / float64(total),
			}
		}
		return &Convention{
			Name:        "naming",
			Description: "camelCase",
			Confidence:  float64(camelCount) / float64(total),
		}
	}

	return nil
}

// detectGoErrorHandling checks error handling patterns in Go source files.
func detectGoErrorHandling(dir string) *Convention {
	wrapCount := 0 // fmt.Errorf("...: %w", err)
	bareCount := 0 // return err (without wrapping)
	sampled := 0

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || sampled >= 20 {
			return filepath.SkipAll
		}
		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}

		data, err := os.ReadFile(path) // #nosec G304,G122 -- read-only convention scan
		if err != nil {
			return nil
		}
		content := string(data)
		wrapCount += len(goWrapErrRe.FindAllString(content, -1))
		bareCount += len(goBareErrRe.FindAllString(content, -1))
		sampled++
		return nil
	})

	total := wrapCount + bareCount
	if total == 0 {
		return nil
	}

	if wrapCount > bareCount {
		return &Convention{
			Name:        "error-handling",
			Description: "Error wrapping with %w",
			Confidence:  float64(wrapCount) / float64(total),
		}
	}
	return &Convention{
		Name:        "error-handling",
		Description: "Bare error returns",
		Confidence:  float64(bareCount) / float64(total),
	}
}

// detectImportOrganization checks if imports are grouped (stdlib vs third-party).
func detectImportOrganization(dir string, lang string) *Convention {
	if lang != "Go" {
		return nil
	}

	groupedCount := 0
	ungroupedCount := 0
	sampled := 0

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || sampled >= 15 {
			return filepath.SkipAll
		}
		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}

		data, err := os.ReadFile(path) // #nosec G304,G122 -- read-only convention scan
		if err != nil {
			return nil
		}
		content := string(data)

		// Find import blocks.
		importStart := strings.Index(content, "import (")
		if importStart == -1 {
			return nil
		}
		importEnd := strings.Index(content[importStart:], ")")
		if importEnd == -1 {
			return nil
		}
		importBlock := content[importStart : importStart+importEnd]

		// Check for blank lines within the import block (indicating grouping).
		if strings.Contains(importBlock, "\n\n") {
			groupedCount++
		} else {
			// Only count as ungrouped if there are multiple imports.
			lines := strings.Split(importBlock, "\n")
			importLines := 0
			for _, l := range lines {
				l = strings.TrimSpace(l)
				if l != "" && l != "import (" && l != ")" && !strings.HasPrefix(l, "//") {
					importLines++
				}
			}
			if importLines > 1 {
				ungroupedCount++
			}
		}
		sampled++
		return nil
	})

	total := groupedCount + ungroupedCount
	if total == 0 {
		return nil
	}

	if groupedCount > ungroupedCount {
		return &Convention{
			Name:        "imports",
			Description: "Grouped imports (stdlib separated from third-party)",
			Confidence:  float64(groupedCount) / float64(total),
		}
	}
	return &Convention{
		Name:        "imports",
		Description: "Ungrouped imports",
		Confidence:  float64(ungroupedCount) / float64(total),
	}
}

// detectTestNaming checks test naming conventions.
func detectTestNaming(dir string, lang string) *Convention {
	if lang != "Go" {
		return nil
	}

	// Check for table-driven tests vs simple tests.
	tableDrivenCount := 0
	simpleCount := 0
	sampled := 0

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || sampled >= 15 {
			return filepath.SkipAll
		}
		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}

		data, err := os.ReadFile(path) // #nosec G304,G122 -- read-only convention scan
		if err != nil {
			return nil
		}
		content := string(data)
		tableDrivenCount += len(goTableTestRe.FindAllString(content, -1))
		simpleCount += len(goSimpleTestRe.FindAllString(content, -1))
		sampled++
		return nil
	})

	if tableDrivenCount > 0 && simpleCount > 0 {
		total := tableDrivenCount + simpleCount
		if tableDrivenCount > simpleCount/2 {
			return &Convention{
				Name:        "test-style",
				Description: "Table-driven tests",
				Confidence:  float64(tableDrivenCount) / float64(total),
			}
		}
	}

	return nil
}

// detectCommitStyle checks git log for conventional commits or other patterns.
func detectCommitStyle(dir string) *Convention {
	cmd := exec.CommandContext(context.Background(), "git", "log", "--oneline", "-20", "--format=%s")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 {
		return nil
	}

	// Check for conventional commits (feat:, fix:, chore:, etc.).
	conventionalCount := 0

	for _, line := range lines {
		if conventionalRe.MatchString(line) {
			conventionalCount++
		}
	}

	if conventionalCount > 0 {
		confidence := float64(conventionalCount) / float64(len(lines))
		if confidence >= 0.3 {
			return &Convention{
				Name:        "commit-style",
				Description: "Conventional commits (feat:, fix:, etc.)",
				Confidence:  confidence,
			}
		}
	}

	return nil
}
