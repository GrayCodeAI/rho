// tint.go — Plain-text colorization for non-TUI output (CLI reports, status).
//
// Unlike the TUI, CLI reports are rendered as plain strings and later printed
// by the command layer. This helper lets internal report formatters colorize
// labels and statuses without depending on the cmd package, honoring the same
// NO_COLOR / FORCE_COLOR / terminal-detection contract as cmd's ShouldColor.

package theme

import (
	"image/color"
	"os"

	lipgloss "charm.land/lipgloss/v2"
	"golang.org/x/term"
)

// ColorEnabled reports whether ANSI color should be emitted. NO_COLOR wins,
// then FORCE_COLOR, then terminal detection on stdout.
func ColorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// Tint colors s for terminal display, honoring ColorEnabled. Returns s
// unchanged when color is disabled or s is empty.
func Tint(s string, c color.Color) string {
	if !ColorEnabled() || s == "" {
		return s
	}
	return lipgloss.NewStyle().Foreground(c).Render(s)
}

// Semantic report colors — fixed brand values, legible on both light and dark
// terminals. Used by internal report formatters that colorize statuses.
var (
	ReportSuccess = lipgloss.Color("#4CAF50") // green
	ReportWarn    = lipgloss.Color("#FFB347") // amber
	ReportError   = lipgloss.Color("#FF6B6B") // coral
	ReportInfo    = lipgloss.Color("#75B1E2") // sky
	ReportMuted   = lipgloss.Color("#9E9E9E") // gray
)

// ReportErrorANSI is the truecolor SGR prefix for ReportError. It is exposed
// for stderr paths that must decide color support independently of stdout.
const ReportErrorANSI = "\x1b[38;2;255;107;107m"
