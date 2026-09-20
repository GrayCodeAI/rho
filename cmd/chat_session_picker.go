package cmd

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/GrayCodeAI/rho/internal/ui/icons"
	"github.com/mattn/go-runewidth"

	sessionfeature "github.com/GrayCodeAI/rho/internal/features/session"
	"github.com/GrayCodeAI/rho/internal/session"
)

// applySessionPickerFilter filters the session entries based on the current search query.
// Matches against session ID, preview text, and CWD. Results are scored by relevance
// and recency.
func (m *chatModel) applySessionPickerFilter() {
	m.sessionPickerFiltered = sessionfeature.FilterEntries(m.sessionPickerEntries, m.sessionPickerInput)
	m.sessionPickerSel = 0
}

// renderSessionPickerOverlay renders the Ctrl+S session picker overlay.
func (m *chatModel) renderSessionPickerOverlay(viewWidth int) string {
	if !m.sessionPickerOpen {
		return ""
	}

	maxVisible := 8
	if viewWidth < 60 {
		viewWidth = 60
	}
	boxWidth := viewWidth - 4
	if boxWidth > 80 {
		boxWidth = 80
	}

	var b strings.Builder

	// Title
	b.WriteString(overlayTitleStyle().Render("  Session Picker"))
	b.WriteString(overlayDimStyle().Render("  (Esc to cancel, Enter to resume)"))
	b.WriteString("\n\n")

	// Search input display
	queryDisplay := m.sessionPickerInput
	if queryDisplay == "" {
		queryDisplay = "type to filter sessions..."
	}
	b.WriteString("  ")
	b.WriteString(overlaySearchIconStyle().Render(icons.Magnify() + " "))
	if m.sessionPickerInput == "" {
		b.WriteString(overlayDimStyle().Italic(true).Render(queryDisplay))
	} else {
		b.WriteString(overlaySearchValueStyle().Render(queryDisplay))
	}
	b.WriteString("\n\n")

	// Results
	if len(m.sessionPickerFiltered) == 0 {
		if len(m.sessionPickerEntries) == 0 {
			b.WriteString(overlayDimStyle().Italic(true).Render("  No saved sessions found"))
		} else {
			b.WriteString(overlayDimStyle().Italic(true).Render("  No matching sessions"))
		}
	} else {
		start := 0
		if m.sessionPickerSel >= maxVisible {
			start = m.sessionPickerSel - maxVisible + 1
		}
		end := start + maxVisible
		if end > len(m.sessionPickerFiltered) {
			end = len(m.sessionPickerFiltered)
		}

		for i := start; i < end; i++ {
			entry := m.sessionPickerFiltered[i]
			line := formatSessionEntry(entry, m.sessionPickerInput, boxWidth-4)
			if i == m.sessionPickerSel {
				// Selected item: add a marker and use distinct style (consistent with history search).
				marker := overlaySelectionMarkerStyle().Render("> ")
				b.WriteString(overlaySelectedStyle().Width(boxWidth).Render(marker + line))
			} else {
				b.WriteString(overlayItemStyle().Width(boxWidth).Render("  " + line))
			}
			b.WriteString("\n")
		}

		// Scroll indicator
		if len(m.sessionPickerFiltered) > maxVisible {
			b.WriteString(overlayDimStyle().Render("  " + strconv.Itoa(m.sessionPickerSel+1) + "/" + strconv.Itoa(len(m.sessionPickerFiltered)) + " sessions"))
		}

		// Share detail for the selected session (Gap-02 P1): deeplink + export
		// path, cached per selection to avoid reloading on every frame.
		if sel := m.sessionPickerFiltered[m.sessionPickerSel]; sel.ID != "" {
			b.WriteString("\n")
			b.WriteString(m.sessionPickerDetailFor(sel))
		}
	}

	return overlayBoxStyle().Width(boxWidth).Render(b.String())
}

