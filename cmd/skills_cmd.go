package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/GrayCodeAI/rho/internal/plugin"
	"github.com/GrayCodeAI/rho/internal/tool"
	"github.com/spf13/cobra"
)

var skillsCmd = &cobra.Command{
	Use:   "skills [command]",
	Short: "Manage skills (list, search, install, remove, audit, info, trending)",
	Long:  "Non-interactive skill management for CI/CD pipelines and scripts.",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Default: list local skills.
		out, err := (tool.SkillTool{}).Execute(context.Background(), nil)
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var skillsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed skills",
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := (tool.SkillTool{}).Execute(context.Background(), nil)
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var skillsSearchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search the community skill registry",
	RunE: func(cmd *cobra.Command, args []string) error {
		query := strings.Join(args, " ")
		category, _ := cmd.Flags().GetString("category")
		jsonOut, _ := cmd.Flags().GetBool("json")

		rc := plugin.NewRegistryClient()
		prog := NewCLIProgress("Skills search", []string{"Searching skill registry"})
		if !jsonOut {
			prog.StartStep(0)
		}
		results, err := rc.Search(query, category)
		if err != nil {
			if !jsonOut {
				prog.Abort()
			}
			return err
		}
		if !jsonOut {
			prog.CompleteStep(0)
			prog.Done()
		}
		if jsonOut {
			data, _ := json.MarshalIndent(results, "", "  ")
			fmt.Println(string(data))
			return nil
		}
		if len(results) == 0 {
			fmt.Println(auditTint("No skills found.", textMuted))
			return nil
		}
		for _, e := range results {
			fmt.Print(plugin.FormatSkillEntry(e))
		}
		return nil
	},
}

var skillsInstallCmd = &cobra.Command{
	Use:   "install <owner/repo> [skill-name]",
	Short: "Install skills from a GitHub repository",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repo := args[0]
		skillName := ""
		if len(args) > 1 {
			skillName = args[1]
		}
		scope, _ := cmd.Flags().GetString("scope")
		rc := plugin.NewRegistryClient()
		prog := NewCLIProgress("Skill install", []string{"Installing skill"})
		prog.StartStep(0)
		msg, err := rc.Install(repo, skillName, scope)
		if err != nil {
			prog.Abort()
			return err
		}
		prog.CompleteStep(0)
		prog.Done()
		fmt.Println(msg)
		return nil
	},
}

var skillsRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove an installed skill",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ok, _ := cmd.Flags().GetBool("yes")
		if !ok {
			var err error
			ok, err = confirmDestructive(fmt.Sprintf("Remove skill %q?", args[0]))
			if err != nil {
				return err
			}
		}
		if !ok {
			fmt.Printf("%s\n", auditTint("Cancelled.", textMuted))
			return nil
		}
		if err := plugin.Remove(args[0]); err != nil {
			return err
		}
		fmt.Printf("%s\n", auditTint("Removed skill "+args[0]+".", textPrimary))
		return nil
	},
}

var skillsInfoCmd = &cobra.Command{
	Use:   "info <name>",
	Short: "Show detailed skill information",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if skill, path, ok := plugin.InstalledSkillInfo(args[0]); ok {
			fmt.Print(plugin.FormatSkillInfo(skill, path))
			return nil
		}
		rc := plugin.NewRegistryClient()
		entry, err := rc.Info(args[0])
		if err != nil {
			return err
		}
		fmt.Printf("%s %s\n", auditTint("Skill:", textMuted), auditTint(entry.Name, textPrimary)+auditTint(" (not installed)", textMuted))
		if entry.Description != "" {
			fmt.Printf("%s %s\n", auditTint("Description:", textMuted), auditTint(entry.Description, textPrimary))
		}
		fmt.Printf("%s %s\n%s %d\n", auditTint("Repo:", textMuted), auditTint(entry.Repo, textPrimary), auditTint("Installs:", textMuted), entry.Installs)
		return nil
	},
}

var skillsTrendingCmd = &cobra.Command{
	Use:   "trending [limit]",
	Short: "Show most popular skills",
	RunE: func(cmd *cobra.Command, args []string) error {
		limit := 10
		if len(args) > 0 {
			if n, err := strconv.Atoi(args[0]); err == nil && n > 0 {
				limit = n
			}
		}
		rc := plugin.NewRegistryClient()
		prog := NewCLIProgress("Trending skills", []string{"Fetching trending skills"})
		prog.StartStep(0)
		results, err := rc.Trending(limit)
		if err != nil {
			prog.Abort()
			return err
		}
		prog.CompleteStep(0)
		prog.Done()
		for i, e := range results {
			fmt.Printf("%s. %s", auditTint(fmt.Sprintf("%d", i+1), textMuted), strings.TrimLeft(plugin.FormatSkillEntry(e), " "))
		}
		return nil
	},
}

var skillsAuditCmd = &cobra.Command{
	Use:   "audit [name-or-file]",
	Short: "Security scan skills for hidden Unicode threats",
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")
		if len(args) > 0 {
			target := args[0]
			if info, err := os.Stat(target); err == nil && !info.IsDir() {
				findings, err := plugin.AuditSkillFile(target)
				if err != nil {
					return err
				}
				r := plugin.AuditResult{Findings: findings, Validation: plugin.ValidateSkillFile(target), Files: 1}
				if jsonOut {
					data, _ := json.MarshalIndent(r, "", "  ")
					fmt.Println(string(data))
					return nil
				}
				fmt.Println(plugin.FormatAuditResultColored(r))
				return nil
			}
			if _, path, ok := plugin.InstalledSkillInfo(target); ok {
				findings, _ := plugin.AuditSkillFile(path)
				r := plugin.AuditResult{Findings: findings, Validation: plugin.ValidateSkillFile(path), Files: 1}
				if jsonOut {
					data, _ := json.MarshalIndent(r, "", "  ")
					fmt.Println(string(data))
					return nil
				}
				fmt.Println(plugin.FormatAuditResultColored(r))
				return nil
			}
			return fmt.Errorf("skill or file %q not found", target)
		}
		result := plugin.AuditAllSkills()
		if jsonOut {
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
			return nil
		}
		fmt.Println(plugin.FormatAuditResultColored(result))
		return nil
	},
}

func init() {
	skillsSearchCmd.Flags().String("category", "", "filter by category")
	skillsSearchCmd.Flags().Bool("json", false, "output as JSON")
	skillsInstallCmd.Flags().String("scope", "user", "installation scope: user or project")
	skillsAuditCmd.Flags().Bool("json", false, "output as JSON")
	skillsRemoveCmd.Flags().Bool("yes", false, "confirm removing the skill")

	skillsCmd.AddCommand(skillsListCmd)
	skillsCmd.AddCommand(skillsSearchCmd)
	skillsCmd.AddCommand(skillsInstallCmd)
	skillsCmd.AddCommand(skillsRemoveCmd)
	skillsCmd.AddCommand(skillsInfoCmd)
	skillsCmd.AddCommand(skillsTrendingCmd)
	skillsCmd.AddCommand(skillsAuditCmd)

	rootCmd.AddCommand(skillsCmd)
}
