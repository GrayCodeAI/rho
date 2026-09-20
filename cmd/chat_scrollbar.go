package cmd

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// scrollbarWidth is the number of terminal columns reserved for the scrollbar column.
const scrollbarWidth = 1

// Scrollbar glyph palette — tuned to look premium in dark terminals.
const (
	scrollbarTrackGlyph  = "│" // dim track keeps the scroll position legible at a glance
	scrollbarThumbGlyph  = "┃" // heavy vertical line for the thumb (visible without reading as a solid block)
	scrollbarTopGlyph    = "╷" // cap at the very top of the track
	scrollbarBottomGlyph = "╵" // cap at the very bottom of the track
)

// scrollbarThumbStyle — Talon Gold thumb so it reads as a brand control.
func scrollbarThumbStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(rhoColor)
}

func scrollbarTrackStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(textDisabled)
}

// chatHasOverflow reports whether chat content exceeds the viewport height.
func (m chatModel) chatHasOverflow() bool {
	h := m.viewport.Height()
	if h <= 0 {
		return false
	}
	return m.contentLines > h
}

// chatScrollbarVisible reports when chat scrollbar should be displayed.
// Show it whenever the chat overflows so users get a stable, truthful indicator
// of position. Hiding it at the bottom leaves slight-overflow cases looking
// broken and makes the reserved gutter appear accidental.
func (m chatModel) chatScrollbarVisible() bool {
	return m.chatHasOverflow()
}

// chatViewportWidth returns the usable viewport width.
// When chat content exceeds the viewport height, one column is reserved for the scrollbar slider
// so text wrapping remains completely stable and smooth as the user scrolls up and down.
func (m chatModel) chatViewportWidth(totalWidth int) int {
	if totalWidth <= 0 {
		return 0
	}
	if mainWidth := m.chatMainWidth(totalWidth); mainWidth != totalWidth {
		totalWidth = mainWidth
	}
	if m.chatHasOverflow() && totalWidth > scrollbarWidth {
		return totalWidth - scrollbarWidth
	}
	return totalWidth
}

// renderScrollbar builds a vertical scrollbar string of exactly vpHeight rows.
//
// Design:
//   - Track character: │  (dim grey — structural, not distracting)
//   - Thumb character: ▊  (Talon Gold — three-quarters block, thick & solid CLI style)
//   - Thumb size: proportional and compact (min 1 row, reduced by 25%)
//   - Thumb position: tracks YOffset relative to max scrollable range
//
// Returns an empty string when there is no overflow.
func (m chatModel) renderScrollbar() string {
	return m.renderScrollbarHeight(m.viewport.Height())
}

func (m chatModel) renderScrollbarHeight(vpH int) string {
	if !m.chatHasOverflow() {
		return ""
	}

	totalLines := m.contentLines
	if vpH <= 0 || totalLines <= 0 {
		return ""
	}

	// Thumb size: proportional to the visible fraction of the scrollback.
	// Do not artificially cap it: when content only slightly overflows, the
	// thumb should nearly fill the track instead of looking like a tiny stub.
	thumbSize := (vpH * vpH) / totalLines
	if thumbSize < 1 {
		thumbSize = 1
	}
	if thumbSize > vpH {
		thumbSize = vpH
	}

	// Track space available for the thumb to move within.
	trackSpace := vpH - thumbSize

	// Thumb position: map YOffset into [0, trackSpace].
	maxOffset := totalLines - vpH
	if maxOffset <= 0 {
		maxOffset = 1
	}
	yOffset := m.viewport.YOffset()
	if yOffset < 0 {
		yOffset = 0
	}
	if yOffset > maxOffset {
		yOffset = maxOffset
	}

	thumbTop := 0
	if trackSpace > 0 {
		thumbTop = (yOffset * trackSpace) / maxOffset
	}
	thumbBottom := thumbTop + thumbSize - 1

	// Build the scrollbar column line by line.
	var sb strings.Builder
	for row := 0; row < vpH; row++ {
		if row >= thumbTop && row <= thumbBottom {
			sb.WriteString(scrollbarThumbStyle().Render(scrollbarThumbGlyph))
		} else if row == 0 {
			sb.WriteString(scrollbarTrackStyle().Render(scrollbarTopGlyph))
		} else if row == vpH-1 {
			sb.WriteString(scrollbarTrackStyle().Render(scrollbarBottomGlyph))
		} else {
			sb.WriteString(scrollbarTrackStyle().Render(scrollbarTrackGlyph))
		}
		if row < vpH-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// padToHeight pads a multi-line string with empty lines until it has exactly height lines.
func padToHeight(s string, height int) string {
	if height <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) >= height {
		return s
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// renderChatPane renders the chat viewport plus the scrollbar slider as a
// side-by-side layout, returning the full-width chat area string.
// When the user has scrolled up past a user message, a sticky header is
// prepended showing the most recent out-of-view prompt.
func (m chatModel) renderChatPane() string {
	chatView := m.viewport.View()
	origVpH := m.viewport.Height()

	// Prepend sticky header when scrolled up.
	sticky := m.renderStickyHeader(m.viewport.Width())
	if sticky != "" {
		chatView = sticky + "\n" + chatView
	}

	lines := strings.Split(chatView, "\n")
	if origVpH > 0 && len(lines) > origVpH {
		lines = lines[:origVpH]
	}
	chatView = strings.Join(lines, "\n")

	if !m.chatScrollbarVisible() {
		chatView = padToHeight(chatView, origVpH)
		return m.joinChatSidebar(chatView, origVpH)
	}

	scrollbar := m.renderScrollbarHeight(origVpH)
	if scrollbar == "" {
		chatView = padToHeight(chatView, origVpH)
		return m.joinChatSidebar(chatView, origVpH)
	}

	targetW := m.viewport.Width()
	if targetW <= 0 {
		targetW = 80
	}

	// Join each line of the chat view with the corresponding scrollbar row.
	chatLines := strings.Split(chatView, "\n")
	for len(chatLines) < origVpH {
		chatLines = append(chatLines, "")
	}
	barLines := strings.Split(scrollbar, "\n")

	var out strings.Builder
	for i, line := range chatLines {
		out.WriteString(line)
		if visW := visibleWidth(line); visW < targetW {
			out.WriteString(strings.Repeat(" ", targetW-visW))
		}
		if i < len(barLines) {
			out.WriteString(barLines[i])
		}
		if i < len(chatLines)-1 {
			out.WriteByte('\n')
		}
	}
	return m.joinChatSidebar(out.String(), origVpH)
}

func (m chatModel) joinChatSidebar(chatView string, height int) string {
	sidebarW := m.chatSidebarWidth(m.width)
	if sidebarW == 0 {
		return chatView
	}
	mainW := m.viewport.Width()
	if mainW <= 0 {
		mainW = m.chatMainWidth(m.width)
	}
	lines := strings.Split(padToHeight(chatView, height), "\n")
	sidebar := strings.Split(m.renderSessionSidebar(sidebarW, height), "\n")
	divider := scrollbarTrackStyle().Render("│")
	var out strings.Builder
	for i := 0; i < height; i++ {
		if i > 0 {
			out.WriteByte('\n')
		}
		line := lines[i]
		if w := visibleWidth(line); w < mainW {
			line += strings.Repeat(" ", mainW-w)
		}
		out.WriteString(line)
		out.WriteString(divider)
		out.WriteString(sidebar[i])
	}
	return out.String()
}
