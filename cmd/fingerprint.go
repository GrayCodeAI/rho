package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/GrayCodeAI/rho/internal/features/fingerprint"
	"github.com/spf13/cobra"
)

var fingerprintFormat string

var fingerprintCmd = &cobra.Command{
	Use:   "fingerprint [dir]",
	Short: "Generate a repository fingerprint (languages, deps, git info)",
	Long: `fingerprint scans a directory and produces a structured summary
including detected languages, dependency counts, CI presence, license,
and git metadata.

Examples:
  rho fingerprint
  rho fingerprint ./myproject
  rho fingerprint --format json .
  rho fingerprint --format markdown /path/to/repo`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := "."
		if len(args) > 0 {
			dir = args[0]
		}

		fp, err := fingerprint.Generate(dir)
		if err != nil {
			return fmt.Errorf("fingerprint failed: %w", err)
		}

		switch fingerprintFormat {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(fp); err != nil {
				return err
			}
		case "markdown":
			fmt.Print(fp.FormatMarkdown())
		default:
			fmt.Print(fp.Format())
		}

		return nil
	},
}

func init() {
	fingerprintCmd.Flags().StringVar(&fingerprintFormat, "format", "text", "output format: text, markdown, json")
}
