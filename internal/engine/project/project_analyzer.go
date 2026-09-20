package project

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ProjectAnalyzer performs deep analysis of a codebase. This file holds the
// analyzer type, the top-level Analyze orchestration, per-module analysis, and
// the detection methods. Quantitative metrics live in project_metrics.go,
// architecture/pattern detection in project_patterns.go, and the report
// formatters in project_report.go.

// ProjectAnalysis holds the full analysis of a project's architecture, patterns, and conventions.
type ProjectAnalysis struct {
	Name         string
	Language     string
	Framework    string
	Architecture string
	EntryPoints  []string
	KeyModules   []ModuleInfo
	Patterns     []Pattern
	Conventions  []string
	Dependencies int
	TestCoverage string
	LOC          int
	Complexity   string
}

// ModuleInfo describes a single module/package within the project.
type ModuleInfo struct {
	Name         string
	Path         string
	Purpose      string
	PublicAPI    []string
	Dependencies []string
	Size         int
}

// Pattern represents an architectural or design pattern detected in the codebase.
type Pattern struct {
	Name        string
	Description string
	Files       []string
	Confidence  float64
}

// ProjectAnalyzer performs deep analysis of a codebase to understand its architecture,
// patterns, and conventions. Inspired by gpt-pilot's importer agent.
type ProjectAnalyzer struct {
	Dir string
	mu  sync.Mutex
}

// NewProjectAnalyzer creates a new ProjectAnalyzer for the given directory.
func NewProjectAnalyzer(dir string) *ProjectAnalyzer {
	return &ProjectAnalyzer{Dir: dir}
}

// Analyze performs a full project scan: detects language/framework, maps architecture,
// identifies entry points, detects patterns, and extracts conventions.
func (pa *ProjectAnalyzer) Analyze() (*ProjectAnalysis, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	if _, err := os.Stat(pa.Dir); os.IsNotExist(err) {
		return nil, fmt.Errorf("project directory does not exist: %s", pa.Dir)
	}

	analysis := &ProjectAnalysis{}

	// Detect project name from go.mod or directory name.
	analysis.Name = pa.detectProjectName()

	// Detect language and framework.
	analysis.Language = pa.detectLanguage()
	analysis.Framework = pa.detectFramework()

	// Detect architecture.
	analysis.Architecture = DetectArchitecture(pa.Dir)

	// Find entry points.
	analysis.EntryPoints = pa.detectEntryPoints()

	// Analyze key modules.
	analysis.KeyModules = pa.analyzeKeyModules()

	// Detect patterns.
	analysis.Patterns = DetectPatterns(pa.Dir)

	// Extract conventions.
	analysis.Conventions = pa.detectConventions()

	// Count dependencies.
	analysis.Dependencies = pa.countDependencies()

	// Calculate LOC.
	analysis.LOC = pa.countLOC()

	// Assess test coverage.
	analysis.TestCoverage = pa.assessTestCoverage()

	// Assess complexity.
	analysis.Complexity = pa.assessComplexity()

	return analysis, nil
}

// AnalyzeModule scans a package directory and extracts its public API, line count, and purpose.
func (pa *ProjectAnalyzer) AnalyzeModule(path string) *ModuleInfo {
	info := &ModuleInfo{
		Name: filepath.Base(path),
		Path: path,
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return info
	}

	fset := token.NewFileSet()
	var totalLines int

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		filePath := filepath.Join(path, entry.Name())

		// Count lines.
		totalLines += countFileLines(filePath)

		// Parse for public API.
		f, parseErr := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
		if parseErr != nil {
			continue
		}

		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name.IsExported() {
					sig := d.Name.Name
					if d.Recv != nil && len(d.Recv.List) > 0 {
						recvType := projAnalyzerExprToString(d.Recv.List[0].Type)
						sig = recvType + "." + sig
					}
					info.PublicAPI = append(info.PublicAPI, sig)
				}
			case *ast.GenDecl:
				if d.Tok == token.TYPE {
					for _, spec := range d.Specs {
						if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.IsExported() {
							info.PublicAPI = append(info.PublicAPI, ts.Name.Name)
						}
					}
				}
			}
		}

		// Extract imports for dependencies.
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if !strings.Contains(importPath, ".") {
				continue // skip stdlib
			}
			info.Dependencies = projAnalyzerAppendUnique(info.Dependencies, importPath)
		}
	}

	info.Size = totalLines
	info.Purpose = pa.inferPurpose(info)

	return info
}

