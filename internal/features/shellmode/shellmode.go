// Package shellmode implements the ! prefix for direct shell command execution
// in the REPL input, bypassing the LLM entirely.
package shellmode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/GrayCodeAI/rho/internal/toolsafety"
)

const (
	// DefaultTimeout is the maximum execution time for a shell command.
	DefaultTimeout = 30 * time.Second
)

// Result holds the output of a shell command execution.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
	Command  string
}

// Format returns a displayable string for the result.
func (r Result) Format() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("$ %s", r.Command))
	if r.Duration > 0 {
		b.WriteString(fmt.Sprintf("  [%s]", r.Duration.Round(time.Millisecond)))
	}
	b.WriteString("\n")

	if r.Stdout != "" {
		b.WriteString(r.Stdout)
		if !strings.HasSuffix(r.Stdout, "\n") {
			b.WriteString("\n")
		}
	}
	if r.Stderr != "" {
		b.WriteString(r.Stderr)
		if !strings.HasSuffix(r.Stderr, "\n") {
			b.WriteString("\n")
		}
	}
	if r.ExitCode != 0 {
		b.WriteString(fmt.Sprintf("exit code: %d\n", r.ExitCode))
	}
	return b.String()
}

// IsShellCommand checks if the input starts with ! indicating a direct shell command.
func IsShellCommand(input string) bool {
	trimmed := strings.TrimSpace(input)
	return strings.HasPrefix(trimmed, "!") && len(trimmed) > 1
}

// ExtractCommand strips the ! prefix and returns the shell command.
func ExtractCommand(input string) string {
	trimmed := strings.TrimSpace(input)
	return strings.TrimSpace(strings.TrimPrefix(trimmed, "!"))
}

// ExecuteShell runs a command directly in the user's shell.
func ExecuteShell(ctx context.Context, cmdStr string) Result {
	return ExecuteShellWithTimeout(ctx, cmdStr, DefaultTimeout)
}

// ExecuteShellWithTimeout runs a command with a custom timeout.
func ExecuteShellWithTimeout(ctx context.Context, cmdStr string, timeout time.Duration) Result {
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	shell, flag := shellAndFlag()
	cmd := exec.CommandContext(ctx, shell, flag, cmdStr) // #nosec G204 -- cmdStr is user-typed shell input; this feature's purpose is to run it directly

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	result := Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
		Duration: duration,
		Command:  cmdStr,
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.ExitCode = 124 // Standard timeout exit code.
			result.Stderr += fmt.Sprintf("\ncommand timed out after %s", timeout)
		} else {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				result.ExitCode = exitErr.ExitCode()
			} else {
				result.ExitCode = 1
				if result.Stderr == "" {
					result.Stderr = err.Error()
				}
			}
		}
	}

	return result
}

// shellAndFlag returns the appropriate shell binary and flag for the current OS.
func shellAndFlag() (string, string) {
	if runtime.GOOS == "windows" {
		return "cmd", "/c"
	}
	// Prefer user's shell if set.
	// (We don't import os here to keep it simple; caller can set env.)
	return "sh", "-c"
}

// ParsePipeline splits a shell command string into individual piped commands
// for display purposes only (execution still uses the full string).
func ParsePipeline(cmdStr string) []string {
	parts := strings.Split(cmdStr, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// IsDestructive uses the same fail-closed classifier as model-driven tool
// execution. Direct shell mode must not have a weaker safety definition than
// Bash, or users get different answers for the same command depending on the
// entry point.
func IsDestructive(cmdStr string) bool {
	return toolsafety.IsDestructiveCommand(cmdStr)
}
