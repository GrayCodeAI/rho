package eval

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

// CoverageReport holds overall coverage metrics and per-file details.
type CoverageReport struct {
	TotalLines         int
	CoveredLines       int
	Percentage         float64
	Files              []FileCoverage
	UncoveredFunctions []string
	Suggestions        []TestSuggestion
}

// FileCoverage holds coverage information for a single source file.
type FileCoverage struct {
	Path            string
	TotalLines      int
	CoveredLines    int
	Percentage      float64
	UncoveredRanges []LineRange
}

// LineRange represents a contiguous range of uncovered lines.
type LineRange struct {
	Start        int
	End          int
	FunctionName string
}

// TestSuggestion recommends a test to write for uncovered code.
type TestSuggestion struct {
	Function string
	File     string
	Priority string
	Reason   string
	Template string
}

// CoverageAnalyzer runs coverage analysis on a Go project.
type CoverageAnalyzer struct {
	ProjectDir string
	mu         sync.Mutex
}

// NewCoverageAnalyzer creates a new CoverageAnalyzer for the given project directory.
func NewCoverageAnalyzer(projectDir string) *CoverageAnalyzer {
	return &CoverageAnalyzer{
		ProjectDir: projectDir,
	}
}

// RunCoverage executes go test with coverage and builds a structured report.
func (ca *CoverageAnalyzer) RunCoverage() (*CoverageReport, error) {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	coverFile := filepath.Join(ca.ProjectDir, "coverage.out")
	defer func() { _ = os.Remove(coverFile) }()

	cmd := exec.CommandContext(context.Background(), "go", "test", "-coverprofile="+coverFile, "./...") // #nosec G204 -- fixed "go" tool invocation; coverFile is derived from ca.ProjectDir, a local project path set by the caller
	cmd.Dir = ca.ProjectDir
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		// Tests may fail but still produce coverage output; only error if no file.
		if _, statErr := os.Stat(coverFile); statErr != nil {
			return nil, fmt.Errorf("coverage run failed: %w", err)
		}
	}

	data, err := os.ReadFile(coverFile) // #nosec G304 -- coverFile is derived from ca.ProjectDir, a local project path set by the caller
	if err != nil {
		return nil, fmt.Errorf("reading coverage profile: %w", err)
	}

	report, err := ParseCoverageProfile(string(data))
	if err != nil {
		return nil, err
	}

	uncovered := FindUncoveredFunctions(report, ca.ProjectDir)
	report.UncoveredFunctions = uncovered
	report.Suggestions = SuggestTests(uncovered)

	return report, nil
}

// ParseCoverageProfile parses the Go coverage profile format.
// Each line after the mode header has the form:
//
//	file.go:startLine.startCol,endLine.endCol numStmts count
func ParseCoverageProfile(data string) (*CoverageReport, error) {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return nil, fmt.Errorf("empty coverage profile")
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty coverage profile")
	}

	// Skip the mode line (e.g., "mode: set" or "mode: count").
	startIdx := 0
	if strings.HasPrefix(lines[0], "mode:") {
		startIdx = 1
	}

	// fileLines maps file path -> line number -> covered.
	fileLines := make(map[string]map[int]bool)

	for i := startIdx; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		// Parse: file.go:startLine.startCol,endLine.endCol numStmts count
		colonIdx := strings.LastIndex(line, ":")
		if colonIdx < 0 {
			continue
		}

		filePath := line[:colonIdx]
		rest := line[colonIdx+1:]

		// Split rest into position part and numbers.
		parts := strings.Fields(rest)
		if len(parts) < 3 {
			continue
		}

		posPart := parts[0]
		countStr := parts[2]

		count, err := strconv.Atoi(countStr)
		if err != nil {
			continue
		}

		// Parse position: startLine.startCol,endLine.endCol
		commaIdx := strings.Index(posPart, ",")
		if commaIdx < 0 {
			continue
		}

		startPart := posPart[:commaIdx]
		endPart := posPart[commaIdx+1:]

		startLine := parseLineFromPos(startPart)
		endLine := parseLineFromPos(endPart)

		if startLine <= 0 || endLine <= 0 {
			continue
		}

		if fileLines[filePath] == nil {
			fileLines[filePath] = make(map[int]bool)
		}

		for ln := startLine; ln <= endLine; ln++ {
			if count > 0 {
				fileLines[filePath][ln] = true
			} else {
				// Only mark uncovered if not already covered.
				if _, exists := fileLines[filePath][ln]; !exists {
					fileLines[filePath][ln] = false
				}
			}
		}
	}

	report := &CoverageReport{}
	var files []FileCoverage

	for path, linesMap := range fileLines {
		fc := FileCoverage{Path: path}
		fc.TotalLines = len(linesMap)

		for _, covered := range linesMap {
			if covered {
				fc.CoveredLines++
			}
		}

		if fc.TotalLines > 0 {
			fc.Percentage = float64(fc.CoveredLines) / float64(fc.TotalLines) * 100
		}

		// Build uncovered ranges.
		var uncoveredLines []int
		for ln, covered := range linesMap {
			if !covered {
				uncoveredLines = append(uncoveredLines, ln)
			}
		}
		sort.Ints(uncoveredLines)

		fc.UncoveredRanges = buildRanges(uncoveredLines)
		files = append(files, fc)

		report.TotalLines += fc.TotalLines
		report.CoveredLines += fc.CoveredLines
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	report.Files = files

	if report.TotalLines > 0 {
		report.Percentage = float64(report.CoveredLines) / float64(report.TotalLines) * 100
	}

	return report, nil
}

