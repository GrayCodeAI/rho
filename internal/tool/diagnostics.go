package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// DiagnosticsTool runs lint/type/compile diagnostics for a file or project.
type DiagnosticsTool struct{}

// DiagnosticsInput is the typed input for DiagnosticsTool.
type DiagnosticsInput struct {
	Path  string `json:"path"`
	Scope string `json:"scope"`
}

func (DiagnosticsTool) Name() string      { return "Diagnostics" }
func (DiagnosticsTool) Aliases() []string { return []string{"diagnostics", "lint"} }
func (DiagnosticsTool) Description() string {
	return `Run lint, type-check, and compile diagnostics for a file or entire project. Use this to:
- Check for compile errors after editing code (Go: go vet + go build)
- Find type errors in TypeScript/JavaScript (tsc --noEmit, eslint)
- Validate Python syntax (py_compile)
- Scope to a single file or the whole project directory
Run this after making code changes to catch errors before committing.`
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (DiagnosticsTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"path":  {Type: "string", Description: "File or directory path to diagnose"},
			"scope": {Type: "string", Enum: []interface{}{"file", "project"}, Description: "Scope of diagnostics: 'file' or 'project' (default: file)"},
		},
		Required: []string{"path"},
	}
}

func (DiagnosticsTool) Parameters() map[string]interface{} {
	return diagnosticsSchema.ToJSONSchema()
}

// diagnosticsSchema is the single source of truth for Diagnostics' input schema.
var diagnosticsSchema = DiagnosticsTool{}.Schema()

func (DiagnosticsTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	args, err := DecodeInput[DiagnosticsInput]("Diagnostics", input)
	if err != nil {
		return "", err
	}
	if args.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if args.Scope == "" {
		args.Scope = "file"
	}

	ext := filepath.Ext(args.Path)
	switch ext {
	case ".go":
		return runGoDiagnostics(ctx, args.Path, args.Scope)
	case ".py":
		return runPythonDiagnostics(ctx, args.Path)
	case ".js", ".ts", ".jsx", ".tsx":
		return runJSTSDiagnostics(ctx, args.Path, ext)
	default:
		if args.Scope == "project" {
			return runGoDiagnostics(ctx, args.Path, "project")
		}
		return "", fmt.Errorf("unsupported file type: %s", ext)
	}
}

func runGoDiagnostics(ctx context.Context, path, scope string) (string, error) {
	var cmd *exec.Cmd
	if scope == "project" {
		dir := path
		cmd = exec.CommandContext(ctx, "go", "vet", "./...")
		cmd.Dir = dir
	} else {
		dir := filepath.Dir(path)
		cmd = exec.CommandContext(ctx, "go", "vet", "./...")
		cmd.Dir = dir
	}
	output, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(output))

	// Also try go build for compile errors
	var buildCmd *exec.Cmd
	if scope == "project" {
		buildCmd = exec.CommandContext(ctx, "go", "build", "./...")
		buildCmd.Dir = path
	} else {
		buildCmd = exec.CommandContext(ctx, "go", "build", path) // #nosec G204 -- fixed compiler and separate package path argument
		buildCmd.Dir = filepath.Dir(path)
	}
	buildOutput, buildErr := buildCmd.CombinedOutput()
	buildResult := strings.TrimSpace(string(buildOutput))

	var parts []string
	if result != "" {
		parts = append(parts, result)
	}
	if buildResult != "" && buildResult != result {
		parts = append(parts, buildResult)
	}

	if len(parts) == 0 {
		if err != nil || buildErr != nil {
			return "Diagnostics completed with warnings (no output captured).", nil
		}
		return "No issues found.", nil
	}
	return strings.Join(parts, "\n"), nil
}

func runPythonDiagnostics(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "python3", "-m", "py_compile", path) // #nosec G204 -- fixed interpreter and separate path argument
	output, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(output))
	if err != nil && result == "" {
		result = err.Error()
	}
	if result == "" {
		return "No issues found.", nil
	}
	return result, nil
}

func runJSTSDiagnostics(ctx context.Context, path, ext string) (string, error) {
	// Try eslint first
	cmd := exec.CommandContext(ctx, "npx", "eslint", path, "--format", "compact") // #nosec G204 -- fixed executable and separate path argument
	output, _ := cmd.CombinedOutput()
	result := strings.TrimSpace(string(output))

	// For TypeScript, also try tsc
	if ext == ".ts" || ext == ".tsx" {
		tscCmd := exec.CommandContext(ctx, "npx", "tsc", "--noEmit", path) // #nosec G204 -- fixed executable and separate path argument
		tscOutput, _ := tscCmd.CombinedOutput()
		tscResult := strings.TrimSpace(string(tscOutput))
		if tscResult != "" {
			if result != "" {
				result += "\n" + tscResult
			} else {
				result = tscResult
			}
		}
	}

	if result == "" {
		return "No issues found.", nil
	}
	return result, nil
}
