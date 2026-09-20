// Package explain traces source lines back to the commit that introduced them.
package explain

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CommandRunner is the small process boundary needed by Explain.
type CommandRunner interface {
	Output(context.Context, string, ...string) ([]byte, error)
}

type osCommandRunner struct{}

func (osCommandRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output() // #nosec G204 -- executable is fixed by Explain
}

// Explain traces path:line through git history using the operating-system
// command runner.
func Explain(path string, line int) (string, error) {
	return ExplainWithRunner(context.Background(), osCommandRunner{}, path, line)
}

// ExplainWithRunner is Explain with an injectable process boundary for tests
// and alternate execution environments.
func ExplainWithRunner(ctx context.Context, runner CommandRunner, path string, line int) (string, error) {
	if runner == nil {
		return "", fmt.Errorf("command runner is required")
	}

	out, err := runner.Output(ctx, "git", "blame", "-L", fmt.Sprintf("%d,%d", line, line), "--porcelain", path)
	if err != nil {
		return "", fmt.Errorf("git blame failed: %w", err)
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return "", fmt.Errorf("no blame output")
	}
	commitHash := strings.Fields(lines[0])[0]
	if commitHash == "0000000000000000000000000000000000000000" {
		return "This line is uncommitted (not yet in git history).", nil
	}

	info, err := runner.Output(ctx, "git", "log", "-1", "--format=%h %s (%an, %ar)", commitHash)
	if err != nil {
		short := commitHash
		if len(short) > 7 {
			short = short[:7]
		}
		return fmt.Sprintf("Commit: %s (details unavailable)", short), nil
	}

	diff, _ := runner.Output(ctx, "git", "log", "-1", "--format=", "-p", "--", path, commitHash)
	diffText := string(diff)
	if len(diffText) > 2000 {
		diffText = diffText[:2000] + "\n... (truncated)"
	}

	result := fmt.Sprintf("**Origin:** %s\n", strings.TrimSpace(string(info)))
	if diffText != "" {
		result += fmt.Sprintf("\n**Changes in that commit:**\n```diff\n%s\n```", diffText)
	}
	return result, nil
}