// parseLineFromPos extracts the line number from "line.col" format.
func parseLineFromPos(pos string) int {
	dotIdx := strings.Index(pos, ".")
	if dotIdx < 0 {
		n, _ := strconv.Atoi(pos)
		return n
	}
	n, _ := strconv.Atoi(pos[:dotIdx])
	return n
}

// buildRanges groups consecutive line numbers into ranges.
func buildRanges(lines []int) []LineRange {
	if len(lines) == 0 {
		return nil
	}

	var ranges []LineRange
	start := lines[0]
	end := lines[0]

	for i := 1; i < len(lines); i++ {
		if lines[i] == end+1 {
			end = lines[i]
		} else {
			ranges = append(ranges, LineRange{Start: start, End: end})
			start = lines[i]
			end = lines[i]
		}
	}
	ranges = append(ranges, LineRange{Start: start, End: end})

	return ranges
}

// FindUncoveredFunctions cross-references coverage data with the AST
// to identify functions that have zero coverage.
func FindUncoveredFunctions(profile *CoverageReport, projectDir string) []string {
	if profile == nil {
		return nil
	}

	// Build a set of uncovered line ranges per file.
	type fileUncovered struct {
		uncoveredLines map[int]bool
		totalLines     map[int]bool
	}
	fileData := make(map[string]*fileUncovered)

	for _, fc := range profile.Files {
		fu := &fileUncovered{
			uncoveredLines: make(map[int]bool),
			totalLines:     make(map[int]bool),
		}
		for _, r := range fc.UncoveredRanges {
			for ln := r.Start; ln <= r.End; ln++ {
				fu.uncoveredLines[ln] = true
			}
		}
		// All lines we know about.
		for ln := 1; ln <= fc.TotalLines+fc.CoveredLines; ln++ {
			fu.totalLines[ln] = true
		}
		fileData[fc.Path] = fu
	}

	var uncoveredFuncs []string

	for _, fc := range profile.Files {
		fu := fileData[fc.Path]
		if fu == nil {
			continue
		}

		// Resolve the file path relative to the project.
		absPath := resolveFilePath(fc.Path, projectDir)
		if absPath == "" {
			continue
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, absPath, nil, 0)
		if err != nil {
			continue
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}

			startLine := fset.Position(fn.Body.Lbrace).Line
			endLine := fset.Position(fn.Body.Rbrace).Line

			// Check if the function has any covered lines.
			hasCoverage := false
			hasAnyLines := false
			for ln := startLine; ln <= endLine; ln++ {
				if _, exists := fu.uncoveredLines[ln]; exists {
					hasAnyLines = true
				} else {
					// If the line is not in uncovered, it might be covered.
					hasCoverage = true
				}
			}

			if hasAnyLines && !hasCoverage {
				name := fn.Name.Name
				if fn.Recv != nil && len(fn.Recv.List) > 0 {
					name = formatReceiver(fn.Recv.List[0].Type) + "." + name
				}
				loc := fmt.Sprintf("%s:%d", fc.Path, fset.Position(fn.Pos()).Line)
				uncoveredFuncs = append(uncoveredFuncs, fmt.Sprintf("%s (%s)", name, loc))
			}
		}
	}

	return uncoveredFuncs
}

// resolveFilePath attempts to find the absolute path for a coverage file entry.
func resolveFilePath(coverPath, projectDir string) string {
	// Coverage paths may be module-relative (e.g., github.com/foo/bar/pkg/file.go).
	// Try stripping the module prefix and joining with projectDir.
	// First, try if it's already a valid path.
	if _, err := os.Stat(coverPath); err == nil {
		return coverPath
	}

	// Try joining with project dir directly.
	candidate := filepath.Join(projectDir, coverPath)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}

	// Try stripping common module prefixes by looking for a "src" or package path.
	parts := strings.Split(coverPath, "/")
	for i := range parts {
		candidate = filepath.Join(projectDir, filepath.Join(parts[i:]...))
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return ""
}

