package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DependencyAuditTool inspects dependency integrity and available updates. It
// never installs, upgrades, or edits dependency files; network access is only
// performed by the package manager when its native audit/outdated command
// requires it and remains subject to the session's network policy.
type DependencyAuditTool struct{}

func (DependencyAuditTool) Name() string      { return "DependencyAudit" }
func (DependencyAuditTool) RiskLevel() string { return "medium" }
func (DependencyAuditTool) Aliases() []string { return []string{"dependency-audit", "deps"} }
func (DependencyAuditTool) Description() string {
	return "Audit dependency integrity and report outdated packages without installing or changing anything. Supports Go, npm, Python, and Cargo projects with structured results."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (DependencyAuditTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action":          {Type: "string", Enum: []interface{}{"check", "outdated", "all"}, Description: "check validates dependency integrity; outdated reports available updates; all runs both."},
			"path":            {Type: "string", Description: "Project directory (default: session working directory)."},
			"timeout_seconds": {Type: "integer", Minimum: 1, Maximum: 300, Description: "Per-command timeout (default 60 seconds)."},
		},
		Required: []string{"action"},
	}
}

func (DependencyAuditTool) Parameters() map[string]interface{} {
	return dependencyAuditSchema.ToJSONSchema()
}

// dependencyAuditSchema is the single source of truth for DependencyAudit's input schema.
var dependencyAuditSchema = DependencyAuditTool{}.Schema()

// DependencyAuditInput is the typed input for DependencyAuditTool.
type DependencyAuditInput struct {
	Action         string `json:"action"`
	Path           string `json:"path"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (DependencyAuditTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	params, err := DecodeInput[DependencyAuditInput]("DependencyAudit", input)
	if err != nil {
		return "", err
	}
	params.Action = strings.ToLower(strings.TrimSpace(params.Action))
	if params.Action != "check" && params.Action != "outdated" && params.Action != "all" {
		return "", fmt.Errorf("unsupported action %q (use check, outdated, or all)", params.Action)
	}
	if params.TimeoutSeconds <= 0 {
		params.TimeoutSeconds = 60
	}
	if params.TimeoutSeconds > 300 {
		params.TimeoutSeconds = 300
	}
	root := params.Path
	if root == "" {
		if tc := GetToolContext(ctx); tc != nil && tc.WorkingDir != "" {
			root = tc.WorkingDir
		} else {
			root, _ = os.Getwd()
		}
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve project path: %w", err)
	}
	if err := validatePathAllowed(ctx, root); err != nil {
		return "", err
	}
	stack := detectProjectStack(root)
	commands := dependencyCommands(stack, params.Action)
	results := make([]verificationResult, 0, len(commands))
	for _, command := range commands {
		results = append(results, runVerificationCommand(ctx, root, command, time.Duration(params.TimeoutSeconds)*time.Second))
	}
	if results == nil {
		results = []verificationResult{}
	}
	return encodeJSON(map[string]interface{}{"project": stack, "results": results})
}

func dependencyCommands(stack projectStack, action string) []verificationCommand {
	var result []verificationCommand
	add := func(c verificationCommand) {
		if _, err := exec.LookPath(c.bin); err == nil {
			result = append(result, c)
		}
	}
	for _, phase := range []string{"check", "outdated"} {
		if action != "all" && action != phase {
			continue
		}
		switch {
		case containsString(stack.Stacks, "go"):
			if phase == "check" {
				add(verificationCommand{action: phase, args: []string{"mod", "verify"}, label: "go mod verify", bin: "go"})
			} else {
				add(verificationCommand{action: phase, args: []string{"list", "-m", "-u", "all"}, label: "go list -m -u all", bin: "go"})
			}
		case containsString(stack.Stacks, "node"):
			if phase == "check" {
				add(verificationCommand{action: phase, args: []string{"audit", "--omit=dev", "--json"}, label: "npm audit --omit=dev --json", bin: "npm"})
			} else {
				add(verificationCommand{action: phase, args: []string{"outdated", "--json"}, label: "npm outdated --json", bin: "npm"})
			}
		case containsString(stack.Stacks, "python"):
			if phase == "check" {
				add(verificationCommand{action: phase, args: []string{"-m", "pip", "check"}, label: "python3 -m pip check", bin: "python3"})
			} else if _, err := exec.LookPath("pip-audit"); err == nil {
				add(verificationCommand{action: phase, args: []string{"-f", "json"}, label: "pip-audit -f json", bin: "pip-audit"})
			}
		case containsString(stack.Stacks, "rust"):
			if phase == "check" {
				if _, err := exec.LookPath("cargo"); err == nil {
					add(verificationCommand{action: phase, args: []string{"tree", "--edges", "normal"}, label: "cargo tree --edges normal", bin: "cargo"})
				}
			} else if _, err := exec.LookPath("cargo"); err == nil {
				add(verificationCommand{action: phase, args: []string{"update", "--dry-run"}, label: "cargo update --dry-run", bin: "cargo"})
			}
		}
	}
	return result
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
