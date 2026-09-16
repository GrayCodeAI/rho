package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/GrayCodeAI/rho/internal/gitcmd"
)

// ImpactTool provides cross-file impact analysis for changed files.
// It combines import graph traversal, co-change history, and risk scoring.
type ImpactTool struct{}

func (ImpactTool) Name() string      { return "Impact" }
func (ImpactTool) RiskLevel() string { return "low" }
func (ImpactTool) Aliases() []string { return []string{"impact", "blast-radius"} }
func (ImpactTool) Description() string {
	return "Analyze cross-file impact of changes: dependencies, dependents, co-change patterns, risk score, and test suggestions."
}

// ImpactInput is the typed input for ImpactTool.
type ImpactInput struct {
	Files []string `json:"files"`
	Depth int      `json:"depth"`
	Root  string   `json:"root"`
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (ImpactTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"files": {Type: "array", Description: "Files to analyze impact for (e.g. changed files). If empty, uses git diff.", Items: &SchemaProperty{Type: "string"}},
			"depth": {Type: "integer", Description: "Max traversal depth for dependency analysis (default: 3)"},
			"root":  {Type: "string", Description: "Project root directory (default: current dir)"},
		},
	}
}

func (ImpactTool) Parameters() map[string]interface{} {
	return impactSchema.ToJSONSchema()
}

// impactSchema is the single source of truth for Impact's input schema.
var impactSchema = ImpactTool{}.Schema()

func (ImpactTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[ImpactInput]("Impact", input)
	if err != nil {
		return "", err
	}

	root := p.Root
	if root == "" {
		root = "."
	}
	if err := validatePathAllowed(ctx, root); err != nil {
		return "", err
	}
	if p.Depth <= 0 {
		p.Depth = 3
	}

	// If no files specified, get from git diff
	if len(p.Files) == 0 {
		files, err := gitDiffFiles(ctx, root)
		if err != nil {
			return "", fmt.Errorf("impact: failed to get git diff: %w", err)
		}
		p.Files = files
	}

	if len(p.Files) == 0 {
		return "No changed files found. Specify files or run from a git repository with changes.", nil
	}

	// Build import graph
	importGraph, err := buildSimpleImportGraph(root)
	if err != nil {
		// Non-fatal: continue without import graph
		importGraph = &simpleImportGraph{edges: make(map[string][]string), reverse: make(map[string][]string)}
	}

	// Build co-change analysis
	coChange, err := buildCoChangeAnalysis(ctx, root, 100)
	if err != nil {
		coChange = &simpleCoChange{pairs: make(map[string]map[string]int)}
	}

	// Analyze each file
	analysis := &ImpactAnalysis{
		ChangedFiles: p.Files,
		Depth:        p.Depth,
		Impacts:      make(map[string]*FileImpact),
	}

	allAffected := make(map[string]bool)
	for _, f := range p.Files {
		allAffected[f] = true
		fi := analyzeFileImpact(f, p.Depth, importGraph, coChange)
		analysis.Impacts[f] = fi
		for _, dep := range fi.Dependents {
			allAffected[dep] = true
		}
		for _, dep := range fi.Dependencies {
			allAffected[dep] = true
		}
		for _, co := range fi.CoChanged {
			allAffected[co] = true
		}
	}

	// Collect all affected files (excluding the changed files themselves)
	for _, f := range p.Files {
		delete(allAffected, f)
	}
	analysis.AllAffected = mapKeys(allAffected)

	// Find test files
	analysis.TestFiles = findRelatedTests(root, p.Files, analysis.AllAffected)

	// Compute risk score
	analysis.RiskScore = computeRiskScore(analysis)

	// Generate suggestions
	analysis.Suggestions = generateSuggestions(analysis)

	return formatImpactReport(analysis), nil
}

// ImpactAnalysis holds the full impact analysis result.
type ImpactAnalysis struct {
	ChangedFiles []string               `json:"changed_files"`
	AllAffected  []string               `json:"all_affected"`
	TestFiles    []string               `json:"test_files"`
	Depth        int                    `json:"depth"`
	Impacts      map[string]*FileImpact `json:"impacts"`
	RiskScore    int                    `json:"risk_score"`
	Suggestions  []string               `json:"suggestions"`
}

