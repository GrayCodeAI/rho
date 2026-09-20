package cmd

import (
	tea "charm.land/bubbletea/v2"
)

// branchSubcommand implements the /branch slash command. It shows
// the current git branch, short HEAD hash, upstream tracking branch,
// and a short status output. This is the first command migrated out
// of chat_commands.go as an exemplar of the SubcommandRegistry
// pattern; future commands should follow this same template. The init
// function registers it in the package-level subcommandRegistry, which
// handleCommand resolves through the shared dispatcher.
type branchSubcommand struct{}

func (b *branchSubcommand) Name() string      { return "branch" }
func (b *branchSubcommand) Aliases() []string { return nil }
func (b *branchSubcommand) Description() string {
	return "show current branch, HEAD, upstream, and status"
}
func (b *branchSubcommand) Usage() string { return "" }
func (b *branchSubcommand) Handle(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
	m.messages = append(m.messages, displayMsg{role: "system", content: branchSummary()})
	return m, nil
}

func init() {
	subcommandRegistry.Register(&branchSubcommand{})
}