// --- Detection methods ---

func (pa *ProjectAnalyzer) detectProjectName() string {
	// Try go.mod first.
	modPath := filepath.Join(pa.Dir, "go.mod")
	// #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if f, err := os.Open(modPath); err == nil {
		defer func() { _ = f.Close() }()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "module ") {
				mod := strings.TrimSpace(strings.TrimPrefix(line, "module"))
				parts := strings.Split(mod, "/")
				return parts[len(parts)-1]
			}
		}
	}

	// Fall back to directory name.
	return filepath.Base(pa.Dir)
}

func (pa *ProjectAnalyzer) detectLanguage() string {
	counts := map[string]int{
		"Go":         0,
		"Python":     0,
		"JavaScript": 0,
		"TypeScript": 0,
		"Rust":       0,
		"Java":       0,
	}

	_ = filepath.WalkDir(pa.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := filepath.Base(path)
			if base == "vendor" || base == "node_modules" || base == ".git" || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(path)
		switch ext {
		case ".go":
			counts["Go"]++
		case ".py":
			counts["Python"]++
		case ".js":
			counts["JavaScript"]++
		case ".ts", ".tsx":
			counts["TypeScript"]++
		case ".rs":
			counts["Rust"]++
		case ".java":
			counts["Java"]++
		}
		return nil
	})

	maxLang := "Go"
	maxCount := 0
	for lang, count := range counts {
		if count > maxCount {
			maxCount = count
			maxLang = lang
		}
	}

	if maxCount == 0 {
		return "unknown"
	}
	return maxLang
}

func (pa *ProjectAnalyzer) detectFramework() string {
	// Check go.mod for known frameworks.
	modPath := filepath.Join(pa.Dir, "go.mod")
	data, err := os.ReadFile(modPath) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return ""
	}
	content := string(data)

	frameworks := map[string]string{
		"github.com/gin-gonic/gin": "Gin",
		"github.com/labstack/echo": "Echo",
		"github.com/gorilla/mux":   "Gorilla",
		"github.com/go-chi/chi":    "Chi",
		"github.com/gofiber/fiber": "Fiber",
		"github.com/spf13/cobra":   "Cobra CLI",
		"github.com/urfave/cli":    "urfave/cli",
		"google.golang.org/grpc":   "gRPC",
		"charm.land/bubbletea/v2":  "Bubbletea TUI",
	}

	var detected []string
	for dep, name := range frameworks {
		if strings.Contains(content, dep) {
			detected = append(detected, name)
		}
	}

	if len(detected) == 0 {
		return ""
	}
	sort.Strings(detected)
	return strings.Join(detected, " + ")
}

func (pa *ProjectAnalyzer) detectEntryPoints() []string {
	var entryPoints []string

	// Look for main packages in cmd/ directory.
	cmdDir := filepath.Join(pa.Dir, "cmd")
	if entries, err := os.ReadDir(cmdDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				mainFile := filepath.Join(cmdDir, entry.Name(), "main.go")
				if _, statErr := os.Stat(mainFile); statErr == nil {
					entryPoints = append(entryPoints, "cmd/"+entry.Name()+"/main.go")
				}
			}
		}
	}

	// Check root for main.go.
	rootMain := filepath.Join(pa.Dir, "main.go")
	if _, err := os.Stat(rootMain); err == nil {
		entryPoints = append(entryPoints, "main.go")
	}

	// Look for root-level cmd entry (Cobra pattern).
	rootCmd := filepath.Join(pa.Dir, "cmd", "root.go")
	if _, err := os.Stat(rootCmd); err == nil {
		entryPoints = append(entryPoints, "cmd/root.go")
	}

	return entryPoints
}

func (pa *ProjectAnalyzer) analyzeKeyModules() []ModuleInfo {
	var modules []ModuleInfo

	entries, err := os.ReadDir(pa.Dir)
	if err != nil {
		return modules
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "testdata" {
			continue
		}

		subPath := filepath.Join(pa.Dir, name)
		if !hasGoFiles(subPath) {
			continue
		}

		info := pa.AnalyzeModule(subPath)
		if info.Size > 0 {
			modules = append(modules, *info)
		}
	}

	// Sort by size descending.
	sort.Slice(modules, func(i, j int) bool {
		return modules[i].Size > modules[j].Size
	})

	// Keep top 10 modules.
	if len(modules) > 10 {
		modules = modules[:10]
	}

	return modules
}

