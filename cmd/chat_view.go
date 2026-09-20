package cmd

import (
	"fmt"
	"image/color"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/term"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/GrayCodeAI/rho/internal/ui/icons"
	"github.com/mattn/go-runewidth"
)

func (m chatModel) showWelcomeBanner() bool {
	return strings.TrimSpace(m.welcomeCache) != ""
}

// hasRealMessages counts messages that are actual chat content (not just the
// welcome header, usage hints, or setup_complete banners).
func (m chatModel) hasRealMessages() int {
	n := 0
	for _, msg := range m.messages {
		switch msg.role {
		case "welcome", "usage", "setup_complete":
			// skip decoration-only messages
		default:
			if msg.role != "" {
				n++
			}
		}
	}
	return n
}

func renderSetupCompleteMessage(model string) string {
	success := lipgloss.NewStyle().Foreground(doneGreen).Bold(true).Inline(true)
	muted := configMutedStyle().Inline(true)
	active := configActiveStyle().Inline(true)
	model = strings.TrimSpace(model)
	if model == "" {
		model = "selected model"
	}
	return lipgloss.JoinHorizontal(
		lipgloss.Left,
		success.Render("Setup complete"),
		muted.Render(" · model selected: "),
		active.Render(model),
		muted.Render(" "),
		success.Render(icons.CheckBold()+" "),
	)
}

var (
	// reModelName matches self-introductions like "I'm ChatGPT" or "My name is Claude".
	reModelName = regexp.MustCompile(`(?i)\b(I['` + "\u2018\u2019" + `]m|I am|my name is)\s+\*{0,2}(ChatGPT|GPT-?\d*[o]?|Claude|Gemini|Gemma|Kimi|DeepSeek|Llama|Qwen|Mistral|Mixtral|Grok|Copilot|Bard|Command R|Yi|Phi|Nova|Titan|BLOOM|Falcon|PaLM|LaMDA|Chinchilla|Vicuna|Alpaca|WizardLM|Orca|Nemotron|Granite|DBRX|OLMo|Pixtral|Ernie|PanGu|Sarvam|MiMo|GLM|Codex|Jurassic|Cohere|Jais|Step|Velvet|Alice|Apertus|Param|YandexGPT|MiniMax)\*{0,2}`)
	// reCreator matches origin claims like "made by OpenAI" or "built by a team at Anthropic".
	reCreator = regexp.MustCompile(`(?i)(made|created|developed|built|trained|designed)\s+by\s+(?:a\s+company\s+called\s+|a\s+team\s+(?:at|called)\s+|the\s+team\s+at\s+)?\*{0,2}(Moonshot\s*AI|OpenAI|Anthropic|Google|Google\s*DeepMind|DeepMind|Meta|Meta\s*AI|Alibaba|Alibaba\s*Cloud|Mistral\s*AI|xAI|Microsoft|Microsoft\s*AI|Amazon|AWS|Cohere|01\.AI|Baidu|Huawei|IBM|Nvidia|EleutherAI|Hugging\s*Face|AI21\s*Labs|Yandex|Databricks|StepFun|Xiaomi|Sarvam\s*AI|MiniMax|BharatGen|Z\.ai|Zhipu\s*AI|Cerebras|Technology\s*Innovation\s*Institute|TII|Inflection\s*AI|Stability\s*AI|Anysphere|Cognition\s*AI|Scale\s*AI|Sakana\s*AI)\*{0,2}`)
)

// sanitizeIdentity replaces model self-identifications with "rho" / "GrayCode AI".
func sanitizeIdentity(s string) string {
	s = reModelName.ReplaceAllStringFunc(s, func(m string) string {
		parts := reModelName.FindStringSubmatch(m)
		return parts[1] + " rho"
	})
	s = reCreator.ReplaceAllString(s, "${1} by GrayCode AI")
	return s
}