// sessionPickerDetailFor returns the cached share detail (deeplink + export
// path + model) for a selected session entry. The deeplink is computed once per
// selection and cached on the model.
func (m *chatModel) sessionPickerDetailFor(e session.Entry) string {
	if m.sessionPickerDetailID == e.ID && m.sessionPickerDetailCached != "" {
		return m.sessionPickerDetailCached
	}
	var b strings.Builder
	if e.Model != "" {
		b.WriteString(overlayDimStyle().Render("  model: "+e.Model) + "\n")
	}
	b.WriteString(overlayDimStyle().Render("  export: "+e.ExportPath) + "\n")
	if link := session.ShareLinkForID(e.ID); link != "" {
		b.WriteString(overlayDimStyle().Render("  share:  " + link))
	}
	detail := strings.TrimRight(b.String(), "\n")
	m.sessionPickerDetailID = e.ID
	m.sessionPickerDetailCached = detail
	return detail
}

// formatSessionEntry formats a single session entry for display.
// Shows: ID, preview, CWD (shortened), and time-ago.
func formatSessionEntry(e session.Entry, query string, maxWidth int) string {
	// Ensure minimum width to avoid negative calculations.
	if maxWidth < 20 {
		maxWidth = 20
	}

	// Format: "  ID  preview  [cwd]  time-ago"
	idStr := e.ID
	if len(idStr) > 8 {
		idStr = idStr[:8]
	}

	preview := e.Preview
	if preview == "" {
		preview = "(no messages)"
	}

	// Shorten CWD for display.
	cwdStr := ""
	if e.CWD != "" {
		cwdStr = shortenHomePath(e.CWD)
		if len(cwdStr) > 20 {
			tail := cwdStr[len(cwdStr)-17:]
			for len(tail) > 0 && !utf8.ValidString(tail) {
				tail = tail[1:]
			}
			cwdStr = "..." + tail
		}
		cwdStr = "[" + cwdStr + "]"
	}

	// Time ago.
	timeAgo := formatTimeAgo(e.UpdatedAt)

	// Build the line: ID + preview + cwd + time-ago
	leftPart := "  " + idStr + "  "
	rightPart := "  " + cwdStr + "  " + timeAgo + " "

	// Calculate available width for preview.
	leftW := runewidth.StringWidth(leftPart)
	rightW := runewidth.StringWidth(rightPart)
	previewMaxW := maxWidth - leftW - rightW
	if previewMaxW < 10 {
		previewMaxW = 10
	}

	previewDisplay := preview
	if runewidth.StringWidth(previewDisplay) > previewMaxW {
		previewDisplay = truncateString(previewDisplay, previewMaxW)
	}

	// Highlight match in preview. indexFold returns a byte span in the original
	// string, so the slice never splits a multi-byte rune even when case-folding
	// changes a rune's encoded length.
	if query != "" {
		if idx, matchLen := indexFold(previewDisplay, query); idx >= 0 {
			matched := previewDisplay[idx : idx+matchLen]
			previewDisplay = previewDisplay[:idx] + overlayMatchStyle().Render(matched) + previewDisplay[idx+matchLen:]
		}
	}

	// Pad preview to fill the gap.
	previewW := runewidth.StringWidth(preview)
	if previewW < previewMaxW {
		previewDisplay += strings.Repeat(" ", previewMaxW-previewW)
	}

	return leftPart + previewDisplay + rightPart
}

// formatTimeAgo returns a human-readable relative time string.
func formatTimeAgo(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "now"
	}
	if d < time.Hour {
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return strconv.Itoa(m) + "m ago"
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return strconv.Itoa(h) + "h ago"
	}
	if d < 30*24*time.Hour {
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return strconv.Itoa(days) + "d ago"
	}
	return t.Format("Jan 02")
}

// resumeSessionByID loads a session by ID and updates the model state.
func (m *chatModel) resumeSessionByID(id string) (tea.Model, tea.Cmd) {
	saved, err := session.Load(id)
	if err != nil {
		m.messages = append(m.messages, displayMsg{role: "error", content: "Failed to load session: " + err.Error()})
		m.viewDirty = true
		m.updateViewportContent()
		return m, nil
	}
	m.sessionID = saved.ID
	m.invalidateViewportCache()
	m.messages = []displayMsg{{role: "welcome", content: m.welcomeCache}}
	hydrated := sessionfeature.Hydrate(saved)
	for _, message := range hydrated.Display {
		m.messages = append(m.messages, displayMsg{role: message.Role, content: message.Content})
	}
	m.session.LoadMessages(hydrated.Runtime)
	m.messages = append(m.messages, displayMsg{role: "system", content: "Resumed session " + saved.ID})
	m.viewDirty = true
	m.autoScroll = false
	return m, nil
}
