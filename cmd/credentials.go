package cmd

import (
	"context"
	"fmt"
	"strings"

	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	"github.com/spf13/cobra"
)

var credentialsCmd = &cobra.Command{
	Use:   "credentials",
	Short: "Manage secure API key storage (macOS Keychain / Linux secret service)",
}

var credentialsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show where API keys are stored",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		cmd.Println(rhoconfig.FormatCredentialCLIStatus(ctx))
		return nil
	},
}

var credentialsRemoveCmd = &cobra.Command{
	Use:   "remove <provider|env-var>",
	Short: "Remove a stored API key from the OS secret store",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		ok, _ := cmd.Flags().GetBool("yes")
		if !ok {
			var err error
			ok, err = confirmDestructive(fmt.Sprintf("Remove stored API key(s) for %q from %s?", args[0], rhoconfig.CredentialStoreName()))
			if err != nil {
				return err
			}
		}
		if !ok {
			cmd.Printf("%s\n", auditTint("Cancelled.", textMuted))
			return nil
		}
		removed, err := rhoconfig.RemoveStoredCredential(ctx, args[0])
		if err != nil {
			return err
		}
		cmd.Printf("%s\n", auditTint(fmt.Sprintf("Removed %d key(s) from %s: %s", len(removed), rhoconfig.CredentialStoreName(), strings.Join(removed, ", ")), doneGreen))
		return nil
	},
}

func init() {
	credentialsCmd.AddCommand(credentialsStatusCmd)
	credentialsRemoveCmd.Flags().Bool("yes", false, "confirm removing stored credentials")
	credentialsCmd.AddCommand(credentialsRemoveCmd)
}