// wrapText wraps text to fit within the given width.
// The first line has no indent (caller provides the prefix); prefixWidth is the visual width
// of the prefix already printed (e.g. icons.Robot() + " " = 2 columns). Continuation lines are
// indented to align with the first line's text start position. width is the total terminal
// width available.
func wrapText(text string, width int, prefixWidth int) string {
	if width < 20 {
		width = 80
	}
	// First line has less room because the prefix is already printed.
	firstLineWidth := width - prefixWidth
	if firstLineWidth < 1 {
		// Narrow terminal: use full width minus 1 to avoid overflow.
		firstLineWidth = width - 1
		if firstLineWidth < 1 {
			firstLineWidth = 1
		}
	}
	// Continuation indent: spaces to align under the first line's text.
	indent := strings.Repeat(" ", prefixWidth)
	indentW := prefixWidth
	contWidth := width - indentW
	if contWidth < 10 {
		contWidth = width
	}
	var result strings.Builder
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		isFirst := (i == 0)
		if !isFirst {
			result.WriteString(indent)
		}

		// Detect leading whitespace on this line so wrapped continuations
		// stay aligned with the original content (e.g. indented bullet lists).
		trimmed := strings.TrimLeft(line, " \t")
		lineLeading := line[:len(line)-len(trimmed)]
		lineLeadingW := runewidth.StringWidth(lineLeading)

		// For bullet-style lines ("* ", "- ", "N. "), add extra indent so
		// continuation text aligns past the bullet marker.
		bulletExtra := ""
		if len(trimmed) > 0 {
			if strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "+ ") {
				bulletExtra = "  "
			} else if idx := strings.Index(trimmed, ". "); idx > 0 && idx <= 3 {
				allDigits := true
				for _, ch := range trimmed[:idx] {
					if ch < '0' || ch > '9' {
						allDigits = false
						break
					}
				}
				if allDigits {
					bulletExtra = strings.Repeat(" ", idx+2)
				}
			}
		}

		// Build the continuation indent for wrapped segments of this line.
		lineIndent := indent + lineLeading + bulletExtra
		lineIndentW := indentW + lineLeadingW + runewidth.StringWidth(bulletExtra)
		lineContWidth := width - lineIndentW
		if lineContWidth < 10 {
			lineContWidth = contWidth
			lineIndent = indent
		}

		maxW := firstLineWidth
		if !isFirst {
			maxW = contWidth - lineLeadingW // account for leading whitespace already written
		}
		if runewidth.StringWidth(line) <= maxW {
			result.WriteString(line)
			result.WriteByte('\n')
			continue
		}
		curWidth := 0
		var curLine strings.Builder
		for _, word := range strings.Fields(line) {
			wordW := runewidth.StringWidth(word)
			// Force-break words longer than available width
			if wordW > maxW && curWidth == 0 {
				runes := []rune(word)
				for len(runes) > 0 {
					chunk := 0
					chunkW := 0
					for chunk < len(runes) && chunkW+runewidth.RuneWidth(runes[chunk]) <= maxW {
						chunkW += runewidth.RuneWidth(runes[chunk])
						chunk++
					}
					if chunk == 0 {
						chunk = 1
						chunkW = runewidth.RuneWidth(runes[0])
					}
					if curLine.Len() > 0 {
						result.WriteString(curLine.String())
						result.WriteByte('\n')
						result.WriteString(lineIndent)
						curLine.Reset()
						maxW = lineContWidth
					}
					curLine.WriteString(string(runes[:chunk]))
					curWidth = chunkW
					runes = runes[chunk:]
					if len(runes) > 0 {
						result.WriteString(curLine.String())
						result.WriteByte('\n')
						result.WriteString(lineIndent)
						curLine.Reset()
						curWidth = 0
						maxW = lineContWidth
					}
				}
			} else if curWidth > 0 && curWidth+1+wordW > maxW {
				result.WriteString(curLine.String())
				result.WriteByte('\n')
				result.WriteString(lineIndent)
				curLine.Reset()
				curLine.WriteString(word)
				curWidth = wordW
				maxW = lineContWidth
			} else if curWidth > 0 {
				curLine.WriteByte(' ')
				curLine.WriteString(word)
				curWidth += 1 + wordW
			} else {
				curLine.WriteString(word)
				curWidth = wordW
			}
		}
		if curLine.Len() > 0 {
			result.WriteString(curLine.String())
			result.WriteByte('\n')
		}
	}
	return strings.TrimRight(result.String(), "\n")
}

