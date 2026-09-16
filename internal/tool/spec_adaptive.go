package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SpecAdaptiveTool struct{}

func (SpecAdaptiveTool) Name() string { return "SpecAdaptive" }

// SpecAdaptiveInput is the typed input for SpecAdaptiveTool.
type SpecAdaptiveInput struct {
	TaskID          string  `json:"task_id"`
	EstimatedEffort int     `json:"estimated_effort"`
	ActualEffort    int     `json:"actual_effort"`
	UnplannedDeps   int     `json:"unplanned_deps"`
	SuperScore      float64 `json:"super_score"`
}

func (SpecAdaptiveTool) Aliases() []string {
	return []string{"spec_adaptive", "spec:adaptive"}
}

func (SpecAdaptiveTool) Description() string {
	return "Collect execution telemetry and compute drift score. Compares actual effort vs estimated, S.U.P.E.R compliance, and unplanned dependencies. Returns drift level (none/mild/significant/severe) and recommended corrective action."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecAdaptiveTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"task_id":          {Type: "string", Description: "Task identifier that was just completed"},
			"estimated_effort": {Type: "integer", Description: "Estimated effort in minutes"},
			"actual_effort":    {Type: "integer", Description: "Actual effort in minutes"},
			"unplanned_deps":   {Type: "integer", Description: "Number of unplanned dependencies encountered"},
			"super_score":      {Type: "number", Description: "S.U.P.E.R compliance score 0.0-1.0"},
		},

		Required: []string{"task_id"},
	}
}

func (SpecAdaptiveTool) Parameters() map[string]interface{} {
	return specAdaptiveSchema.ToJSONSchema()
}

// specAdaptiveSchema is the single source of truth for SpecAdaptive's input schema.
var specAdaptiveSchema = SpecAdaptiveTool{}.Schema()

type AdaptiveResult struct {
	DriftScore     float64 `json:"drift_score"`
	DriftLevel     string  `json:"drift_level"`
	Recommendation string  `json:"recommendation"`
}