// FileImpact holds impact data for a single file.
type FileImpact struct {
	File         string   `json:"file"`
	Dependents   []string `json:"dependents"`   // files that import this file
	Dependencies []string `json:"dependencies"` // files this file imports
	CoChanged    []string `json:"co_changed"`   // files that frequently change with this file
}

// simpleImportGraph is a lightweight import graph for the impact tool.
type simpleImportGraph struct {
	edges   map[string][]string // file -> imported files
	reverse map[string][]string // file -> files that import it
}

// simpleCoChange tracks co-change patterns.
type simpleCoChange struct {
	pairs map[string]map[string]int // fileA -> fileB -> count
}

func analyzeFileImpact(file string, depth int, graph *simpleImportGraph, coChange *simpleCoChange) *FileImpact {
	fi := &FileImpact{File: file}

	// BFS for dependents (files that import this file)
	visited := make(map[string]bool)
	queue := []string{file}
	for d := 0; d < depth && len(queue) > 0; d++ {
		var next []string
		for _, f := range queue {
			if visited[f] {
				continue
			}
			visited[f] = true
			for _, dep := range graph.reverse[f] {
				if !visited[dep] && dep != file {
					fi.Dependents = append(fi.Dependents, dep)
					next = append(next, dep)
				}
			}
		}
		queue = next
	}

	// BFS for dependencies (files this file imports)
	visited = make(map[string]bool)
	queue = []string{file}
	for d := 0; d < depth && len(queue) > 0; d++ {
		var next []string
		for _, f := range queue {
			if visited[f] {
				continue
			}
			visited[f] = true
			for _, dep := range graph.edges[f] {
				if !visited[dep] && dep != file {
					fi.Dependencies = append(fi.Dependencies, dep)
					next = append(next, dep)
				}
			}
		}
		queue = next
	}

	// Co-change files
	if coChange.pairs[file] != nil {
		type scored struct {
			path  string
			count int
		}
		var candidates []scored
		for path, count := range coChange.pairs[file] {
			if count >= 2 { // at least 2 co-occurrences
				candidates = append(candidates, scored{path, count})
			}
		}
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].count > candidates[j].count
		})
		limit := 10
		if limit > len(candidates) {
			limit = len(candidates)
		}
		for i := 0; i < limit; i++ {
			fi.CoChanged = append(fi.CoChanged, candidates[i].path)
		}
	}

	return fi
}

