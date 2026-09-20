package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewRunner(t *testing.T) {
	r := NewRunner("gpt-4", "openai")
	if r.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", r.Model, "gpt-4")
	}
	if r.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", r.Provider, "openai")
	}
	if r.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want %d", r.MaxAttempts, 3)
	}
	if r.Timeout != 5*time.Minute {
		t.Errorf("Timeout = %v, want %v", r.Timeout, 5*time.Minute)
	}
}

func TestRunNilSuite(t *testing.T) {
	r := NewRunner("test", "test")
	_, err := r.Run(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil suite")
	}
}

func TestRunSingleNilTask(t *testing.T) {
	r := NewRunner("test", "test")
	_, err := r.RunSingle(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil task")
	}
}

func TestRunSinglePassingTask(t *testing.T) {
	task := BenchmarkTask{
		ID:          "test-pass",
		Description: "A task that always passes",
		SetupFn: func(workDir string) error {
			return nil
		},
		ValidateFn: func(workDir string) (bool, string) {
			return true, "ok"
		},
		Prompt:    "Do nothing",
		TimeLimit: 10 * time.Second,
		Tags:      []string{"test"},
	}

	r := NewRunner("test", "test")
	result, err := r.RunSingle(context.Background(), &task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected task to pass")
	}
	if result.TaskID != "test-pass" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "test-pass")
	}
}

func TestRunSingleFailingTask(t *testing.T) {
	task := BenchmarkTask{
		ID:          "test-fail",
		Description: "A task that always fails validation",
		SetupFn: func(workDir string) error {
			return nil
		},
		ValidateFn: func(workDir string) (bool, string) {
			return false, "validation failed"
		},
		Prompt:    "Fix it",
		TimeLimit: 10 * time.Second,
		Tags:      []string{"test"},
	}

	r := NewRunner("test", "test")
	r.MaxAttempts = 1
	result, err := r.RunSingle(context.Background(), &task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected task to fail")
	}
	if result.Error == "" {
		t.Error("expected error message in result")
	}
}

// recordingLLM notes whether Complete was invoked.
type recordingLLM struct {
	called   bool
	response string
}

func (s *recordingLLM) Complete(ctx context.Context, model, prompt string) (string, int, float64, error) {
	s.called = true
	return s.response, 7, 0.01, nil
}

// TestRunSingleCacheHitSkipsLLM exercises the cache-hit branch (the
// goto applyResponse path) to guard the per-attempt closure refactor: a cached
// response must be applied without calling the LLM, and the task must still run
// and pass in its isolated work directory.
func TestRunSingleCacheHitSkipsLLM(t *testing.T) {
	cache := &Cache{Dir: t.TempDir()}
	const prompt = "Fix the bug"
	if err := cache.Put("test-model", prompt, "cached solution", 42, 0.5); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}

	llm := &recordingLLM{response: "fresh solution"}
	r := NewRunner("test-model", "test")
	r.LLM = llm
	r.Cache = cache
	r.MaxAttempts = 1

	task := BenchmarkTask{
		ID:          "test-cache-hit",
		Description: "A task served from cache",
		SetupFn:     func(workDir string) error { return nil },
		ValidateFn:  func(workDir string) (bool, string) { return true, "ok" },
		Prompt:      prompt,
		TimeLimit:   10 * time.Second,
	}

	result, err := r.RunSingle(context.Background(), &task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected task to pass")
	}
	if llm.called {
		t.Error("cache hit should skip the LLM call (goto applyResponse path)")
	}
	if result.TokensUsed != 42 {
		t.Errorf("TokensUsed = %d, want cached 42", result.TokensUsed)
	}
}

func TestRunSingleContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	task := BenchmarkTask{
		ID:          "test-cancel",
		Description: "A task that should be cancelled",
		SetupFn: func(workDir string) error {
			return nil
		},
		ValidateFn: func(workDir string) (bool, string) {
			return true, "ok"
		},
		Prompt:    "Do something",
		TimeLimit: 1 * time.Millisecond,
		Tags:      []string{"test"},
	}

	r := NewRunner("test", "test")
	result, err := r.RunSingle(ctx, &task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The context is cancelled but the result should indicate the issue.
	if result.Passed {
		// Depending on timing this may or may not pass - just ensure no panic.
		t.Log("task passed despite cancellation (timing dependent)")
	}
}

func TestRunSuite(t *testing.T) {
	suite := &BenchmarkSuite{
		Name: "Test Suite",
		Tasks: []BenchmarkTask{
			{
				ID:          "pass-1",
				Description: "Passes",
				SetupFn:     func(workDir string) error { return nil },
				ValidateFn:  func(workDir string) (bool, string) { return true, "" },
				Prompt:      "pass",
				TimeLimit:   10 * time.Second,
			},
			{
				ID:          "fail-1",
				Description: "Fails",
				SetupFn:     func(workDir string) error { return nil },
				ValidateFn:  func(workDir string) (bool, string) { return false, "nope" },
				Prompt:      "fail",
				TimeLimit:   10 * time.Second,
			},
			{
				ID:          "pass-2",
				Description: "Passes again",
				SetupFn:     func(workDir string) error { return nil },
				ValidateFn:  func(workDir string) (bool, string) { return true, "" },
				Prompt:      "pass again",
				TimeLimit:   10 * time.Second,
			},
		},
	}

	r := NewRunner("test", "test")
	r.MaxAttempts = 1
	result, err := r.Run(context.Background(), suite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TotalTasks != 3 {
		t.Errorf("TotalTasks = %d, want %d", result.TotalTasks, 3)
	}
	if result.Passed != 2 {
		t.Errorf("Passed = %d, want %d", result.Passed, 2)
	}
	if result.Failed != 1 {
		t.Errorf("Failed = %d, want %d", result.Failed, 1)
	}
	expectedRate := 2.0 / 3.0
	if result.PassRate < expectedRate-0.01 || result.PassRate > expectedRate+0.01 {
		t.Errorf("PassRate = %f, want ~%f", result.PassRate, expectedRate)
	}
	if len(result.Results) != 3 {
		t.Errorf("Results count = %d, want %d", len(result.Results), 3)
	}
}

func TestRunSuiteContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	callCount := 0
	suite := &BenchmarkSuite{
		Name: "Cancel Suite",
		Tasks: []BenchmarkTask{
			{
				ID:      "task-1",
				SetupFn: func(workDir string) error { return nil },
				ValidateFn: func(workDir string) (bool, string) {
					callCount++
					cancel() // Cancel after first task.
					return true, ""
				},
				Prompt:    "first",
				TimeLimit: 10 * time.Second,
			},
			{
				ID:      "task-2",
				SetupFn: func(workDir string) error { return nil },
				ValidateFn: func(workDir string) (bool, string) {
					callCount++
					return true, ""
				},
				Prompt:    "second",
				TimeLimit: 10 * time.Second,
			},
		},
	}

	r := NewRunner("test", "test")
	result, err := r.Run(ctx, suite)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled error, got %v", err)
	}
	// First task should have run.
	if result.Passed < 1 {
		t.Error("expected at least the first task to pass")
	}
}

// Test that GoTasks setup functions work correctly.
func TestGoTasksSetupFunctions(t *testing.T) {
	suite := GoTasks()
	if len(suite.Tasks) != 15 {
		t.Fatalf("expected 15 tasks, got %d", len(suite.Tasks))
	}

	for _, task := range suite.Tasks {
		t.Run(task.ID, func(t *testing.T) {
			workDir := t.TempDir()
			err := task.SetupFn(workDir)
			if err != nil {
				t.Fatalf("SetupFn failed: %v", err)
			}

			// Verify that files were created.
			entries, err := os.ReadDir(workDir)
			if err != nil {
				t.Fatalf("failed to read workDir: %v", err)
			}
			if len(entries) == 0 {
				t.Error("SetupFn created no files")
			}

			// Verify go.mod exists.
			goModPath := filepath.Join(workDir, "go.mod")
			if _, err := os.Stat(goModPath); os.IsNotExist(err) {
				t.Error("SetupFn did not create go.mod")
			}

			// Verify main.go exists.
			mainPath := filepath.Join(workDir, "main.go")
			if _, err := os.Stat(mainPath); os.IsNotExist(err) {
				t.Error("SetupFn did not create main.go")
			}
		})
	}
}