// formatReceiver returns a string representation of a receiver type.
func formatReceiver(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return "*" + formatReceiver(t.X)
	case *ast.Ident:
		return t.Name
	default:
		return "?"
	}
}

// SuggestTests generates test suggestions for uncovered functions.
func SuggestTests(uncovered []string) []TestSuggestion {
	var suggestions []TestSuggestion

	for _, entry := range uncovered {
		// Parse "FuncName (file:line)" format.
		parenIdx := strings.Index(entry, " (")
		if parenIdx < 0 {
			continue
		}
		funcName := entry[:parenIdx]
		fileLoc := strings.Trim(entry[parenIdx+2:], "()")

		// Determine priority and reason.
		priority, reason := classifyFunction(funcName)

		// Extract base function name for test template.
		baseName := funcName
		if dotIdx := strings.LastIndex(funcName, "."); dotIdx >= 0 {
			baseName = funcName[dotIdx+1:]
		}

		template := GenerateTestTemplate(baseName, "", "")

		suggestions = append(suggestions, TestSuggestion{
			Function: funcName,
			File:     fileLoc,
			Priority: priority,
			Reason:   reason,
			Template: template,
		})
	}

	// Sort by priority: HIGH first, then MED, then LOW.
	priorityOrder := map[string]int{"HIGH": 0, "MED": 1, "LOW": 2}
	sort.SliceStable(suggestions, func(i, j int) bool {
		return priorityOrder[suggestions[i].Priority] < priorityOrder[suggestions[j].Priority]
	})

	return suggestions
}

// classifyFunction determines the priority and reason for testing a function.
func classifyFunction(funcName string) (priority, reason string) {
	baseName := funcName
	if dotIdx := strings.LastIndex(funcName, "."); dotIdx >= 0 {
		baseName = funcName[dotIdx+1:]
	}

	isExported := len(baseName) > 0 && baseName[0] >= 'A' && baseName[0] <= 'Z'

	// Check common patterns for higher priority.
	lowerName := strings.ToLower(baseName)

	switch {
	case isExported && (strings.Contains(lowerName, "handle") || strings.Contains(lowerName, "handler")):
		return "HIGH", "exported, HTTP handler"
	case isExported && (strings.Contains(lowerName, "error") || strings.Contains(lowerName, "err")):
		return "HIGH", "exported, has error return"
	case isExported && (strings.Contains(lowerName, "parse") || strings.Contains(lowerName, "validate")):
		return "HIGH", "exported, has error return"
	case isExported && (strings.Contains(lowerName, "new") || strings.Contains(lowerName, "create")):
		return "HIGH", "exported, constructor"
	case isExported:
		return "MED", "exported"
	default:
		return "LOW", "unexported helper"
	}
}

// GenerateTestTemplate creates a table-driven test template for a function.
func GenerateTestTemplate(funcName, pkg, signature string) string {
	testName := "Test" + funcName

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("func %s(t *testing.T) {\n", testName))
	sb.WriteString("\ttests := []struct {\n")
	sb.WriteString("\t\tname string\n")
	sb.WriteString("\t\t// TODO: add test fields\n")
	sb.WriteString("\t}{\n")
	sb.WriteString("\t\t{\"basic case\" /* ... */},\n")
	sb.WriteString("\t}\n")
	sb.WriteString("\tfor _, tt := range tests {\n")
	sb.WriteString("\t\tt.Run(tt.name, func(t *testing.T) {\n")
	sb.WriteString("\t\t\t// TODO: implement\n")
	sb.WriteString("\t\t})\n")
	sb.WriteString("\t}\n")
	sb.WriteString("}\n")

	return sb.String()
}

