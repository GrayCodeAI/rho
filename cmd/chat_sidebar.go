package cmd

import (
	"fmt"
	"strings"
	"time"

	lipgloss "charm.land/lipgloss/v2"
)

const (
	chatSidebarMinTerminalWidth = 110
	chatSidebarDefaultWidth     = 30
)

// chatSidebarVisible keeps the split layout out of narrow terminals and modal
// configuration screens. Showing it before the first message makes the active
// model, context budget, tools, and permission posture visible immediately.
func (m chatModel) chatSidebarVisible() bool {
	return !m.configOpen && m.width >= chatSidebarMinTerminalWidth
}

func (m chatModel) chatSidebarWidth(totalWidth int) int {
	if !m.chatSidebarVisible() {
		return 0
	}
	w := chatSidebarDefaultWidth
	if totalWidth >= 160 {
		w = 34
	}
	if totalWidth-w-1 < 40 {
		return 0
	}
	return w
}

func (m chatModel) chatMainWidth(totalWidth int) int {
	if totalWidth <= 0 {
		return totalWidth
	}
	if sidebarW := m.chatSidebarWidth(totalWidth); sidebarW > 0 {
		return totalWidth - sidebarW - 1
	}
	return totalWidth
}

func (m chatModel) renderSessionSidebar(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	muted := lipgloss.NewStyle().Foreground(textMuted)
	label := lipgloss.NewStyle().Foreground(textPrimary).Bold(true)
	accent := lipgloss.NewStyle().Foreground(rhoColor).Bold(true)
	value := lipgloss.NewStyle().Foreground(textPrimary)
	dim := lipgloss.NewStyle().Foreground(textDisabled)

	lines := []string{
		label.Render("SESSION"),
		muted.Render("────────────────────────────"),
	}
	started := m.sessionStartedAt
	if started.IsZero() {
		started = time.Now()
	}
	lines = append(lines,
		value.Render("New session"),
		dim.Render(started.Format("2006-01-02 15:04")),
		"",
		label.Render("CONTEXT"),
		value.Render(formatRhoTokenCount(sessionContextUsedTokens(m.session))+" tokens"),
		dim.Render(sidebarContextUsage(m)),
		value.Render(formatSidebarCost(m)),
		"",
		label.Render("MODEL"),
		accent.Render(sidebarModelName(m)),
		"",
		label.Render("TOOLS"),
		value.Render(sidebarToolCount(m)),
		"",
		label.Render("PERMISSIONS"),
		value.Render(sidebarPermissionTier(m)),
		dim.Render(sidebarPermissionRules(m)),
	)
	if m.waiting {
		lines = append(lines, "", accent.Render("● working"))
	}

	for i := range lines {
		lines[i] = clipFooterLine(lines[i], width)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func sidebarModelName(m chatModel) string {
	if m.session == nil || strings.TrimSpace(m.session.Model()) == "" {
		return "not selected"
	}
	return m.session.Model()
}

func sidebarToolCount(m chatModel) string {
	if m.registry == nil {
		return "available"
	}
	return fmt.Sprintf("%d enabled", len(m.registry.ModelVisibleNames()))
}

func sidebarPermissionTier(m chatModel) string {
	if m.session == nil || m.session.PermSvc() == nil {
		return "not configured"
	}
	state := m.session.PermSvc().RuntimeState()
	if state.DryRun {
		return "dry-run (all denied)"
	}
	level := state.Autonomy
	if level == 0 && !state.AutonomyExplicit {
		level = DefaultAutonomy
	}
	return autonomyTierName(level)
}

func sidebarPermissionRules(m chatModel) string {
	if m.session == nil || m.session.PermSvc() == nil {
		return "no remembered rules"
	}
	view := m.session.PermSvc().PolicyView()
	if view.Grants == nil {
		return "session policy active"
	}
	rules := len(view.Grants)
	return fmt.Sprintf("%d remembered rule%s", rules, pluralSuffix(rules))
}

func pluralSuffix(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func formatSidebarCost(m chatModel) string {
	if m.session == nil || m.session.CostValue() == nil {
		return "$0.00 spent"
	}
	return fmt.Sprintf("$%.2f spent", m.session.CostValue().TotalUSD())
}

func sidebarContextUsage(m chatModel) string {
	_, window, pct := m.contextUsagePercentForBar()
	if window <= 0 {
		return "0% used"
	}
	return fmt.Sprintf("%d%% used", pct)
}
