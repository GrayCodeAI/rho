// Package workspace owns repository and workspace context used by the CLI.
package workspace

import (
	"context"
	"os/exec"
	"strings"
)

// GitOutput runs the fixed git executable and returns trimmed stdout. Git
// warnings remain on stderr so callers receive stable values for UI output.
func GitOutput(args ...string) (string, error) {
	out, err := exec.CommandContext(context.Background(), "git", args...).Output() // #nosec G204 -- fixed git executable
	return strings.TrimSpace(string(out)), err
}

// BranchSummary returns a compact branch, revision, upstream, and status
// report suitable for interactive CLI output.
func BranchSummary() string {
	branch, err := GitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branch == "" {
		return "No git repository detected."
	}
	head, _ := GitOutput("rev-parse", "--short", "HEAD")
	upstream, _ := GitOutput("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	status, _ := GitOutput("status", "--short", "--branch")
	var b strings.Builder
	b.WriteString("Branch: " + branch)
	if head != "" {
		b.WriteString(" @ " + head)
	}
	if upstream != "" {
		b.WriteString("\nUpstream: " + upstream)
	}
	if status != "" {
		b.WriteString("\n\n" + status)
	}
	return b.String()
}

// FilesSummary returns a compact modified-files report.
func FilesSummary() string {
	status, err := GitOutput("status", "--short")
	if err != nil {
		return "No git repository detected."
	}
	if strings.TrimSpace(status) == "" {
		return "No modified files."
	}
	return "Modified files:\n" + status
}