// chatBottomBarLines counts fixed rows below the chat viewport (must stay in sync with View).
func (m chatModel) chatBottomBarLines() int {
	if m.configOpen {
		return 0
	}
	if m.cachedBottomBarLines > 0 {
		return m.cachedBottomBarLines
	}
	return m.computeChatBottomBarLines()
}

func (m chatModel) computeChatBottomBarLines() int {
	footerW := m.width
	if footerW < 40 {
		footerW = 80
	}
	inputBoxLines := m.measureInputBoxLines(footerW)
	lines := 1 + 1 + inputBoxLines // 1 top chrome divider + 1 container/model row + input box (measured)
	if val := m.input.Value(); strings.Count(val, "\n") > 0 {
		lines++ // multiline indicator row ("¶ N lines (Shift+Enter for newline)")
	}
	if m.ghostText != nil {
		if ghost := m.ghostText.Get(); ghost != "" && m.input.Value() == "" {
			lines++
		}
	}
	lines += m.visibleSlashSuggestionLines()
	lines += len(renderStatusBar(&m, footerW)) // exact status bar line count
	if m.manualCompacting {
		lines += 2 // "Compacting conversation..." + progress bar
	}
	if m.inScrollbackFocus() {
		lines++ // scrollback focus hint
	}
	return lines
}

func (m *chatModel) updateViewportContent() {
	viewWidth := m.width
	if viewWidth <= 0 {
		viewWidth = 80
	}

	*m = m.withSyncedLayout()

	if !m.viewDirty {
		return
	}
	m.viewDirty = false

	// /config overlay: config panel only.
	if m.configOpen {
		var content strings.Builder
		content.WriteString(m.configPanelView())
		contentString := content.String()
		m.contentLines = renderedLineCount(contentString)
		m.viewport.SetWidth(m.chatViewportWidth(viewWidth))
		m.viewport.SetContent(contentString)
		// The config lists paginate internally. Never retain the chat
		// transcript's outer Y offset, or the table header/help can disappear.
		m.viewport.GotoTop()
		return
	}

	atBottom := m.viewport.AtBottom()
	preserveScroll := !m.autoScroll && !atBottom
	prevYOffset := m.viewport.YOffset()
	contentStr, contentWidth, contentLines := m.renderViewportContentForLayout(viewWidth)
	m.contentLines = contentLines

	welcomeOnly := m.hasRealMessages() == 0 && !m.waiting

	m.viewport.SetWidth(contentWidth)
	m.viewport.SetContent(contentStr)
	switch {
	case welcomeOnly:
		// Start at top so the user sees the welcome screen without needing
		// to scroll up.
		m.viewport.GotoTop()
	case preserveScroll:
		m.viewport.SetYOffset(prevYOffset)
	case atBottom || (m.autoScroll && m.streamFollow):
		m.viewport.GotoBottom()
	}
}

func (m *chatModel) primeInitialViewportContent() {
	m.viewDirty = true
	m.updateViewportContent()
}

func (m *chatModel) renderViewportContentForLayout(viewWidth int) (string, int, int) {
	contentWidth := m.viewport.Width()
	if contentWidth <= 0 {
		contentWidth = m.chatViewportWidth(viewWidth)
	}
	if contentWidth < 20 {
		contentWidth = 80
	}

	contentStr := m.assembleViewportContent(contentWidth)
	contentLines := renderedLineCount(contentStr)

	// Overflow changes the usable width once the scrollbar gutter is visible.
	// Re-render once at the final width so wrapping, line counting, and
	// scrollbar state all describe the same layout.
	if m.viewport.Height() > 0 && contentLines > m.viewport.Height() && viewWidth >= 20 {
		narrowWidth := contentWidth - scrollbarWidth
		if narrowWidth < 1 {
			narrowWidth = 1
		}
		if narrowWidth != contentWidth {
			contentWidth = narrowWidth
			contentStr = m.assembleViewportContent(contentWidth)
			contentLines = renderedLineCount(contentStr)
		}
	}

	return contentStr, contentWidth, contentLines
}