// Test that tasks where setup creates already-correct code pass validation.
// These are the tasks where setup includes the solution (tasks that validate
// the harness itself).
func TestGoTasksAlreadyCorrect(t *testing.T) {
	// Tasks where the setup already contains the correct solution:
	correctTasks := []string{
		"go-write-unit-test",
		"go-refactor-duplicate",
		"go-fix-race-condition",
		"go-implement-sort",
		"go-parse-json",
		"go-add-context-cancellation",
		"go-fix-off-by-one",
		"go-implement-retry-backoff",
		"go-callback-to-channel",
		"go-add-input-validation",
		"go-fix-goroutine-leak",
		"go-implement-http-handler",
	}

	suite := GoTasks()
	taskMap := make(map[string]BenchmarkTask)
	for _, task := range suite.Tasks {
		taskMap[task.ID] = task
	}

	for _, id := range correctTasks {
		t.Run(id, func(t *testing.T) {
			task, ok := taskMap[id]
			if !ok {
				t.Fatalf("task %q not found", id)
			}

			workDir := t.TempDir()
			err := task.SetupFn(workDir)
			if err != nil {
				t.Fatalf("SetupFn failed: %v", err)
			}

			passed, msg := task.ValidateFn(workDir)
			if !passed {
				t.Errorf("validation failed for correct task: %s", msg)
			}
		})
	}
}

// Test report generation.
func TestGenerateReport(t *testing.T) {
	result := &SuiteResult{
		Suite:         "Test Suite",
		TotalTasks:    3,
		Passed:        2,
		Failed:        1,
		TotalDuration: 5 * time.Second,
		TotalTokens:   1500,
		TotalCostUSD:  0.0045,
		PassRate:      0.6667,
		Results: []TaskResult{
			{TaskID: "task-1", Passed: true, Duration: 2 * time.Second, TokensUsed: 500, CostUSD: 0.0015, Attempts: 1},
			{TaskID: "task-2", Passed: true, Duration: 1 * time.Second, TokensUsed: 400, CostUSD: 0.0012, Attempts: 1},
			{TaskID: "task-3", Passed: false, Duration: 2 * time.Second, TokensUsed: 600, CostUSD: 0.0018, Attempts: 3, Error: "tests failed"},
		},
	}

	report := GenerateReport(result)
	if !strings.Contains(report, "Test Suite") {
		t.Error("report should contain suite name")
	}
	if !strings.Contains(report, "66.7%") {
		t.Error("report should contain pass rate")
	}
	if !strings.Contains(report, "PASS") {
		t.Error("report should contain PASS status")
	}
	if !strings.Contains(report, "FAIL") {
		t.Error("report should contain FAIL status")
	}
	if !strings.Contains(report, "Failures") {
		t.Error("report should contain failures section")
	}
}

func TestGenerateReportNil(t *testing.T) {
	report := GenerateReport(nil)
	if !strings.Contains(report, "No Results") {
		t.Error("nil result should produce 'No Results' report")
	}
}

