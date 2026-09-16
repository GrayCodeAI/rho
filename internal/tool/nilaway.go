package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// NilAwayTool provides nil panic detection using Uber's NilAway static analyzer.
// Research shows NilAway catches nil pointer dereferences that other linters miss.
// It uses sophisticated interprocedural analysis to track nilability through
// function calls, interfaces, and complex control flow.
type NilAwayTool struct{}

// NilAwayInput is the typed input for NilAwayTool.
type NilAwayInput struct {
	Path string `json:"path"`
	Fix  bool   `json:"fix"`
}

func (NilAwayTool) Name() string      { return "NilAway" }
func (NilAwayTool) Aliases() []string { return []string{"nilaway", "nil"} }
func (NilAwayTool) Description() string {
	return "Detect potential nil panics using NilAway static analyzer. Catches nil pointer dereferences that other linters miss."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (NilAwayTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"path": {Type: "string", Description: "Path to analyze (default: current directory)"},
			"fix":  {Type: "boolean", Description: "Show suggested fixes", Default: false},
		},
	}
}

func (NilAwayTool) Parameters() map[string]interface{} {
	return nilAwaySchema.ToJSONSchema()
}

// nilAwaySchema is the single source of truth for NilAway's input schema.
var nilAwaySchema = NilAwayTool{}.Schema()

func (NilAwayTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[NilAwayInput]("NilAway", input)
	if err != nil {
		return "", err
	}

	if p.Path == "" {
		p.Path = "."
	}

	return runNilAway(ctx, p.Path, p.Fix)
}

func runNilAway(ctx context.Context, path string, showFix bool) (string, error) {
	// Try to run nilaway
	args := []string{"-json", "./..."}
	if showFix {
		args = append(args, "-fix")
	}

	cmd := exec.CommandContext(ctx, "nilaway", args...)
	cmd.Dir = path

	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))

	if err != nil {
		// Check if nilaway is not installed
		if strings.Contains(result, "command not found") || strings.Contains(result, "not found") {
			return "NilAway not installed. Install with: go install go.uber.org/nilaway/cmd/nilaway@latest", nil
		}

		// nilaway returns non-zero when issues found
		if result == "" {
			return fmt.Sprintf("NilAway exited with error: %v", err), nil
		}
	}

	if result == "" {
		return "No nil safety issues found.", nil
	}

	// Format the output
	var formatted strings.Builder
	formatted.WriteString("## NilAway Analysis\n\n")

	// Try to parse as JSON
	var issues []NilAwayIssue
	if err := json.Unmarshal([]byte(result), &issues); err == nil && len(issues) > 0 {
		formatted.WriteString(fmt.Sprintf("Found %d potential nil panics:\n\n", len(issues)))
		for _, issue := range issues {
			formatted.WriteString(fmt.Sprintf("- **%s** in %s:%d\n  %s\n\n",
				issue.Severity, issue.Position.Filename, issue.Position.Line, issue.Message))
		}
	} else {
		// Raw output
		formatted.WriteString("```\n")
		formatted.WriteString(result)
		formatted.WriteString("\n```\n")
	}

	return formatted.String(), nil
}

// NilAwayIssue represents a NilAway finding.
type NilAwayIssue struct {
	Severity string   `json:"severity"`
	Message  string   `json:"message"`
	Position Position `json:"position"`
}

type Position struct {
	Filename string `json:"filename"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}

// ReviveTool provides fast Go linting using revive.
// Research shows revive is 6x faster than golint with more rules.
type ReviveTool struct{}

// ReviveInput is the typed input for ReviveTool.
type ReviveInput struct {
	Path   string `json:"path"`
	Config string `json:"config"`
}

func (ReviveTool) Name() string      { return "Revive" }
func (ReviveTool) Aliases() []string { return []string{"revive"} }
func (ReviveTool) Description() string {
	return "Fast Go linter (6x faster than golint). Configurable rules for code quality."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (ReviveTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"path":   {Type: "string", Description: "Path to lint (default: current directory)"},
			"config": {Type: "string", Description: "Config file path (default: uses defaults)"},
		},
	}
}

func (ReviveTool) Parameters() map[string]interface{} {
	return reviveSchema.ToJSONSchema()
}

// reviveSchema is the single source of truth for Revive's input schema.
var reviveSchema = ReviveTool{}.Schema()

func (ReviveTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[ReviveInput]("Revive", input)
	if err != nil {
		return "", err
	}

	if p.Path == "" {
		p.Path = "."
	}

	return runRevive(ctx, p.Path, p.Config)
}

func runRevive(ctx context.Context, path, config string) (string, error) {
	args := []string{"-formatter", "json", "./..."}
	if config != "" {
		args = append(args, "-config", config)
	}

	cmd := exec.CommandContext(ctx, "revive", args...)
	cmd.Dir = path

	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))

	if err != nil {
		if strings.Contains(result, "command not found") || strings.Contains(result, "not found") {
			return "Revive not installed. Install with: go install github.com/mgechev/revive@latest", nil
		}
	}

	if result == "" || result == "[]" {
		return "No lint issues found.", nil
	}

	// Format the output
	var formatted strings.Builder
	formatted.WriteString("## Revive Lint Results\n\n")

	var issues []ReviveIssue
	if err := json.Unmarshal([]byte(result), &issues); err == nil && len(issues) > 0 {
		formatted.WriteString(fmt.Sprintf("Found %d issues:\n\n", len(issues)))
		for _, issue := range issues {
			severity := issue.Severity
			if severity == "" {
				severity = "warning"
			}
			formatted.WriteString(fmt.Sprintf("- [%s] **%s** in %s:%d\n  %s\n\n",
				severity, issue.RuleName, issue.Position.Start.Filename,
				issue.Position.Start.Line, issue.Failure))
		}
	} else {
		formatted.WriteString("```\n")
		formatted.WriteString(result)
		formatted.WriteString("\n```\n")
	}

	return formatted.String(), nil
}

// ReviveIssue represents a revive lint finding.
type ReviveIssue struct {
	RuleName string         `json:"ruleName"`
	Severity string         `json:"severity"`
	Failure  string         `json:"failure"`
	Position RevivePosition `json:"position"`
}

type RevivePosition struct {
	Start ReviveLocation `json:"start"`
	End   ReviveLocation `json:"end"`
}

type ReviveLocation struct {
	Filename string `json:"filename"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}
