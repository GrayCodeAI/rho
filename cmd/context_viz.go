package cmd

import (
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// ContextVisualization renders token usage as a visual bar in the TUI.
type ContextVisualization struct {
	ContextWindowSize int
	CurrentTokens     int
	CompactThreshold  int
	WarningThreshold  int
	BlockingThreshold int
}

// NewContextVisualization creates a visualization with default thresholds.
func NewContextVisualization(windowSize int) *ContextVisualization {
	return &ContextVisualization{
		ContextWindowSize: windowSize,
		CompactThreshold:  int(float64(windowSize) * 0.80),
		WarningThreshold:  int(float64(windowSize) * 0.90),
		BlockingThreshold: int(float64(windowSize) * 0.95),
	}
}

// Update sets the current token count.
func (cv *ContextVisualization) Update(tokens int) {
	cv.CurrentTokens = tokens
}

// PercentUsed returns the percentage of context window used.
func (cv *ContextVisualization) PercentUsed() float64 {
	if cv.ContextWindowSize == 0 {
		return 0
	}
	return float64(cv.CurrentTokens) / float64(cv.ContextWindowSize) * 100
}

// State returns the current warning state.
func (cv *ContextVisualization) State() ContextState {
	if cv.CurrentTokens >= cv.BlockingThreshold {
		return ContextBlocking
	}
	if cv.CurrentTokens >= cv.WarningThreshold {
		return ContextError
	}
	if cv.CurrentTokens >= cv.CompactThreshold {
		return ContextWarning
	}
	return ContextNormal
}

// ContextState represents the urgency level of context usage.
type ContextState int

const (
	ContextNormal   ContextState = iota
	ContextWarning               // approaching compact threshold
	ContextError                 // approaching blocking
	ContextBlocking              // at capacity
)

// Render produces a styled context bar for the TUI status line.
func (cv *ContextVisualization) Render(width int) string {
	if width < 20 {
		return cv.renderCompact()
	}

	pct := cv.PercentUsed()
	barWidth := width - 15 // leave room for label
	filled := int(pct / 100.0 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	if filled < 0 {
		filled = 0
	}

	var barStyle lipgloss.Style
	switch cv.State() {
	case ContextNormal:
		barStyle = lipgloss.NewStyle().Foreground(successTeal)
	case ContextWarning:
		barStyle = lipgloss.NewStyle().Foreground(warnAmber)
	case ContextError:
		barStyle = lipgloss.NewStyle().Foreground(errorCoral)
	case ContextBlocking:
		barStyle = lipgloss.NewStyle().Foreground(errorCoral).Bold(true)
	}

	filledBar := barStyle.Render(strings.Repeat("█", filled))
	emptyBar := lipgloss.NewStyle().Foreground(textDisabled).Render(strings.Repeat("░", barWidth-filled))

	label := fmt.Sprintf(" %3.0f%%", pct)
	return filledBar + emptyBar + label
}

func (cv *ContextVisualization) renderCompact() string {
	pct := cv.PercentUsed()
	switch cv.State() {
	case ContextBlocking:
		return fmt.Sprintf("CTX:FULL(%0.f%%)", pct)
	case ContextError:
		return fmt.Sprintf("CTX:HIGH(%0.f%%)", pct)
	case ContextWarning:
		return fmt.Sprintf("CTX:%0.f%%", pct)
	default:
		return fmt.Sprintf("CTX:%0.f%%", pct)
	}
}

// TokenBreakdown provides a detailed breakdown of token usage by category.
type TokenBreakdown struct {
	System     int `json:"system"`
	UserMsgs   int `json:"user_messages"`
	Assistant  int `json:"assistant"`
	ToolUse    int `json:"tool_use"`
	ToolResult int `json:"tool_results"`
	Total      int `json:"total"`
}

// RenderBreakdown produces a multi-line token breakdown for /context command.
func RenderBreakdown(tb TokenBreakdown, windowSize int) string {
	var b strings.Builder
	pct := func(n int) float64 {
		if windowSize == 0 {
			return 0
		}
		return float64(n) / float64(windowSize) * 100
	}

	b.WriteString(fmt.Sprintf("Context Window: %dk / %dk tokens (%.1f%% used)\n",
		tb.Total/1000, windowSize/1000, pct(tb.Total)))
	b.WriteString(strings.Repeat("─", 40) + "\n")
	b.WriteString(fmt.Sprintf("  System:       %6dk  (%4.1f%%)\n", tb.System/1000, pct(tb.System)))
	b.WriteString(fmt.Sprintf("  User msgs:    %6dk  (%4.1f%%)\n", tb.UserMsgs/1000, pct(tb.UserMsgs)))
	b.WriteString(fmt.Sprintf("  Assistant:    %6dk  (%4.1f%%)\n", tb.Assistant/1000, pct(tb.Assistant)))
	b.WriteString(fmt.Sprintf("  Tool use:     %6dk  (%4.1f%%)\n", tb.ToolUse/1000, pct(tb.ToolUse)))
	b.WriteString(fmt.Sprintf("  Tool results: %6dk  (%4.1f%%)\n", tb.ToolResult/1000, pct(tb.ToolResult)))

	return b.String()
}