func TestGenerateLeaderboard(t *testing.T) {
	results := []SuiteResult{
		{Suite: "Model A", TotalTasks: 10, Passed: 8, Failed: 2, PassRate: 0.8, TotalDuration: 30 * time.Second, TotalTokens: 5000, TotalCostUSD: 0.05},
		{Suite: "Model B", TotalTasks: 10, Passed: 9, Failed: 1, PassRate: 0.9, TotalDuration: 45 * time.Second, TotalTokens: 7000, TotalCostUSD: 0.07},
		{Suite: "Model C", TotalTasks: 10, Passed: 7, Failed: 3, PassRate: 0.7, TotalDuration: 20 * time.Second, TotalTokens: 3000, TotalCostUSD: 0.03},
	}

	leaderboard := GenerateLeaderboard(results)
	if !strings.Contains(leaderboard, "Leaderboard") {
		t.Error("should contain leaderboard header")
	}
	// Model B should be ranked first (highest pass rate).
	bIdx := strings.Index(leaderboard, "Model B")
	aIdx := strings.Index(leaderboard, "Model A")
	cIdx := strings.Index(leaderboard, "Model C")
	if bIdx > aIdx || aIdx > cIdx {
		t.Errorf("expected ranking B > A > C, got positions B=%d A=%d C=%d", bIdx, aIdx, cIdx)
	}
}

func TestGenerateLeaderboardEmpty(t *testing.T) {
	leaderboard := GenerateLeaderboard(nil)
	if !strings.Contains(leaderboard, "No results") {
		t.Error("empty results should show 'No results'")
	}
}

func TestCompareModels(t *testing.T) {
	results := map[string]*SuiteResult{
		"claude-4": {
			Suite: "Go Tasks", TotalTasks: 5, Passed: 4, Failed: 1,
			PassRate: 0.8, TotalDuration: 30 * time.Second, TotalTokens: 5000, TotalCostUSD: 0.05,
			Results: []TaskResult{
				{TaskID: "task-1", Passed: true},
				{TaskID: "task-2", Passed: true},
				{TaskID: "task-3", Passed: false},
			},
		},
		"gpt-4o": {
			Suite: "Go Tasks", TotalTasks: 5, Passed: 3, Failed: 2,
			PassRate: 0.6, TotalDuration: 45 * time.Second, TotalTokens: 7000, TotalCostUSD: 0.07,
			Results: []TaskResult{
				{TaskID: "task-1", Passed: true},
				{TaskID: "task-2", Passed: false},
				{TaskID: "task-3", Passed: true},
			},
		},
	}

	comparison := CompareModels(results)
	if !strings.Contains(comparison, "Model Comparison") {
		t.Error("should contain comparison header")
	}
	if !strings.Contains(comparison, "claude-4") {
		t.Error("should contain model name claude-4")
	}
	if !strings.Contains(comparison, "gpt-4o") {
		t.Error("should contain model name gpt-4o")
	}
	if !strings.Contains(comparison, "Pass Rate") {
		t.Error("should contain Pass Rate metric")
	}
	if !strings.Contains(comparison, "PASS") {
		t.Error("should contain per-task PASS")
	}
	if !strings.Contains(comparison, "FAIL") {
		t.Error("should contain per-task FAIL")
	}
}

func TestCompareModelsEmpty(t *testing.T) {
	comparison := CompareModels(nil)
	if !strings.Contains(comparison, "No results") {
		t.Error("empty map should show 'No results'")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Millisecond, "500ms"},
		{1500 * time.Millisecond, "1.5s"},
		{30 * time.Second, "30.0s"},
		{90 * time.Second, "1m30s"},
		{5 * time.Minute, "5m0s"},
	}

	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestBenchmarkSuiteStructure(t *testing.T) {
	suite := GoTasks()
	if suite.Name == "" {
		t.Error("suite name should not be empty")
	}
	if len(suite.Tasks) != 15 {
		t.Errorf("expected 15 tasks, got %d", len(suite.Tasks))
	}

	// Verify all tasks have required fields.
	ids := make(map[string]bool)
	for _, task := range suite.Tasks {
		if task.ID == "" {
			t.Error("task ID should not be empty")
		}
		if ids[task.ID] {
			t.Errorf("duplicate task ID: %s", task.ID)
		}
		ids[task.ID] = true

		if task.Description == "" {
			t.Errorf("task %s: description should not be empty", task.ID)
		}
		if task.Prompt == "" {
			t.Errorf("task %s: prompt should not be empty", task.ID)
		}
		if task.SetupFn == nil {
			t.Errorf("task %s: SetupFn should not be nil", task.ID)
		}
		if task.ValidateFn == nil {
			t.Errorf("task %s: ValidateFn should not be nil", task.ID)
		}
		if task.TimeLimit == 0 {
			t.Errorf("task %s: TimeLimit should not be zero", task.ID)
		}
		if len(task.Tags) == 0 {
			t.Errorf("task %s: should have at least one tag", task.ID)
		}
	}
}

