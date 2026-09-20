package eval

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

func TestNewParallelRunner_DefaultWorkers(t *testing.T) {
	r := NewParallelRunner(0)
	if r.MaxWorkers != runtime.NumCPU() {
		t.Errorf("expected MaxWorkers=%d, got %d", runtime.NumCPU(), r.MaxWorkers)
	}
	if r.Timeout != 5*time.Minute {
		t.Errorf("expected Timeout=5m, got %v", r.Timeout)
	}
	if r.Results == nil {
		t.Error("expected Results map to be initialized")
	}
}

func TestNewParallelRunner_CustomWorkers(t *testing.T) {
	r := NewParallelRunner(4)
	if r.MaxWorkers != 4 {
		t.Errorf("expected MaxWorkers=4, got %d", r.MaxWorkers)
	}
}

func TestNewParallelRunner_NegativeWorkers(t *testing.T) {
	r := NewParallelRunner(-1)
	if r.MaxWorkers != runtime.NumCPU() {
		t.Errorf("expected MaxWorkers=%d for negative input, got %d", runtime.NumCPU(), r.MaxWorkers)
	}
}

func TestParseTestJSON_PassingTests(t *testing.T) {
	input := `{"Time":"2024-01-01T00:00:00Z","Action":"run","Package":"example","Test":"TestA"}
{"Time":"2024-01-01T00:00:00Z","Action":"output","Package":"example","Test":"TestA","Output":"=== RUN   TestA\n"}
{"Time":"2024-01-01T00:00:00Z","Action":"output","Package":"example","Test":"TestA","Output":"--- PASS: TestA (0.01s)\n"}
{"Time":"2024-01-01T00:00:01Z","Action":"pass","Package":"example","Test":"TestA","Elapsed":0.01}
{"Time":"2024-01-01T00:00:00Z","Action":"run","Package":"example","Test":"TestB"}
{"Time":"2024-01-01T00:00:00Z","Action":"output","Package":"example","Test":"TestB","Output":"=== RUN   TestB\n"}
{"Time":"2024-01-01T00:00:00Z","Action":"output","Package":"example","Test":"TestB","Output":"--- PASS: TestB (0.05s)\n"}
{"Time":"2024-01-01T00:00:01Z","Action":"pass","Package":"example","Test":"TestB","Elapsed":0.05}`

	results := ParseTestJSON(input)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Name != "TestA" {
		t.Errorf("expected first test name=TestA, got %s", results[0].Name)
	}
	if !results[0].Passed {
		t.Error("expected TestA to pass")
	}
	if results[0].Duration != 10*time.Millisecond {
		t.Errorf("expected TestA duration=10ms, got %v", results[0].Duration)
	}

	if results[1].Name != "TestB" {
		t.Errorf("expected second test name=TestB, got %s", results[1].Name)
	}
	if !results[1].Passed {
		t.Error("expected TestB to pass")
	}
}

func TestParseTestJSON_FailingTest(t *testing.T) {
	input := `{"Time":"2024-01-01T00:00:00Z","Action":"run","Package":"example","Test":"TestFail"}
{"Time":"2024-01-01T00:00:00Z","Action":"output","Package":"example","Test":"TestFail","Output":"=== RUN   TestFail\n"}
{"Time":"2024-01-01T00:00:00Z","Action":"output","Package":"example","Test":"TestFail","Output":"    expected 1, got 2\n"}
{"Time":"2024-01-01T00:00:00Z","Action":"output","Package":"example","Test":"TestFail","Output":"--- FAIL: TestFail (0.00s)\n"}
{"Time":"2024-01-01T00:00:01Z","Action":"fail","Package":"example","Test":"TestFail","Elapsed":0.0}`

	results := ParseTestJSON(input)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Passed {
		t.Error("expected TestFail to not pass")
	}
	if results[0].Name != "TestFail" {
		t.Errorf("expected name=TestFail, got %s", results[0].Name)
	}
}

func TestParseTestJSON_SkippedTest(t *testing.T) {
	input := `{"Time":"2024-01-01T00:00:00Z","Action":"run","Package":"example","Test":"TestSkip"}
{"Time":"2024-01-01T00:00:00Z","Action":"output","Package":"example","Test":"TestSkip","Output":"=== RUN   TestSkip\n"}
{"Time":"2024-01-01T00:00:00Z","Action":"output","Package":"example","Test":"TestSkip","Output":"    skipping in CI\n"}
{"Time":"2024-01-01T00:00:01Z","Action":"skip","Package":"example","Test":"TestSkip","Elapsed":0.0}`

	results := ParseTestJSON(input)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Output != "SKIP" {
		t.Errorf("expected skipped test output=SKIP, got %s", results[0].Output)
	}
}

