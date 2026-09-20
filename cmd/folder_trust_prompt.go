package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/GrayCodeAI/rho/internal/engine"
	"golang.org/x/term"
)

func isInteractiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// promptForInteractiveFolderTrust gives an interactive user an explicit
// choice before project-scoped automation can become available. Declining is
// safe and useful: the TUI still opens, but remains restricted.
func promptForInteractiveFolderTrust(in io.Reader, out io.Writer, status engine.ProjectTrustStatus, trustFn func() error) bool {
	if !status.Blocked {
		return false
	}

	_, _ = fmt.Fprintf(out, "\nThis folder is not trusted:\n  %s\n", status.Path)
	_, _ = fmt.Fprintln(out, "Project hooks, MCP servers, and custom specialists are disabled.")
	_, _ = fmt.Fprint(out, "Trust this folder for project automation? [y/N] ")

	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(answer), "y") {
		_, _ = fmt.Fprintln(out, "Continuing in restricted mode.")
		return false
	}
	if err := trustFn(); err != nil {
		_, _ = fmt.Fprintf(out, "Could not save folder trust: %v\nContinuing in restricted mode.\n", err)
		return false
	}
	_, _ = fmt.Fprintln(out, "Folder trusted. Project automation may load.")
	return true
}
