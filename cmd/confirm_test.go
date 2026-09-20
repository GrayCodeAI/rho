package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestParseConfirm(t *testing.T) {
	yes := []string{"y", "Y", "yes", "Yes", "YES", " y ", "\tyes\n"}
	for _, in := range yes {
		if !parseConfirm(in) {
			t.Errorf("parseConfirm(%q) = false, want true", in)
		}
	}
	no := []string{"", "n", "N", "no", "No", "NO", "maybe", "1", "true", "yess", " yyy"}
	for _, in := range no {
		if parseConfirm(in) {
			t.Errorf("parseConfirm(%q) = true, want false", in)
		}
	}
}

func TestConfirmDestructiveFailsClosedWhenQuiet(t *testing.T) {
	previous := quietFlag
	quietFlag = true
	defer func() { quietFlag = previous }()

	ok, err := confirmDestructive("remove everything")
	if err == nil {
		t.Fatal("confirmDestructive should reject non-interactive destructive actions")
	}
	if ok {
		t.Fatal("confirmDestructive approved a non-interactive destructive action")
	}
}

func TestDestructiveCommandsExposeExplicitConfirmation(t *testing.T) {
	commands := []*cobra.Command{
		credentialsRemoveCmd,
		skillsRemoveCmd,
		checkpointDeleteCmd,
		tasteResetCmd,
		trustRemoveCmd,
		agentRemoveCmd,
		learnClearCmd,
		permissionsResetCmd,
	}
	for _, command := range commands {
		if command.Flags().Lookup("yes") == nil {
			t.Errorf("%s is missing explicit --yes confirmation", command.CommandPath())
		}
	}
}
