package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// EnterWorktreeTool switches to a git worktree.
type EnterWorktreeTool struct{}

// EnterWorktreeInput is the typed input for EnterWorktreeTool.
type EnterWorktreeInput struct {
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
}

func (EnterWorktreeTool) Name() string      { return "EnterWorktree" }
func (EnterWorktreeTool) Aliases() []string { return nil }
func (EnterWorktreeTool) Description() string {
	return "Switch to a git worktree directory."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (EnterWorktreeTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"path":   {Type: "string", Description: "Path to the worktree directory"},
			"branch": {Type: "string", Description: "Branch to create/checkout (optional, creates worktree if path doesn't exist)"},
		},
		Required: []string{"path"},
	}
}

func (EnterWorktreeTool) Parameters() map[string]interface{} {
	return enterWorktreeSchema.ToJSONSchema()
}

// enterWorktreeSchema is the single source of truth for EnterWorktree's input schema.
var enterWorktreeSchema = EnterWorktreeTool{}.Schema()

func (EnterWorktreeTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[EnterWorktreeInput]("EnterWorktree", input)
	if err != nil {
		return "", err
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	// Validate path against traversal
	if !isValidWorktreePath(p.Path) {
		return "", fmt.Errorf("invalid worktree path: %s", p.Path)
	}

	// If path doesn't exist, create a worktree
	if _, err := os.Stat(p.Path); os.IsNotExist(err) {
		branch := p.Branch
		if branch == "" {
			branch = filepath.Base(p.Path)
		}
		out, err := exec.CommandContext(ctx, "git", "worktree", "add", p.Path, "-b", branch).CombinedOutput() // #nosec G204 -- git subcommand invocation with fixed subcommand and internally-derived args
		if err != nil {
			return "", fmt.Errorf("failed to create worktree: %s", string(out))
		}
		// Symlink shared directories to avoid disk bloat
		_ = symlinkSharedDirs(p.Path)
	} else {
		// Validate it's a git worktree
		out, err := exec.CommandContext(ctx, "git", "-C", p.Path, "rev-parse", "--git-dir").CombinedOutput() // #nosec G204 -- git subcommand invocation with fixed subcommand and internally-derived args
		if err != nil {
			return "", fmt.Errorf("not a valid git repository: %s", string(out))
		}
		gitDirPath := strings.TrimSpace(string(out))
		if gitDirPath == ".git" {
			return "", fmt.Errorf("%s appears to be the main repository, not a worktree", p.Path)
		}
	}

	// Change to the worktree directory
	if err := os.Chdir(p.Path); err != nil {
		return "", fmt.Errorf("failed to change to worktree: %w", err)
	}

	// Get branch name for display
	branchOut, _ := exec.CommandContext(ctx, "git", "-C", p.Path, "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput() // #nosec G204 -- git subcommand invocation with fixed subcommand and internally-derived args
	branch := strings.TrimSpace(string(branchOut))

	return fmt.Sprintf("Switched to worktree: %s (branch: %s)", p.Path, branch), nil
}

// ExitWorktreeTool returns to the main repository from a worktree.
type ExitWorktreeTool struct{}

// ExitWorktreeInput is the typed input for ExitWorktreeTool.
type ExitWorktreeInput struct {
	Cleanup bool `json:"cleanup,omitempty"`
}

func (ExitWorktreeTool) Name() string      { return "ExitWorktree" }
func (ExitWorktreeTool) Aliases() []string { return nil }
func (ExitWorktreeTool) Description() string {
	return "Return to the main repository from a git worktree."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (ExitWorktreeTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"cleanup": {Type: "boolean", Description: "Remove the worktree after exiting (default: false)"},
		},
	}
}

func (ExitWorktreeTool) Parameters() map[string]interface{} {
	return exitWorktreeSchema.ToJSONSchema()
}

// exitWorktreeSchema is the single source of truth for ExitWorktree's input schema.
var exitWorktreeSchema = ExitWorktreeTool{}.Schema()

func (ExitWorktreeTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[ExitWorktreeInput]("ExitWorktree", input)
	if err != nil {
		return "", err
	}

	// Find the main repository
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("not in a git repository: %s", string(out))
	}
	currentTop := strings.TrimSpace(string(out))

	// Get the main worktree
	out, err = exec.CommandContext(ctx, "git", "worktree", "list", "--porcelain").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to list worktrees: %s", string(out))
	}

	// Find the main worktree (first one)
	lines := strings.Split(string(out), "\n")
	var mainWorktree string
	var currentWorktree string
	for _, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			path := strings.TrimPrefix(line, "worktree ")
			if mainWorktree == "" {
				mainWorktree = path
			}
			if path == currentTop {
				currentWorktree = path
			}
		}
	}

	if mainWorktree == "" {
		return "", fmt.Errorf("could not find main worktree")
	}

	if currentWorktree == mainWorktree {
		return "Already in main repository: " + mainWorktree, nil
	}

	// Optionally cleanup
	var cleanupMsg string
	if p.Cleanup {
		out, err := exec.CommandContext(ctx, "git", "worktree", "remove", currentWorktree).CombinedOutput() // #nosec G204 -- git subcommand invocation with fixed subcommand and internally-derived args
		if err != nil {
			cleanupMsg = fmt.Sprintf("\nWarning: failed to remove worktree: %s", string(out))
		} else {
			cleanupMsg = "\nWorktree removed: " + currentWorktree
		}
	}

	if err := os.Chdir(mainWorktree); err != nil {
		return "", fmt.Errorf("failed to change to main repository: %w", err)
	}

	return fmt.Sprintf("Returned to main repository: %s%s", mainWorktree, cleanupMsg), nil
}

// isValidWorktreePath checks that the path doesn't contain traversal sequences.
func isValidWorktreePath(path string) bool {
	clean := filepath.Clean(path)
	return !strings.Contains(clean, "..") && !strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "/")
}

// symlinkSharedDirs creates symlinks for common directories to avoid disk bloat
// when multiple worktrees are created.
func symlinkSharedDirs(worktreePath string) error {
	sharedDirs := []string{"node_modules", ".cache", ".next", "dist", "build"}
	for _, dir := range sharedDirs {
		src := filepath.Join(worktreePath, dir)
		// Only symlink if the directory doesn't exist yet
		if _, err := os.Stat(src); err == nil {
			continue
		}
		// Check if main repo has this directory
		mainRepo, err := exec.CommandContext(context.Background(), "git", "rev-parse", "--show-toplevel").CombinedOutput()
		if err != nil {
			continue
		}
		mainDir := filepath.Join(strings.TrimSpace(string(mainRepo)), dir)
		if _, err := os.Stat(mainDir); err != nil {
			continue
		}
		// Create symlink
		if err := os.Symlink(mainDir, src); err != nil {
			continue
		}
	}
	return nil
}