// FormatReport produces a human-readable coverage report with visual bars.
func FormatReport(report *CoverageReport) string {
	if report == nil {
		return "No coverage data available.\n"
	}

	var sb strings.Builder

	sb.WriteString("Test Coverage Report:\n")
	sb.WriteString("════════════════════════════════\n")
	sb.WriteString(fmt.Sprintf("Overall: %.1f%% (%s/%s lines)\n\n",
		report.Percentage,
		formatNumber(report.CoveredLines),
		formatNumber(report.TotalLines)))

	if len(report.Files) > 0 {
		sb.WriteString("By file:\n")

		// Find the longest path for alignment.
		maxPathLen := 0
		for _, fc := range report.Files {
			if len(fc.Path) > maxPathLen {
				maxPathLen = len(fc.Path)
			}
		}
		if maxPathLen > 40 {
			maxPathLen = 40
		}

		for _, fc := range report.Files {
			path := fc.Path
			if len(path) > 40 {
				tail := path[len(path)-37:]
				for len(tail) > 0 && !utf8.ValidString(tail) {
					tail = tail[1:]
				}
				path = "..." + tail
			}

			bar := renderBar(fc.Percentage, 20)
			status := ""
			if fc.Percentage == 0 {
				status = "  " + icons.CloseThick()
			} else if fc.Percentage < 50 {
				status = "  " + icons.Alert()
			}

			sb.WriteString(fmt.Sprintf("  %-*s  %3.0f%%  %s%s\n",
				maxPathLen, path, fc.Percentage, bar, status))
		}
		sb.WriteString("\n")
	}

	if len(report.UncoveredFunctions) > 0 {
		sb.WriteString(fmt.Sprintf("Uncovered functions (%d):\n", len(report.UncoveredFunctions)))
		for _, fn := range report.UncoveredFunctions {
			sb.WriteString(fmt.Sprintf("  %s\n", fn))
		}
		sb.WriteString("\n")
	}

	if len(report.Suggestions) > 0 {
		sb.WriteString("Suggestions:\n")
		for i, s := range report.Suggestions {
			sb.WriteString(fmt.Sprintf("  %d. [%s] Test%s — %s\n",
				i+1, s.Priority, extractBaseName(s.Function), s.Reason))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// renderBar creates a visual progress bar of the given width.
func renderBar(percentage float64, width int) string {
	filled := int(percentage / 100 * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}

	var sb strings.Builder
	for i := 0; i < filled; i++ {
		sb.WriteRune('█')
	}
	for i := filled; i < width; i++ {
		sb.WriteRune('░')
	}
	return sb.String()
}

// extractBaseName gets the simple function name from a potentially qualified name.
func extractBaseName(name string) string {
	if dotIdx := strings.LastIndex(name, "."); dotIdx >= 0 {
		return name[dotIdx+1:]
	}
	return name
}

// formatNumber adds commas to an integer for readability.
func formatNumber(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}

	var result strings.Builder
	remainder := len(s) % 3
	if remainder > 0 {
		result.WriteString(s[:remainder])
	}
	for i := remainder; i < len(s); i += 3 {
		if result.Len() > 0 {
			result.WriteByte(',')
		}
		result.WriteString(s[i : i+3])
	}
	return result.String()
}

// DeltaCoverage computes and formats the difference between two coverage reports.
func DeltaCoverage(before, after *CoverageReport) string {
	if before == nil || after == nil {
		return "Cannot compute delta: missing report data.\n"
	}

	var sb strings.Builder
	sb.WriteString("Coverage Delta:\n")
	sb.WriteString("════════════════════════════════\n")

	delta := after.Percentage - before.Percentage
	direction := "▲"
	if delta < 0 {
		direction = "▼"
	} else if delta == 0 {
		direction = "◆"
	}

	sb.WriteString(fmt.Sprintf("Overall: %.1f%% -> %.1f%% (%s%.1f%%)\n",
		before.Percentage, after.Percentage, direction, delta))
	sb.WriteString(fmt.Sprintf("Lines:   %s/%s -> %s/%s\n\n",
		formatNumber(before.CoveredLines), formatNumber(before.TotalLines),
		formatNumber(after.CoveredLines), formatNumber(after.TotalLines)))

	// Show per-file changes.
	beforeFiles := make(map[string]float64)
	for _, fc := range before.Files {
		beforeFiles[fc.Path] = fc.Percentage
	}

	var changes []string
	for _, fc := range after.Files {
		prev, existed := beforeFiles[fc.Path]
		if !existed {
			changes = append(changes, fmt.Sprintf("  + %-40s  %3.0f%% (new)\n", fc.Path, fc.Percentage))
		} else if fc.Percentage != prev {
			fileDelta := fc.Percentage - prev
			dir := "▲"
			if fileDelta < 0 {
				dir = "▼"
			}
			changes = append(changes, fmt.Sprintf("  %s %-40s  %.0f%% -> %.0f%% (%+.1f%%)\n",
				dir, fc.Path, prev, fc.Percentage, fileDelta))
		}
	}

	if len(changes) > 0 {
		sb.WriteString("Changed files:\n")
		for _, c := range changes {
			sb.WriteString(c)
		}
	} else {
		sb.WriteString("No per-file changes detected.\n")
	}

	return sb.String()
}