// TestRunProgressCallback asserts the Progress callback fires once per task
// with the zero-based index, total count, and task ID, in order.
func TestRunProgressCallback(t *testing.T) {
	mk := func(id string) BenchmarkTask {
		return BenchmarkTask{
			ID:          id,
			Description: "passes immediately",
			SetupFn:     func(workDir string) error { return nil },
			ValidateFn:  func(workDir string) (bool, string) { return true, "ok" },
			Prompt:      "Do nothing",
			TimeLimit:   10 * time.Second,
		}
	}
	suite := &BenchmarkSuite{
		Name:  "progress",
		Tasks: []BenchmarkTask{mk("a"), mk("b"), mk("c")},
	}

	var calls []string
	r := NewRunner("test", "test")
	r.Progress = func(i, total int, taskID string) {
		if total != 3 {
			t.Errorf("total = %d, want 3", total)
		}
		calls = append(calls, taskID)
		if i != len(calls)-1 {
			t.Errorf("i = %d, want %d", i, len(calls)-1)
		}
	}

	if _, err := r.Run(context.Background(), suite); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"a", "b", "c"}
	if len(calls) != len(want) {
		t.Fatalf("callback called %d times, want %d (%v)", len(calls), len(want), calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, calls[i], want[i])
		}
	}
}

// stubLLM returns a scripted sequence of responses, one per Complete call.
type stubLLM struct {
	responses []string
	i         int
}

func (s *stubLLM) Complete(_ context.Context, _, _ string) (string, int, float64, error) {
	resp := s.responses[s.i%len(s.responses)]
	s.i++
	return resp, 10, 0.01, nil
}

func consensusTask() BenchmarkTask {
	return BenchmarkTask{
		ID:          "consensus",
		Description: "task whose solution must contain VALID",
		SetupFn:     func(workDir string) error { return nil },
		ValidateFn: func(workDir string) (bool, string) {
			data, err := os.ReadFile(filepath.Join(workDir, "solution.go"))
			if err != nil {
				return false, "no solution"
			}
			return strings.Contains(string(data), "VALID"), "ok"
		},
		Prompt:      "solve",
		MaxAttempts: 1,
	}
}

func TestRunConsensus_MajorityVerdict(t *testing.T) {
	r := NewRunner("m", "p")
	r.MaxAttempts = 1
	r.LLM = &stubLLM{responses: []string{"GOOD_VALID", "GOOD_VALID", "WRONG"}}
	r.Samples = 3

	task := consensusTask()
	result, err := r.RunConsensus(context.Background(), &task)
	if err != nil {
		t.Fatalf("RunConsensus: %v", err)
	}
	if !result.Passed {
		t.Error("consensus should be PASS (2/3 samples valid)")
	}
	if result.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", result.Attempts)
	}
	if result.TokensUsed != 30 {
		t.Errorf("TokensUsed = %d, want 30 (3 samples x 10)", result.TokensUsed)
	}
}

func TestRunConsensus_MinorityFails(t *testing.T) {
	r := NewRunner("m", "p")
	r.MaxAttempts = 1
	r.LLM = &stubLLM{responses: []string{"GOOD_VALID", "WRONG", "WRONG"}}
	r.Samples = 3

	task := consensusTask()
	result, err := r.RunConsensus(context.Background(), &task)
	if err != nil {
		t.Fatalf("RunConsensus: %v", err)
	}
	if result.Passed {
		t.Error("consensus should be FAIL (only 1/3 valid)")
	}
}
