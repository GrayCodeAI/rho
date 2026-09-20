package cmd

import (
	"context"
	"fmt"
	"image/color"
	"sort"
	"strings"

	"github.com/mattn/go-runewidth"

	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	"github.com/GrayCodeAI/rho/internal/engine"
	"github.com/GrayCodeAI/rho/internal/session"
	"github.com/GrayCodeAI/rho/internal/tool"
	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

type welcomeStatusSnapshot struct {
	setup    rhoconfig.SetupState
	agentsOK bool
}

func loadWelcomeStatusSnapshot() welcomeStatusSnapshot {
	ctx := context.Background()
	return welcomeStatusSnapshot{
		setup:    rhoconfig.EvaluateSetupCached(ctx),
		agentsOK: rhoconfig.LoadAgentsMD() != "",
	}
}

func (m *chatModel) refreshWelcomeStatusSnapshot() {
	snapshot := loadWelcomeStatusSnapshot()
	m.welcomeSetupState = snapshot.setup
	m.welcomeAgentsOK = snapshot.agentsOK
}

func (m chatModel) welcomeStatusSnapshot() welcomeStatusSnapshot {
	return welcomeStatusSnapshot{
		setup:    m.welcomeSetupState,
		agentsOK: m.welcomeAgentsOK,
	}
}

func (m *chatModel) rebuildWelcomeCache(opts ...any) {
	frame := m.eyeFrame
	if len(opts) > 0 {
		switch v := opts[0].(type) {
		case int:
			frame = v
		case bool:
			if v {
				frame = 2
			} else {
				frame = 0
			}
		}
	}
	width := m.width
	if width <= 0 {
		width = 80
	}
	height := m.height
	if height <= 0 {
		height = 24
	}
	skillsCount := 0
	if m.pluginRuntime != nil {
		skillsCount = len(m.pluginRuntime.SmartSkills)
	}
	m.welcomeCache = buildWelcomeMessageWithSnapshotAndMascot(m.session, m.sessionID, m.registry, nil, m.settings, skillsCount, connectedMCPCount(m.registry), frame, width, height, m.welcomeStatusSnapshot(), m.lastCommand, m.mascotEnabled)
}

// buildWelcomeMessage renders the branded inline RHO welcome block.
func buildWelcomeMessage(sess *engine.Session, sessionID string, registry *tool.Registry, saved *session.Session, settings rhoconfig.Settings, skillsCount int, blinkClosed bool, width, height int) string {
	frame := 0
	if blinkClosed {
		frame = 2
	}
	return buildWelcomeMessageWithSnapshot(sess, sessionID, registry, saved, settings, skillsCount, connectedMCPCount(registry), frame, width, height, loadWelcomeStatusSnapshot(), "")
}

func buildWelcomeMessageWithSnapshot(sess *engine.Session, sessionID string, registry *tool.Registry, saved *session.Session, settings rhoconfig.Settings, skillsCount, mcpCount int, eyeFrame int, width, height int, snapshot welcomeStatusSnapshot, lastCommand string) string {
	return buildWelcomeMessageWithSnapshotAndMascot(sess, sessionID, registry, saved, settings, skillsCount, mcpCount, eyeFrame, width, height, snapshot, lastCommand, false)
}

func buildWelcomeMessageWithSnapshotAndMascot(sess *engine.Session, sessionID string, registry *tool.Registry, saved *session.Session, settings rhoconfig.Settings, skillsCount, mcpCount int, eyeFrame int, width, height int, snapshot welcomeStatusSnapshot, lastCommand string, useMascot bool) string {
	// Talon Gold is used for the RHO wordmark. All escapes come from the
	// theme palette (theme.go) so a rebrand stays a one-file change.
	logoC := ansiOrange
	dimC := ansiDim
	// Indicator colors — same as the rest of the TUI palette (success
	// teal, error coral) so the ✓/× marks match the colors used
	// elsewhere for success/error states.
	greenC := ansiTeal
	sepC := ansiGrayDim
	rst := ansiReset

	// Status marks — green ✓ = present, dim ○ = none (not an error),
	// red × = actual problem. Using a
	// neutral mark for "none" avoids the alarming all-red look on a fresh repo.
	markPresent := greenC + ansiBold + icons.CheckBold() + rst
	markNone := sepC + "○" + rst

	totalW := width
	if totalW < 40 {
		totalW = 80
	}
	totalH := height
	if totalH <= 0 {
		totalH = 24
	}
	tight := totalH < 30 || totalW < 72

	center := func(visW int, styled string) string {
		if visW <= 0 {
			visW = runewidth.StringWidth(styled)
		}
		pad := (totalW - visW) / 2
		if pad < 0 {
			pad = 0
		}
		return strings.Repeat(" ", pad) + styled
	}

	art := rhoLogoArtLines
	useMascot = useMascot && !tight
	if useMascot {
		// The inline mascot is emitted by the TUI init command. Keep one empty
		// art row in the layout so the welcome content retains its spacing;
		// unsupported terminals continue through the ASCII path below.
		art = []string{""}
	}
	var eyeGlyph string
	switch eyeFrame {
	case 1, 3:
		eyeGlyph = "|o\\/o|"
	case 2:
		eyeGlyph = "|-\\/-|"
	}
	if eyeGlyph != "" && !useMascot {
		art = append([]string(nil), rhoLogoArtLines...)
		for i, line := range art {
			art[i] = strings.Replace(line, "|0\\/0|", eyeGlyph, 1)
		}
	}

	// Inject the version into the rho's body — centered in the lower gap.
	verStr := DisplayVersion()
	if verStr != "" && !strings.HasPrefix(verStr, "v") && !strings.HasPrefix(verStr, "V") {
		verStr = "v" + verStr
	}
	const verGap = 14
	if len(verStr) > verGap {
		verStr = verStr[:verGap]
	}
	verLeft := (verGap - len(verStr)) / 2
	verRight := verGap - len(verStr) - verLeft
	verWing := strings.Repeat(" ", verLeft) + verStr + strings.Repeat(" ", verRight)
	for i, line := range art {
		art[i] = strings.Replace(line, "(\\              /)", "(\\"+verWing+"/)", 1)
	}

	var b strings.Builder

	// Top breathing room so the wordmark isn't flush against the terminal edge.
	b.WriteString("\n")

	if tight {
		// Compact single-line wordmark for small terminals — version sits
		// inline so it's always visible even when the full rho is hidden.
		verDisplay := DisplayVersion()
		if verDisplay != "" && !strings.HasPrefix(verDisplay, "v") && !strings.HasPrefix(verDisplay, "V") {
			verDisplay = "v" + verDisplay
		}
		compactArt := logoC + "RHO" + rst + "  " + verDisplay
		b.WriteString(center(runewidth.StringWidth("RHO   "+verDisplay), compactArt) + "\n")
	} else {
		artW := blockLinesWidth(art)
		for _, line := range art {
			b.WriteString(center(artW, logoC+line+rst) + "\n")
		}
	}

	modeBadge := ""
	cpLine := ""
	if sess != nil {
		cpLine = welcomeControlPlaneLine(sess, dimC, rst)
	}
	modeLine := modeBadge
	if cpLine != "" {
		if modeBadge != "" {
			modeLine += "  ·  " + cpLine
		} else {
			modeLine = cpLine
		}
	}
	b.WriteString("\n")
	b.WriteString(center(visibleWidth(modeLine), modeLine) + "\n")

	indicators := welcomeIndicatorRow(skillsCount, snapshot.agentsOK, mcpCount, greenC, sepC, rst, markPresent, markNone)
	b.WriteByte('\n')
	b.WriteString(center(visibleWidth(indicators), indicators) + "\n")

	if resume := actLine(saved, sessionID); resume != "" {
		b.WriteString("\n")
		b.WriteString(center(runewidth.StringWidth(resume), dimC+resume+rst) + "\n")
	}

	return b.String()
}

// welcomeControlPlaneLine renders the work-mode · folder-trust indicator on
// the welcome screen (moved out of the footer bar).
func welcomeControlPlaneLine(sess *engine.Session, dimC, rst string) string {
	boldIcon := func(color, glyph string) string {
		return color + ansiBold + glyph + ansiReset + color
	}
	work := sess.WorkMode()
	// Use the denser terminal glyphs here. The UI already has the semantic
	// text; these icons should add contrast, not vanish into the line height.
	modeIcon := icons.Terminal()
	modeLabel := "Action Mode"
	modeColor := ansiCyan
	switch work {
	case engine.WorkModePlan:
		modeIcon = icons.Brain()
		modeLabel = "Planning Mode"
		modeColor = ansiMagenta
	case engine.WorkModeReview:
		modeIcon = icons.Magnify()
		modeLabel = "Review Mode"
		modeColor = ansiAmber
	}

	tr := engine.ProjectTrust("")
	var trustIcon string
	trustColor := dimC
	if !tr.Enforced {
		trustIcon = icons.CircleOutline()
		trustColor = dimC
	} else if tr.Trusted {
		trustIcon = icons.CheckDecagram()
		trustColor = ansiVividGreen
	} else if tr.Blocked {
		trustIcon = icons.CloseCircle()
		trustColor = ansiCoral
	} else {
		trustIcon = icons.CloseThick()
		trustColor = ansiAmber
	}
	trustLabel := tr.String()
	switch trustLabel {
	case "trusted":
		trustLabel = "Trusted"
	case "blocked":
		trustLabel = "Blocked"
	case "":
		trustLabel = "Untrusted"
	default:
		if len(trustLabel) > 0 {
			trustLabel = strings.ToUpper(trustLabel[:1]) + trustLabel[1:]
		}
	}

	return boldIcon(modeColor, modeIcon) + " " + modeLabel + rst +
		"  ·  " + boldIcon(trustColor, trustIcon) + " " + trustLabel + rst
}

type mcpServerNamed interface {
	MCPServerName() string
}

func connectedMCPCount(registry *tool.Registry) int {
	if registry == nil {
		return 0
	}
	servers := make(map[string]struct{})
	for _, candidate := range registry.PrimaryTools() {
		mcpTool, ok := candidate.(mcpServerNamed)
		if !ok || mcpTool.MCPServerName() == "" {
			continue
		}
		servers[mcpTool.MCPServerName()] = struct{}{}
	}
	return len(servers)
}

func welcomeIndicatorRow(skillsCount int, agentsOK bool, mcpCount int, activeC, idleC, rst, markPresent, markNone string) string {
	boldIcon := func(color, glyph string) string {
		// Keep the label's color after making only the glyph bold. This gives
		// narrow Nerd Font icons more visual weight without bolding the copy.
		return color + ansiBold + glyph + ansiReset + color
	}
	skillsColor, skillsMark := idleC, markNone
	if skillsCount > 0 {
		skillsColor, skillsMark = ansiLightPink, markPresent
	}

	agentsColor, agentsMark := idleC, markNone
	if agentsOK {
		agentsColor, agentsMark = ansiMagenta, markPresent
	}

	mcpColor, mcpMark := idleC, markNone
	if mcpCount > 0 {
		mcpColor, mcpMark = ansiCyan, markPresent
	}
	return fmt.Sprintf(
		"%s Skills (%d)%s %s  ·  %s AGENTS.md%s %s  ·  %s MCPs (%d)%s %s",
		boldIcon(skillsColor, icons.Bolt()), skillsCount, rst, skillsMark,
		boldIcon(agentsColor, icons.Robot()), rst, agentsMark,
		boldIcon(mcpColor, icons.Network()), mcpCount, rst, mcpMark,
	)
}

// welcomeModeBadge is intentionally absent; rho uses host permission tiers.

func actLine(saved *session.Session, sessionID string) string {
	if saved != nil && len(sessionID) >= 8 {
		return "Resumed session " + sessionID[:8]
	}
	return ""
}

func toolListSummary(registry *tool.Registry) string {
	if registry == nil {
		return "No tools enabled."
	}
	tools := registry.FluxTools()
	registered := len(registry.PrimaryTools())
	if len(tools) == 0 {
		return "No tools enabled."
	}
	var b strings.Builder
	if registered > len(tools) {
		b.WriteString(fmt.Sprintf("Model-visible tools (%d of %d registered — lazy surface):\n", len(tools), registered))
	} else {
		b.WriteString(fmt.Sprintf("Enabled tools (%d):\n", len(tools)))
	}
	for _, t := range tools {
		desc := t.Description
		if runes := []rune(desc); len(runes) > 96 {
			desc = string(runes[:96]) + "..."
		}
		b.WriteString(fmt.Sprintf("  %s — %s\n", t.Name, desc))
	}
	if registered > len(tools) {
		b.WriteString("\nUnlock more: ToolSearch with query select:<ToolName>")
	}
	return strings.TrimRight(b.String(), "\n")
}

func envSummary(provider, model string) string {
	return envSummaryWithSelection(provider, model, true)
}

func envSummaryWithSelection(provider, model string, includeSelection bool) string {
	var providers []string
	for _, gateway := range rhoconfig.GatewayStatuses(context.Background(), provider, model) {
		providers = append(providers, gateway.ID)
	}
	sort.Strings(providers)
	var b strings.Builder
	if includeSelection {
		b.WriteString(fmt.Sprintf("Provider: %s\nModel: %s\n\n", provider, model))
	}
	b.WriteString(fmt.Sprintf("Credentials (%s):\n", rhoconfig.CredentialStoreName()))
	for _, providerID := range providers {
		b.WriteString(fmt.Sprintf("  %s: %s\n", providerID, rhoconfig.EnvKeyStatus(providerID)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func configCommandSummary(settings rhoconfig.Settings) string {
	_ = settings
	providerName := displayConfigValue(rhoconfig.ActiveProvider(context.Background()))
	modelName := displayConfigValue(rhoconfig.ActiveModel(context.Background()))
	keys := configuredKeyList()
	keysColor := infoSky
	if keys == "(none)" {
		keysColor = textMuted
	}
	return fmt.Sprintf(`%s

  /config  → paste API key (OS keychain) + pick model
  /path    → verify readiness in TUI
  rho path (CLI)

%s:
  %s %s
  %s %s
  %s %s

Model catalog and routing live in flux — rho is the UI only.`,
		auditTint("Setup (flux)", textPrimary),
		auditTint("Current", textPrimary),
		auditTint("provider:", textMuted), auditTint(providerName, infoSky),
		auditTint("model:", textMuted), auditTint(modelName, infoSky),
		auditTint("keys:", textMuted), auditTint(keys, keysColor))
}

func apiKeyConfigSummary() string {
	return auditTint("API keys ("+rhoconfig.CredentialStoreName()+")", textPrimary) + "\n" + indentedAPIKeyLines()
}

func configuredKeyList() string {
	var providers []string
	for _, line := range apiKeyStatusLines() {
		name, status, ok := strings.Cut(line, ": ")
		if ok && status == "set" {
			providers = append(providers, name)
		}
	}
	if len(providers) == 0 {
		return "(none)"
	}
	return strings.Join(providers, ", ")
}

func indentedAPIKeyLines() string {
	lines := apiKeyStatusLines()
	if len(lines) == 0 {
		return "  " + auditTint("(empty)", textMuted)
	}
	var b strings.Builder
	for _, line := range lines {
		name, status, ok := strings.Cut(line, ": ")
		if !ok {
			b.WriteString("  " + line + "\n")
			continue
		}
		b.WriteString("  " + auditTint(name, textPrimary) + ": " + auditTint(status, apiKeyStatusColor(status)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func apiKeyStatusColor(status string) color.Color {
	switch status {
	case "set":
		return doneGreen
	case "local":
		return infoSky
	default:
		return textMuted
	}
}

func apiKeyStatusLines() []string {
	providers := rhoconfig.AllSetupGateways()
	sort.Strings(providers)
	var lines []string
	for _, provider := range providers {
		lines = append(lines, fmt.Sprintf("%s: %s", provider, rhoconfig.EnvKeyStatus(provider)))
	}
	return lines
}

func displayConfigValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(empty)"
	}
	return value
}
