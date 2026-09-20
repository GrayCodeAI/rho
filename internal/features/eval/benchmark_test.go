package eval

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestNewModelBenchmark(t *testing.T) {
	models := []ModelConfig{
		{Name: "model-a", Provider: "provider-a", Model: "a", Temperature: 0.7, MaxTokens: 1000},
	}
	mb := NewModelBenchmark("test-bench", models)
	if mb.Name != "test-bench" {
		t.Errorf("Name = %q, want %q", mb.Name, "test-bench")
	}
	if len(mb.Models) != 1 {
		t.Errorf("Models count = %d, want %d", len(mb.Models), 1)
	}
	if mb.Runs != 1 {
		t.Errorf("Runs = %d, want %d", mb.Runs, 1)
	}
	if mb.Results == nil {
		t.Error("Results map should be initialized")
	}
}

func TestRunAllBasic(t *testing.T) {
	models := []ModelConfig{
		{Name: "fast-model", Provider: "test", Model: "fast", Temperature: 0.0, MaxTokens: 500},
		{Name: "slow-model", Provider: "test", Model: "slow", Temperature: 0.5, MaxTokens: 2000},
	}
	mb := NewModelBenchmark("basic-bench", models)
	mb.Tasks = []BenchmarkTask{
		{ID: "task-1", Prompt: "do something", Tags: []string{"coding"}},
		{ID: "task-2", Prompt: "do another thing", Tags: []string{"debugging"}},
	}
	mb.Runs = 2

	callCount := 0
	chatFn := func(ctx context.Context, cfg ModelConfig, prompt string) (string, int, float64, error) {
		callCount++
		if cfg.Name == "fast-model" {
			return "result", 100, 0.001, nil
		}
		// slow-model fails on task-2
		if strings.Contains(prompt, "another") {
			return "", 200, 0.005, fmt.Errorf("model error")
		}
		return "result", 200, 0.005, nil
	}

	err := mb.RunAll(context.Background(), chatFn)
	if err != nil {
		t.Fatalf("RunAll failed: %v", err)
	}

	// 2 models * 2 tasks * 2 runs = 8 calls.
	if callCount != 8 {
		t.Errorf("expected 8 calls, got %d", callCount)
	}

	// fast-model should have 100% pass rate.
	fastResult := mb.Results["fast-model"]
	if fastResult == nil {
		t.Fatal("fast-model result is nil")
	}
	if fastResult.PassRate != 1.0 {
		t.Errorf("fast-model PassRate = %f, want 1.0", fastResult.PassRate)
	}
	if fastResult.AvgTokens != 100 {
		t.Errorf("fast-model AvgTokens = %d, want 100", fastResult.AvgTokens)
	}

	// slow-model should have 50% pass rate (passes task-1 twice, fails task-2 twice).
	slowResult := mb.Results["slow-model"]
	if slowResult == nil {
		t.Fatal("slow-model result is nil")
	}
	if slowResult.PassRate != 0.5 {
		t.Errorf("slow-model PassRate = %f, want 0.5", slowResult.PassRate)
	}
}

