package cmd

import (
	"strconv"
	"strings"

	chatfeature "github.com/GrayCodeAI/rho/internal/features/chat"
	"github.com/GrayCodeAI/rho/internal/textutil"
	"github.com/GrayCodeAI/rho/internal/ui/icons"
	"github.com/mattn/go-runewidth"
)

// applyHistorySearchFilter filters the input history based on the current search query.
// Results are ordered by relevance: exact prefix matches first, then substring matches,
// then subsequence matches. Within each tier, most recent entries come first.
func (m *chatModel) applyHistorySearchFilter() {
	m.historySearchFiltered = chatfeature.SearchHistory(m.history.Entries(), m.historySearchInput)
	m.historySearchSel = 0
}

// renderHistorySearchOverlay renders the Ctrl+R history search overlay.
func (m *chatModel) renderHistorySearchOverlay(viewWidth int) string {
	if !m.historySearchOpen {
		return ""
	}

	maxVisible := 8
	if viewWidth < 60 {
		viewWidth = 60
	}
	boxWidth := viewWidth - 4
	if boxWidth > 70 {
		boxWidth = 70
	}

	var b strings.Builder

	// Title
	b.WriteString(overlayTitleStyle().Render("  History Search"))
	b.WriteString(overlayDimStyle().Render("  (Esc to cancel, Enter to select)"))
	b.WriteString("\n\n")

	// Search input display
	queryDisplay := m.historySearchInput
	if queryDisplay == "" {
		queryDisplay = "type to search..."
	}
	b.WriteString("  ")
	b.WriteString(overlaySearchIconStyle().Render(icons.Magnify() + " "))
	if m.historySearchInput == "" {
		b.WriteString(overlayDimStyle().Italic(true).Render(queryDisplay))
	} else {
		b.WriteString(overlaySearchValueStyle().Render(queryDisplay))
	}
	b.WriteString("\n\n")

	// Results
	if len(m.historySearchFiltered) == 0 {
		b.WriteString(overlayDimStyle().Render("  No matching history"))
	} else {
		start := 0
		if m.historySearchSel >= maxVisible {
			start = m.historySearchSel - maxVisible + 1
		}
		end := start + maxVisible
		if end > len(m.historySearchFiltered) {
			end = len(m.historySearchFiltered)
		}

		for i := start; i < end; i++ {
			entry := m.historySearchFiltered[i]
			// Highlight the matching portion.
			displayEntry := highlightMatch(entry, m.historySearchInput, boxWidth-6)
			if i == m.historySearchSel {
				// Selected item: add a marker and use distinct style.
				marker := overlaySelectionMarkerStyle().Render("> ")
				b.WriteString(overlaySelectedStyle().Width(boxWidth).Render(marker + displayEntry))
			} else {
				b.WriteString(overlayItemStyle().Width(boxWidth).Render("  " + displayEntry))
			}
			b.WriteString("\n")
		}

		// Scroll indicator
		if len(m.historySearchFiltered) > maxVisible {
			b.WriteString(overlayDimStyle().Render("  " + strconv.Itoa(m.historySearchSel+1) + "/" + strconv.Itoa(len(m.historySearchFiltered)) + " results"))
		}
	}

	return overlayBoxStyle().Width(boxWidth).Render(b.String())
}

// highlightMatch highlights the matching substring in the entry.
func highlightMatch(entry, query string, maxWidth int) string {
	if query == "" {
		if runewidth.StringWidth(entry) > maxWidth-4 {
			return truncateString(entry, maxWidth-4)
		}
		return "  " + entry
	}

	display := entry
	if idx, matchLen := indexFold(entry, query); idx >= 0 {
		// Insert highlight around the match. indexFold returns a byte span in
		// the original string, so this never splits a multi-byte rune even when
		// case-folding changes a rune's encoded length.
		matched := entry[idx : idx+matchLen]
		display = entry[:idx] + overlayMatchStyle().Render(matched) + entry[idx+matchLen:]
	}

	if runewidth.StringWidth(entry) > maxWidth-4 {
		display = truncateString(entry, maxWidth-4)
	}

	return "  " + display
}

// indexFold returns the byte offset and byte length of the first
// case-insensitive match of substr in s. The returned span refers to the
// original string and never splits a multi-byte rune; see textutil.IndexFold.
func indexFold(s, substr string) (idx, matchLen int) {
	return textutil.IndexFold(s, substr)
}

// truncateString truncates a string to the given width with ellipsis.
func truncateString(s string, maxWidth int) string {
	if runewidth.StringWidth(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	w := 0
	for i, r := range runes {
		w += runewidth.RuneWidth(r)
		if w > maxWidth-1 {
			return string(runes[:i]) + "…"
		}
	}
	return s
}