func (pa *ProjectAnalyzer) detectConventions() []string {
	var conventions []string

	// Check for error wrapping convention.
	if pa.hasPatternInFiles("%w") {
		conventions = append(conventions, "Error wrapping with %w")
	}

	// Check for table-driven tests.
	if pa.hasPatternInTestFiles("tests := []struct") || pa.hasPatternInTestFiles("testCases := []struct") || pa.hasPatternInTestFiles("cases := []struct") {
		conventions = append(conventions, "Table-driven tests")
	}

	// Check for Cobra CLI.
	if pa.hasPatternInFiles("cobra.Command") {
		conventions = append(conventions, "Cobra for CLI commands")
	}

	// Check for context usage.
	if pa.hasPatternInFiles("context.Context") {
		conventions = append(conventions, "Context propagation")
	}

	// Check for interface-first design.
	interfaceCount := pa.countInterfaces()
	if interfaceCount >= 5 {
		conventions = append(conventions, fmt.Sprintf("Interface-first design (%d interfaces)", interfaceCount))
	}

	// Check for structured logging.
	if pa.hasPatternInFiles("slog.") || pa.hasPatternInFiles("zap.") || pa.hasPatternInFiles("logrus.") {
		conventions = append(conventions, "Structured logging")
	}

	// Check for goroutine + sync patterns.
	if pa.hasPatternInFiles("sync.Mutex") || pa.hasPatternInFiles("sync.RWMutex") {
		conventions = append(conventions, "Mutex-based concurrency")
	}

	// Check for channels.
	if pa.hasPatternInFiles("make(chan ") {
		conventions = append(conventions, "Channel-based communication")
	}

	return conventions
}

func (pa *ProjectAnalyzer) inferPurpose(info *ModuleInfo) string {
	name := strings.ToLower(info.Name)

	purposeMap := map[string]string{
		"cmd":         "CLI entry point and command definitions",
		"engine":      "Core agent loop and orchestration",
		"tool":        "Built-in tool implementations",
		"config":      "Configuration and settings management",
		"session":     "Session persistence and state management",
		"daemon":      "Background service and HTTP server",
		"permissions": "Permission and path safety",
		"memory":      "Persistent cross-session memory",
		"planner":     "Task decomposition and planning",
		"repomap":     "Code intelligence and file relevance",
		"hooks":       "Event-driven plugin system",
		"mcp":         "Model Context Protocol client",
		"permission":  "User approval and access control",
		"circuit":     "Circuit breaker pattern for resilience",
		"ratelimit":   "Rate limiting and throttling",
		"retry":       "Retry logic with backoff",
		"health":      "Diagnostics and health checks",
		"parallel":    "Parallel execution and worktrees",
		"mission":     "Multi-agent orchestration",
		"agents":      "Custom persona definitions",
		"internal":    "Private implementation details",
		"pkg":         "Shared utilities and helpers",
		"api":         "API endpoints and handlers",
		"model":       "Data models and types",
		"store":       "Data persistence layer",
		"service":     "Business logic layer",
		"handler":     "Request handlers",
		"middleware":  "Request/response middleware",
		"util":        "Utility functions",
		"common":      "Shared common code",
	}

	if purpose, ok := purposeMap[name]; ok {
		return purpose
	}

	// Try partial matching.
	for key, purpose := range purposeMap {
		if strings.Contains(name, key) {
			return purpose
		}
	}

	// Infer from public API names.
	if len(info.PublicAPI) > 0 {
		sample := strings.Join(info.PublicAPI[:projAnalyzerMin(3, len(info.PublicAPI))], ", ")
		return fmt.Sprintf("Provides: %s", sample)
	}

	return "Module functionality"
}

// --- Small AST/string helpers ---

func projAnalyzerExprToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return projAnalyzerExprToString(t.X)
	case *ast.SelectorExpr:
		return projAnalyzerExprToString(t.X) + "." + t.Sel.Name
	default:
		return "T"
	}
}

func projAnalyzerAppendUnique(slice []string, s string) []string {
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}

func projAnalyzerMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
