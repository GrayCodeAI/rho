package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine/token"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"

	"github.com/GrayCodeAI/rho/internal/engine"
	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

const compactProgressBarWidth = 40

func (m chatModel) contextWindowTokens() int {
	_, _, ctxLabel := m.connectionStatusParts()
	window := parseContextWindowLabel(ctxLabel)
	if window <= 0 && m.session != nil {
		window = m.session.ContextWindowSize()
	}
	if window <= 0 {
		window = engine.DefaultContextWindow
	}
	return window
}

func contextFillPercent(used, window int) int {
	if used <= 0 || window <= 0 {
		return 0
	}
	pct := int(float64(used) / float64(window) * 100)
	if pct > 100 {
		return 100
	}
	return pct
}

func formatContextPercentLabel(used, window int) string {
	pct := contextFillPercent(used, window)
	if used > 0 && pct == 0 {
		return "<1%"
	}
	return fmt.Sprintf("%d%%", pct)
}

// contextUsagePercentForBar returns 0–100 for the compact progress strip.
func (m chatModel) contextUsagePercentForBar() (used, window, barPct int) {
	if m.manualCompacting && m.compactBarWindow > 0 {
		used = m.compactBarUsed
		window = m.compactBarWindow
	} else {
		window = m.contextWindowTokens()
		used = sessionContextUsedTokens(m.session)
	}
	barPct = contextFillPercent(used, window)
	if used > 0 && barPct == 0 {
		barPct = 1 // visible sliver when context is non-empty but below 1%
	}
	return used, window, barPct
}

func renderContextUsageBar(barWidth, pct int) string {
	if barWidth < 8 {
		barWidth = 8
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := barWidth * pct / 100
	if pct > 0 && filled == 0 {
		filled = 1
	}
	filledStyle := lipgloss.NewStyle().Foreground(textWhite).Inline(true)
	emptyStyle := lipgloss.NewStyle().Foreground(dimColor).Inline(true)
	var parts []string
	for i := 0; i < barWidth; i++ {
		if i < filled {
			parts = append(parts, filledStyle.Render("█"))
		} else {
			parts = append(parts, emptyStyle.Render("░"))
		}
	}
	return strings.Join(parts, "")
}

// renderCompactProgressPanel is shown only during explicit /compact (not auto-compact).
// The bar reflects context fill before summarization — not compaction job progress.
func (m chatModel) renderCompactProgressPanel(totalWidth int) string {
	if totalWidth < 20 {
		totalWidth = 80
	}
	used, window, barPct := m.contextUsagePercentForBar()
	glyph := icons.CircleFilled()
	if m.brailleSpinner != nil {
		glyph = m.brailleSpinner.GlyphChar()
	}
	titleStyle := lipgloss.NewStyle().Foreground(infoSky).Bold(true).Inline(true)
	title := titleStyle.Render(glyph + " Summarizing conversation…")
	hint := configMutedStyle().Inline(true).Render("  esc cancel")

	ctxLabel := formatContextUsedLabel(used) + "/" + formatModelTableContext(window)
	pctText := formatContextPercentLabel(used, window)
	ctxPlain := fmt.Sprintf("context %s (%s)", ctxLabel, pctText)
	ctxPart := configMutedStyle().Inline(true).Render(ctxPlain)

	barWidth := compactProgressBarWidth
	ctxPartVis := runewidth.StringWidth(ctxPlain)
	if totalWidth < barWidth+ctxPartVis+4 {
		barWidth = totalWidth - ctxPartVis - 4
		if barWidth < 8 {
			barWidth = 8
		}
	}
	bar := renderContextUsageBar(barWidth, barPct)
	gap := totalWidth - runewidth.StringWidth(bar) - ctxPartVis
	if gap < 1 {
		gap = 1
	}
	return title + hint + "\n" + bar + strings.Repeat(" ", gap) + ctxPart
}

func compactTickCmd() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return compactTickMsg{} })
}