func renderedLineCount(s string) int {
	lines := strings.Count(s, "\n") + 1
	if lines < 1 {
		return 1
	}
	return lines
}

func (m chatModel) View() tea.View {
	if m.quitting {
		return m.terminalView("")
	}
	m = m.withSyncedLayout()

	viewWidth := m.width
	if viewWidth <= 0 {
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
			viewWidth = w
		} else {
			viewWidth = 80
		}
	}

	// Build the fixed bottom bar
	var bottomBar strings.Builder

	if !m.configOpen {
		totalW := viewWidth
		if totalW < 40 {
			totalW = 80
		}
		slashOpen := m.slashMenuOpen()
		footerW := m.footerContentWidth(totalW)
		bottomBar.WriteString(m.finishFooterLine("", totalW) + "\n")
		modelRendered, _, ctxRendered, ctxVisLen := m.renderConnectionStatusSplit()
		rightLine := modelRendered
		if ctxVisLen > 0 {
			if modelRendered != "" {
				rightLine += configMutedStyle().Inline(true).Render(" · ")
			}
			rightLine += ctxRendered
		}
		// Keep permission posture on the left and connection/model metadata on
		// the right. Neither belongs in the transcript or workspace footer.
		topRow := layoutFooterRow(renderPermissionStatus(&m), rightLine, footerW)
		bottomBar.WriteString(m.finishFooterLine(topRow, totalW) + "\n")
		if m.manualCompacting {
			compactLine := clipFooterLine(m.renderCompactProgressPanel(footerW), footerW)
			bottomBar.WriteString(m.finishFooterLine(compactLine, totalW) + "\n")
		}
		if bar := m.renderScrollbackFocusBar(footerW); bar != "" {
			bottomBar.WriteString(m.finishFooterLine(bar, totalW) + "\n")
		}
		inputBox := inputBorderStyle.Width(footerW).Render(func() string {
			if m.useConfigInput {
				return m.configInput.View()
			}
			return m.input.View()
		}())
		inputBox = clipRenderedBlock(inputBox, footerW)
		bottomBar.WriteString(inputBox + "\n")
		// Persistence failure banner — surfaced once, persistently, so the
		// user knows recent messages may not survive a crash.
		if m.durabilityWarning != "" {
			warnLine := errorStyle.Render(clipFooterLine(m.durabilityWarning, footerW))
			bottomBar.WriteString(m.finishFooterLine(warnLine, totalW) + "\n")
		}
		// Multiline indicator — shows line count when input has newlines.
		if val := m.input.Value(); strings.Count(val, "\n") > 0 {
			lines := strings.Count(val, "\n") + 1
			mlHint := statusDimStyle.Render(fmt.Sprintf("  ¶ %d lines  (Shift+Enter for newline)", lines))
			bottomBar.WriteString(m.finishFooterLine(mlHint, totalW) + "\n")
		}
		if m.ghostText != nil {
			if ghost := m.ghostText.Get(); ghost != "" && m.input.Value() == "" {
				ghostLine := ghostHintStyle().Render("  → " + ghost + " (Tab to accept)")
				bottomBar.WriteString(m.finishFooterLine(ghostLine, totalW) + "\n")
			}
		}
		if slashOpen {
			if sugs := m.slashSuggestionsFor(m.input.Value()); len(sugs) > 0 {
				// NOTE: slashSel is clamped in Update (chat_update.go) and
				// submit (chat_submit.go), not here — View is by-value, so any
				// mutation would be silently discarded.
				cmdStyle := slashCmdStyle
				descStyle := slashDescStyle
				selCmdStyle := slashSelCmdStyle
				selDescStyle := slashSelDescStyle
				maxVisible := 6
				start := 0
				if m.slashSel >= maxVisible {
					start = m.slashSel - maxVisible + 1
				}
				end := start + maxVisible
				if end > len(sugs) {
					end = len(sugs)
				}
				for i := start; i < end; i++ {
					s := sugs[i]
					cmdPart := s
					descPart := ""
					if fields := strings.SplitN(s, "  ", 2); len(fields) == 2 {
						cmdPart = fields[0]
						descPart = fields[1]
					}
					pad := 20 - runewidth.StringWidth(cmdPart)
					if pad < 2 {
						pad = 2
					}
					if i == m.slashSel {
						bottomBar.WriteString("  " + selCmdStyle.Render(cmdPart) + strings.Repeat(" ", pad) + selDescStyle.Render(descPart) + "\n")
					} else {
						bottomBar.WriteString("  " + cmdStyle.Render(cmdPart) + strings.Repeat(" ", pad) + descStyle.Render(descPart) + "\n")
					}
				}
			}
		}
		stats := renderStatusBar(&m, footerW)
		for _, line := range stats {
			bottomBar.WriteString(m.finishFooterLine(line, totalW) + "\n")
		}
	}

	var frame strings.Builder
	frame.WriteString(m.renderChatPane())

	// Command palette overlay
	if m.commandPalette != nil && m.commandPalette.IsOpen() {
		paletteView := m.commandPalette.Render(viewWidth)
		frame.WriteByte('\n')
		frame.WriteString(paletteView)
		return m.terminalView(frame.String())
	}

	// Input history search overlay (Ctrl+R)
	if m.historySearchOpen {
		searchView := m.renderHistorySearchOverlay(viewWidth)
		frame.WriteByte('\n')
		frame.WriteString(searchView)
		return m.terminalView(frame.String())
	}

	// Session picker overlay (Ctrl+S)
	if m.sessionPickerOpen {
		pickerView := m.renderSessionPickerOverlay(viewWidth)
		frame.WriteByte('\n')
		frame.WriteString(pickerView)
		return m.terminalView(frame.String())
	}

	// Autonomy tier picker overlay
	if m.themePicker != nil && m.themePicker.IsOpen() {
		pickerView := lipgloss.NewStyle().Width(viewWidth).Render(m.themePicker.View().Content)
		frame.WriteByte('\n')
		frame.WriteString(pickerView)
		return m.terminalView(frame.String())
	}
	if m.autonomyPicker != nil && m.autonomyPicker.IsOpen() {
		pickerView := m.autonomyPicker.Render(viewWidth)
		frame.WriteByte('\n')
		frame.WriteString(pickerView)
		return m.terminalView(frame.String())
	}

	// Spec workflow picker overlay
	if m.specPicker != nil && m.specPicker.IsOpen() {
		pickerView := m.specPicker.Render(viewWidth)
		frame.WriteByte('\n')
		frame.WriteString(pickerView)
		return m.terminalView(frame.String())
	}

	// Agent Status HUD overlay
	if m.hudOpen {
		hudView := renderAgentStatusPanel(m.hudData, viewWidth)
		frame.WriteByte('\n')
		frame.WriteString(hudView)
		return m.terminalView(frame.String())
	}

	if bottomBar.Len() > 0 {
		frame.WriteByte('\n')
		frame.WriteString(bottomBar.String())
	}
	return m.terminalView(frame.String())
}