func TestParseTestJSON_EmptyInput(t *testing.T) {
	results := ParseTestJSON("")
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(results))
	}
}

func TestParseTestJSON_InvalidJSON(t *testing.T) {
	input := "this is not json\nalso not json"
	results := ParseTestJSON(input)
	if len(results) != 0 {
		t.Errorf("expected 0 results for invalid JSON, got %d", len(results))
	}
}

func TestParseTestJSON_MixedResults(t *testing.T) {
	input := `{"Time":"2024-01-01T00:00:00Z","Action":"run","Package":"example","Test":"TestPass"}
{"Time":"2024-01-01T00:00:01Z","Action":"pass","Package":"example","Test":"TestPass","Elapsed":0.5}
{"Time":"2024-01-01T00:00:00Z","Action":"run","Package":"example","Test":"TestFail"}
{"Time":"2024-01-01T00:00:01Z","Action":"fail","Package":"example","Test":"TestFail","Elapsed":1.2}
{"Time":"2024-01-01T00:00:00Z","Action":"run","Package":"example","Test":"TestSkip"}
{"Time":"2024-01-01T00:00:01Z","Action":"skip","Package":"example","Test":"TestSkip","Elapsed":0.0}`

	results := ParseTestJSON(input)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if !results[0].Passed {
		t.Error("expected TestPass to pass")
	}
	if results[1].Passed {
		t.Error("expected TestFail to fail")
	}
	if results[2].Output != "SKIP" {
		t.Error("expected TestSkip to be skipped")
	}
}

