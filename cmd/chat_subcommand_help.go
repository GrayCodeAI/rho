package cmd

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	commandfeature "github.com/GrayCodeAI/rho/internal/features/commands"
)

// helpCategory returns the display category for a slash command name,
// matching the categories used in the command palette. This keeps /help
// output and the palette consistent.
func helpCategory(cmdName string) string {
	return commandfeature.Category(cmdName)
}

// helpSubcommand implements the /help and /commands slash commands.
// It prints the dynamically-generated help table of all registered
// subcommands (sorted by name, with description).
type helpSubcommand struct{}

func (h *helpSubcommand) Name() string        { return "help" }
func (h *helpSubcommand) Aliases() []string   { return []string{"commands"} }
func (h *helpSubcommand) Description() string { return "show this help" }
func (h *helpSubcommand) Usage() string       { return "" }
func (h *helpSubcommand) Handle(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
	// /help <topic> — show detailed help for a specific command or topic.
	// Supports: /help /commit, /help commit, /help "git operations"
	topic := strings.TrimSpace(strings.Join(args, " "))
	if topic == "" {
		topic = strings.TrimPrefix(strings.TrimSpace(text), "/help")
		topic = strings.TrimSpace(topic)
	}
	if topic != "" {
		// Normalize: ensure topic starts with "/" for command lookups.
		lookup := topic
		if !strings.HasPrefix(lookup, "/") {
			lookup = "/" + lookup
		}
		if m.contextualHelp != nil {
			if entry := m.contextualHelp.GetHelp(lookup); entry != nil {
				m.messages = append(m.messages, displayMsg{role: "system", content: m.contextualHelp.FormatHelp(entry)})
				return m, nil
			}
			// Not found by exact topic — try fuzzy search.
			if results := m.contextualHelp.SearchHelp(topic); len(results) > 0 {
				m.messages = append(m.messages, displayMsg{role: "system", content: m.contextualHelp.FormatSearchResults(results)})
				return m, nil
			}
		}
		// Fallback: show hint with available help topics.
		m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("No help found for %q. Type /help for all commands, or try: /help /commit, /help /test, /help /autonomy", topic)})
		return m, nil
	}
	m.messages = append(m.messages, displayMsg{role: "system", content: dynamicHelpText()})
	return m, nil
}

// helpCategoryOrder defines the display order for categories in /help output.
var helpCategoryOrder = []string{
	"Core", "Workflow", "Agent", "Memory", "Tools", "Diagnostics", "Settings", "Other",
}

// dynamicHelpText generates the help table from the live
// SubcommandRegistry, grouped by category so users can scan related
// commands together. Within each category, entries are sorted
// alphabetically. Each entry is formatted as
// `/<name> <args>     — <description>`.
func dynamicHelpText() string {
	all := subcommandRegistry.All()
	type entry struct {
		cmd  string
		desc string
	}
	// Group entries by category.
	catEntries := map[string][]entry{}
	for _, sub := range all {
		usage := sub.Usage()
		if usage != "" && !strings.HasPrefix(usage, " ") {
			usage = " " + usage
		}
		cmd := "/" + sub.Name()
		cat := helpCategory(cmd)
		catEntries[cat] = append(catEntries[cat], entry{cmd: cmd + usage, desc: sub.Description()})
	}

	var b strings.Builder
	wroteCat := false
	for _, cat := range helpCategoryOrder {
		entries, ok := catEntries[cat]
		if !ok || len(entries) == 0 {
			continue
		}
		// Sort entries within the category alphabetically.
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].cmd < entries[j].cmd
		})
		// Compute column width for this category.
		maxLen := 0
		for _, e := range entries {
			if len(e.cmd) > maxLen {
				maxLen = len(e.cmd)
			}
		}
		if maxLen > 40 {
			maxLen = 40
		}
		// Blank line between categories (not before the first).
		if wroteCat {
			b.WriteByte('\n')
		}
		wroteCat = true
		b.WriteString(cat + ":\n")
		for _, e := range entries {
			pad := maxLen - len(e.cmd) + 1
			if pad < 1 {
				pad = 1
			}
			b.WriteString("  " + e.cmd)
			for i := 0; i < pad; i++ {
				b.WriteByte(' ')
			}
			b.WriteString("— ")
			b.WriteString(e.desc)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func init() {
	subcommandRegistry.Register(&helpSubcommand{})
}
