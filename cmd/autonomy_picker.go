package cmd

import (
	"strings"

	"github.com/GrayCodeAI/rho/internal/engine/safety"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"
)

// autonomyPickerEntry is one selectable row in the picker.
type autonomyPickerEntry struct {
	Level       safety.AutonomyLevel
	Name        string
	Description string
}

// allAutonomyTiers lists every tier in strictness order (most to least
// cautious), for the picker only. This is intentionally separate from
// autonomyTiers (the Ctrl+L cycle), which deliberately excludes
// Supervised so repeated key-presses can't land you in max-friction mode by
// accident — the picker is a deliberate selection, so Supervised is fine here.
var allAutonomyTiers = []safety.AutonomyLevel{
	safety.AutonomySupervised,
	safety.AutonomyBasic,
	safety.AutonomySemi,
	safety.AutonomyFull,
	safety.AutonomyYOLO,
}

// AutonomyPicker is a Ctrl+L-adjacent quick-select overlay for choosing a
// trust tier directly, modeled on CommandPalette's interaction pattern
// (arrow keys to navigate, Enter to select, Esc to dismiss, type to filter).
type AutonomyPicker struct {
	open       bool
	input      textinput.Model
	inputReady bool
	entries    []autonomyPickerEntry
	filtered   []autonomyPickerEntry
	sel        int
	width      int
}

func (ap *AutonomyPicker) ensureInput() {
	if ap.inputReady {
		return
	}
	ti := textinput.New()
	ti.Placeholder = "Type to filter…"
	ti.CharLimit = 40
	ti.SetWidth(40)
	ap.input = ti
	ap.inputReady = true
}

// NewAutonomyPicker creates a new autonomy tier picker.
func NewAutonomyPicker(width int) *AutonomyPicker {
	entries := make([]autonomyPickerEntry, 0, len(allAutonomyTiers))
	for _, level := range allAutonomyTiers {
		entries = append(entries, autonomyPickerEntry{
			Level:       level,
			Name:        autonomyTierName(level),
			Description: autonomyTierDescription(level),
		})
	}

	ap := &AutonomyPicker{width: width, entries: entries, filtered: entries}
	ap.ensureInput()
	ap.input.Focus()
	return ap
}

// Open opens the picker, pre-selecting the currently active tier.
func (ap *AutonomyPicker) Open(current safety.AutonomyLevel) {
	ap.ensureInput()
	if len(ap.entries) == 0 {
		for _, level := range allAutonomyTiers {
			ap.entries = append(ap.entries, autonomyPickerEntry{
				Level: level, Name: autonomyTierName(level), Description: autonomyTierDescription(level),
			})
		}
	}
	ap.open = true
	ap.input.SetValue("")
	ap.input.Focus()
	ap.filtered = ap.entries
	ap.sel = 0
	for i, e := range ap.entries {
		if e.Level == current {
			ap.sel = i
			break
		}
	}
}

// Close closes the picker.
func (ap *AutonomyPicker) Close() {
	ap.open = false
	ap.input.SetValue("")
	ap.sel = 0
}

// IsOpen returns whether the picker is open.
func (ap *AutonomyPicker) IsOpen() bool {
	return ap.open
}

// Selected returns the currently highlighted entry, or nil.
func (ap *AutonomyPicker) Selected() *autonomyPickerEntry {
	if ap.sel >= 0 && ap.sel < len(ap.filtered) {
		return &ap.filtered[ap.sel]
	}
	return nil
}

// Update handles key events. Returns (chosen, handled) — chosen is non-nil
// only on the keypress that commits a selection.
func (ap *AutonomyPicker) Update(msg tea.KeyMsg) (*autonomyPickerEntry, bool) {
	if !ap.open {
		return nil, false
	}

	switch key := msg.Key(); key.Code {
	case tea.KeyEsc:
		ap.Close()
		return nil, true
	case tea.KeyEnter:
		sel := ap.Selected()
		ap.Close()
		return sel, true
	case tea.KeyUp:
		if len(ap.filtered) > 0 {
			ap.sel--
			if ap.sel < 0 {
				ap.sel = len(ap.filtered) - 1
			}
		}
		return nil, true
	case tea.KeyDown:
		if len(ap.filtered) > 0 {
			ap.sel = (ap.sel + 1) % len(ap.filtered)
		}
		return nil, true
	default:
		var cmd tea.Cmd
		ap.input, cmd = ap.input.Update(msg)
		_ = cmd
		ap.filter(ap.input.Value())
		ap.sel = 0
		return nil, true
	}
}

func (ap *AutonomyPicker) filter(query string) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		ap.filtered = ap.entries
		return
	}
	filtered := make([]autonomyPickerEntry, 0, len(ap.entries))
	for _, e := range ap.entries {
		if strings.Contains(strings.ToLower(e.Name), query) || strings.Contains(strings.ToLower(e.Description), query) {
			filtered = append(filtered, e)
		}
	}
	ap.filtered = filtered
}

// Render renders the picker as a string, styled to match CommandPalette.
func (ap *AutonomyPicker) Render(viewWidth int) string {
	if !ap.open {
		return ""
	}

	if viewWidth < 60 {
		viewWidth = 60
	}
	boxWidth := viewWidth - 4
	if boxWidth > 78 {
		boxWidth = 78
	}

	var b strings.Builder
	b.WriteString(paletteTitleStyle().Render("  Autonomy"))
	b.WriteString(paletteDimStyle().Render("  (↑↓ navigate · Enter select · Esc dismiss)"))
	b.WriteString("\n\n")

	ap.input.SetWidth(boxWidth - 4)
	b.WriteString(paletteInputStyle().Width(boxWidth - 2).Render(ap.input.View()))
	b.WriteString("\n\n")

	if len(ap.filtered) == 0 {
		b.WriteString(paletteDimStyle().Render("  No matching tiers"))
	} else {
		nameWidth := 0
		for _, e := range ap.filtered {
			if w := runewidth.StringWidth(e.Name); w > nameWidth {
				nameWidth = w
			}
		}
		for i, e := range ap.filtered {
			name := lipgloss.NewStyle().Bold(true).Foreground(autonomyTierColor(e.Level)).Render(padRight(e.Name, nameWidth))
			line := "  " + name + "  " + e.Description
			if i == ap.sel {
				b.WriteString(paletteSelStyle().Width(boxWidth).Render("  " + padRight(e.Name, nameWidth) + "  " + e.Description))
			} else {
				b.WriteString(paletteItemStyle().Width(boxWidth).Render(line))
			}
			b.WriteString("\n")
		}
	}

	return paletteBoxStyle().Width(boxWidth).Render(strings.TrimRight(b.String(), "\n"))
}

func padRight(s string, width int) string {
	w := runewidth.StringWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}