func (m chatModel) terminalView(content string) tea.View {
	view := tea.NewView(content)
	view.AltScreen = true
	view.ReportFocus = true
	if m.mouseEnabled() {
		view.MouseMode = tea.MouseModeCellMotion
	}
	return view
}

// renderPermissionBox renders a prominent inline permission prompt. When
// timeoutAt is non-zero, a visual countdown bar is shown above the options
// so the user can see how long they have to decide before the prompt auto-dismisses.
func renderPermissionBox(summary string, width int, timeoutAt time.Time) string {
	title := lipgloss.NewStyle().Foreground(warnAmber).Bold(true).Render(icons.Alert() + " Permission required")
	// Multi-line body: risk badge, action summary, short "why" (from FormatPermissionDisplay).
	bodyWidth := width - 10
	if bodyWidth < 20 {
		bodyWidth = 20
	}
	bodyLines := strings.Split(summary, "\n")
	var bodyParts []string
	for i, line := range bodyLines {
		wrapped := wrapText(line, bodyWidth, 0)
		if i == 0 {
			bodyParts = append(bodyParts, lipgloss.NewStyle().Foreground(warnAmber).Bold(true).Render(wrapped))
		} else if i == len(bodyLines)-1 && strings.HasPrefix(strings.TrimSpace(line), "Why:") {
			bodyParts = append(bodyParts, lipgloss.NewStyle().Foreground(textMuted).Render(wrapped))
		} else {
			bodyParts = append(bodyParts, lipgloss.NewStyle().Foreground(textWhite).Render(wrapped))
		}
	}
	body := lipgloss.JoinVertical(lipgloss.Left, bodyParts...)
	options := lipgloss.NewStyle().Foreground(rhoColor).Render(strings.Join([]string{
		"[y] allow once     [n] deny once",
		"[s] allow this action for session",
		"[d] deny this tool for session",
		"[p] allow exact for project    [x] deny exact for project",
	}, "\n"))
	hint := lipgloss.NewStyle().Foreground(textMuted).Render("Session rules reset on exit · Project rules persist · Esc denies · 5m timeout")

	return renderPromptCard(title, body, options, hint, warnAmber, width, timeoutAt)
}