func computeRiskScore(analysis *ImpactAnalysis) int {
	score := 0

	// Factor 1: Number of affected files
	affected := len(analysis.AllAffected)
	switch {
	case affected > 20:
		score += 40
	case affected > 10:
		score += 30
	case affected > 5:
		score += 20
	case affected > 0:
		score += 10
	}

	// Factor 2: Number of changed files
	changed := len(analysis.ChangedFiles)
	score += minInt(changed*5, 20)

	// Factor 3: Test coverage
	testCount := len(analysis.TestFiles)
	if testCount == 0 && affected > 0 {
		score += 20 // no tests found for affected code
	} else if testCount > 0 {
		score -= minInt(testCount*2, 10) // reduce risk if tests exist
	}

	// Factor 4: Deep dependencies (depth > 1)
	deepDeps := 0
	for _, fi := range analysis.Impacts {
		for _, dep := range fi.Dependents {
			if !stringContains(analysis.ChangedFiles, dep) {
				deepDeps++
			}
		}
	}
	if deepDeps > 5 {
		score += 15
	}

	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

func generateSuggestions(analysis *ImpactAnalysis) []string {
	var suggestions []string

	if len(analysis.TestFiles) > 0 {
		suggestions = append(suggestions, fmt.Sprintf("Run tests: %s", strings.Join(analysis.TestFiles[:minInt(3, len(analysis.TestFiles))], ", ")))
	}

	if analysis.RiskScore > 60 {
		suggestions = append(suggestions, "HIGH RISK: Consider breaking this change into smaller PRs")
	}

	if len(analysis.AllAffected) > 10 {
		suggestions = append(suggestions, fmt.Sprintf("Large blast radius (%d files affected). Review dependency chain carefully.", len(analysis.AllAffected)))
	}

	// Check for files with many dependents (high fan-in)
	for _, fi := range analysis.Impacts {
		if len(fi.Dependents) > 5 {
			suggestions = append(suggestions, fmt.Sprintf("%s has %d dependents — changes here affect many files", fi.File, len(fi.Dependents)))
		}
	}

	if len(analysis.TestFiles) == 0 && len(analysis.AllAffected) > 0 {
		suggestions = append(suggestions, "No related test files found. Consider adding tests for affected code.")
	}

	return suggestions
}

func formatImpactReport(analysis *ImpactAnalysis) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("## Impact Analysis (%d changed files)\n\n", len(analysis.ChangedFiles)))

	// Risk score
	riskLabel := "LOW"
	riskColor := "LOW"
	if analysis.RiskScore > 60 {
		riskLabel = "HIGH"
		riskColor = "CRIT"
	} else if analysis.RiskScore > 30 {
		riskLabel = "MEDIUM"
		riskColor = "MED"
	}
	b.WriteString(fmt.Sprintf("**Risk Score:** %s %d/100 (%s)\n\n", riskColor, analysis.RiskScore, riskLabel))

	// Changed files
	b.WriteString("### Changed Files\n")
	for _, f := range analysis.ChangedFiles {
		b.WriteString(fmt.Sprintf("- `%s`\n", f))
	}
	b.WriteString("\n")

	// Affected files
	if len(analysis.AllAffected) > 0 {
		b.WriteString(fmt.Sprintf("### Affected Files (%d)\n", len(analysis.AllAffected)))
		limit := 20
		if limit > len(analysis.AllAffected) {
			limit = len(analysis.AllAffected)
		}
		for _, f := range analysis.AllAffected[:limit] {
			b.WriteString(fmt.Sprintf("- `%s`\n", f))
		}
		if len(analysis.AllAffected) > 20 {
			b.WriteString(fmt.Sprintf("- ... and %d more\n", len(analysis.AllAffected)-20))
		}
		b.WriteString("\n")
	}

	// Per-file details
	b.WriteString("### Per-File Impact\n")
	for _, fi := range analysis.Impacts {
		b.WriteString(fmt.Sprintf("\n**`%s`**\n", fi.File))
		if len(fi.Dependents) > 0 {
			b.WriteString(fmt.Sprintf("  Dependents (%d): %s\n", len(fi.Dependents), strings.Join(fi.Dependents[:minInt(5, len(fi.Dependents))], ", ")))
		}
		if len(fi.Dependencies) > 0 {
			b.WriteString(fmt.Sprintf("  Dependencies (%d): %s\n", len(fi.Dependencies), strings.Join(fi.Dependencies[:minInt(5, len(fi.Dependencies))], ", ")))
		}
		if len(fi.CoChanged) > 0 {
			b.WriteString(fmt.Sprintf("  Co-changed (%d): %s\n", len(fi.CoChanged), strings.Join(fi.CoChanged[:minInt(5, len(fi.CoChanged))], ", ")))
		}
	}

	// Test files
	if len(analysis.TestFiles) > 0 {
		b.WriteString("\n### Related Tests\n")
		for _, f := range analysis.TestFiles {
			b.WriteString(fmt.Sprintf("- `%s`\n", f))
		}
	}

	// Suggestions
	if len(analysis.Suggestions) > 0 {
		b.WriteString("\n### Suggestions\n")
		for _, s := range analysis.Suggestions {
			b.WriteString(fmt.Sprintf("- %s\n", s))
		}
	}

	return b.String()
}

// ── Helpers ──

func gitDiffFiles(ctx context.Context, root string) ([]string, error) {
	// Try staged + unstaged
	cmd := gitcmd.Command(ctx, "diff", "--name-only", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		// Try initial commit
		cmd = gitcmd.Command(ctx, "diff", "--name-only", "--cached")
		cmd.Dir = root
		out, err = cmd.Output()
		if err != nil {
			return nil, err
		}
	}
	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func buildSimpleImportGraph(root string) (*simpleImportGraph, error) {
	g := &simpleImportGraph{
		edges:   make(map[string][]string),
		reverse: make(map[string][]string),
	}

	rootAbs, err := guardedAbs(root)
	if err != nil {
		return nil, err
	}
	rootHandle, err := os.OpenRoot(rootAbs)
	if err != nil {
		return nil, fmt.Errorf("open impact root: %w", err)
	}
	defer func() { _ = rootHandle.Close() }()

	err = fs.WalkDir(rootHandle.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".go" && ext != ".py" && ext != ".ts" && ext != ".js" {
			return nil
		}

		data, err := fs.ReadFile(rootHandle.FS(), path)
		if err != nil {
			return nil
		}

		relPath := filepath.FromSlash(path)
		imports := parseImports(string(data), ext)
		for _, imp := range imports {
			// Try to resolve to local file
			resolved := resolveImport(imp, ext, filepath.Dir(filepath.Join(rootAbs, filepath.FromSlash(path))), rootAbs)
			if resolved != "" {
				g.edges[relPath] = append(g.edges[relPath], resolved)
				g.reverse[resolved] = append(g.reverse[resolved], relPath)
			}
		}
		return nil
	})

	return g, err
}

