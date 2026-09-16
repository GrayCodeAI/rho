package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
)

// ToolHealthTool reports the executable tool surface and common runtime
// prerequisites without exposing environment variables or credentials.
// It is deliberately read-only so the model can diagnose missing capabilities
// before attempting a task.
type ToolHealthTool struct{}

// ToolHealthInput is the typed input for ToolHealthTool.
type ToolHealthInput struct {
	IncludeOptional *bool `json:"include_optional"`
}

func (ToolHealthTool) Name() string      { return "ToolHealth" }
func (ToolHealthTool) RiskLevel() string { return "low" }
func (ToolHealthTool) Aliases() []string { return []string{"tool-health", "tools_health"} }
func (ToolHealthTool) Description() string {
	return "Inspect Rho's registered/model-visible tools and common runtime prerequisites (git, go, node, Python, Docker, gh, and Chrome) without revealing secrets or changing state."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (ToolHealthTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"include_optional": {Type: "boolean", Description: "Include lazy-registered tools in the report (default true)."},
		},
	}
}

func (ToolHealthTool) Parameters() map[string]interface{} {
	return toolHealthSchema.ToJSONSchema()
}

// toolHealthSchema is the single source of truth for ToolHealth's input schema.
var toolHealthSchema = ToolHealthTool{}.Schema()

type toolHealthReport struct {
	Registered    []toolHealthEntry    `json:"registered_tools"`
	Visible       []string             `json:"model_visible_tools"`
	Prerequisites []prerequisiteStatus `json:"prerequisites"`
}

type toolHealthEntry struct {
	Name string `json:"name"`
	Risk string `json:"risk"`
}

type prerequisiteStatus struct {
	Name       string `json:"name"`
	Executable string `json:"executable"`
	Available  bool   `json:"available"`
}

func (ToolHealthTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params ToolHealthInput
	if len(input) > 0 && string(input) != "null" {
		decoded, err := DecodeInput[ToolHealthInput]("ToolHealth", input)
		if err != nil {
			return "", err
		}
		params = decoded
	}
	includeOptional := true
	if params.IncludeOptional != nil {
		includeOptional = *params.IncludeOptional
	}

	report := toolHealthReport{
		Prerequisites: runtimePrerequisites(),
	}
	if tc := GetToolContext(ctx); tc != nil && tc.Registry != nil {
		visible := tc.Registry.ModelVisibleNames()
		sort.Strings(visible)
		report.Visible = visible
		for _, candidate := range tc.Registry.PrimaryTools() {
			if !includeOptional && !tc.Registry.IsModelVisible(candidate.Name()) {
				continue
			}
			risk := "medium"
			if rp, ok := candidate.(RiskLevelProvider); ok && rp.RiskLevel() != "" {
				risk = rp.RiskLevel()
			}
			report.Registered = append(report.Registered, toolHealthEntry{Name: candidate.Name(), Risk: risk})
		}
	} else if tc != nil {
		for _, candidate := range tc.AvailableTools {
			risk := "medium"
			if rp, ok := candidate.(RiskLevelProvider); ok && rp.RiskLevel() != "" {
				risk = rp.RiskLevel()
			}
			report.Registered = append(report.Registered, toolHealthEntry{Name: candidate.Name(), Risk: risk})
		}
	}

	if report.Registered == nil {
		report.Registered = []toolHealthEntry{}
	}
	if report.Visible == nil {
		report.Visible = []string{}
	}
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode health report: %w", err)
	}
	return string(out), nil
}

func runtimePrerequisites() []prerequisiteStatus {
	checks := []struct {
		name string
		exec string
	}{
		{"git", "git"},
		{"go", "go"},
		{"node", "node"},
		{"npm", "npm"},
		{"python", "python3"},
		{"docker", "docker"},
		{"github_cli", "gh"},
		{"chrome", "google-chrome"},
		{"chromium", "chromium"},
	}
	result := make([]prerequisiteStatus, 0, len(checks))
	for _, check := range checks {
		_, err := exec.LookPath(check.exec)
		result = append(result, prerequisiteStatus{Name: check.name, Executable: check.exec, Available: err == nil})
	}
	return result
}
