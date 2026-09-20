// Package execution owns autonomous and one-shot execution workflows.
package execution

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// TestFirstConfig controls the test-first fix workflow.
type TestFirstConfig struct {
	TestCmd     string
	MaxRounds   int
	FailPattern string
}

// DefaultTestFirstConfig returns sensible defaults for Go projects.
func DefaultTestFirstConfig() TestFirstConfig {
	return TestFirstConfig{TestCmd: "go test ./...", MaxRounds: 5, FailPattern: "FAIL"}
}

// TestFirstResult holds the outcome of a test-first workflow run.
type TestFirstResult struct {
	Rounds      int
	FinalOutput string
	Passed      bool
	FixPrompts  []string
}

// ChatFunc receives a failure-fix prompt and returns the model's response.
type ChatFunc func(context.Context, string) (string, error)

// RunTestFirst executes tests and feeds failures into an LLM fix cycle.
func RunTestFirst(cfg TestFirstConfig, chat ChatFunc) TestFirstResult {
	if cfg.MaxRounds <= 0 {
		cfg.MaxRounds = 5
	}
	if cfg.TestCmd == "" {
		cfg.TestCmd = "go test ./..."
	}
	result := TestFirstResult{}
	for round := 0; round < cfg.MaxRounds; round++ {
		result.Rounds = round + 1
		output, passed := RunTests(cfg.TestCmd)
		result.FinalOutput = output
		if passed {
			result.Passed = true
			return result
		}
		prompt := BuildTestFixPrompt(output, round+1, cfg.MaxRounds)
		result.FixPrompts = append(result.FixPrompts, prompt)
		if chat == nil {
			return result
		}
		if _, err := chat(context.Background(), prompt); err != nil {
			result.FinalOutput = fmt.Sprintf("LLM error on round %d: %v\n\nTest output:\n%s", round+1, err, output)
			return result
		}
	}
	result.FinalOutput = fmt.Sprintf("Exceeded %d fix rounds. Last test output:\n%s", cfg.MaxRounds, result.FinalOutput)
	return result
}

// RunTests executes a simple whitespace-delimited test command without a
// shell, avoiding shell interpolation and redirection surprises.
func RunTests(testCmd string) (string, bool) {
	parts := strings.Fields(testCmd)
	if len(parts) == 0 {
		return "", false
	}
	cmd := exec.CommandContext(context.Background(), parts[0], parts[1:]...) // #nosec G204 -- configured test command is explicitly user-owned
	cmd.Dir, _ = os.Getwd()
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err == nil
}

// BuildTestFixPrompt creates a bounded prompt for the model.
func BuildTestFixPrompt(testOutput string, round, maxRounds int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Tests failed (round %d/%d). Fix the failures:\n\n", round, maxRounds))
	b.WriteString("```\n")
	if len(testOutput) > 4000 {
		lines := strings.Split(testOutput, "\n")
		if len(lines) > 80 {
			testOutput = strings.Join(lines[:40], "\n") + fmt.Sprintf("\n... (%d lines omitted) ...\n", len(lines)-80) + strings.Join(lines[len(lines)-40:], "\n")
		}
	}
	b.WriteString(testOutput)
	b.WriteString("\n```\n\n")
	b.WriteString("Fix the failing tests. Make minimal changes. Do not remove tests — fix the code they test.")
	return b.String()
}