// renderApprovalBox uses the same keyboard-first interaction language as the
// normal permission prompt, while making the extra high-risk checkpoint
// visually distinct.
func renderApprovalBox(summary string, width int, timeoutAt time.Time) string {
	title := lipgloss.NewStyle().Foreground(errorCoral).Bold(true).Render(icons.Alert() + " High-risk approval required")
	body := renderPromptBody(summary, width)
	options := lipgloss.NewStyle().Foreground(rhoColor).Render(strings.Join([]string{
		"[y] approve once     [n] deny once",
		"[s] approve category for session",
		"[5] approve next 5 actions",
	}, "\n"))
	hint := lipgloss.NewStyle().Foreground(textMuted).Render("Esc denies · expires after 5 minutes")
	return renderPromptCard(title, body, options, hint, errorCoral, width, timeoutAt)
}

func renderCredentialBox(summary string, width int, timeoutAt time.Time) string {
	title := lipgloss.NewStyle().Foreground(warnAmber).Bold(true).Render(icons.Key() + " Credential access required")
	body := renderPromptBody(summary, width)
	options := lipgloss.NewStyle().Foreground(rhoColor).Render("[y] allow access     [n] deny access")
	hint := lipgloss.NewStyle().Foreground(textMuted).Render("Credentials are never shown in the transcript · Esc denies")
	return renderPromptCard(title, body, options, hint, warnAmber, width, timeoutAt)
}

func renderQuestionBox(question string, width int, timeoutAt time.Time) string {
	title := lipgloss.NewStyle().Foreground(successTeal).Bold(true).Render(icons.HelpCircle() + " Input required")
	body := renderPromptBody(question, width)
	options := lipgloss.NewStyle().Foreground(rhoColor).Render("Type your answer · [Enter] submit · [Esc] deny")
	hint := lipgloss.NewStyle().Foreground(textMuted).Render("No answer is sent if the prompt expires")
	return renderPromptCard(title, body, options, hint, successTeal, width, timeoutAt)
}

func renderPromptBody(content string, width int) string {
	bodyWidth := width - 10
	if bodyWidth < 20 {
		bodyWidth = 20
	}
	return lipgloss.NewStyle().Foreground(textWhite).Render(wrapText(content, bodyWidth, 0))
}