func (m *chatModel) clearCompactCancel() {
	if m.compactCancel != nil {
		m.compactCancel()
		m.compactCancel = nil
	}
}

func isCompactCancelKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "esc", "ctrl+c", "ctrl+[", "ctrl+\\":
		return true
	}
	return false
}

// cancelManualCompact aborts an in-flight /compact and restores the input loop.
func (m *chatModel) cancelManualCompact(reason string) (chatModel, tea.Cmd) {
	m.clearCompactCancel()
	m.manualCompacting = false
	m.compacting = false
	m.compactBarUsed = 0
	m.compactBarWindow = 0
	if m.brailleSpinner != nil {
		m.brailleSpinner.SetLabel(m.spinnerVerb)
	}
	if reason != "" {
		m.messages = append(m.messages, displayMsg{role: "system", content: reason})
	}
	m.invalidateConnStatus()
	m.viewDirty = true
	m.updateViewportContent()
	return *m, m.input.Focus()
}

// startManualCompact begins async compaction; Esc or /compact again cancels.
func (m *chatModel) startManualCompact() (chatModel, tea.Cmd) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	m.compactCancel = cancel
	m.manualCompacting = true
	m.compacting = true
	m.compactBarWindow = m.contextWindowTokens()
	m.compactBarUsed = sessionContextUsedTokens(m.session)
	if m.compactBarUsed <= 0 && m.session != nil {
		m.compactBarUsed = token.EstimateTokens(m.session.RawMessages())
	}
	if m.brailleSpinner != nil {
		m.brailleSpinner.SetLabel("")
	}
	m.viewDirty = true
	m.updateViewportContent()
	sess := m.session
	return *m, tea.Batch(compactTickCmd(), m.input.Focus(), runManualCompactCmd(ctx, sess))
}

// finishManualCompact applies compactDoneMsg after the background job exits.
func (m *chatModel) finishManualCompact(msg compactDoneMsg) (chatModel, tea.Cmd) {
	m.compactCancel = nil
	wasActive := m.manualCompacting
	m.manualCompacting = false
	m.compacting = false
	m.compactBarUsed = 0
	m.compactBarWindow = 0
	if m.brailleSpinner != nil {
		m.brailleSpinner.SetLabel(m.spinnerVerb)
	}
	focus := m.input.Focus()
	if !wasActive {
		m.viewDirty = true
		m.updateViewportContent()
		return *m, focus
	}
	var line string
	switch {
	case errors.Is(msg.err, context.Canceled):
		line = "Compaction cancelled."
	case msg.err != nil:
		line = fmt.Sprintf("Compacted with fallback: %d → %d messages (%v)", msg.beforeCount, msg.afterCount, msg.err)
	default:
		line = fmt.Sprintf("Compacted (%s): %d → %d messages, ~%dk → ~%dk tokens",
			msg.strategy, msg.beforeCount, msg.afterCount, msg.tokensBefore/1000, msg.tokensAfter/1000)
	}
	if m.sessionID != "" {
		line += "\nTranscript: " + sessionExportPath(m.sessionID) + " (/export)"
	}
	m.messages = append(m.messages, displayMsg{role: "system", content: line})
	m.invalidateConnStatus()
	m.viewDirty = true
	m.updateViewportContent()
	return *m, focus
}

// runManualCompactCmd runs CompactConversation off the UI thread.
func runManualCompactCmd(ctx context.Context, sess *engine.Session) tea.Cmd {
	return func() tea.Msg {
		if sess == nil {
			return compactDoneMsg{err: fmt.Errorf("no session")}
		}
		before := sess.MessageCount()
		compactStrategy, tokBefore, tokAfter, err := sess.CompactConversation(ctx)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) && err == nil {
			err = ctx.Err()
		}
		return compactDoneMsg{
			strategy:     compactStrategy,
			tokensBefore: tokBefore,
			tokensAfter:  tokAfter,
			err:          err,
			beforeCount:  before,
			afterCount:   sess.MessageCount(),
		}
	}
}