func TestRunAllContextCancellation(t *testing.T) {
	models := []ModelConfig{
		{Name: "model-a", Provider: "test", Model: "a"},
	}
	mb := NewModelBenchmark("cancel-bench", models)
	mb.Tasks = []BenchmarkTask{
		{ID: "task-1", Prompt: "prompt-1", Tags: []string{"general"}},
		{ID: "task-2", Prompt: "prompt-2", Tags: []string{"general"}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	chatFn := func(ctx context.Context, cfg ModelConfig, prompt string) (string, int, float64, error) {
		return "ok", 10, 0.001, nil
	}

	err := mb.RunAll(ctx, chatFn)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestCompareOutput(t *testing.T) {
	models := []ModelConfig{
		{Name: "claude-sonnet", Provider: "anthropic", Model: "sonnet"},
		{Name: "gpt-4o", Provider: "openai", Model: "gpt-4o"},
	}
	mb := NewModelBenchmark("Go Coding Tasks", models)
	mb.Results = map[string]*ModelResult{
		"claude-sonnet": {
			Model:       "claude-sonnet",
			PassRate:    0.933,
			AvgTokens:   1240,
			AvgCostUSD:  0.012,
			AvgDuration: 2100 * time.Millisecond,
			P50Duration: 2000 * time.Millisecond,
			P95Duration: 3000 * time.Millisecond,
		},
		"gpt-4o": {
			Model:       "gpt-4o",
			PassRate:    0.867,
			AvgTokens:   1580,
			AvgCostUSD:  0.015,
			AvgDuration: 1800 * time.Millisecond,
			P50Duration: 1700 * time.Millisecond,
			P95Duration: 2500 * time.Millisecond,
		},
	}

	output := mb.Compare()

	if !strings.Contains(output, "Go Coding Tasks") {
		t.Error("output should contain benchmark name")
	}
	if !strings.Contains(output, "claude-sonnet") {
		t.Error("output should contain model name claude-sonnet")
	}
	if !strings.Contains(output, "gpt-4o") {
		t.Error("output should contain model name gpt-4o")
	}
	if !strings.Contains(output, "Pass Rate") {
		t.Error("output should contain header 'Pass Rate'")
	}
	if !strings.Contains(output, "Avg Tokens") {
		t.Error("output should contain header 'Avg Tokens'")
	}
	if !strings.Contains(output, "Cost/Task") {
		t.Error("output should contain header 'Cost/Task'")
	}
	if !strings.Contains(output, "Latency") {
		t.Error("output should contain header 'Latency'")
	}
	if !strings.Contains(output, "Best quality") {
		t.Error("output should contain 'Best quality'")
	}
	if !strings.Contains(output, "Best speed") {
		t.Error("output should contain 'Best speed'")
	}
	if !strings.Contains(output, "Best value") {
		t.Error("output should contain 'Best value'")
	}
}

func TestCompareEmptyResults(t *testing.T) {
	mb := NewModelBenchmark("empty", nil)
	output := mb.Compare()
	if !strings.Contains(output, "No benchmark results") {
		t.Errorf("expected 'No benchmark results', got: %s", output)
	}
}

func TestRankModelsByQuality(t *testing.T) {
	mb := NewModelBenchmark("rank-test", nil)
	mb.Results = map[string]*ModelResult{
		"low":  {Model: "low", PassRate: 0.5, AvgCostUSD: 0.001, AvgDuration: 500 * time.Millisecond},
		"mid":  {Model: "mid", PassRate: 0.75, AvgCostUSD: 0.005, AvgDuration: 1 * time.Second},
		"high": {Model: "high", PassRate: 0.95, AvgCostUSD: 0.015, AvgDuration: 2 * time.Second},
	}

	ranked := mb.RankModels("quality")
	if len(ranked) != 3 {
		t.Fatalf("expected 3 results, got %d", len(ranked))
	}
	if ranked[0].Model != "high" {
		t.Errorf("rank 1 = %q, want %q", ranked[0].Model, "high")
	}
	if ranked[1].Model != "mid" {
		t.Errorf("rank 2 = %q, want %q", ranked[1].Model, "mid")
	}
	if ranked[2].Model != "low" {
		t.Errorf("rank 3 = %q, want %q", ranked[2].Model, "low")
	}
}

func TestRankModelsByCost(t *testing.T) {
	mb := NewModelBenchmark("cost-test", nil)
	mb.Results = map[string]*ModelResult{
		"expensive": {Model: "expensive", PassRate: 0.95, AvgCostUSD: 0.05, AvgDuration: 2 * time.Second},
		"cheap":     {Model: "cheap", PassRate: 0.7, AvgCostUSD: 0.001, AvgDuration: 500 * time.Millisecond},
		"mid":       {Model: "mid", PassRate: 0.8, AvgCostUSD: 0.01, AvgDuration: 1 * time.Second},
	}

	ranked := mb.RankModels("cost")
	if ranked[0].Model != "cheap" {
		t.Errorf("cheapest should be first, got %q", ranked[0].Model)
	}
	if ranked[2].Model != "expensive" {
		t.Errorf("most expensive should be last, got %q", ranked[2].Model)
	}
}

func TestRankModelsBySpeed(t *testing.T) {
	mb := NewModelBenchmark("speed-test", nil)
	mb.Results = map[string]*ModelResult{
		"slow":   {Model: "slow", PassRate: 0.9, AvgCostUSD: 0.01, AvgDuration: 5 * time.Second},
		"fast":   {Model: "fast", PassRate: 0.7, AvgCostUSD: 0.005, AvgDuration: 500 * time.Millisecond},
		"medium": {Model: "medium", PassRate: 0.8, AvgCostUSD: 0.008, AvgDuration: 2 * time.Second},
	}

	ranked := mb.RankModels("speed")
	if ranked[0].Model != "fast" {
		t.Errorf("fastest should be first, got %q", ranked[0].Model)
	}
	if ranked[2].Model != "slow" {
		t.Errorf("slowest should be last, got %q", ranked[2].Model)
	}
}

func TestRankModelsByValue(t *testing.T) {
	mb := NewModelBenchmark("value-test", nil)
	mb.Results = map[string]*ModelResult{
		// value = 0.9 / 0.02 = 45
		"expensive-good": {Model: "expensive-good", PassRate: 0.9, AvgCostUSD: 0.02},
		// value = 0.8 / 0.002 = 400
		"cheap-good": {Model: "cheap-good", PassRate: 0.8, AvgCostUSD: 0.002},
		// value = 0.5 / 0.01 = 50
		"mid-mid": {Model: "mid-mid", PassRate: 0.5, AvgCostUSD: 0.01},
	}

	ranked := mb.RankModels("value")
	if ranked[0].Model != "cheap-good" {
		t.Errorf("best value should be first, got %q", ranked[0].Model)
	}
}

func TestRankModelsEmpty(t *testing.T) {
	mb := NewModelBenchmark("empty", nil)
	ranked := mb.RankModels("quality")
	if ranked != nil {
		t.Errorf("expected nil for empty results, got %v", ranked)
	}
}

func TestRecommendModelSimple(t *testing.T) {
	mb := NewModelBenchmark("recommend-test", nil)
	mb.Results = map[string]*ModelResult{
		"expensive-great": {Model: "expensive-great", PassRate: 0.95, AvgCostUSD: 0.05},
		"cheap-good":      {Model: "cheap-good", PassRate: 0.85, AvgCostUSD: 0.002},
		"cheap-bad":       {Model: "cheap-bad", PassRate: 0.60, AvgCostUSD: 0.001},
	}

	// "simple" = cheapest with >80% pass rate.
	rec := mb.RecommendModel("simple")
	if rec != "cheap-good" {
		t.Errorf("simple recommendation = %q, want %q", rec, "cheap-good")
	}
}

func TestRecommendModelComplex(t *testing.T) {
	mb := NewModelBenchmark("recommend-test", nil)
	mb.Results = map[string]*ModelResult{
		"expensive-great": {Model: "expensive-great", PassRate: 0.95, AvgCostUSD: 0.05},
		"cheap-good":      {Model: "cheap-good", PassRate: 0.85, AvgCostUSD: 0.002},
	}

	// "complex" = highest pass rate.
	rec := mb.RecommendModel("complex")
	if rec != "expensive-great" {
		t.Errorf("complex recommendation = %q, want %q", rec, "expensive-great")
	}
}

func TestRecommendModelBudget(t *testing.T) {
	mb := NewModelBenchmark("recommend-test", nil)
	mb.Results = map[string]*ModelResult{
		// value = 0.95 / 0.05 = 19
		"expensive-great": {Model: "expensive-great", PassRate: 0.95, AvgCostUSD: 0.05},
		// value = 0.85 / 0.002 = 425
		"cheap-good": {Model: "cheap-good", PassRate: 0.85, AvgCostUSD: 0.002},
	}

	// "budget" = best value ratio.
	rec := mb.RecommendModel("budget")
	if rec != "cheap-good" {
		t.Errorf("budget recommendation = %q, want %q", rec, "cheap-good")
	}
}

func TestRecommendModelEmpty(t *testing.T) {
	mb := NewModelBenchmark("empty", nil)
	rec := mb.RecommendModel("simple")
	if rec != "" {
		t.Errorf("expected empty string for no results, got %q", rec)
	}
}

func TestRecommendModelSimpleNoThreshold(t *testing.T) {
	// When no model meets >80%, return cheapest.
	mb := NewModelBenchmark("low-pass", nil)
	mb.Results = map[string]*ModelResult{
		"model-a": {Model: "model-a", PassRate: 0.6, AvgCostUSD: 0.01},
		"model-b": {Model: "model-b", PassRate: 0.5, AvgCostUSD: 0.005},
	}

	rec := mb.RecommendModel("simple")
	if rec != "model-b" {
		t.Errorf("simple (no threshold) = %q, want %q (cheapest)", rec, "model-b")
	}
}

func TestAnalyzeStrengths(t *testing.T) {
	mb := NewModelBenchmark("analyze-test", nil)
	mb.Tasks = []BenchmarkTask{
		{ID: "task-1", Tags: []string{"coding", "go"}},
		{ID: "task-2", Tags: []string{"coding", "go"}},
		{ID: "task-3", Tags: []string{"debugging"}},
		{ID: "task-4", Tags: []string{"debugging"}},
		{ID: "task-5", Tags: []string{"refactoring"}},
	}
	mb.Results = map[string]*ModelResult{
		"test-model": {
			Model: "test-model",
			TaskResults: []TaskResult{
				{TaskID: "task-1", Passed: true},
				{TaskID: "task-2", Passed: true},
				{TaskID: "task-3", Passed: false},
				{TaskID: "task-4", Passed: false},
				{TaskID: "task-5", Passed: true},
			},
		},
	}

	strengths, weaknesses := mb.AnalyzeStrengths("test-model")

	// "coding" and "go" have 100% pass rate -> strengths.
	foundCoding := false
	foundGo := false
	for _, s := range strengths {
		if s == "coding" {
			foundCoding = true
		}
		if s == "go" {
			foundGo = true
		}
	}
	if !foundCoding {
		t.Error("expected 'coding' in strengths")
	}
	if !foundGo {
		t.Error("expected 'go' in strengths")
	}

	// "debugging" has 0% pass rate -> weakness.
	foundDebugging := false
	for _, w := range weaknesses {
		if w == "debugging" {
			foundDebugging = true
		}
	}
	if !foundDebugging {
		t.Error("expected 'debugging' in weaknesses")
	}
}

func TestAnalyzeStrengthsUnknownModel(t *testing.T) {
	mb := NewModelBenchmark("test", nil)
	strengths, weaknesses := mb.AnalyzeStrengths("nonexistent")
	if strengths != nil {
		t.Errorf("expected nil strengths, got %v", strengths)
	}
	if weaknesses != nil {
		t.Errorf("expected nil weaknesses, got %v", weaknesses)
	}
}

func TestAnalyzeStrengthsEmptyResults(t *testing.T) {
	mb := NewModelBenchmark("test", nil)
	mb.Results = map[string]*ModelResult{
		"empty-model": {Model: "empty-model", TaskResults: nil},
	}
	strengths, weaknesses := mb.AnalyzeStrengths("empty-model")
	if strengths != nil {
		t.Errorf("expected nil strengths, got %v", strengths)
	}
	if weaknesses != nil {
		t.Errorf("expected nil weaknesses, got %v", weaknesses)
	}
}

func TestExportCSV(t *testing.T) {
	mb := NewModelBenchmark("csv-test", nil)
	mb.Results = map[string]*ModelResult{
		"model-a": {
			Model: "model-a",
			TaskResults: []TaskResult{
				{TaskID: "task-1", Passed: true, Duration: 2 * time.Second, TokensUsed: 500, CostUSD: 0.005},
				{TaskID: "task-2", Passed: false, Duration: 3 * time.Second, TokensUsed: 800, CostUSD: 0.008, Error: "validation failed"},
			},
		},
		"model-b": {
			Model: "model-b",
			TaskResults: []TaskResult{
				{TaskID: "task-1", Passed: true, Duration: 1 * time.Second, TokensUsed: 300, CostUSD: 0.003},
			},
		},
	}

	csv := mb.ExportCSV()

	// Check header.
	lines := strings.Split(csv, "\n")
	if len(lines) < 2 {
		t.Fatal("CSV should have at least a header and one data line")
	}
	if lines[0] != "model,task_id,passed,duration_ms,tokens,cost_usd,error" {
		t.Errorf("unexpected header: %q", lines[0])
	}

	// Check that all models appear.
	if !strings.Contains(csv, "model-a") {
		t.Error("CSV should contain model-a")
	}
	if !strings.Contains(csv, "model-b") {
		t.Error("CSV should contain model-b")
	}

	// Check pass/fail values.
	if !strings.Contains(csv, "true") {
		t.Error("CSV should contain 'true' for passed tasks")
	}
	if !strings.Contains(csv, "false") {
		t.Error("CSV should contain 'false' for failed tasks")
	}

	// Check error field.
	if !strings.Contains(csv, "validation failed") {
		t.Error("CSV should contain error message")
	}

	// Count data lines (excluding header and trailing newline).
	dataLines := 0
	for _, line := range lines[1:] {
		if line != "" {
			dataLines++
		}
	}
	if dataLines != 3 {
		t.Errorf("expected 3 data lines, got %d", dataLines)
	}
}

func TestExportCSVEmpty(t *testing.T) {
	mb := NewModelBenchmark("empty", nil)
	csv := mb.ExportCSV()
	if !strings.Contains(csv, "model,task_id,passed") {
		t.Error("empty CSV should still have header")
	}
	lines := strings.Split(strings.TrimSpace(csv), "\n")
	if len(lines) != 1 {
		t.Errorf("empty CSV should have only header, got %d lines", len(lines))
	}
}

func TestPercentile(t *testing.T) {
	durations := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		300 * time.Millisecond,
		400 * time.Millisecond,
		500 * time.Millisecond,
		600 * time.Millisecond,
		700 * time.Millisecond,
		800 * time.Millisecond,
		900 * time.Millisecond,
		1000 * time.Millisecond,
	}

	p50 := Percentile(durations, 50)
	// Nearest rank: 50/100 * 10 = 5, index 5 = 600ms.
	if p50 != 600*time.Millisecond {
		t.Errorf("P50 = %v, want 600ms", p50)
	}

	p95 := Percentile(durations, 95)
	// Nearest rank: 95/100 * 10 = 9.5, index 9 = 1000ms.
	if p95 != 1000*time.Millisecond {
		t.Errorf("P95 = %v, want 1000ms", p95)
	}

	p0 := Percentile(durations, 0)
	if p0 != 100*time.Millisecond {
		t.Errorf("P0 = %v, want 100ms", p0)
	}

	p100 := Percentile(durations, 100)
	if p100 != 1000*time.Millisecond {
		t.Errorf("P100 = %v, want 1000ms", p100)
	}
}

func TestPercentileEmpty(t *testing.T) {
	p := Percentile(nil, 50)
	if p != 0 {
		t.Errorf("Percentile of empty slice = %v, want 0", p)
	}
}

func TestPercentileSingleElement(t *testing.T) {
	durations := []time.Duration{500 * time.Millisecond}
	p50 := Percentile(durations, 50)
	if p50 != 500*time.Millisecond {
		t.Errorf("P50 of single element = %v, want 500ms", p50)
	}
}

func TestPercentileUnsorted(t *testing.T) {
	// Ensure Percentile sorts internally and does not modify the original.
	durations := []time.Duration{
		900 * time.Millisecond,
		100 * time.Millisecond,
		500 * time.Millisecond,
		300 * time.Millisecond,
		700 * time.Millisecond,
	}
	original := make([]time.Duration, len(durations))
	copy(original, durations)

	p50 := Percentile(durations, 50)
	// Sorted: 100, 300, 500, 700, 900. Rank 2.5 -> index 2 = 500ms.
	if p50 != 500*time.Millisecond {
		t.Errorf("P50 = %v, want 500ms", p50)
	}

	// Verify original is not modified.
	for i := range durations {
		if durations[i] != original[i] {
			t.Errorf("Percentile modified original slice at index %d", i)
			break
		}
	}
}

func TestSingleModelBenchmark(t *testing.T) {
	models := []ModelConfig{
		{Name: "dev-model", Provider: "test", Model: "dev"},
	}
	mb := NewModelBenchmark("single-model-test", models)
	mb.Tasks = []BenchmarkTask{
		{ID: "task-a", Prompt: "solve A", Tags: []string{"math"}},
		{ID: "task-b", Prompt: "solve B", Tags: []string{"coding"}},
		{ID: "task-c", Prompt: "solve C", Tags: []string{"math"}},
	}
	mb.Runs = 3

	chatFn := func(ctx context.Context, cfg ModelConfig, prompt string) (string, int, float64, error) {
		// Fail on task-b.
		if strings.Contains(prompt, "B") {
			return "", 50, 0.001, fmt.Errorf("cannot solve B")
		}
		return "answer", 100, 0.002, nil
	}

	err := mb.RunAll(context.Background(), chatFn)
	if err != nil {
		t.Fatalf("RunAll failed: %v", err)
	}

	result := mb.Results["dev-model"]
	if result == nil {
		t.Fatal("dev-model result is nil")
	}

	// 3 tasks * 3 runs = 9 total. task-b fails all 3 runs = 3 failures.
	// Pass rate = 6/9 = 0.667.
	expectedRate := 6.0 / 9.0
	if result.PassRate < expectedRate-0.01 || result.PassRate > expectedRate+0.01 {
		t.Errorf("PassRate = %f, want ~%f", result.PassRate, expectedRate)
	}

	// Total task results should be 9.
	if len(result.TaskResults) != 9 {
		t.Errorf("TaskResults count = %d, want 9", len(result.TaskResults))
	}

	// Compare output should work with single model.
	output := mb.Compare()
	if !strings.Contains(output, "dev-model") {
		t.Error("Compare output should contain 'dev-model'")
	}

	// RankModels with single model should return single element.
	ranked := mb.RankModels("quality")
	if len(ranked) != 1 {
		t.Errorf("RankModels should return 1 element, got %d", len(ranked))
	}
}

func TestFormatBenchDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Nanosecond, "500ns"},
		{500 * time.Microsecond, "500000ns"},
		{50 * time.Millisecond, "50ms"},
		{1500 * time.Millisecond, "1.5s"},
		{2100 * time.Millisecond, "2.1s"},
	}

	for _, tt := range tests {
		got := formatBenchDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatBenchDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestValueRatio(t *testing.T) {
	// Normal case.
	r := ModelResult{PassRate: 0.9, AvgCostUSD: 0.01}
	v := valueRatio(r)
	if v != 90.0 {
		t.Errorf("valueRatio = %f, want 90.0", v)
	}

	// Zero cost with non-zero pass rate -> very high value.
	r2 := ModelResult{PassRate: 0.5, AvgCostUSD: 0.0}
	v2 := valueRatio(r2)
	if v2 != 1e9 {
		t.Errorf("valueRatio(zero cost) = %f, want 1e9", v2)
	}

	// Zero cost and zero pass rate -> 0.
	r3 := ModelResult{PassRate: 0.0, AvgCostUSD: 0.0}
	v3 := valueRatio(r3)
	if v3 != 0 {
		t.Errorf("valueRatio(zero/zero) = %f, want 0", v3)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate(short, 10) = %q, want %q", got, "short")
	}
	long := "this-is-a-very-long-model-name"
	got := truncate(long, 20)
	if len(got) > 20 {
		t.Errorf("truncated string length %d > 20", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncated string should end with '...', got %q", got)
	}
	// Exact length should be 20.
	if len(got) != 20 {
		t.Errorf("truncated string length = %d, want 20", len(got))
	}
}
