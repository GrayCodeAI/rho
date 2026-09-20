package eval

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// GenerateReport produces a markdown report from a suite result.
func GenerateReport(result *SuiteResult) string {
	if result == nil {
		return "# No Results\n\nNo benchmark results available.\n"
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Benchmark Report: %s\n\n", result.Suite))

	// Summary section.
	sb.WriteString("## Summary\n\n")
	sb.WriteString("| Metric | Value |\n")
	sb.WriteString("|--------|-------|\n")
	sb.WriteString(fmt.Sprintf("| Total Tasks | %d |\n", result.TotalTasks))
	sb.WriteString(fmt.Sprintf("| Passed | %d |\n", result.Passed))
	sb.WriteString(fmt.Sprintf("| Failed | %d |\n", result.Failed))
	sb.WriteString(fmt.Sprintf("| Pass Rate | %.1f%% |\n", result.PassRate*100))
	sb.WriteString(fmt.Sprintf("| Total Duration | %s |\n", formatDuration(result.TotalDuration)))
	sb.WriteString(fmt.Sprintf("| Total Tokens | %d |\n", result.TotalTokens))
	sb.WriteString(fmt.Sprintf("| Total Cost | $%.4f |\n", result.TotalCostUSD))
	sb.WriteString("\n")

	// Task details.
	sb.WriteString("## Task Results\n\n")
	sb.WriteString("| Task | Status | Duration | Tokens | Cost | Attempts |\n")
	sb.WriteString("|------|--------|----------|--------|------|----------|\n")

	for _, r := range result.Results {
		status := "PASS"
		if !r.Passed {
			status = "FAIL"
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %d | $%.4f | %d |\n",
			r.TaskID, status, formatDuration(r.Duration), r.TokensUsed, r.CostUSD, r.Attempts))
	}
	sb.WriteString("\n")

	// Failed task details.
	var failures []TaskResult
	for _, r := range result.Results {
		if !r.Passed {
			failures = append(failures, r)
		}
	}
	if len(failures) > 0 {
		sb.WriteString("## Failures\n\n")
		for _, f := range failures {
			sb.WriteString(fmt.Sprintf("### %s\n\n", f.TaskID))
			sb.WriteString(fmt.Sprintf("**Error:** %s\n\n", f.Error))
		}
	}

	return sb.String()
}

// GenerateLeaderboard produces a markdown leaderboard comparing multiple suite results.
func GenerateLeaderboard(results []SuiteResult) string {
	if len(results) == 0 {
		return "# Leaderboard\n\nNo results to display.\n"
	}

	// Sort by pass rate descending, then by duration ascending.
	sorted := make([]SuiteResult, len(results))
	copy(sorted, results)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].PassRate != sorted[j].PassRate {
			return sorted[i].PassRate > sorted[j].PassRate
		}
		return sorted[i].TotalDuration < sorted[j].TotalDuration
	})

	var sb strings.Builder

	sb.WriteString("# Leaderboard\n\n")
	sb.WriteString("| Rank | Suite | Pass Rate | Passed | Failed | Duration | Tokens | Cost |\n")
	sb.WriteString("|------|-------|-----------|--------|--------|----------|--------|------|\n")

	for i, r := range sorted {
		sb.WriteString(fmt.Sprintf("| %d | %s | %.1f%% | %d/%d | %d | %s | %d | $%.4f |\n",
			i+1, r.Suite, r.PassRate*100, r.Passed, r.TotalTasks, r.Failed,
			formatDuration(r.TotalDuration), r.TotalTokens, r.TotalCostUSD))
	}
	sb.WriteString("\n")

	return sb.String()
}

// CompareModels produces a side-by-side comparison table for multiple models.
func CompareModels(results map[string]*SuiteResult) string {
	if len(results) == 0 {
		return "# Model Comparison\n\nNo results to compare.\n"
	}

	// Collect and sort model names for consistent ordering.
	models := make([]string, 0, len(results))
	for model := range results {
		models = append(models, model)
	}
	sort.Strings(models)

	var sb strings.Builder

	sb.WriteString("# Model Comparison\n\n")

	// Overall summary table.
	sb.WriteString("## Overall Performance\n\n")
	sb.WriteString("| Metric |")
	for _, m := range models {
		sb.WriteString(fmt.Sprintf(" %s |", m))
	}
	sb.WriteString("\n|--------|")
	for range models {
		sb.WriteString("--------|")
	}
	sb.WriteString("\n")

	// Pass rate row.
	sb.WriteString("| Pass Rate |")
	for _, m := range models {
		sb.WriteString(fmt.Sprintf(" %.1f%% |", results[m].PassRate*100))
	}
	sb.WriteString("\n")

	// Passed row.
	sb.WriteString("| Passed |")
	for _, m := range models {
		sb.WriteString(fmt.Sprintf(" %d/%d |", results[m].Passed, results[m].TotalTasks))
	}
	sb.WriteString("\n")

	// Duration row.
	sb.WriteString("| Duration |")
	for _, m := range models {
		sb.WriteString(fmt.Sprintf(" %s |", formatDuration(results[m].TotalDuration)))
	}
	sb.WriteString("\n")

	// Tokens row.
	sb.WriteString("| Tokens |")
	for _, m := range models {
		sb.WriteString(fmt.Sprintf(" %d |", results[m].TotalTokens))
	}
	sb.WriteString("\n")

	// Cost row.
	sb.WriteString("| Cost |")
	for _, m := range models {
		sb.WriteString(fmt.Sprintf(" $%.4f |", results[m].TotalCostUSD))
	}
	sb.WriteString("\n\n")

	// Per-task comparison.
	sb.WriteString("## Per-Task Comparison\n\n")

	// Collect all task IDs from the first result set.
	var taskIDs []string
	for _, m := range models {
		if results[m] != nil {
			for _, r := range results[m].Results {
				taskIDs = append(taskIDs, r.TaskID)
			}
			break
		}
	}

	if len(taskIDs) > 0 {
		sb.WriteString("| Task |")
		for _, m := range models {
			sb.WriteString(fmt.Sprintf(" %s |", m))
		}
		sb.WriteString("\n|------|")
		for range models {
			sb.WriteString("--------|")
		}
		sb.WriteString("\n")

		for _, taskID := range taskIDs {
			sb.WriteString(fmt.Sprintf("| %s |", taskID))
			for _, m := range models {
				found := false
				for _, r := range results[m].Results {
					if r.TaskID == taskID {
						if r.Passed {
							sb.WriteString(" PASS |")
						} else {
							sb.WriteString(" FAIL |")
						}
						found = true
						break
					}
				}
				if !found {
					sb.WriteString(" - |")
				}
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// formatDuration formats a duration for human-readable display.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}
