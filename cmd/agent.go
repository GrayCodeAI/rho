package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/GrayCodeAI/rho/internal/multiagent/agents"
	"github.com/GrayCodeAI/rho/internal/theme"
	"github.com/spf13/cobra"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage custom agent personas",
	Long:  "Create, list, and manage custom agent personas stored in Rho user state.",
}

var agentListJSON bool

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all available agents",
	RunE:  runAgentList,
}

var agentCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new agent persona",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentCreate,
}

var agentShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show an agent's configuration",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentShow,
}

var agentRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove an agent persona",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentRemove,
}

var (
	agentCreateDesc  string
	agentCreateModel string
)

func init() {
	agentCreateCmd.Flags().StringVarP(&agentCreateDesc, "description", "d", "", "Agent description")
	agentCreateCmd.Flags().StringVarP(&agentCreateModel, "model", "m", "", "Model to use (empty = inherit)")

	agentListCmd.Flags().BoolVar(&agentListJSON, "json", false, "output agents as JSON")
	agentRemoveCmd.Flags().Bool("yes", false, "confirm removing the agent")
	agentCmd.AddCommand(agentListCmd)
	agentCmd.AddCommand(agentCreateCmd)
	agentCmd.AddCommand(agentShowCmd)
	agentCmd.AddCommand(agentRemoveCmd)
}

func runAgentList(cmd *cobra.Command, _ []string) error {
	all, err := agents.ListAll()
	if err != nil {
		return err
	}
	if agentListJSON {
		if all == nil {
			all = []*agents.Agent{}
		}
		out, err := json.MarshalIndent(all, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling agents: %w", err)
		}
		cmd.Println(string(out))
		return nil
	}
	if len(all) == 0 {
		fmt.Printf("%s\n", auditTint("No agents found. Create one with: rho agent create <name>", textMuted))
		fmt.Printf("%s\n", auditTint("Agent directory: "+agents.DefaultDir(), textPrimary))
		return nil
	}

	rows := make([][]string, 0, len(all))
	for _, a := range all {
		model := a.Model
		if model == "" {
			model = "(inherit)"
		}
		desc := a.Description
		if len(desc) > 50 {
			// Rune-safe truncation: never split a multibyte UTF-8 sequence.
			if runes := []rune(desc); len(runes) > 50 {
				desc = string(runes[:50]) + "..."
			}
		}
		rows = append(rows, []string{a.Name, model, desc})
	}
	return theme.PrintTable(os.Stdout, []string{"NAME", "MODEL", "DESCRIPTION"}, rows)
}

func runAgentCreate(_ *cobra.Command, args []string) error {
	name := args[0]
	dir := agents.DefaultDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

	path := filepath.Join(dir, name+".md")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("agent %q already exists at %s", name, path)
	}

	modelLine := "inherit"
	if agentCreateModel != "" {
		modelLine = agentCreateModel
	}
	desc := agentCreateDesc
	if desc == "" {
		desc = "Custom agent: " + name
	}

	content := fmt.Sprintf(`---
name: %s
description: %s
model: %s
---
# %s

You are a specialized agent. Complete tasks according to your expertise.

## Guidelines
- Be precise and focused
- Follow project conventions
- Report results clearly
	`, name, desc, modelLine, cases.Title(language.English).String(name))

	// #nosec G306 -- agent definition is a user-facing markdown doc, intended to be normally readable/editable
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}

	fmt.Printf("%s\n", auditTint("Created agent "+name+" at ", doneGreen)+auditTint(path, textPrimary))
	fmt.Printf("%s\n", auditTint("Edit the file to customize the system prompt.", textMuted))
	return nil
}

func runAgentShow(_ *cobra.Command, args []string) error {
	a, err := agents.Get(args[0])
	if err != nil {
		return err
	}

	fmt.Printf("%s %s\n", auditTint("Name:", textMuted), auditTint(a.Name, textPrimary))
	fmt.Printf("%s %s\n", auditTint("Description:", textMuted), auditTint(a.Description, textPrimary))
	model := a.Model
	if model == "" {
		model = "(inherit from session)"
	}
	fmt.Printf("%s %s\n", auditTint("Model:", textMuted), auditTint(model, textPrimary))
	fmt.Printf("%s %s\n", auditTint("File:", textMuted), auditTint(a.FilePath, textPrimary))
	fmt.Printf("\n%s\n%s\n", auditTint("--- Prompt ---", rhoColor), a.Prompt)
	return nil
}

func runAgentRemove(cmd *cobra.Command, args []string) error {
	a, err := agents.Get(args[0])
	if err != nil {
		return err
	}

	ok, _ := cmd.Flags().GetBool("yes")
	if !ok {
		var err error
		ok, err = confirmDestructive(fmt.Sprintf("Remove agent %q (%s)?", a.Name, a.FilePath))
		if err != nil {
			return err
		}
	}
	if !ok {
		fmt.Printf("%s\n", auditTint("Cancelled.", textMuted))
		return nil
	}

	if err := os.Remove(a.FilePath); err != nil {
		return fmt.Errorf("remove %s: %w", a.FilePath, err)
	}
	fmt.Printf("%s\n", auditTint("Removed agent "+a.Name+" ("+a.FilePath+")", textPrimary))
	return nil
}
