package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

// ParallelRunner executes test packages concurrently using a worker pool.
type ParallelRunner struct {
	MaxWorkers int
	Timeout    time.Duration
	Results    map[string]*PackageResult
	mu         sync.RWMutex
}

// PackageResult holds the test results for a single Go package.
type PackageResult struct {
	Package  string
	Passed   int
	Failed   int
	Skipped  int
	Duration time.Duration
	Output   string
	Tests    []TestCaseResult
	Error    string
}

// TestCaseResult represents the outcome of a single test case.
type TestCaseResult struct {
	Name     string
	Passed   bool
	Duration time.Duration
	Output   string
}

// RunnerResult aggregates test results from all packages.
type RunnerResult struct {
	Packages     []PackageResult
	TotalPassed  int
	TotalFailed  int
	TotalSkipped int
	Duration     time.Duration
	Parallel     int
}

// NewParallelRunner creates a ParallelRunner with the specified worker count.
// If workers <= 0, defaults to runtime.NumCPU().
func NewParallelRunner(workers int) *ParallelRunner {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	return &ParallelRunner{
		MaxWorkers: workers,
		Timeout:    5 * time.Minute,
		Results:    make(map[string]*PackageResult),
	}
}

// RunAll discovers all test packages under projectDir and runs them in parallel.
func (r *ParallelRunner) RunAll(ctx context.Context, projectDir string) (*RunnerResult, error) {
	packages, err := DiscoverTestPackages(projectDir)
	if err != nil {
		return nil, fmt.Errorf("discovering test packages: %w", err)
	}
	if len(packages) == 0 {
		return &RunnerResult{Parallel: r.MaxWorkers}, nil
	}
	return r.RunPackages(ctx, packages)
}

// RunPackages runs the specified packages concurrently using a worker pool.
func (r *ParallelRunner) RunPackages(ctx context.Context, packages []string) (*RunnerResult, error) {
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	jobs := make(chan string, len(packages))
	results := make(chan *PackageResult, len(packages))

	// Start workers.
	var wg sync.WaitGroup
	workerCount := r.MaxWorkers
	if workerCount > len(packages) {
		workerCount = len(packages)
	}
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for pkg := range jobs {
				res := r.RunSinglePackage(ctx, pkg)
				r.mu.Lock()
				r.Results[pkg] = res
				r.mu.Unlock()
				results <- res
			}
		}()
	}

	// Enqueue jobs.
	for _, pkg := range packages {
		jobs <- pkg
	}
	close(jobs)

	// Wait for all workers to finish then close results.
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results.
	var pkgResults []PackageResult
	for res := range results {
		pkgResults = append(pkgResults, *res)
	}

	// Sort by package name for deterministic output.
	sort.Slice(pkgResults, func(i, j int) bool {
		return pkgResults[i].Package < pkgResults[j].Package
	})

	// Aggregate totals.
	result := &RunnerResult{
		Packages: pkgResults,
		Duration: time.Since(start),
		Parallel: workerCount,
	}
	for _, pr := range pkgResults {
		result.TotalPassed += pr.Passed
		result.TotalFailed += pr.Failed
		result.TotalSkipped += pr.Skipped
	}

	return result, nil
}

