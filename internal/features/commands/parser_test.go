package commands

import "testing"

func TestParseHelpShorthand(t *testing.T) {
	got := Parse("? config")
	if got.Text != "/help config" || got.Command != "/help" || len(got.Parts) != 2 {
		t.Fatalf("Parse = %#v", got)
	}
}

func TestParsePreservesNonSlashCommandCase(t *testing.T) {
	got := Parse("Build this")
	if got.Command != "Build" || got.Text != "Build this" {
		t.Fatalf("Parse = %#v", got)
	}
}
