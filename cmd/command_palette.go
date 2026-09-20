package cmd

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	commandfeature "github.com/GrayCodeAI/rho/internal/features/commands"
	"github.com/GrayCodeAI/rho/internal/plugin"
)

// CommandPaletteEntry represents a single command in the palette.
type CommandPaletteEntry struct {
	Name        string
	Description string
	Category    string
	Action      string // slash command to execute
}

// CommandPalette is a Ctrl+K command palette for quick command discovery.
type CommandPalette struct {
	open       bool
	input      textinput.Model
	inputReady bool
	entries    []CommandPaletteEntry
	filtered   []CommandPaletteEntry
	sel        int
	width      int
}

func (cp *CommandPalette) ensureInput() {
	if cp.inputReady {
		return
	}
	ti := textinput.New()
	ti.Placeholder = "Type to search commands..."
	ti.SetWidth(40)
	cp.input = ti
	cp.inputReady = true
}

// Palette styles are functions rather than package-level values so live theme
// changes apply to every picker, not only to newly created widgets.
func paletteTitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(rhoColor)
}

func paletteInputStyle() lipgloss.Style {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(borderDim).Padding(0, 1)
}

func paletteItemStyle() lipgloss.Style {
	return lipgloss.NewStyle().Padding(0, 1).Foreground(textPrimary)
}

func paletteSelStyle() lipgloss.Style {
	return lipgloss.NewStyle().Padding(0, 1).Background(bgCode).Foreground(textPrimary)
}

func paletteDescStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(textMuted)
}

func paletteCategoryStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(rhoColor).Bold(true)
}

func paletteDimStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(textDisabled)
}

func paletteBoxStyle() lipgloss.Style {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(borderDim).Padding(0, 1)
}

// NewCommandPalette creates a new command palette with all available commands.
func NewCommandPalette(width int) *CommandPalette {
	return newCommandPalette(width, nil)
}

func NewCommandPaletteWithRuntime(width int, runtime *plugin.Runtime) *CommandPalette {
	return newCommandPalette(width, runtime)
}

func newCommandPalette(width int, runtime *plugin.Runtime) *CommandPalette {
	cp := &CommandPalette{width: width}
	cp.ensureInput()
	cp.input.Focus()
	cp.entries = cp.buildEntries(runtime)
	cp.filtered = cp.entries
	return cp
}

