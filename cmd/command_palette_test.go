package cmd

import "testing"

func TestCommandPaletteBuildEntriesUsesSlashCommands(t *testing.T) {
	cp := NewCommandPalette(120)
	want := map[string]bool{}
	seen := map[string]bool{}
	for _, cmd := range slashCommands() {
		want[cmd] = false
	}
	for _, entry := range cp.entries {
		if seen[entry.Name] {
			t.Fatalf("command palette contains duplicate entry %s", entry.Name)
		}
		seen[entry.Name] = true
		if _, ok := want[entry.Name]; ok {
			want[entry.Name] = true
		}
		if entry.Action != entry.Name {
			t.Fatalf("entry %q action = %q, want same command", entry.Name, entry.Action)
		}
		if entry.Description == "" {
			t.Fatalf("entry %q missing description", entry.Name)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("command palette missing slash command %s", name)
		}
	}
}

func TestSlashCommandsIncludesRegisteredSubcommands(t *testing.T) {
	commands := map[string]bool{}
	for _, cmd := range slashCommands() {
		commands[cmd] = true
	}
	for _, sub := range subcommandRegistry.All() {
		name := "/" + sub.Name()
		if !commands[name] {
			t.Fatalf("slashCommands missing registered subcommand %s", name)
		}
		for _, alias := range sub.Aliases() {
			name := "/" + alias
			if !commands[name] {
				t.Fatalf("slashCommands missing registered alias %s", name)
			}
		}
	}
}