// DiscoverTestPackages finds all directories containing _test.go files
// under projectDir and returns their relative import paths.
func DiscoverTestPackages(projectDir string) ([]string, error) {
	var packages []string
	seen := make(map[string]bool)

	err := filepath.WalkDir(projectDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err // propagate errors
		}
		// Skip hidden directories and vendor.
		name := d.Name()
		if d.IsDir() && (strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(name, "_test.go") {
			dir := filepath.Dir(path)
			if !seen[dir] {
				seen[dir] = true
				// Convert to a relative path prefixed with "./"
				rel, relErr := filepath.Rel(projectDir, dir)
				if relErr != nil {
					rel = dir
				}
				packages = append(packages, "./"+filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking project directory: %w", err)
	}

	sort.Strings(packages)
	return packages, nil
}

// RunSinglePackage runs `go test -v -json` for a single package and parses output.
func (r *ParallelRunner) RunSinglePackage(ctx context.Context, pkg string) *PackageResult {
	start := time.Now()
	result := &PackageResult{
		Package: pkg,
	}

	cmd := exec.CommandContext(ctx, "go", "test", "-v", "-json", pkg) // #nosec G204 -- fixed Go executable
	out, err := cmd.CombinedOutput()
	result.Duration = time.Since(start)
	result.Output = string(out)

	if ctx.Err() == context.DeadlineExceeded {
		result.Error = "test timed out"
		return result
	}

	// Parse JSON output for structured results.
	result.Tests = ParseTestJSON(result.Output)

	// Count pass/fail/skip from parsed tests.
	for _, tc := range result.Tests {
		if tc.Passed {
			result.Passed++
		} else if tc.Output == "SKIP" {
			result.Skipped++
		} else {
			result.Failed++
		}
	}

	// If we couldn't parse any tests but got an error, record it.
	if err != nil && len(result.Tests) == 0 {
		result.Error = err.Error()
	}

	return result
}

// testEvent represents a single JSON event from `go test -json`.
type testEvent struct {
	Time    time.Time `json:"Time"`
	Action  string    `json:"Action"`
	Package string    `json:"Package"`
	Test    string    `json:"Test"`
	Elapsed float64   `json:"Elapsed"`
	Output  string    `json:"Output"`
}

// ParseTestJSON parses `go test -json` output into structured test results.
func ParseTestJSON(output string) []TestCaseResult {
	type testState struct {
		name    string
		output  strings.Builder
		elapsed float64
		passed  bool
		skipped bool
		done    bool
	}

	tests := make(map[string]*testState)
	var order []string

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var ev testEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}

		// Only process test-level events (not package-level).
		if ev.Test == "" {
			continue
		}

		state, exists := tests[ev.Test]
		if !exists {
			state = &testState{name: ev.Test}
			tests[ev.Test] = state
			order = append(order, ev.Test)
		}

		switch ev.Action {
		case "output":
			state.output.WriteString(ev.Output)
		case "pass":
			state.passed = true
			state.elapsed = ev.Elapsed
			state.done = true
		case "fail":
			state.passed = false
			state.elapsed = ev.Elapsed
			state.done = true
		case "skip":
			state.skipped = true
			state.elapsed = ev.Elapsed
			state.done = true
		}
	}

	var results []TestCaseResult
	for _, name := range order {
		state := tests[name]
		if !state.done {
			continue
		}
		tc := TestCaseResult{
			Name:     state.name,
			Passed:   state.passed,
			Duration: time.Duration(state.elapsed * float64(time.Second)),
			Output:   strings.TrimSpace(state.output.String()),
		}
		if state.skipped {
			tc.Output = "SKIP"
		}
		results = append(results, tc)
	}

	return results
}

// FormatResult returns a human-readable summary of test results.
func FormatResult(result *RunnerResult) string {
	var b strings.Builder

	header := fmt.Sprintf("Test Results (%d packages, %d workers, %s):",
		len(result.Packages), result.Parallel, formatDuration(result.Duration))
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(strings.Repeat("═", 50))
	b.WriteString("\n")

	for _, pkg := range result.Packages {
		if pkg.Failed > 0 {
			b.WriteString(fmt.Sprintf(icons.CloseThick()+" %-20s %d passed, %d FAILED (%s)\n",
				pkg.Package, pkg.Passed, pkg.Failed, formatDuration(pkg.Duration)))
			// List failed test details.
			for _, tc := range pkg.Tests {
				if !tc.Passed && tc.Output != "SKIP" {
					// Extract first meaningful line from output.
					msg := firstOutputLine(tc.Output)
					b.WriteString(fmt.Sprintf("  - %s: %s\n", tc.Name, msg))
				}
			}
		} else if pkg.Error != "" {
			b.WriteString(fmt.Sprintf(icons.CloseThick()+" %-20s ERROR: %s\n", pkg.Package, pkg.Error))
		} else {
			skipped := ""
			if pkg.Skipped > 0 {
				skipped = fmt.Sprintf(", %d skipped", pkg.Skipped)
			}
			b.WriteString(fmt.Sprintf(icons.CheckBold()+" %-20s %d passed%s (%s)\n",
				pkg.Package, pkg.Passed, skipped, formatDuration(pkg.Duration)))
		}
	}

	b.WriteString(strings.Repeat("═", 50))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("Total: %d passed, %d failed, %d skipped\n",
		result.TotalPassed, result.TotalFailed, result.TotalSkipped))

	return b.String()
}

// GetFailed returns all failed test cases across all packages.
func (r *ParallelRunner) GetFailed() []TestCaseResult {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var failed []TestCaseResult
	for _, pkg := range r.Results {
		for _, tc := range pkg.Tests {
			if !tc.Passed && tc.Output != "SKIP" {
				failed = append(failed, tc)
			}
		}
	}
	return failed
}

// GetSlowest returns the n slowest test cases across all packages.
func (r *ParallelRunner) GetSlowest(n int) []TestCaseResult {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var all []TestCaseResult
	for _, pkg := range r.Results {
		all = append(all, pkg.Tests...)
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Duration > all[j].Duration
	})

	if n > len(all) {
		n = len(all)
	}
	return all[:n]
}

// firstOutputLine extracts the first non-empty meaningful line from test output.
func firstOutputLine(output string) string {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip common prefixes from go test output.
		if strings.HasPrefix(line, "=== RUN") || strings.HasPrefix(line, "--- FAIL") {
			continue
		}
		// Trim to a reasonable length.
		if len(line) > 80 {
			// Rune-safe truncation: never split a multibyte UTF-8 sequence.
			if runes := []rune(line); len(runes) > 80 {
				return string(runes[:77]) + "..."
			}
		}
		return line
	}
	return "test failed"
}