func parseImports(src string, ext string) []string {
	var imports []string
	switch ext {
	case ".go":
		inBlock := false
		for _, line := range strings.Split(src, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "import (") {
				inBlock = true
				continue
			}
			if inBlock && line == ")" {
				inBlock = false
				continue
			}
			if inBlock || strings.HasPrefix(line, "import ") {
				// Extract quoted path
				if idx := strings.Index(line, "\""); idx >= 0 {
					end := strings.Index(line[idx+1:], "\"")
					if end >= 0 {
						imports = append(imports, line[idx+1:idx+1+end])
					}
				}
			}
		}
	case ".py":
		for _, line := range strings.Split(src, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "from ") && strings.Contains(line, " import ") {
				parts := strings.SplitN(line, " import ", 2)
				if len(parts) == 2 {
					mod := strings.TrimPrefix(parts[0], "from ")
					mod = strings.TrimSpace(mod)
					if !strings.Contains(mod, ".") || strings.HasPrefix(mod, ".") {
						imports = append(imports, mod)
					}
				}
			} else if strings.HasPrefix(line, "import ") {
				mod := strings.TrimPrefix(line, "import ")
				mod = strings.TrimSpace(mod)
				if idx := strings.Index(mod, " "); idx > 0 {
					mod = mod[:idx]
				}
				imports = append(imports, mod)
			}
		}
	case ".ts", ".js":
		for _, line := range strings.Split(src, "\n") {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "from ") {
				if idx := strings.Index(line, "from \""); idx >= 0 {
					end := strings.Index(line[idx+6:], "\"")
					if end >= 0 {
						imports = append(imports, line[idx+6:idx+6+end])
					}
				} else if idx := strings.Index(line, "from '"); idx >= 0 {
					end := strings.Index(line[idx+6:], "'")
					if end >= 0 {
						imports = append(imports, line[idx+6:idx+6+end])
					}
				}
			}
		}
	}
	return imports
}

func resolveImport(imp, ext, dir, root string) string {
	switch ext {
	case ".go":
		// Internal package import
		if strings.Contains(imp, "/") {
			// Try to find the package directory
			parts := strings.Split(imp, "/")
			pkgName := parts[len(parts)-1]
			candidate := filepath.Join(root, filepath.Join(parts...))
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				// Find a .go file in the package
				entries, _ := os.ReadDir(candidate)
				for _, e := range entries {
					if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
						rel, _ := filepath.Rel(root, filepath.Join(candidate, e.Name()))
						return rel
					}
				}
			}
			_ = pkgName
		}
	case ".py":
		if strings.HasPrefix(imp, ".") {
			// Relative import
			modPath := strings.ReplaceAll(strings.TrimPrefix(imp, "."), ".", "/")
			candidate := filepath.Join(dir, modPath)
			if info, err := os.Stat(candidate + ".py"); err == nil && !info.IsDir() {
				rel, _ := filepath.Rel(root, candidate+".py")
				return rel
			}
			if info, err := os.Stat(filepath.Join(candidate, "__init__.py")); err == nil && !info.IsDir() {
				rel, _ := filepath.Rel(root, filepath.Join(candidate, "__init__.py"))
				return rel
			}
		}
	case ".ts", ".js":
		if strings.HasPrefix(imp, ".") {
			// Relative import
			candidate := filepath.Join(dir, imp)
			// Try with extensions
			for _, ext := range []string{".ts", ".tsx", ".js", ".jsx"} {
				if info, err := os.Stat(candidate + ext); err == nil && !info.IsDir() {
					rel, _ := filepath.Rel(root, candidate+ext)
					return rel
				}
			}
			// Try index files
			for _, ext := range []string{".ts", ".tsx", ".js", ".jsx"} {
				idx := filepath.Join(candidate, "index"+ext)
				if info, err := os.Stat(idx); err == nil && !info.IsDir() {
					rel, _ := filepath.Rel(root, idx)
					return rel
				}
			}
		}
	}
	return ""
}

