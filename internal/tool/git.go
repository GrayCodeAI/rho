package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/GrayCodeAI/rho/internal/gitcmd"
)

// allowedGitSubcommands is the set of git subcommands the agent may run.
var allowedGitSubcommands = map[string]bool{
	"status":   true,
	"diff":     true,
	"log":      true,
	"show":     true,
	"branch":   true,
	"checkout": true,
	"add":      true,
	"commit":   true,
	"pull":     true,
	"push":     true,
	"fetch":    true,
	"stash":    true,
	"rebase":   true,
	"merge":    true,
	"reset":    true,
	"tag":      true,
}

// GitTool executes structured git commands.
type GitTool struct {
	// WorkDir is the git repository working directory. If empty, uses CWD.
	WorkDir string
}

func (GitTool) Name() string      { return "Git" }
func (GitTool) RiskLevel() string { return "medium" }
func (GitTool) Aliases() []string { return []string{"git"} }
func (GitTool) Description() string {
	return "Run git commands in the project worktree. Supports: status, diff, log, show, branch, checkout, add, commit, pull, push, fetch, stash, rebase, merge, reset, tag."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (GitTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"subcommand": {Type: "string", Description: "Git subcommand to run (e.g. status, diff, add, commit)"},
			"args":       {Type: "array", Items: &SchemaProperty{Type: "string"}, Description: "Arguments for the subcommand (e.g. [\"-m\", \"fix bug\"])"},
		},
		Required: []string{"subcommand"},
	}
}

func (GitTool) Parameters() map[string]interface{} {
	return gitSchema.ToJSONSchema()
}

// gitSchema is the single source of truth for Git's input schema.
var gitSchema = GitTool{}.Schema()

// GitInput is the typed input for GitTool.
type GitInput struct {
	Subcommand string   `json:"subcommand"`
	Args       []string `json:"args,omitempty"`
}

func (t GitTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := DecodeInput[GitInput]("Git", input)
	if err != nil {
		return "", err
	}

	if in.Subcommand == "" {
		return "", fmt.Errorf("subcommand is required")
	}

	if !allowedGitSubcommands[in.Subcommand] {
		return "", fmt.Errorf("git subcommand %q is not allowed", in.Subcommand)
	}

	args := append([]string{in.Subcommand}, in.Args...)
	cmd := gitcmd.Command(ctx, args...) // #nosec G204 -- git subcommand invocation with fixed subcommand and internally-derived args
	if t.WorkDir != "" {
		cmd.Dir = t.WorkDir
	}

	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Sprintf("exit code: %d\n%s", exitErr.ExitCode(), output), nil
		}
		return "", fmt.Errorf("git exec: %w", err)
	}

	return output, nil
}

func (t GitTool) RequiresApproval(input json.RawMessage) bool {
	var in struct {
		Subcommand string   `json:"subcommand"`
		Args       []string `json:"args,omitempty"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return false
	}

	if in.Subcommand == "push" {
		return true
	}
	if gitArgsContainForce(in.Args) {
		return true
	}
	if in.Subcommand == "reset" {
		for _, arg := range in.Args {
			if arg == "--hard" {
				return true
			}
		}
	}
	return false
}

// gitArgsContainForce checks if git args contain --force or -f.
func gitArgsContainForce(args []string) bool {
	for _, a := range args {
		if a == "--force" || a == "-f" || strings.HasPrefix(a, "--force=") {
			return true
		}
	}
	return false
}