// buildEntries builds the full list of palette entries from slash commands.
func (cp *CommandPalette) buildEntries(runtime *plugin.Runtime) []CommandPaletteEntry {
	commands := slashCommandsFor(runtime)
	descriptions := slashDescriptionsFor(runtime)
	entries := make([]CommandPaletteEntry, 0, len(commands))
	for _, name := range commands {
		desc := descriptions[name]
		if desc == "" {
			desc = slashCommandDescription(name)
		}
		entries = append(entries, CommandPaletteEntry{
			Name:        name,
			Description: desc,
			Category:    slashCommandCategory(name),
			Action:      name,
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Category != entries[j].Category {
			return entries[i].Category < entries[j].Category
		}
		return entries[i].Name < entries[j].Name
	})
	return entries
}

func (cp *CommandPalette) RefreshRuntime(runtime *plugin.Runtime) {
	cp.entries = cp.buildEntries(runtime)
	cp.filtered = cp.entries
	cp.sel = 0
}

func slashCommandDescription(name string) string {
	if desc := slashDescriptions[name]; desc != "" {
		return desc
	}
	cmdName := strings.TrimPrefix(name, "/")
	if cmd, ok := subcommandRegistry.Lookup(cmdName); ok {
		return cmd.Description()
	}
	return "Run " + name
}

func slashCommandCategory(name string) string {
	return commandfeature.Category(name)
}

// Open opens the command palette.
func (cp *CommandPalette) Open() {
	cp.ensureInput()
	if len(cp.entries) == 0 {
		cp.entries = cp.buildEntries(nil)
	}
	cp.open = true
	cp.input.SetValue("")
	cp.filtered = cp.entries
	cp.sel = 0
	cp.input.Focus()
}

// Close closes the command palette.
func (cp *CommandPalette) Close() {
	cp.open = false
	cp.input.SetValue("")
	cp.sel = 0
}

// IsOpen returns whether the palette is open.
func (cp *CommandPalette) IsOpen() bool {
	return cp.open
}

// Selected returns the currently selected entry, or nil.
func (cp *CommandPalette) Selected() *CommandPaletteEntry {
	if cp.sel >= 0 && cp.sel < len(cp.filtered) {
		return &cp.filtered[cp.sel]
	}
	return nil
}

// Update handles key events for the command palette. The returned command is
// the text input's follow-up work (for example cursor blink); callers must
// return it to Bubble Tea rather than discarding it.
func (cp *CommandPalette) Update(msg tea.KeyMsg) (string, bool, tea.Cmd) {
	if !cp.open {
		return "", false, nil
	}

	// Ctrl+K toggles the palette — press again to close (editor muscle memory).
	// Must check string form; there's no tea.KeyCtrlK constant in this Bubble Tea version.
	if msg.String() == "ctrl+k" || msg.String() == "ctrl+p" {
		cp.Close()
		return "", true, nil
	}

	switch key := msg.Key(); key.Code {
	case tea.KeyEsc:
		cp.Close()
		return "", true, nil
	case tea.KeyEnter:
		if sel := cp.Selected(); sel != nil {
			action := sel.Action
			cp.Close()
			return action, true, nil
		}
		return "", true, nil
	case tea.KeyUp:
		if len(cp.filtered) > 0 {
			cp.sel--
			if cp.sel < 0 {
				cp.sel = len(cp.filtered) - 1
			}
		}
		return "", true, nil
	case tea.KeyDown:
		if len(cp.filtered) > 0 {
			cp.sel = (cp.sel + 1) % len(cp.filtered)
		}
		return "", true, nil
	case tea.KeyTab:
		if sel := cp.Selected(); sel != nil {
			cp.input.SetValue(sel.Action + " ")
			cp.input.CursorEnd()
			cp.filter(cp.input.Value())
		}
		return "", true, nil
	default:
		var cmd tea.Cmd
		cp.input, cmd = cp.input.Update(msg)
		cp.filter(cp.input.Value())
		cp.sel = 0
		return "", true, cmd
	}
}

// filter applies fuzzy search to the entries, using scored ranking for
// relevance. Entries are sorted by score so the best match is first.
// When the query is empty, recently-used commands are promoted to the top
// under a "Recent" category.
func (cp *CommandPalette) filter(query string) {
	query = strings.TrimSpace(query)
	if query == "" {
		recent := recentCommands()
		if len(recent) == 0 {
			cp.filtered = cp.entries
			return
		}
		// Build a "Recent" section from history, followed by all commands.
		recentSet := make(map[string]bool, len(recent))
		entries := make([]CommandPaletteEntry, 0, len(recent)+len(cp.entries))
		for _, name := range recent {
			entries = append(entries, CommandPaletteEntry{
				Name:        name,
				Description: slashCommandDescription(name),
				Category:    "Recent",
				Action:      name,
			})
			recentSet[name] = true
		}
		// Append the full list, skipping duplicates already in Recent.
		for _, e := range cp.entries {
			if recentSet[e.Name] {
				continue
			}
			entries = append(entries, e)
		}
		cp.filtered = entries
		return
	}

	ranked := RankFuzzyResults(query, cp.entries)
	cp.filtered = make([]CommandPaletteEntry, 0, len(ranked))
	for _, r := range ranked {
		cp.filtered = append(cp.filtered, r.Entry)
	}
}

// Render renders the command palette as a string.
func (cp *CommandPalette) Render(viewWidth int) string {
	if !cp.open {
		return ""
	}

	maxVisible := 10
	if viewWidth < 60 {
		viewWidth = 60
	}
	boxWidth := viewWidth - 4
	if boxWidth > 70 {
		boxWidth = 70
	}

	var b strings.Builder
	// Title
	b.WriteString(paletteTitleStyle().Render("  Command Palette"))
	b.WriteString(paletteDimStyle().Render("  (Esc to close, Enter to run, Tab to edit)"))
	b.WriteString("\n\n")

	// Input
	cp.input.SetWidth(boxWidth - 4)
	b.WriteString(paletteInputStyle().Width(boxWidth - 2).Render(cp.input.View()))
	b.WriteString("\n\n")

	// Results
	if len(cp.filtered) == 0 {
		b.WriteString(paletteDimStyle().Render("  No matching commands"))
	} else {
		start := 0
		if cp.sel >= maxVisible {
			start = cp.sel - maxVisible + 1
		}
		end := start + maxVisible
		if end > len(cp.filtered) {
			end = len(cp.filtered)
		}

		// Group by category
		currentCat := ""
		for i := start; i < end; i++ {
			e := cp.filtered[i]
			if e.Category != currentCat {
				currentCat = e.Category
				b.WriteString(paletteCategoryStyle().Render("  "+currentCat) + "\n")
			}

			line := fmt.Sprintf("  %-18s %s", e.Name, paletteDescStyle().Render(e.Description))
			if i == cp.sel {
				b.WriteString(paletteSelStyle().Width(boxWidth).Render(line))
			} else {
				b.WriteString(paletteItemStyle().Width(boxWidth).Render(line))
			}
			b.WriteString("\n")
		}

		// Scroll indicator
		if len(cp.filtered) > maxVisible {
			b.WriteString(paletteDimStyle().Render(fmt.Sprintf("  %d/%d results", cp.sel+1, len(cp.filtered))))
		}
	}

	return paletteBoxStyle().Width(boxWidth).Render(b.String())
}