func (SpecAdaptiveTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[SpecAdaptiveInput]("SpecAdaptive", input)
	if err != nil {
		return "", err
	}

	driftScore := computeDrift(p.EstimatedEffort, p.ActualEffort, p.UnplannedDeps, p.SuperScore)
	driftLevel := classifyDrift(driftScore)
	recommendation := recommendAction(driftLevel)

	dir, err := specDir(ctx)
	if err == nil {
		recordTelemetry(dir, p.TaskID, driftScore, driftLevel, p)
	}

	result := AdaptiveResult{
		DriftScore:     driftScore,
		DriftLevel:     driftLevel,
		Recommendation: recommendation,
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## Adaptive Control Report\n\n")
	fmt.Fprintf(&b, "**Drift Score**: %.2f\n", result.DriftScore)
	fmt.Fprintf(&b, "**Drift Level**: %s\n", result.DriftLevel)
	fmt.Fprintf(&b, "**Recommendation**: %s\n\n", result.Recommendation)

	if driftLevel != "none" {
		b.WriteString("### Telemetry Summary\n\n")
		if p.EstimatedEffort > 0 {
			ratio := float64(p.ActualEffort) / float64(p.EstimatedEffort)
			fmt.Fprintf(&b, "- **Effort Ratio**: %.1fx estimated (%d min estimated, %d min actual)\n", ratio, p.EstimatedEffort, p.ActualEffort)
		}
		fmt.Fprintf(&b, "- **Unplanned Dependencies**: %d\n", p.UnplannedDeps)
		fmt.Fprintf(&b, "- **S.U.P.E.R Compliance**: %.0f%%\n\n", p.SuperScore*100)
	}

	aggregate := loadAggregateTelemetry(dir)
	if aggregate.Count > 1 {
		fmt.Fprintf(&b, "### Cumulative Drift (%d tasks)\n\n", aggregate.Count)
		fmt.Fprintf(&b, "- **Mean Drift**: %.2f\n", aggregate.MeanDrift)
		fmt.Fprintf(&b, "- **Trend**: %s\n", aggregate.Trend)
		cumulativeLevel := classifyDrift(aggregate.MeanDrift)
		if cumulativeLevel != driftLevel {
			fmt.Fprintf(&b, "- **Cumulative Level**: %s\n", cumulativeLevel)
		}
	}

	return strings.TrimSpace(b.String()), nil
}

func computeDrift(estimated, actual, unplannedDeps int, superScore float64) float64 {
	var effortDrift float64
	if estimated > 0 {
		effortDrift = math.Abs(float64(actual-estimated)) / float64(estimated)
	} else if actual > 0 {
		effortDrift = 1.0
	}

	depDrift := math.Min(float64(unplannedDeps)*0.15, 0.6)
	superDrift := (1.0 - superScore) * 0.3

	drift := effortDrift*0.5 + depDrift*0.3 + superDrift*0.2
	return math.Min(drift, 1.0)
}

func classifyDrift(score float64) string {
	switch {
	case score >= 0.6:
		return "severe"
	case score >= 0.4:
		return "significant"
	case score >= 0.2:
		return "mild"
	default:
		return "none"
	}
}

func recommendAction(level string) string {
	switch level {
	case "severe":
		return "HALT: Return to Intent Refinement phase. Scope or plan needs re-evaluation."
	case "significant":
		return "RECOMPOSE: Re-decompose remaining tasks. Update estimates and dependencies."
	case "mild":
		return "ANNOTATE: Continue with caution. Add warning to next task."
	default:
		return "PROCEED: Execution on track. Continue with next task."
	}
}

type taskTelemetry struct {
	TaskID    string  `json:"task_id"`
	Drift     float64 `json:"drift"`
	Level     string  `json:"level"`
	Timestamp string  `json:"timestamp"`
}

type aggregateTelemetry struct {
	Count     int     `json:"count"`
	MeanDrift float64 `json:"mean_drift"`
	Trend     string  `json:"trend"`
}

func recordTelemetry(dir, taskID string, drift float64, level string, p struct {
	TaskID          string  `json:"task_id"`
	EstimatedEffort int     `json:"estimated_effort"`
	ActualEffort    int     `json:"actual_effort"`
	UnplannedDeps   int     `json:"unplanned_deps"`
	SuperScore      float64 `json:"super_score"`
},
) {
	telemetryPath := filepath.Join(dir, ".telemetry.json")

	var telemetry []taskTelemetry
	if data, err := os.ReadFile(telemetryPath); err == nil {
		_ = json.Unmarshal(data, &telemetry)
	}

	entry := taskTelemetry{
		TaskID:    taskID,
		Drift:     drift,
		Level:     level,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	telemetry = append(telemetry, entry)

	if data, err := json.MarshalIndent(telemetry, "", "  "); err == nil {
		_ = os.WriteFile(telemetryPath, data, 0o600)
	}
}

func loadAggregateTelemetry(dir string) aggregateTelemetry {
	telemetryPath := filepath.Join(dir, ".telemetry.json")

	var telemetry []taskTelemetry
	if data, err := os.ReadFile(telemetryPath); err != nil {
		return aggregateTelemetry{}
	} else if err := json.Unmarshal(data, &telemetry); err != nil {
		return aggregateTelemetry{}
	}

	if len(telemetry) == 0 {
		return aggregateTelemetry{}
	}

	var sum float64
	for _, t := range telemetry {
		sum += t.Drift
	}
	mean := sum / float64(len(telemetry))

	trend := "stable"
	if len(telemetry) >= 3 {
		recent := telemetry[len(telemetry)-3:]
		increasing := 0
		for i := 1; i < len(recent); i++ {
			if recent[i].Drift > recent[i-1].Drift {
				increasing++
			}
		}
		if increasing >= 2 {
			trend = "worsening"
		} else if increasing == 0 {
			trend = "improving"
		}
	}

	return aggregateTelemetry{
		Count:     len(telemetry),
		MeanDrift: mean,
		Trend:     trend,
	}
}

func init() {
	_ = SpecAdaptiveTool{}
}
