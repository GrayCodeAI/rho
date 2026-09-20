package cmd

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// IsQuiet returns true when the user has requested machine-parseable output
// via --quiet / -q. When quiet, spinners, progress bars, decorative boxes, and
// ANSI color should be suppressed.
func IsQuiet() bool {
	return quietFlag
}

// CanPrompt returns true when the user can be interactively prompted (stdin is
// a TTY and --quiet is not set).
func CanPrompt() bool {
	return !quietFlag && stdinIsTerminal()
}

// confirmDestructive prompts the user to confirm a destructive, hard-to-undo
// operation. It returns true only on an explicit "y"/"yes". When the user is
// not interactive (--quiet, piped stdin, or no TTY), it fails closed so a
// destructive command cannot be approved accidentally by automation.
func confirmDestructive(prompt string) (bool, error) {
	if !CanPrompt() {
		return false, fmt.Errorf("non-interactive destructive action requires explicit confirmation")
	}
	input := openPromptInput()
	defer input.close()
	answer, err := input.readLine(prompt + " [y/N] ")
	if err != nil {
		return false, err
	}
	return parseConfirm(answer), nil
}

// parseConfirm reports whether a user answer is an explicit affirmative
// ("y" / "yes", case-insensitive, trimmed). Anything else declines.
func parseConfirm(answer string) bool {
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

// ShouldColor returns true when colored output is appropriate for stdout.
// Respects --quiet, NO_COLOR, FORCE_COLOR, and TTY state.
func ShouldColor() bool {
	if quietFlag {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	return stdoutIsTerminal()
}

// ShouldUnicode returns true when Unicode box-drawing and glyphs are
// appropriate for stdout. Same gates as ShouldColor but also requires a TTY.
func ShouldUnicode() bool {
	if !ShouldColor() {
		return false
	}
	return stdoutIsTerminal()
}

// TermSize returns the terminal dimensions for the given stream, falling back
// to sensible defaults when not a terminal.
func TermSize() (width, height int) {
	fd := int(os.Stdout.Fd())
	if !term.IsTerminal(fd) {
		return 80, 24
	}
	w, h, err := term.GetSize(fd)
	if err != nil || w == 0 || h == 0 {
		return 80, 24
	}
	return w, h
}
