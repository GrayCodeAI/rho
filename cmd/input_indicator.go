package cmd

import (
	lipgloss "charm.land/lipgloss/v2"

	"github.com/GrayCodeAI/rho/internal/features/shellmode"
	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

// InputClass represents the classification of user input.
type InputClass int

const (
	InputClassNeutral InputClass = iota // empty or undetermined
	InputClassShell                     // shell command (starts with !)
	InputClassAgent                     // AI query
	InputClassSlash                     // slash command (/config, /model, etc.)
)

// InputIndicator provides real-time visual feedback on input classification.
type InputIndicator struct {
	current InputClass
}

func inputIndicatorShellStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(warnAmber).Bold(true)
}

func inputIndicatorAgentStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(successTeal).Bold(true)
}

func inputIndicatorSlashStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(rhoColor).Bold(true)
}

func inputIndicatorNeutralStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(textDisabled)
}

// Classify determines the input class from the current buffer text and mode.
func (ind *InputIndicator) Classify(input string, mode shellmode.Mode) InputClass {
	if input == "" {
		ind.current = InputClassNeutral
		return InputClassNeutral
	}
	trimmed := input
	for len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '\t') {
		trimmed = trimmed[1:]
	}
	if len(trimmed) == 0 {
		ind.current = InputClassNeutral
		return InputClassNeutral
	}
	switch {
	case trimmed[0] == '!':
		ind.current = InputClassShell
	case trimmed[0] == '/':
		ind.current = InputClassSlash
	default:
		switch mode {
		case shellmode.ModeShell:
			ind.current = InputClassShell
		case shellmode.ModeAgent:
			ind.current = InputClassAgent
		default:
			cls := shellmode.ClassifyInput(trimmed)
			if cls == shellmode.ClassShell {
				ind.current = InputClassShell
			} else {
				ind.current = InputClassAgent
			}
		}
	}
	return ind.current
}

// Render returns the colored indicator character for the current classification.
func (ind *InputIndicator) Render() string {
	switch ind.current {
	case InputClassShell:
		return inputIndicatorShellStyle().Render(icons.CircleFilled())
	case InputClassAgent:
		return inputIndicatorAgentStyle().Render(icons.CircleFilled())
	case InputClassSlash:
		return inputIndicatorSlashStyle().Render(icons.CircleFilled())
	default:
		return inputIndicatorNeutralStyle().Render(icons.CircleOutline())
	}
}

// Label returns a short text label for the current classification.
func (ind *InputIndicator) Label() string {
	switch ind.current {
	case InputClassShell:
		return inputIndicatorShellStyle().Render("SHELL")
	case InputClassAgent:
		return inputIndicatorAgentStyle().Render("AGENT")
	case InputClassSlash:
		return inputIndicatorSlashStyle().Render("CMD")
	default:
		return inputIndicatorNeutralStyle().Render("...")
	}
}
