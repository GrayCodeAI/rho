package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ProjectVerifyTool runs bounded, project-aware verification commands without
// going through a shell. It is the safe structured counterpart to asking Bash
// to guess a build or test command.
type ProjectVerifyTool struct{}

// ProjectVerifyInput is the typed input for ProjectVerifyTool.
type ProjectVerifyInput struct {
	Action         string `json:"action"`
	Path           string `json:"path"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (ProjectVerifyTool) Name() string      { return "ProjectVerify" }
func (ProjectVerifyTool) RiskLevel() string { return "medium" }
func (ProjectVerifyTool) Aliases() []string { return []string{"project-verify", "verify_project"} }
func (ProjectVerifyTool) Description() string {
	return "Detect the project stack and run bounded build, test, lint, or format checks using fixed argument lists (no shell interpolation). Returns structured results with exit codes and durations."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (ProjectVerifyTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action":          {Type: "string", Enum: []interface{}{"detect", "build", "test", "lint", "format", "all"}, Description: "Verification action. detect only inspects files; all runs build, test, lint, and format checks that are available."},
			"path":            {Type: "string", Description: "Project directory (default: session working directory)."},
			"timeout_seconds": {Type: "integer", Minimum: 1, Maximum: 600, Description: "Per-command timeout (default 120 seconds, max 600)."},
		},
		Required: []string{"action"},
	}
}

func (ProjectVerifyTool) Parameters() map[string]interface{} {
	return projectVerifySchema.ToJSONSchema()
}

// projectVerifySchema is the single source of truth for ProjectVerify's input schema.
var projectVerifySchema = ProjectVerifyTool{}.Schema()

type projectStack struct {
	Root    string   `json:"root"`
	Markers []string `json:"markers"`
	Stacks  []string `json:"stacks"`
}

type verificationResult struct {
	Action   string `json:"action"`
	Command  string `json:"command"`
	Status   string `json:"status"`
	ExitCode int    `json:"exit_code"`
	Duration string `json:"duration"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (ProjectVerifyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	params, err := DecodeInput[ProjectVerifyInput]("ProjectVerify", input)
	if err != nil {
		return "", err
	}
	params.Action = strings.ToLower(strings.TrimSpace(params.Action))
	if params.Action == "" {
		return "", fmt.Errorf("action is required")
	}
	switch params.Action {
	case "detect", "build", "test", "lint", "format", "all":
	default:
		return "", fmt.Errorf("unsupported action %q (use detect, build, test, lint, format, or all)", params.Action)
	}
	if params.TimeoutSeconds <= 0 {
		params.TimeoutSeconds = 120
	}
	if params.TimeoutSeconds > 600 {
		params.TimeoutSeconds = 600
	}

	root := params.Path
	if root == "" {
		if tc := GetToolContext(ctx); tc != nil && tc.WorkingDir != "" {
			root = tc.WorkingDir
		} else {
			root, _ = os.Getwd()
		}
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve project path: %w", err)
	}
	if err := validatePathAllowed(ctx, absRoot); err != nil {
		return "", err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return "", fmt.Errorf("project path: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project path is not a directory: %s", absRoot)
	}

	stack := detectProjectStack(absRoot)
	if params.Action == "detect" {
		return encodeJSON(stack)
	}

	commands := verificationCommands(stack, params.Action)
	if len(commands) == 0 {
		return encodeJSON(map[string]interface{}{
			"project": stack,
			"results": []verificationResult{{Action: params.Action, Status: "skipped", Error: "no supported verification command detected"}},
		})
	}

	results := make([]verificationResult, 0, len(commands))
	for _, spec := range commands {
		result := runVerificationCommand(ctx, absRoot, spec, time.Duration(params.TimeoutSeconds)*time.Second)
		results = append(results, result)
		if result.Status == "failed" && params.Action != "all" {
			break
		}
	}
	return encodeJSON(map[string]interface{}{"project": stack, "results": results})
}

type verificationCommand struct {
	action string
	args   []string
	label  string
	bin    string
}

func detectProjectStack(root string) projectStack {
	markers := []string{}
	stacks := map[string]bool{}
	checks := []struct {
		file  string
		stack string
	}{
		{"go.mod", "go"},
		{"package.json", "node"},
		{"pyproject.toml", "python"},
		{"requirements.txt", "python"},
		{"pytest.ini", "python"},
		{"Cargo.toml", "rust"},
		{"pom.xml", "java"},
		{"build.gradle", "java"},
		{"Makefile", "make"},
	}
	for _, check := range checks {
		if _, err := os.Stat(filepath.Join(root, check.file)); err == nil {
			markers = append(markers, check.file)
			stacks[check.stack] = true
		}
	}
	sort.Strings(markers)
	stackNames := make([]string, 0, len(stacks))
	for stack := range stacks {
		stackNames = append(stackNames, stack)
	}
	sort.Strings(stackNames)
	return projectStack{Root: root, Markers: markers, Stacks: stackNames}
}

func verificationCommands(stack projectStack, action string) []verificationCommand {
	has := func(name string) bool {
		for _, candidate := range stack.Stacks {
			if candidate == name {
				return true
			}
		}
		return false
	}
	commands := make([]verificationCommand, 0, 4)
	add := func(candidate verificationCommand) {
		if _, err := exec.LookPath(candidate.bin); err == nil {
			commands = append(commands, candidate)
		}
	}
	for _, phase := range []string{"build", "test", "lint", "format"} {
		if action != "all" && action != phase {
			continue
		}
		switch {
		case has("go"):
			args, label := []string{"test", "./..."}, "go test ./..."
			switch phase {
			case "build":
				args, label = []string{"build", "./..."}, "go build ./..."
			case "lint":
				args, label = []string{"vet", "./..."}, "go vet ./..."
			case "format":
				args, label = []string{"fmt", "./..."}, "gofmt check unavailable; gofmt is not run in write mode"
			}
			if phase == "format" {
				// `gofmt -l` is a read-only format check. Build the file list
				// in Go rather than asking a shell to expand an untrusted glob.
				files := projectSourceFiles(stack.Root, ".go")
				if len(files) > 0 {
					add(verificationCommand{action: phase, args: append([]string{"-l"}, files...), label: "gofmt -l <project Go files>", bin: "gofmt"})
				}
			} else {
				add(verificationCommand{action: phase, args: args, label: label, bin: "go"})
			}
		case has("node"):
			if phase == "format" {
				add(verificationCommand{action: phase, args: []string{"--check", "."}, label: "prettier --check .", bin: "prettier"})
			} else if phase == "lint" {
				add(verificationCommand{action: phase, args: []string{".", "--no-ignore"}, label: "eslint . --no-ignore", bin: "eslint"})
			} else {
				// npm scripts are project-owned commands, so execution remains
				// medium-risk and is still subject to the session approval gate.
				add(verificationCommand{action: phase, args: []string{"run", phase, "--if-present"}, label: "npm run " + phase + " --if-present", bin: "npm"})
			}
		case has("python"):
			if phase == "test" {
				add(verificationCommand{action: phase, args: []string{"-m", "pytest"}, label: "python3 -m pytest", bin: "python3"})
			} else if phase == "lint" {
				if _, err := exec.LookPath("ruff"); err == nil {
					add(verificationCommand{action: phase, args: []string{"check", "."}, label: "ruff check .", bin: "ruff"})
				}
			} else if phase == "format" {
				if _, err := exec.LookPath("ruff"); err == nil {
					add(verificationCommand{action: phase, args: []string{"format", "--check", "."}, label: "ruff format --check .", bin: "ruff"})
				}
			}
		case has("rust"):
			args, label := []string{"test"}, "cargo test"
			if phase == "build" {
				args, label = []string{"check"}, "cargo check"
			} else if phase != "test" {
				continue
			}
			add(verificationCommand{action: phase, args: args, label: label, bin: "cargo"})
		}
	}
	return commands
}

func projectSourceFiles(root, extension string) []string {
	var files []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == ".rho" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == extension {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files
}

func runVerificationCommand(parent context.Context, root string, spec verificationCommand, timeout time.Duration) verificationResult {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	started := time.Now()
	// #nosec G204 -- verificationCommands constructs fixed executable/argument
	// lists from detected project markers; no shell interpolation is used.
	cmd := exec.CommandContext(ctx, spec.bin, spec.args...)
	cmd.Dir = root
	var output boundedCommandOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	text := strings.TrimSpace(output.String())
	if output.Truncated() {
		text += "\n[output truncated]"
	}
	result := verificationResult{Action: spec.action, Command: spec.label, Status: "passed", ExitCode: 0, Duration: time.Since(started).Round(time.Millisecond).String(), Output: text}
	if err != nil {
		result.Status = "failed"
		result.ExitCode = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.Error = err.Error()
		}
	}
	if ctx.Err() != nil {
		result.Status = "timeout"
		result.Error = ctx.Err().Error()
	}
	return result
}

const verificationOutputLimit = 200_000

// boundedCommandOutput prevents a noisy project checker from consuming
// unbounded memory while preserving enough output for diagnosis.
type boundedCommandOutput struct {
	buffer    bytes.Buffer
	truncated bool
}

func (o *boundedCommandOutput) Write(p []byte) (int, error) {
	remaining := verificationOutputLimit - o.buffer.Len()
	if remaining <= 0 {
		o.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = o.buffer.Write(p[:remaining])
		o.truncated = true
		return len(p), nil
	}
	return o.buffer.Write(p)
}

func (o *boundedCommandOutput) String() string { return o.buffer.String() }

func (o *boundedCommandOutput) Truncated() bool { return o.truncated }

func encodeJSON(value interface{}) (string, error) {
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode result: %w", err)
	}
	return string(out), nil
}
