package shellmode

import (
	"fmt"
	"strings"
)

// DisplayResult is the TUI-neutral interpretation of a shell result.
type DisplayResult struct {
	Output    string
	ExitError string
}

// FormatInteractiveResult combines and bounds shell output for an interactive
// session. It deliberately does not add UI-specific icons or message roles.
func FormatInteractiveResult(result Result) DisplayResult {
	output := strings.TrimRight(result.Stdout+result.Stderr, "\n")
	if len(output) > 4000 {
		lines := strings.Split(output, "\n")
		if len(lines) > 40 {
			head := strings.Join(lines[:20], "\n")
			tail := strings.Join(lines[len(lines)-20:], "\n")
			output = head + fmt.Sprintf("\n\n... (%d lines omitted) ...\n\n", len(lines)-40) + tail
		}
	}
	display := DisplayResult{Output: output}
	if result.ExitCode != 0 && output == "" {
		display.ExitError = fmt.Sprintf("exit code: %d", result.ExitCode)
	}
	return display
}