func buildCoChangeAnalysis(ctx context.Context, root string, commitLimit int) (*simpleCoChange, error) {
	cmd := gitcmd.Command(ctx, "log", "--name-only", "--pretty=format:", fmt.Sprintf("-%d", commitLimit)) // #nosec G204 -- git subcommand invocation with fixed subcommand and internally-derived args
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return &simpleCoChange{pairs: make(map[string]map[string]int)}, nil
	}

	cc := &simpleCoChange{pairs: make(map[string]map[string]int)}

	// Parse commits separated by blank lines
	var currentCommit []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			if len(currentCommit) >= 2 {
				for i := 0; i < len(currentCommit); i++ {
					for j := i + 1; j < len(currentCommit); j++ {
						cc.record(currentCommit[i], currentCommit[j])
					}
				}
			}
			currentCommit = nil
		} else {
			currentCommit = append(currentCommit, line)
		}
	}
	// Handle last commit
	if len(currentCommit) >= 2 {
		for i := 0; i < len(currentCommit); i++ {
			for j := i + 1; j < len(currentCommit); j++ {
				cc.record(currentCommit[i], currentCommit[j])
			}
		}
	}

	return cc, nil
}

func (cc *simpleCoChange) record(a, b string) {
	if cc.pairs[a] == nil {
		cc.pairs[a] = make(map[string]int)
	}
	if cc.pairs[b] == nil {
		cc.pairs[b] = make(map[string]int)
	}
	cc.pairs[a][b]++
	cc.pairs[b][a]++
}

func findRelatedTests(root string, changed, affected []string) []string {
	testFiles := make(map[string]bool)
	allFiles := append(changed, affected...)

	for _, f := range allFiles {
		dir := filepath.Dir(f)
		base := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))

		// Go: look for _test.go files
		if strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, "_test.go") {
			testFile := filepath.Join(dir, base+"_test.go")
			if _, err := os.Stat(filepath.Join(root, testFile)); err == nil {
				testFiles[testFile] = true
			}
			// Also check for any _test.go in the same package
			entries, _ := os.ReadDir(filepath.Join(root, dir))
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), "_test.go") {
					testFiles[filepath.Join(dir, e.Name())] = true
				}
			}
		}

		// Python: look for test_*.py or *_test.py
		if strings.HasSuffix(f, ".py") {
			testFile1 := filepath.Join(dir, "test_"+base+".py")
			testFile2 := filepath.Join(dir, base+"_test.py")
			if _, err := os.Stat(filepath.Join(root, testFile1)); err == nil {
				testFiles[testFile1] = true
			}
			if _, err := os.Stat(filepath.Join(root, testFile2)); err == nil {
				testFiles[testFile2] = true
			}
		}

		// JS/TS: look for *.test.ts, *.spec.ts, __tests__/
		if strings.HasSuffix(f, ".ts") || strings.HasSuffix(f, ".js") || strings.HasSuffix(f, ".tsx") || strings.HasSuffix(f, ".jsx") {
			ext := filepath.Ext(f)
			testFile1 := filepath.Join(dir, base+".test"+ext)
			testFile2 := filepath.Join(dir, base+".spec"+ext)
			if _, err := os.Stat(filepath.Join(root, testFile1)); err == nil {
				testFiles[testFile1] = true
			}
			if _, err := os.Stat(filepath.Join(root, testFile2)); err == nil {
				testFiles[testFile2] = true
			}
			// Check __tests__ directory
			testsDir := filepath.Join(dir, "__tests__")
			if entries, err := os.ReadDir(filepath.Join(root, testsDir)); err == nil {
				for _, e := range entries {
					if strings.Contains(e.Name(), base) {
						testFiles[filepath.Join("__tests__", e.Name())] = true
					}
				}
			}
		}
	}

	var result []string
	for f := range testFiles {
		result = append(result, f)
	}
	sort.Strings(result)
	return result
}

func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func stringContains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
