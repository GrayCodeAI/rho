package eval

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/GrayCodeAI/rho/internal/textutil"
)

// ModelBenchmark orchestrates benchmarking multiple LLM models on standardized tasks.
type ModelBenchmark struct {
	Name    string
	Models  []ModelConfig
	Tasks   []BenchmarkTask
	Runs    int
	Results map[string]*ModelResult
}

// ModelConfig describes an LLM model configuration for benchmarking.
type ModelConfig struct {
	Name        string
	Provider    string
	Model       string
	Temperature float64
	MaxTokens   int
}

// ModelResult holds aggregated benchmark results for a single model.
type ModelResult struct {
	Model       string
	PassRate    float64
	AvgTokens   int
	AvgCostUSD  float64
	AvgDuration time.Duration
	P50Duration time.Duration
	P95Duration time.Duration
	TaskResults []TaskResult
	Strengths   []string
	Weaknesses  []string
}

// NewModelBenchmark creates a new benchmark configured with the given models.
func NewModelBenchmark(name string, models []ModelConfig) *ModelBenchmark {
	return &ModelBenchmark{
		Name:    name,
		Models:  models,
		Tasks:   nil,
		Runs:    1,
		Results: make(map[string]*ModelResult),
	}
}

// RunAll executes all tasks for all models, repeating Runs times each.
// chatFn is called with (ctx, model config, prompt) and returns (response, tokens, cost, error).
func (mb *ModelBenchmark) RunAll(ctx context.Context, chatFn func(context.Context, ModelConfig, string) (string, int, float64, error)) error {
	if mb.Runs < 1 {
		mb.Runs = 1
	}

	for _, model := range mb.Models {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		result := &ModelResult{
			Model:       model.Name,
			TaskResults: make([]TaskResult, 0, len(mb.Tasks)*mb.Runs),
		}

		var durations []time.Duration
		totalTokens := 0
		totalCost := 0.0
		passed := 0
		total := 0

		for _, task := range mb.Tasks {
			for run := 0; run < mb.Runs; run++ {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				start := time.Now()
				_, tokens, cost, err := chatFn(ctx, model, task.Prompt)
				duration := time.Since(start)

				tr := TaskResult{
					TaskID:     task.ID,
					Duration:   duration,
					TokensUsed: tokens,
					CostUSD:    cost,
					Attempts:   1,
				}

				if err != nil {
					tr.Passed = false
					tr.Error = err.Error()
				} else {
					tr.Passed = true
					passed++
				}

				total++
				totalTokens += tokens
				totalCost += cost
				durations = append(durations, duration)
				result.TaskResults = append(result.TaskResults, tr)
			}
		}

		if total > 0 {
			result.PassRate = float64(passed) / float64(total)
			result.AvgTokens = totalTokens / total
			result.AvgCostUSD = totalCost / float64(total)

			var totalDur time.Duration
			for _, d := range durations {
				totalDur += d
			}
			result.AvgDuration = totalDur / time.Duration(total)
			result.P50Duration = Percentile(durations, 50)
			result.P95Duration = Percentile(durations, 95)
		}

		strengths, weaknesses := mb.analyzeModelStrengths(result)
		result.Strengths = strengths
		result.Weaknesses = weaknesses

		mb.Results[model.Name] = result
	}

	return nil
}

