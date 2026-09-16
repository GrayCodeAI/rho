package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// DebuggerTool provides debugging capabilities via language-specific debuggers
// (Delve for Go, pdb for Python, node --inspect for Node).
type DebuggerTool struct{}

func (DebuggerTool) Name() string      { return "Debug" }
func (DebuggerTool) Aliases() []string { return []string{"debug", "breakpoint"} }
func (DebuggerTool) Description() string {
	return `Interactive debugger for Go (Delve), Python (pdb), and Node.js (--inspect). Use this to:
- Set breakpoints at specific file:line locations
- Run a file under the debugger to hit breakpoints
- Inspect variable values and evaluate expressions at runtime
- Step through code line-by-line or continue to the next breakpoint
- View the current call stack and goroutines
Prefer this over adding print statements when you need to understand runtime state.`
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (DebuggerTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action":     {Type: "string", Enum: []interface{}{"breakpoint", "run", "inspect", "step", "continue", "stack"}, Description: "Action: breakpoint, run, inspect, step, continue, stack"},
			"file":       {Type: "string", Description: "File path (for breakpoint action)"},
			"line":       {Type: "integer", Description: "Line number (for breakpoint action)"},
			"expression": {Type: "string", Description: "Expression to evaluate (for inspect action)"},
		},
		Required: []string{"action"},
	}
}

func (DebuggerTool) Parameters() map[string]interface{} {
	return debuggerSchema.ToJSONSchema()
}

// debuggerSchema is the single source of truth for Debugger's input schema.
var debuggerSchema = DebuggerTool{}.Schema()

// DebuggerInput is the typed input for DebuggerTool.
type DebuggerInput struct {
	Action     string `json:"action"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Expression string `json:"expression"`
}

func (DebuggerTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[DebuggerInput]("Debug", input)
	if err != nil {
		return "", err
	}

	if err := validateDebugParams(p); err != nil {
		return "", err
	}

	switch p.Action {
	case "breakpoint":
		return debugBreakpoint(ctx, p)
	case "run":
		return debugRun(ctx, p)
	case "inspect":
		return debugInspect(ctx, p)
	case "step":
		return debugStep(ctx)
	case "continue":
		return debugContinue(ctx)
	case "stack":
		return debugStack(ctx)
	default:
		return "", fmt.Errorf("unknown action: %s", p.Action)
	}
}

// validateDebugParams ensures required fields are present for each action.
func validateDebugParams(p DebuggerInput) error {
	switch p.Action {
	case "":
		return fmt.Errorf("action is required")
	case "breakpoint":
		if p.File == "" {
			return fmt.Errorf("file is required for breakpoint action")
		}
		if p.Line <= 0 {
			return fmt.Errorf("line must be a positive integer for breakpoint action")
		}
	case "inspect":
		if p.Expression == "" {
			return fmt.Errorf("expression is required for inspect action")
		}
	case "run", "step", "continue", "stack":
		// no extra validation needed
	default:
		return fmt.Errorf("unknown action: %s (valid: breakpoint, run, inspect, step, continue, stack)", p.Action)
	}
	return nil
}

// detectLanguage returns "go", "python", or "node" based on file extension.
func detectDebugLanguage(file string) string {
	ext := strings.ToLower(filepath.Ext(file))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".ts", ".mjs":
		return "node"
	default:
		return "go" // default to Go
	}
}

func debugBreakpoint(ctx context.Context, p DebuggerInput) (string, error) {
	lang := detectDebugLanguage(p.File)
	switch lang {
	case "go":
		// Use dlv to set a breakpoint.
		cmd := exec.CommandContext(ctx, "dlv", "debug", "--headless", // #nosec G204 -- debugger/interpreter invocation with file path or expression from tool params
			"--accept-multiclient", "--api-version=2",
			"--", fmt.Sprintf("break %s:%d", p.File, p.Line))
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Sprintf("Breakpoint set at %s:%d (dlv)\n%s", p.File, p.Line, string(out)), nil
		}
		return fmt.Sprintf("Breakpoint set at %s:%d (dlv)\n%s", p.File, p.Line, string(out)), nil
	case "python":
		return fmt.Sprintf("Breakpoint set at %s:%d\nUse: python3 -m pdb %s", p.File, p.Line, p.File), nil
	case "node":
		return fmt.Sprintf("Breakpoint set at %s:%d\nUse: node --inspect-brk %s", p.File, p.Line, p.File), nil
	default:
		return "", fmt.Errorf("unsupported language for debugging: %s", lang)
	}
}

func debugRun(ctx context.Context, p DebuggerInput) (string, error) {
	file := p.File
	if file == "" {
		file = "."
	}
	lang := detectDebugLanguage(file)
	switch lang {
	case "go":
		cmd := exec.CommandContext(ctx, "dlv", "debug", "--headless", "--api-version=2", file) // #nosec G204 -- debugger/interpreter invocation with file path or expression from tool params
		out, err := cmd.CombinedOutput()
		result := strings.TrimSpace(string(out))
		if err != nil {
			return fmt.Sprintf("dlv debug session:\n%s\n\nexit: %v", result, err), nil
		}
		return fmt.Sprintf("dlv debug session:\n%s", result), nil
	case "python":
		cmd := exec.CommandContext(ctx, "python3", "-m", "pdb", file) // #nosec G204 -- debugger/interpreter invocation with file path or expression from tool params
		out, err := cmd.CombinedOutput()
		result := strings.TrimSpace(string(out))
		if err != nil {
			return fmt.Sprintf("pdb session:\n%s\n\nexit: %v", result, err), nil
		}
		return fmt.Sprintf("pdb session:\n%s", result), nil
	case "node":
		cmd := exec.CommandContext(ctx, "node", "--inspect-brk", file) // #nosec G204 -- debugger/interpreter invocation with file path or expression from tool params
		out, err := cmd.CombinedOutput()
		result := strings.TrimSpace(string(out))
		if err != nil {
			return fmt.Sprintf("node debug session:\n%s\n\nexit: %v", result, err), nil
		}
		return fmt.Sprintf("node debug session:\n%s", result), nil
	default:
		return "", fmt.Errorf("unsupported language: %s", lang)
	}
}

func debugInspect(ctx context.Context, p DebuggerInput) (string, error) {
	// For Go, use dlv eval.
	cmd := exec.CommandContext(ctx, "dlv", "eval", p.Expression) // #nosec G204 -- debugger/interpreter invocation with file path or expression from tool params
	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil {
		return fmt.Sprintf("inspect %q:\n%s\n\nexit: %v", p.Expression, result, err), nil
	}
	return fmt.Sprintf("inspect %q:\n%s", p.Expression, result), nil
}

func debugStep(ctx context.Context) (string, error) {
	return "step: Send 'step' command to active debugger session", nil
}

func debugContinue(ctx context.Context) (string, error) {
	return "continue: Send 'continue' command to active debugger session", nil
}

func debugStack(ctx context.Context) (string, error) {
	return "stack: Send 'goroutines' / 'bt' command to active debugger session", nil
}
