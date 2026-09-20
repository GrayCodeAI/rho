package cmd

import (
	"fmt"
	"strings"

	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

// Viewport render cache — avoids re-wrapping and re-rendering markdown for the
// entire scrollback on every 50ms stream tick. Stable prefix is cached; only
// new/changed messages and the live tail (partial + spinner) are rebuilt.

func (m *chatModel) invalidateViewportCache() {
	m.vpStableContent = ""
	m.vpRenderedMsgs = 0
	m.vpRenderWidth = 0
	m.vpLastMsgLen = 0
	m.streamMDPrefixRaw = ""
	m.streamMDPrefixOut = ""
	m.streamMDWidth = 0
}

// Tool result collapse thresholds. Tool results longer than
// toolResultCollapseLines are collapsed to toolResultPreviewLines with a
// toggle indicator; the user presses Enter in scrollback focus to expand.
const (
	toolResultCollapseLines = 15 // collapse rendered output longer than this
	toolResultPreviewLines  = 8  // preview lines shown when collapsed
)

func renderDisplayMessage(msg displayMsg, i int, messages []displayMsg, viewWidth int, expanded map[int]bool) string {
	rhoC := ansiOrange
	rst := ansiReset
	bgDark := "\033[48;2;30;30;40m"

	// Sanitize untrusted content once, before any branch renders it. This is
	// the single choke point for the whole scrollback: tool results, model
	// output, thinking, and system messages all flow through here.
	msg.content = sanitizeDisplay(msg.content)

	var b strings.Builder

	switch msg.role {
	case "user":
		if i > 0 {
			b.WriteByte('\n')
		}
		wrapped := wrapText(msg.content, viewWidth-1, 3)
		wrappedLines := strings.Split(wrapped, "\n")
		for li, wl := range wrappedLines {
			if li == 0 {
				b.WriteString(bgDark + rhoC + "█" + rst + bgDark + "  " + wl)
			} else {
				b.WriteString(bgDark + "   " + wl)
			}
			visW := 3 + visibleWidth(wl)
			if pad := viewWidth - visW; pad > 0 {
				b.WriteString(strings.Repeat(" ", pad))
			}
			b.WriteString(rst)
			if li < len(wrappedLines)-1 {
				b.WriteByte('\n')
			}
		}
	case "assistant":
		content := strings.TrimLeft(msg.content, "\n\r")
		if strings.TrimSpace(content) == "" {
			// The model called a tool without any preceding text — skip the
			// message entirely rather than leaving an orphan "◈" line.
			return ""
		}
		b.WriteString(rhoC + icons.Robot() + " " + rst + renderMarkdown(content, viewWidth-3))
	case "tool_use":
		b.WriteString(toolStyle.Render(icons.CircleFilled() + " " + msg.content))
	case "tool_result":
		var inner strings.Builder
		if looksLikeGitDiff(msg.content) {
			rendered := renderGitDiffOutput(msg.content, viewWidth-6)
			inner.WriteString("    " + strings.ReplaceAll(rendered, "\n", "\n    "))
		} else if strings.Contains(msg.content, "diff ") && strings.Contains(msg.content, " lines") {
			parts := strings.SplitN(msg.content, "\ndiff ", 2)
			mainContent := parts[0]
			diffPart := ""
			if len(parts) > 1 {
				diffPart = "diff " + parts[1]
			}
			toolWrapped := wrapText(mainContent, viewWidth-6, 0)
			inner.WriteString(toolDimStyle.Render("    " + strings.ReplaceAll(toolWrapped, "\n", "\n    ")))
			if diffPart != "" {
				inner.WriteString("\n")
				diffStyled := renderDiffSummary(diffPart, viewWidth-6)
				inner.WriteString("    " + diffStyled)
			}
		} else if strings.Contains(msg.content, "Self-review found issues") {
			inner.WriteString(errorStyle.Render("    " + icons.CloseThick() + " " + msg.content))
		} else if strings.Contains(msg.content, "## Self-Reflection") {
			parts := strings.SplitN(msg.content, "## Self-Reflection", 2)
			mainContent := parts[0]
			reflectionPart := ""
			if len(parts) > 1 {
				reflectionPart = "## Self-Reflection" + parts[1]
			}
			toolWrapped := wrapText(mainContent, viewWidth-6, 0)
			inner.WriteString(toolDimStyle.Render("    " + strings.ReplaceAll(toolWrapped, "\n", "\n    ")))
			if reflectionPart != "" {
				inner.WriteString("\n")
				reflStyled := renderReflectionBox(reflectionPart, viewWidth-6)
				inner.WriteString("    " + reflStyled)
			}
		} else {
			display := formatToolResultDisplay(msg.content)
			toolWrapped := wrapText(display, viewWidth-6, 0)
			inner.WriteString(toolDimStyle.Render("    " + strings.ReplaceAll(toolWrapped, "\n", "\n    ")))
		}
		b.WriteString(collapseToolResult(inner.String(), i, expanded))
	case "thinking":
		thinkWrapped := wrapText(msg.content, viewWidth-4, 3)
		b.WriteString(dimStyle.Render(icons.Brain() + " " + thinkWrapped))
	case "welcome":
		// rendered in fixed welcome pane, not scrollback
	case "system":
		sysWrapped := wrapText(msg.content, viewWidth-2, 0)
		b.WriteString(dimStyle.Render(sysWrapped))
	case "setup_complete":
		b.WriteString(renderSetupCompleteMessage(msg.content))
	case "permission":
		b.WriteString(renderPermissionBox(msg.content, viewWidth, msg.timeoutAt))
	case "approval":
		b.WriteString(renderApprovalBox(msg.content, viewWidth, msg.timeoutAt))
	case "credential":
		b.WriteString(renderCredentialBox(msg.content, viewWidth, msg.timeoutAt))
	case "question":
		b.WriteString(renderQuestionBox(msg.content, viewWidth, msg.timeoutAt))
	case "usage":
		b.WriteString(dimStyle.Render("  " + msg.content))
	case "warning":
		warnWrapped := wrapText(msg.content, viewWidth-8, 0)
		b.WriteString(warnStyle.Render(warnWrapped))
	case "error":
		errWrapped := wrapText(msg.content, viewWidth-8, 7)
		b.WriteString(errorStyle.Render("Error: " + errWrapped))
	}

	switch msg.role {
	case "user":
		if i+1 < len(messages) && messages[i+1].role == "tool_use" {
			// A tool call the model runs immediately in response to this
			// command reads as a continuation of it, not a new block — no
			// blank-line gap.
			b.WriteByte('\n')
		} else {
			b.WriteString("\n\n")
		}
	case "tool_use":
		if i+1 < len(messages) && messages[i+1].role == "tool_result" {
			b.WriteByte('\n')
		} else {
			b.WriteString("\n\n")
		}
	case "tool_result":
		if i+1 < len(messages) && messages[i+1].role == "tool_use" {
			b.WriteByte('\n')
		} else {
			b.WriteString("\n\n")
		}
	case "usage":
		b.WriteByte('\n')
	case "welcome":
		// no trailing space
	default:
		if msg.role != "" {
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

// collapseToolResult truncates long tool result output to a preview with a
// [+N lines] toggle indicator when the result is not expanded. Short results
// (≤ toolResultCollapseLines) pass through unchanged. The expanded map is keyed
// by message index.
func collapseToolResult(rendered string, msgIdx int, expanded map[int]bool) string {
	if expanded[msgIdx] {
		return rendered
	}
	// Count visible lines. ANSI styling codes don't contain '\n', so a simple
	// split accurately reflects the rendered line count.
	lines := strings.Split(rendered, "\n")
	if len(lines) <= toolResultCollapseLines {
		return rendered
	}
	preview := strings.Join(lines[:toolResultPreviewLines], "\n")
	hidden := len(lines) - toolResultPreviewLines
	toggle := fmt.Sprintf("[+%d lines — Enter to expand]", hidden)
	return preview + "\n" + toolDimStyle.Render("    "+toggle)
}

func renderMessagesRange(messages []displayMsg, start, end int, viewWidth int, expanded map[int]bool) string {
	var b strings.Builder
	for i := start; i < end && i < len(messages); i++ {
		b.WriteString(renderDisplayMessage(messages[i], i, messages, viewWidth, expanded))
	}
	return b.String()
}

// renderStreamTail renders the live streaming partial. The stable prefix
// (every completed markdown block) is sanitized+rendered once and cached, so
// each 50ms tick only re-renders the small tail after the last block
// boundary instead of the whole accumulated response (which is O(n²) over a
// long stream). The cache self-validates via HasPrefix, so width changes,
// stream restarts, and partial resets all fall back to a clean rebuild.
func (m *chatModel) renderStreamTail(viewWidth int) string {
	raw := strings.TrimLeft(m.partial.String(), "\n\r")
	if raw == "" {
		return m.renderWaitingSpinnerLine() + "\n\n"
	}

	if m.streamMDWidth != viewWidth || !strings.HasPrefix(raw, m.streamMDPrefixRaw) {
		m.streamMDPrefixRaw = ""
		m.streamMDPrefixOut = ""
		m.streamMDWidth = viewWidth
	}
	// Fold any newly-completed blocks into the cached prefix, rendering ONLY
	// the new blocks (not the whole prefix) so cost stays linear over the
	// stream. The scan resumes from the last boundary, always outside a fence.
	if boundary := streamStableBoundary(raw, len(m.streamMDPrefixRaw)); boundary > len(m.streamMDPrefixRaw) {
		newBlocks := raw[len(m.streamMDPrefixRaw):boundary]
		rendered := renderMarkdown(sanitizeDisplay(sanitizeIdentity(newBlocks)), viewWidth-3)
		m.streamMDPrefixOut = appendRendered(m.streamMDPrefixOut, rendered, m.streamMDPrefixRaw)
		m.streamMDPrefixRaw = raw[:boundary]
	}

	out := m.streamMDPrefixOut
	if tail := raw[len(m.streamMDPrefixRaw):]; tail != "" {
		rendered := renderMarkdown(sanitizeDisplay(sanitizeIdentity(tail)), viewWidth-3)
		out = appendRendered(out, rendered, m.streamMDPrefixRaw)
	}
	return ansiOrange + icons.Robot() + " " + ansiReset + out + "\n\n"
}

// streamStableBoundary returns the offset just past the last blank-line block
// separator in raw that lies outside a fenced code block, mirroring
// renderMarkdown's fence rules (any ``` opens; a bare ``` closes). Splitting
// there and rendering the halves independently reproduces the full render.
// The scan starts at from, which callers guarantee is a previous boundary
// (always outside a fence), so the whole buffer is not re-scanned each tick.
func streamStableBoundary(raw string, from int) int {
	boundary := from
	inFence := false
	pos := from
	for {
		nl := strings.IndexByte(raw[pos:], '\n')
		if nl < 0 {
			break
		}
		trimmed := strings.TrimSpace(raw[pos : pos+nl])
		if !inFence && strings.HasPrefix(trimmed, "```") {
			inFence = true
		} else if inFence && trimmed == "```" {
			inFence = false
		}
		lineEnd := pos + nl + 1
		if !inFence && lineEnd < len(raw) && raw[lineEnd] == '\n' {
			boundary = lineEnd + 1
		}
		pos = lineEnd
	}
	return boundary
}

// appendRendered concatenates a freshly rendered segment onto already-rendered
// output, re-inserting the blank-line separator renderMarkdown trims. prevRaw
// is the raw text behind out, whose trailing blank lines are the separator.
func appendRendered(out, rendered, prevRaw string) string {
	if out == "" {
		return rendered
	}
	return out + trailingNewlines(prevRaw) + rendered
}

// trailingNewlines returns the newlines in s's trailing whitespace run —
// the blank-line separator renderMarkdown trims from a prefix render.
func trailingNewlines(s string) string {
	n := 0
	for i := len(s) - 1; i >= 0; i-- {
		switch s[i] {
		case '\n':
			n++
		case ' ', '\t', '\r':
			// blank-line content; keep scanning
		default:
			return strings.Repeat("\n", n)
		}
	}
	return strings.Repeat("\n", n)
}

// toolResultIndexAtViewportCenter returns the index of the tool_result message
// whose rendered content contains the viewport's center line, or -1 if none.
// It fully renders each tool_result (ignoring collapse state) to compute true
// line heights, then finds which message spans the center Y offset.
func (m *chatModel) toolResultIndexAtViewportCenter(viewWidth int) int {
	if m.viewport.Height() <= 0 {
		return -1
	}
	centerY := m.viewport.YOffset() + m.viewport.Height()/2
	cumulative := 0
	expanded := m.fullExpandedMap(len(m.messages))
	for i, msg := range m.messages {
		if msg.role != "tool_result" {
			// Still need to count lines for non-tool messages to track offset.
			rendered := renderDisplayMessage(msg, i, m.messages, viewWidth, expanded)
			cumulative += strings.Count(rendered, "\n")
			continue
		}
		// Render fully (expanded) to get true line count.
		rendered := renderDisplayMessage(msg, i, m.messages, viewWidth, expanded)
		lineCount := strings.Count(rendered, "\n")
		if centerY >= cumulative && centerY < cumulative+lineCount {
			return i
		}
		cumulative += lineCount
	}
	return -1
}

// fullExpandedMap returns a map where every index is expanded — used to compute
// true (uncollapsed) line heights for viewport position math.
// Uses a cached map to avoid repeated allocations.
func (m *chatModel) fullExpandedMap(n int) map[int]bool {
	if m.cachedExpandedMap != nil && m.cachedExpandedLen == n {
		return m.cachedExpandedMap
	}
	expanded := make(map[int]bool, n)
	for i := 0; i < n; i++ {
		expanded[i] = true
	}
	m.cachedExpandedMap = expanded
	m.cachedExpandedLen = n
	return expanded
}

// codeBlockAtViewportCenter finds the fenced code block in an assistant message
// whose rendered content contains the viewport's center line. Returns the raw
// code content (without fences) and true if found.
//
// Performance: Uses the cached viewport content (m.vpStableContent) to avoid
// re-rendering all messages. Falls back to full render only when cache is stale.
func (m *chatModel) codeBlockAtViewportCenter() (string, bool) {
	if m.viewport.Height() <= 0 {
		return "", false
	}
	centerY := m.viewport.YOffset() + m.viewport.Height()/2
	viewWidth := m.width
	if viewWidth <= 0 {
		viewWidth = 80
	}

	// Fast path: use cached viewport content if available and valid.
	// The viewport content is already rendered, so we can parse it directly.
	// It answers confidently only for single-block messages; otherwise it
	// returns ok=false and we fall through to the exact slow path below.
	if m.vpStableContent != "" && m.vpRenderedMsgs == len(m.messages) {
		if content, ok := m.codeBlockFromCachedViewport(centerY, viewWidth); ok {
			return content, true
		}
	}

	// Slow path: render messages incrementally, stopping early when we pass center.
	cumulative := 0
	expanded := m.fullExpandedMap(len(m.messages))
	for i, msg := range m.messages {
		rendered := renderDisplayMessage(msg, i, m.messages, viewWidth, expanded)
		lineCount := strings.Count(rendered, "\n")

		// Skip messages before the viewport center.
		if centerY >= cumulative+lineCount {
			cumulative += lineCount
			continue
		}

		// This message contains the center line.
		if msg.role == "assistant" {
			bestBlock := findClosestCodeBlock(msg.content, centerY-cumulative, viewWidth)
			if bestBlock != "" {
				return bestBlock, true
			}
		}
		// If not an assistant message or no code block found, continue searching.
		cumulative += lineCount
	}
	return "", false
}

// codeBlockFromCachedViewport extracts a code block from the cached viewport content.
// This is the fast path that avoids re-rendering all messages.
func (m *chatModel) codeBlockFromCachedViewport(centerY int, viewWidth int) (string, bool) {
	// Get the visible viewport content.
	content := m.viewport.View()
	if content == "" {
		return "", false
	}

	lines := strings.Split(content, "\n")
	// centerY is an absolute scrollback coordinate (YOffset + Height/2), but
	// View() returns only the visible rows (indexed 0..Height-1). Convert to a
	// viewport-relative line index so the search targets the true center even
	// when scrolled down.
	relCenter := centerY - m.viewport.YOffset()
	if relCenter >= len(lines) {
		relCenter = len(lines) - 1
	}
	if relCenter < 0 {
		relCenter = 0
	}

	// Find the assistant message containing the center line by looking for
	// the robot icon marker in the rendered output.
	// The robot icon marks the start of assistant messages.
	robotMarker := icons.Robot()

	// Search backwards from center to find the start of the assistant message.
	msgStart := relCenter
	for i := relCenter; i >= 0; i-- {
		if strings.Contains(lines[i], robotMarker) {
			msgStart = i
			break
		}
	}

	// Search forwards to find the end of the assistant message.
	msgEnd := relCenter
	for i := relCenter; i < len(lines); i++ {
		// Assistant messages end at the next user message (█ marker) or end of content.
		if i > msgStart && strings.Contains(lines[i], "█") {
			msgEnd = i - 1
			break
		}
		msgEnd = i
	}

	// Extract the assistant message content from the rendered lines.
	// This is approximate but good enough for code block extraction.
	var msgContent strings.Builder
	for i := msgStart; i <= msgEnd && i < len(lines); i++ {
		msgContent.WriteString(lines[i])
		msgContent.WriteByte('\n')
	}

	// Try to find code blocks in the extracted content.
	// Since this is rendered output (with ANSI codes), we need to strip them first.
	plainContent := stripAnsi(msgContent.String())
	blocks := extractCodeBlocksFromRendered(plainContent)
	// Only answer confidently when the message has exactly one code block —
	// that is unambiguously the block to copy. With zero or multiple blocks the
	// rendered-output heuristics cannot reliably pick the one under the cursor,
	// so return false and let the caller fall back to the exact slow path
	// (findClosestCodeBlock operates on raw content with true line positions).
	if len(blocks) == 1 {
		return blocks[0], true
	}

	return "", false
}

// extractCodeBlocksFromRendered extracts code blocks from rendered (plain text) output.
// Code blocks in rendered output are indented and have a language label above them.
func extractCodeBlocksFromRendered(content string) []string {
	var blocks []string
	lines := strings.Split(content, "\n")

	inCodeBlock := false
	var currentBlock strings.Builder

	for _, line := range lines {
		// Code blocks in rendered output start with indentation and have
		// a consistent background. We detect them by looking for lines
		// that start with spaces followed by code-like content.
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and language labels.
		if trimmed == "" {
			if inCodeBlock && currentBlock.Len() > 0 {
				// Empty line might end the code block.
				blocks = append(blocks, strings.TrimRight(currentBlock.String(), "\n"))
				currentBlock.Reset()
				inCodeBlock = false
			}
			continue
		}

		// Detect code block start: indented line with code-like content.
		if !inCodeBlock && strings.HasPrefix(line, "  ") && len(trimmed) > 0 {
			// Check if this looks like code (has typical code characters).
			if looksLikeCode(trimmed) {
				inCodeBlock = true
				currentBlock.WriteString(trimmed)
				currentBlock.WriteByte('\n')
			}
		} else if inCodeBlock {
			if strings.HasPrefix(line, "  ") || trimmed == "" {
				currentBlock.WriteString(trimmed)
				currentBlock.WriteByte('\n')
			} else {
				// End of code block.
				if currentBlock.Len() > 0 {
					blocks = append(blocks, strings.TrimRight(currentBlock.String(), "\n"))
					currentBlock.Reset()
				}
				inCodeBlock = false
			}
		}
	}

	// Flush any remaining code block.
	if currentBlock.Len() > 0 {
		blocks = append(blocks, strings.TrimRight(currentBlock.String(), "\n"))
	}

	return blocks
}

// looksLikeCode returns true if the line looks like code (has typical code characters).
func looksLikeCode(s string) bool {
	// Code typically has: braces, parentheses, semicolons, operators, etc.
	codeIndicators := []string{"{", "}", "(", ")", ";", "=", "+", "-", "*", "/", "<", ">", "[", "]", ":", "."}
	for _, indicator := range codeIndicators {
		if strings.Contains(s, indicator) {
			return true
		}
	}
	// Also check for common keywords.
	keywords := []string{"func", "def", "class", "import", "from", "return", "if", "for", "while", "var", "let", "const"}
	lower := strings.ToLower(s)
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// findClosestCodeBlock finds the code block in the content whose position is
// closest to the target line offset within the rendered message. It extracts
// code blocks with their raw positions, maps the target rendered line back to
// an approximate content line, and returns the nearest block.
func findClosestCodeBlock(content string, targetLine int, viewWidth int) string {
	// Extract code blocks with their line positions in the raw content.
	type rawBlock struct {
		lineIdx int // line index where opening ``` appears
		code    string
	}
	var rawBlocks []rawBlock

	lines := strings.Split(content, "\n")
	inFence := false
	fenceStart := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if !inFence {
				inFence = true
				fenceStart = i
			} else {
				inFence = false
				// Extract code between fences.
				var code strings.Builder
				for j := fenceStart + 1; j < i; j++ {
					if code.Len() > 0 {
						code.WriteByte('\n')
					}
					code.WriteString(lines[j])
				}
				if code.Len() > 0 {
					rawBlocks = append(rawBlocks, rawBlock{lineIdx: fenceStart, code: code.String()})
				}
			}
		}
	}

	if len(rawBlocks) == 0 {
		return ""
	}

	// Map the target rendered line to an approximate content line.
	// Code blocks render with roughly: 1 label line + N code lines + padding.
	// A simple proportional mapping works well enough for "closest" selection.
	totalContentLines := len(lines)
	rendered := renderMarkdown(content, viewWidth-3)
	totalRenderedLines := strings.Count(rendered, "\n") + 1
	if totalRenderedLines == 0 {
		return rawBlocks[0].code
	}
	contentTarget := targetLine * totalContentLines / totalRenderedLines

	// Find the raw block closest to the mapped target.
	bestDist := int(^uint(0) >> 1)
	bestCode := rawBlocks[0].code
	for _, b := range rawBlocks {
		dist := abs(b.lineIdx - contentTarget)
		if dist < bestDist {
			bestDist = dist
			bestCode = b.code
		}
	}
	return bestCode
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// assembleViewportContent builds scrollback using the render cache. Returns the
// full viewport string ready for SetContent.
func (m *chatModel) assembleViewportContent(viewWidth int) string {
	fullRebuild := m.vpRenderWidth != viewWidth ||
		m.vpRenderedMsgs > len(m.messages) ||
		m.vpStableContent == ""

	if !fullRebuild && m.vpRenderedMsgs < len(m.messages) {
		var b strings.Builder
		b.WriteString(m.vpStableContent)
		for i := m.vpRenderedMsgs; i < len(m.messages); i++ {
			b.WriteString(renderDisplayMessage(m.messages[i], i, m.messages, viewWidth, m.toolResultExpanded))
		}
		m.vpStableContent = b.String()
		m.vpRenderedMsgs = len(m.messages)
	} else if !fullRebuild && m.vpRenderedMsgs == len(m.messages) && m.vpRenderedMsgs > 0 {
		last := m.messages[m.vpRenderedMsgs-1]
		if len(last.content) != m.vpLastMsgLen {
			prefix := renderMessagesRange(m.messages, 0, m.vpRenderedMsgs-1, viewWidth, m.toolResultExpanded)
			tail := renderDisplayMessage(last, m.vpRenderedMsgs-1, m.messages, viewWidth, m.toolResultExpanded)
			m.vpStableContent = prefix + tail
		}
	}

	if fullRebuild {
		m.vpStableContent = renderMessagesRange(m.messages, 0, len(m.messages), viewWidth, m.toolResultExpanded)
		m.vpRenderedMsgs = len(m.messages)
		m.vpRenderWidth = viewWidth
	}

	if m.vpRenderedMsgs > 0 {
		m.vpLastMsgLen = len(m.messages[m.vpRenderedMsgs-1].content)
	} else {
		m.vpLastMsgLen = 0
	}

	welcome := m.renderWelcomeScreen(viewWidth)

	// Welcome-only state: no real messages yet — return just the welcome
	// screen so the viewport starts at top and the content fills it
	// naturally. Once the user sends a message, real content takes over.
	if m.vpRenderedMsgs == 0 && !m.waiting && welcome != "" {
		return welcome
	}

	content := m.vpStableContent
	if welcome != "" {
		content = welcome + "\n\n" + content
	}
	if m.waiting && !m.manualCompacting {
		content += m.renderStreamTail(viewWidth)
	}
	return content
}