// Compare produces a side-by-side model comparison string.
func (mb *ModelBenchmark) Compare() string {
	if len(mb.Results) == 0 {
		return "No benchmark results available.\n"
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Model Benchmark: %q\n", mb.Name))
	sb.WriteString(strings.Repeat("═", 60))
	sb.WriteString("\n\n")

	// Header
	sb.WriteString(fmt.Sprintf("%-22s %-12s %-13s %-12s %s\n",
		"Model", "Pass Rate", "Avg Tokens", "Cost/Task", "Latency"))
	sb.WriteString(strings.Repeat("─", 70))
	sb.WriteString("\n")

	// Sort models by pass rate descending for display.
	ranked := mb.RankModels("quality")
	for _, r := range ranked {
		sb.WriteString(fmt.Sprintf(
			"%-22s %-12s %-13s %-12s %s\n",
			truncate(r.Model, 20),
			fmt.Sprintf("%.1f%%", r.PassRate*100),
			fmt.Sprintf("%d", r.AvgTokens),
			fmt.Sprintf("$%.3f", r.AvgCostUSD),
			formatBenchDuration(r.AvgDuration),
		))
	}

	sb.WriteString(strings.Repeat("─", 70))
	sb.WriteString("\n\n")

	// Summary recommendations.
	if bestValue := mb.findBestValue(); bestValue != nil {
		sb.WriteString(fmt.Sprintf("Best value: %s (%.0f%% pass at $%.3f/task)\n",
			bestValue.Model, bestValue.PassRate*100, bestValue.AvgCostUSD))
	}
	if bestQuality := mb.findBestQuality(); bestQuality != nil {
		sb.WriteString(fmt.Sprintf("Best quality: %s (%.0f%% pass)\n",
			bestQuality.Model, bestQuality.PassRate*100))
	}
	if bestSpeed := mb.findBestSpeed(); bestSpeed != nil {
		sb.WriteString(fmt.Sprintf("Best speed: %s (%s avg)\n",
			bestSpeed.Model, formatBenchDuration(bestSpeed.AvgDuration)))
	}

	return sb.String()
}

// RankModels returns model results sorted by the given criterion.
func (mb *ModelBenchmark) RankModels(by string) []ModelResult {
	if len(mb.Results) == 0 {
		return nil
	}

	results := make([]ModelResult, 0, len(mb.Results))
	for _, r := range mb.Results {
		results = append(results, *r)
	}

	switch by {
	case "quality":
		sort.Slice(results, func(i, j int) bool {
			return results[i].PassRate > results[j].PassRate
		})
	case "cost":
		sort.Slice(results, func(i, j int) bool {
			return results[i].AvgCostUSD < results[j].AvgCostUSD
		})
	case "speed":
		sort.Slice(results, func(i, j int) bool {
			return results[i].AvgDuration < results[j].AvgDuration
		})
	case "value":
		sort.Slice(results, func(i, j int) bool {
			vi := valueRatio(results[i])
			vj := valueRatio(results[j])
			return vi > vj
		})
	default:
		// Default to quality.
		sort.Slice(results, func(i, j int) bool {
			return results[i].PassRate > results[j].PassRate
		})
	}

	return results
}

// RecommendModel recommends the best model for the given task type based on results.
func (mb *ModelBenchmark) RecommendModel(taskType string) string {
	if len(mb.Results) == 0 {
		return ""
	}

	switch taskType {
	case "simple":
		// Cheapest model with >80% pass rate.
		ranked := mb.RankModels("cost")
		for _, r := range ranked {
			if r.PassRate > 0.80 {
				return r.Model
			}
		}
		// If none meet threshold, return cheapest anyway.
		if len(ranked) > 0 {
			return ranked[0].Model
		}
	case "complex":
		// Highest pass rate model.
		ranked := mb.RankModels("quality")
		if len(ranked) > 0 {
			return ranked[0].Model
		}
	case "budget":
		// Best value ratio (pass_rate / cost_per_task).
		ranked := mb.RankModels("value")
		if len(ranked) > 0 {
			return ranked[0].Model
		}
	default:
		// Default to quality.
		ranked := mb.RankModels("quality")
		if len(ranked) > 0 {
			return ranked[0].Model
		}
	}
	return ""
}

// AnalyzeStrengths returns (strengths, weaknesses) for the given model by looking
// at which task categories the model excels or fails at.
func (mb *ModelBenchmark) AnalyzeStrengths(model string) ([]string, []string) {
	result, ok := mb.Results[model]
	if !ok || len(result.TaskResults) == 0 {
		return nil, nil
	}

	// Build a map of task ID to tags.
	taskTags := make(map[string][]string)
	for _, task := range mb.Tasks {
		taskTags[task.ID] = task.Tags
	}

	// Count pass/fail per tag.
	tagPass := make(map[string]int)
	tagFail := make(map[string]int)

	for _, tr := range result.TaskResults {
		tags := taskTags[tr.TaskID]
		if len(tags) == 0 {
			tags = []string{"general"}
		}
		for _, tag := range tags {
			if tr.Passed {
				tagPass[tag]++
			} else {
				tagFail[tag]++
			}
		}
	}

	var strengths, weaknesses []string

	// A tag is a strength if pass rate >= 80%, weakness if <= 50%.
	allTags := make([]string, 0, len(tagPass)+len(tagFail))
	tagSeen := make(map[string]bool)
	for tag := range tagPass {
		if !tagSeen[tag] {
			allTags = append(allTags, tag)
			tagSeen[tag] = true
		}
	}
	for tag := range tagFail {
		if !tagSeen[tag] {
			allTags = append(allTags, tag)
			tagSeen[tag] = true
		}
	}
	sort.Strings(allTags)

	for _, tag := range allTags {
		p := tagPass[tag]
		f := tagFail[tag]
		total := p + f
		if total == 0 {
			continue
		}
		rate := float64(p) / float64(total)
		if rate >= 0.80 {
			strengths = append(strengths, tag)
		} else if rate <= 0.50 {
			weaknesses = append(weaknesses, tag)
		}
	}

	return strengths, weaknesses
}

// ExportCSV exports benchmark results as CSV for external analysis.
func (mb *ModelBenchmark) ExportCSV() string {
	var sb strings.Builder

	sb.WriteString("model,task_id,passed,duration_ms,tokens,cost_usd,error\n")

	// Sort model names for deterministic output.
	models := make([]string, 0, len(mb.Results))
	for name := range mb.Results {
		models = append(models, name)
	}
	sort.Strings(models)

	for _, modelName := range models {
		result := mb.Results[modelName]
		for _, tr := range result.TaskResults {
			passedStr := "false"
			if tr.Passed {
				passedStr = "true"
			}
			errStr := strings.ReplaceAll(tr.Error, ",", ";")
			errStr = strings.ReplaceAll(errStr, "\n", " ")
			sb.WriteString(fmt.Sprintf("%s,%s,%s,%d,%d,%.6f,%s\n",
				modelName, tr.TaskID, passedStr,
				tr.Duration.Milliseconds(), tr.TokensUsed, tr.CostUSD, errStr))
		}
	}

	return sb.String()
}

// Percentile computes the given percentile from a slice of durations.
// pct should be between 0 and 100 (e.g., 50 for P50, 95 for P95).
func Percentile(durations []time.Duration, pct float64) time.Duration {
	if len(durations) == 0 {
		return 0
	}

	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	if pct <= 0 {
		return sorted[0]
	}
	if pct >= 100 {
		return sorted[len(sorted)-1]
	}

	// Use nearest-rank method.
	rank := (pct / 100.0) * float64(len(sorted))
	idx := int(rank)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	if idx < 0 {
		idx = 0
	}

	return sorted[idx]
}

// analyzeModelStrengths computes strengths/weaknesses for a model result during RunAll.
func (mb *ModelBenchmark) analyzeModelStrengths(result *ModelResult) ([]string, []string) {
	if len(result.TaskResults) == 0 {
		return nil, nil
	}

	// Build a map of task ID to tags.
	taskTags := make(map[string][]string)
	for _, task := range mb.Tasks {
		taskTags[task.ID] = task.Tags
	}

	tagPass := make(map[string]int)
	tagFail := make(map[string]int)

	for _, tr := range result.TaskResults {
		tags := taskTags[tr.TaskID]
		if len(tags) == 0 {
			tags = []string{"general"}
		}
		for _, tag := range tags {
			if tr.Passed {
				tagPass[tag]++
			} else {
				tagFail[tag]++
			}
		}
	}

	var strengths, weaknesses []string

	allTags := make([]string, 0, len(tagPass)+len(tagFail))
	tagSeen := make(map[string]bool)
	for tag := range tagPass {
		if !tagSeen[tag] {
			allTags = append(allTags, tag)
			tagSeen[tag] = true
		}
	}
	for tag := range tagFail {
		if !tagSeen[tag] {
			allTags = append(allTags, tag)
			tagSeen[tag] = true
		}
	}
	sort.Strings(allTags)

	for _, tag := range allTags {
		p := tagPass[tag]
		f := tagFail[tag]
		total := p + f
		if total == 0 {
			continue
		}
		rate := float64(p) / float64(total)
		if rate >= 0.80 {
			strengths = append(strengths, tag)
		} else if rate <= 0.50 {
			weaknesses = append(weaknesses, tag)
		}
	}

	return strengths, weaknesses
}

// findBestValue returns the model with the best value ratio.
func (mb *ModelBenchmark) findBestValue() *ModelResult {
	ranked := mb.RankModels("value")
	if len(ranked) == 0 {
		return nil
	}
	return &ranked[0]
}

// findBestQuality returns the model with the highest pass rate.
func (mb *ModelBenchmark) findBestQuality() *ModelResult {
	ranked := mb.RankModels("quality")
	if len(ranked) == 0 {
		return nil
	}
	return &ranked[0]
}

// findBestSpeed returns the model with the lowest average duration.
func (mb *ModelBenchmark) findBestSpeed() *ModelResult {
	ranked := mb.RankModels("speed")
	if len(ranked) == 0 {
		return nil
	}
	return &ranked[0]
}

// valueRatio computes pass_rate / cost_per_task, handling zero cost.
func valueRatio(r ModelResult) float64 {
	if r.AvgCostUSD <= 0 {
		if r.PassRate > 0 {
			return 1e9 // effectively infinite value
		}
		return 0
	}
	return r.PassRate / r.AvgCostUSD
}

// truncate shortens a string to at most maxLen bytes.
func truncate(s string, maxLen int) string {
	return textutil.Truncate(s, maxLen)
}

// formatBenchDuration formats a duration for benchmark display.
func formatBenchDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dns", d.Nanoseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
