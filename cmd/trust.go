package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/GrayCodeAI/rho/internal/theme"

	"github.com/GrayCodeAI/rho/internal/flags"
	"github.com/GrayCodeAI/rho/internal/trust"
	"github.com/spf13/cobra"
)

var trustCmd = &cobra.Command{
	Use:   "trust",
	Short: "Manage folder trust for project automation",
	Long: `Folder trust controls whether project-scoped hooks, MCP servers, LSP
configs, and plugins may load from a repository.

When RHO_Y0_FOLDER_TRUST is enabled (default after Year 0 PACK-03),
untrusted projects cannot run project automation (RCE mitigation).

User-global plugins under the Rho state directory always load.`,
}

var trustAddCmd = &cobra.Command{
	Use:   "add [path]",
	Short: "Trust a project directory (default: cwd)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := ""
		if len(args) > 0 {
			path = args[0]
		} else {
			var err error
			path, err = os.Getwd()
			if err != nil {
				return err
			}
		}
		s, err := trust.Open("")
		if err != nil {
			return err
		}
		reason, _ := cmd.Flags().GetString("reason")
		if err := s.Trust(path, reason); err != nil {
			return err
		}
		cmd.Printf("%s\n", auditTint("Trusted ", doneGreen)+auditTint(path, textPrimary))
		return nil
	},
}

var trustRemoveCmd = &cobra.Command{
	Use:   "remove [path]",
	Short: "Remove trust for a project directory (default: cwd)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := ""
		if len(args) > 0 {
			path = args[0]
		} else {
			var err error
			path, err = os.Getwd()
			if err != nil {
				return err
			}
		}
		s, err := trust.Open("")
		if err != nil {
			return err
		}
		ok, _ := cmd.Flags().GetBool("yes")
		if !ok {
			var err error
			ok, err = confirmDestructive(fmt.Sprintf("Remove trust for %q?", path))
			if err != nil {
				return err
			}
		}
		if !ok {
			cmd.Printf("%s\n", auditTint("Cancelled.", textMuted))
			return nil
		}
		if err := s.Untrust(path); err != nil {
			return err
		}
		cmd.Printf("%s\n", auditTint("Removed trust for ", textPrimary)+auditTint(path, textMuted))
		return nil
	},
}

var trustListJSON bool

var trustListCmd = &cobra.Command{
	Use:   "list",
	Short: "List trusted directories",
	RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := trust.Open("")
		if err != nil {
			return err
		}
		entries := s.List()
		if len(entries) == 0 {
			if trustListJSON {
				fmt.Println("[]")
			} else {
				cmd.Println(auditTint("No trusted directories.", textMuted))
				cmd.Printf("%s\n", auditTint(fmt.Sprintf("Folder trust enforcement: %v (RHO_Y0_FOLDER_TRUST)", flags.FolderTrust()), textMuted))
			}
			return nil
		}
		if trustListJSON {
			out, err := json.MarshalIndent(entries, "", "  ")
			if err != nil {
				return fmt.Errorf("marshaling trust entries: %w", err)
			}
			fmt.Println(string(out))
			return nil
		}
		rows := make([][]string, 0, len(entries))
		for _, e := range entries {
			rows = append(rows, []string{e.Path, e.TrustedAt.Format("2006-01-02 15:04"), e.Reason})
		}
		return theme.PrintTable(cmd.OutOrStdout(), []string{"PATH", "TRUSTED_AT", "REASON"}, rows)
	},
}

var trustCheckCmd = &cobra.Command{
	Use:   "check [path]",
	Short: "Check whether a path is trusted (default: cwd)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := ""
		if len(args) > 0 {
			path = args[0]
		} else {
			var err error
			path, err = os.Getwd()
			if err != nil {
				return err
			}
		}
		s, err := trust.Open("")
		if err != nil {
			return err
		}
		enforced := flags.FolderTrust()
		trusted := s.IsTrusted(path)
		cmd.Printf("%s %s\n", auditTint("path:", textMuted), auditTint(path, textPrimary))
		trustedColor := doneGreen
		if !trusted {
			trustedColor = errorCoral
		}
		cmd.Printf("%s %s\n", auditTint("trusted:", textMuted), auditTint(fmt.Sprintf("%v", trusted), trustedColor))
		cmd.Printf("%s %s\n", auditTint("enforcement:", textMuted), auditTint(fmt.Sprintf("%v", enforced), textPrimary))
		if enforced && !trusted {
			return fmt.Errorf("not trusted")
		}
		return nil
	},
}

func init() {
	trustAddCmd.Flags().String("reason", "", "Optional reason recorded in the trust store")
	trustListCmd.Flags().BoolVar(&trustListJSON, "json", false, "output trusted directories as JSON")
	trustRemoveCmd.Flags().Bool("yes", false, "confirm removing trust")
	trustCmd.AddCommand(trustAddCmd)
	trustCmd.AddCommand(trustRemoveCmd)
	trustCmd.AddCommand(trustListCmd)
	trustCmd.AddCommand(trustCheckCmd)
	rootCmd.AddCommand(trustCmd)
}