// renderPromptCard is the shared shell for all blocking approval cards. The
// content differs by policy layer, but width safety, countdown placement, and
// visual hierarchy must remain identical.
func renderPromptCard(title, body, options, hint string, border color.Color, width int, timeoutAt time.Time) string {
	cardWidth := promptCardWidth(width)
	rows := []string{title, "", body}
	if !timeoutAt.IsZero() {
		rows = append(rows, "", renderCountdownBar(timeoutAt, cardWidth-6))
	}
	rows = append(rows, "", options, hint)
	inner := lipgloss.JoinVertical(lipgloss.Left, rows...)
	box := lipgloss.NewStyle().
		Width(cardWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Background(permissionBg).
		Padding(0, 1).
		Render(inner)
	return box
}

// promptCardWidth keeps approvals readable on wide terminals. A permission
// request is a focused decision, not a full-width status panel.
func promptCardWidth(width int) int {
	if width < 20 {
		return 20
	}
	cardWidth := width - 4
	if cardWidth > 96 {
		cardWidth = 96
	}
	return cardWidth
}

// renderCountdownBar renders a horizontal progress bar showing time remaining
// until a deadline. Fills from left to right as time passes, shifting from
// teal → amber → coral as the deadline approaches.
// Includes text label for accessibility (screen readers).
func renderCountdownBar(timeoutAt time.Time, width int) string {
	const totalDuration = interactivePromptTimeout
	if width < 10 {
		width = 20
	}
	remaining := time.Until(timeoutAt)
	if remaining <= 0 {
		return lipgloss.NewStyle().Foreground(errorCoral).Render("  [TIMEOUT] expired")
	}
	if remaining > totalDuration {
		remaining = totalDuration
	}
	fraction := float64(remaining) / float64(totalDuration)
	filled := int(fraction * float64(width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	empty := width - filled
	// Color shifts: teal (>60%), amber (30-60%), coral (<30%).
	// Text label indicates urgency level for accessibility.
	color := successTeal
	urgency := "OK"
	if fraction < 0.3 {
		color = errorCoral
		urgency = "URGENT"
	} else if fraction < 0.6 {
		color = warnAmber
		urgency = "SOON"
	}
	bar := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("█", filled) + strings.Repeat("░", empty))
	mins := int(remaining.Minutes())
	secs := int(remaining.Seconds()) % 60
	// Include text label for screen readers: [URGENT] 0:45
	return fmt.Sprintf("  [%s] %s %d:%02d", urgency, bar, mins, secs)
}

// renderDiffSummary renders a diff summary line with colored +/- indicators.
func renderDiffSummary(diffLine string, width int) string {
	addStyle := lipgloss.NewStyle().Foreground(doneGreen)
	delStyle := lipgloss.NewStyle().Foreground(errorCoral)
	fileStyle := lipgloss.NewStyle().Foreground(textMuted).Italic(true)

	// Parse "diff <file>: +N -N lines"
	parts := strings.SplitN(diffLine, ":", 2)
	if len(parts) != 2 {
		return dimStyle.Render(diffLine)
	}

	filePart := strings.TrimSpace(parts[0])  // "diff <file>"
	statsPart := strings.TrimSpace(parts[1]) // "+N -N lines"

	// Color the +/- numbers
	styled := statsPart
	styled = strings.ReplaceAll(styled, "+", addStyle.Render("+"))
	styled = strings.ReplaceAll(styled, "-", delStyle.Render("-"))

	return fileStyle.Render(filePart) + ": " + styled
}

// renderReflectionBox renders a self-reflection in a distinct styled box.
func renderReflectionBox(reflection string, width int) string {
	boxW := width - 2
	if boxW < 40 {
		boxW = 40
	}

	titleStyle := lipgloss.NewStyle().Foreground(rhoColor).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(infoSky).Bold(true)
	contentStyle := lipgloss.NewStyle().Foreground(textPrimary)

	var b strings.Builder
	lines := strings.Split(reflection, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			b.WriteString(titleStyle.Render(line) + "\n")
		} else if strings.HasPrefix(line, "**") && strings.Contains(line, ":**") {
			// "**What failed:** ..." format
			colonIdx := strings.Index(line, ":**")
			if colonIdx >= 0 {
				label := line[:colonIdx+3]
				rest := line[colonIdx+3:]
				b.WriteString(labelStyle.Render(label) + contentStyle.Render(rest) + "\n")
			} else {
				b.WriteString(contentStyle.Render(line) + "\n")
			}
		} else {
			b.WriteString(contentStyle.Render(line) + "\n")
		}
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(rhoColor).
		Width(boxW).
		Padding(0, 1)

	return border.Render(strings.TrimRight(b.String(), "\n"))
}

// renderWaitingSpinnerLine is the live status strip while the model works.
func (m chatModel) renderWaitingSpinnerLine() string {
	sep := ansiDim + " " + icons.CircleOutline() + " " + ansiReset

	var b strings.Builder
	b.WriteString(m.brailleSpinner.Frame())
	b.WriteString(sep)
	b.WriteString(ansiTeal + fmt.Sprintf("%.1fs", m.spinnerElapsed().Seconds()) + ansiReset)
	b.WriteString(sep)
	b.WriteString(m.renderTokenCounters())
	b.WriteString(" ")
	b.WriteString(ansiDim + "(esc stop)" + ansiReset)
	return b.String()
}

// renderTokenCounters formats the live per-turn token counters that ride
// next to the spinner. Uses ↑ for input (prompt) and ↓ for output
// (completion) tokens. The displayed numbers are lerped each render
// frame toward the engine's actual values (factor 0.25) so the counter
// slides smoothly instead of jumping when a usage event arrives
// mid-stream.
//
// Both arrows are always rendered (even at 0). ↓ is magenta (live model
// output) and ↑ is cyan (session context) — each purpose its own hue,
// both at the same bright intensity as the rest of the line.
func (m *chatModel) renderTokenCounters() string {
	inTok := int(m.displayInTok + 0.5)
	outTok := int(m.displayOutTok + 0.5)

	var b strings.Builder
	b.WriteString(ansiMagenta + ansiBold + "↓" + ansiReset)
	b.WriteString(ansiMagenta)
	b.WriteString(formatRhoTokenCount(outTok))
	b.WriteString(ansiReset)
	b.WriteString(ansiDim + "  " + ansiReset)
	b.WriteString(ansiCyan + ansiBold + "↑" + ansiReset)
	b.WriteString(ansiCyan)
	b.WriteString(formatRhoTokenCount(inTok))
	b.WriteString(ansiReset)
	return b.String()
}

// spinnerElapsed returns how long the spinner has been running. Tool
// start time wins (so per-tool elapsed resets between tools), otherwise
// we fall back to the moment the current turn started. Lazy-initialized
// on first render so every submit path gets a valid elapsed time without
// having to remember to set it explicitly.
func (m *chatModel) spinnerElapsed() time.Duration {
	if !m.toolStartTime.IsZero() {
		return time.Since(m.toolStartTime)
	}
	return time.Since(m.startedAt)
}

// tokenInputTarget is the target value the input-token display lerps
// toward. Uses the engine's reported number when available, else the
// session context estimate (real measurement of session state).
func (m *chatModel) tokenInputTarget() int {
	if m.turnInputTokens > 0 {
		return m.turnInputTokens
	}
	return sessionContextUsedTokens(m.session)
}

// tokenOutputTarget is the target value the output-token display lerps
// toward. Uses the engine's reported number when available, else the
// incremental rune count accumulated as streaming chunks arrive.
func (m *chatModel) tokenOutputTarget() int {
	if m.turnOutputTokens > 0 {
		return m.turnOutputTokens
	}
	return m.turnEstimatedOutputRunes / 4
}

// formatRhoTokenCount renders a token count in rho's compact form:
// ≥1m  → "1.5m", ≥10k → "150k", else raw digits.
func formatRhoTokenCount(tokens int) string {
	if tokens <= 0 {
		return "0"
	}
	switch {
	case tokens >= 1_000_000:
		v := float64(tokens) / 1_000_000
		if v == float64(int(v)) {
			return fmt.Sprintf("%dm", int(v))
		}
		return fmt.Sprintf("%.1fm", v)
	case tokens >= 10_000:
		return fmt.Sprintf("%dk", tokens/1000)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}