func TestDiscoverTestPackages(t *testing.T) {
	// Create a temp directory structure with some _test.go files.
	tmpDir := t.TempDir()

	// Create packages with test files.
	dirs := []string{
		"pkg/auth",
		"pkg/config",
		"internal/handler",
	}
	for _, d := range dirs {
		dir := filepath.Join(tmpDir, d)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		// Write a dummy _test.go file.
		testFile := filepath.Join(dir, "main_test.go")
		if err := os.WriteFile(testFile, []byte("package "+filepath.Base(d)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Create a directory without test files.
	noTestDir := filepath.Join(tmpDir, "pkg/utils")
	if err := os.MkdirAll(noTestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(noTestDir, "utils.go"), []byte("package utils\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a vendor directory that should be skipped.
	vendorDir := filepath.Join(tmpDir, "vendor/somelib")
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendorDir, "lib_test.go"), []byte("package somelib\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	packages, err := DiscoverTestPackages(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(packages) != 3 {
		t.Fatalf("expected 3 packages, got %d: %v", len(packages), packages)
	}

	// Check vendor is excluded.
	for _, pkg := range packages {
		if strings.Contains(pkg, "vendor") {
			t.Errorf("vendor package should be excluded: %s", pkg)
		}
	}

	// Check that results are sorted and properly formatted.
	for _, pkg := range packages {
		if !strings.HasPrefix(pkg, "./") {
			t.Errorf("package should start with './', got: %s", pkg)
		}
	}
}

func TestDiscoverTestPackages_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	packages, err := DiscoverTestPackages(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packages) != 0 {
		t.Errorf("expected 0 packages in empty dir, got %d", len(packages))
	}
}

func TestDiscoverTestPackages_HiddenDirsSkipped(t *testing.T) {
	tmpDir := t.TempDir()

	hiddenDir := filepath.Join(tmpDir, ".hidden")
	if err := os.MkdirAll(hiddenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hiddenDir, "hidden_test.go"), []byte("package hidden\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	packages, err := DiscoverTestPackages(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packages) != 0 {
		t.Errorf("expected hidden dir to be skipped, got: %v", packages)
	}
}

func TestFormatResult_AllPassing(t *testing.T) {
	result := &RunnerResult{
		Packages: []PackageResult{
			{Package: "pkg/auth", Passed: 10, Duration: 1200 * time.Millisecond},
			{Package: "pkg/handler", Passed: 5, Duration: 800 * time.Millisecond},
		},
		TotalPassed:  15,
		TotalFailed:  0,
		TotalSkipped: 0,
		Duration:     2 * time.Second,
		Parallel:     4,
	}

	output := FormatResult(result)

	if !strings.Contains(output, "2 packages") {
		t.Error("expected output to contain package count")
	}
	if !strings.Contains(output, "4 workers") {
		t.Error("expected output to contain worker count")
	}
	if !strings.Contains(output, icons.CheckBold()) {
		t.Error("expected checkmark for passing packages")
	}
	if !strings.Contains(output, "Total: 15 passed, 0 failed, 0 skipped") {
		t.Errorf("unexpected total line in output:\n%s", output)
	}
}

func TestFormatResult_WithFailures(t *testing.T) {
	result := &RunnerResult{
		Packages: []PackageResult{
			{
				Package:  "pkg/config",
				Passed:   3,
				Failed:   2,
				Duration: 1500 * time.Millisecond,
				Tests: []TestCaseResult{
					{Name: "TestLoad", Passed: true, Duration: 100 * time.Millisecond},
					{Name: "TestValidate", Passed: false, Duration: 200 * time.Millisecond, Output: "expected error, got nil"},
					{Name: "TestSave", Passed: true, Duration: 50 * time.Millisecond},
					{Name: "TestDelete", Passed: false, Duration: 300 * time.Millisecond, Output: "file not found"},
					{Name: "TestList", Passed: true, Duration: 80 * time.Millisecond},
				},
			},
		},
		TotalPassed: 3,
		TotalFailed: 2,
		Duration:    2 * time.Second,
		Parallel:    2,
	}

	output := FormatResult(result)

	if !strings.Contains(output, icons.CloseThick()) {
		t.Error("expected failure marker for failing packages")
	}
	if !strings.Contains(output, "2 FAILED") {
		t.Error("expected FAILED count in output")
	}
	if !strings.Contains(output, "TestValidate") {
		t.Error("expected failed test name in output")
	}
	if !strings.Contains(output, "expected error, got nil") {
		t.Error("expected failure message in output")
	}
}

func TestFormatResult_WithSkipped(t *testing.T) {
	result := &RunnerResult{
		Packages: []PackageResult{
			{Package: "pkg/auth", Passed: 8, Skipped: 2, Duration: 500 * time.Millisecond},
		},
		TotalPassed:  8,
		TotalSkipped: 2,
		Duration:     time.Second,
		Parallel:     2,
	}

	output := FormatResult(result)

	if !strings.Contains(output, "2 skipped") {
		t.Errorf("expected skipped count in output:\n%s", output)
	}
}

func TestFormatResult_WithError(t *testing.T) {
	result := &RunnerResult{
		Packages: []PackageResult{
			{Package: "pkg/broken", Error: "build failed", Duration: 100 * time.Millisecond},
		},
		Duration: time.Second,
		Parallel: 2,
	}

	output := FormatResult(result)

	if !strings.Contains(output, "ERROR") {
		t.Error("expected ERROR in output for packages with errors")
	}
	if !strings.Contains(output, "build failed") {
		t.Error("expected error message in output")
	}
}

func TestGetFailed(t *testing.T) {
	r := NewParallelRunner(2)
	r.Results["pkg/a"] = &PackageResult{
		Tests: []TestCaseResult{
			{Name: "TestPass", Passed: true},
			{Name: "TestFail1", Passed: false, Output: "assertion failed"},
			{Name: "TestSkip", Passed: false, Output: "SKIP"},
		},
	}
	r.Results["pkg/b"] = &PackageResult{
		Tests: []TestCaseResult{
			{Name: "TestFail2", Passed: false, Output: "timeout"},
		},
	}

	failed := r.GetFailed()
	if len(failed) != 2 {
		t.Fatalf("expected 2 failed tests (skipped excluded), got %d", len(failed))
	}

	names := make(map[string]bool)
	for _, tc := range failed {
		names[tc.Name] = true
	}
	if !names["TestFail1"] || !names["TestFail2"] {
		t.Errorf("unexpected failed tests: %v", failed)
	}
}

func TestGetFailed_NoFailures(t *testing.T) {
	r := NewParallelRunner(2)
	r.Results["pkg/a"] = &PackageResult{
		Tests: []TestCaseResult{
			{Name: "TestPass", Passed: true},
		},
	}

	failed := r.GetFailed()
	if len(failed) != 0 {
		t.Errorf("expected 0 failed tests, got %d", len(failed))
	}
}

func TestGetSlowest(t *testing.T) {
	r := NewParallelRunner(2)
	r.Results["pkg/a"] = &PackageResult{
		Tests: []TestCaseResult{
			{Name: "TestFast", Passed: true, Duration: 10 * time.Millisecond},
			{Name: "TestSlow", Passed: true, Duration: 5 * time.Second},
			{Name: "TestMedium", Passed: true, Duration: 500 * time.Millisecond},
		},
	}
	r.Results["pkg/b"] = &PackageResult{
		Tests: []TestCaseResult{
			{Name: "TestSlowest", Passed: true, Duration: 10 * time.Second},
			{Name: "TestQuick", Passed: true, Duration: 1 * time.Millisecond},
		},
	}

	slowest := r.GetSlowest(3)
	if len(slowest) != 3 {
		t.Fatalf("expected 3 results, got %d", len(slowest))
	}
	if slowest[0].Name != "TestSlowest" {
		t.Errorf("expected slowest to be TestSlowest, got %s", slowest[0].Name)
	}
	if slowest[1].Name != "TestSlow" {
		t.Errorf("expected second slowest to be TestSlow, got %s", slowest[1].Name)
	}
	if slowest[2].Name != "TestMedium" {
		t.Errorf("expected third slowest to be TestMedium, got %s", slowest[2].Name)
	}
}

func TestGetSlowest_RequestMoreThanAvailable(t *testing.T) {
	r := NewParallelRunner(2)
	r.Results["pkg/a"] = &PackageResult{
		Tests: []TestCaseResult{
			{Name: "TestOnly", Passed: true, Duration: 100 * time.Millisecond},
		},
	}

	slowest := r.GetSlowest(10)
	if len(slowest) != 1 {
		t.Errorf("expected 1 result when requesting more than available, got %d", len(slowest))
	}
}

func TestGetSlowest_Empty(t *testing.T) {
	r := NewParallelRunner(2)
	slowest := r.GetSlowest(5)
	if len(slowest) != 0 {
		t.Errorf("expected 0 results for empty runner, got %d", len(slowest))
	}
}

func TestRunPackages_CancelledContext(t *testing.T) {
	r := NewParallelRunner(2)
	r.Timeout = 10 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	// Use a non-existent package to avoid actually running tests.
	result, err := r.RunPackages(ctx, []string{"./nonexistent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With cancelled context, the package should have an error.
	if len(result.Packages) != 1 {
		t.Fatalf("expected 1 package result, got %d", len(result.Packages))
	}
}

func TestRunAll_EmptyProject(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewParallelRunner(2)

	result, err := r.RunAll(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Packages) != 0 {
		t.Errorf("expected 0 packages for empty project, got %d", len(result.Packages))
	}
}

func TestFormatDurationInRunner(t *testing.T) {
	// formatDuration is defined in report.go; verify it works as expected.
	tests := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Millisecond, "500ms"},
		{1500 * time.Millisecond, "1.5s"},
		{10 * time.Second, "10.0s"},
		{100 * time.Millisecond, "100ms"},
		{90 * time.Second, "1m30s"},
	}

	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %s, want %s", tt.d, got, tt.want)
		}
	}
}

func TestFirstOutputLine(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "test failed"},
		{"=== RUN   TestFoo\n--- FAIL: TestFoo\nexpected 1, got 2", "expected 1, got 2"},
		{"simple error message", "simple error message"},
		{strings.Repeat("a", 100), strings.Repeat("a", 77) + "..."},
	}

	for _, tt := range tests {
		got := firstOutputLine(tt.input)
		if got != tt.want {
			t.Errorf("firstOutputLine(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPackageResult_Counting(t *testing.T) {
	// Simulate what RunSinglePackage does with parsed results.
	tests := []TestCaseResult{
		{Name: "TestA", Passed: true},
		{Name: "TestB", Passed: true},
		{Name: "TestC", Passed: false, Output: "failed"},
		{Name: "TestD", Passed: false, Output: "SKIP"},
		{Name: "TestE", Passed: true},
	}

	var passed, failed, skipped int
	for _, tc := range tests {
		if tc.Passed {
			passed++
		} else if tc.Output == "SKIP" {
			skipped++
		} else {
			failed++
		}
	}

	if passed != 3 {
		t.Errorf("expected 3 passed, got %d", passed)
	}
	if failed != 1 {
		t.Errorf("expected 1 failed, got %d", failed)
	}
	if skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", skipped)
	}
}
