package cmd

import (
	"fmt"
	"image/color"
	"sort"
	"strings"

	feature "github.com/GrayCodeAI/rho/internal/features"
	"github.com/spf13/cobra"
)

var featuresCmd = &cobra.Command{
	Use:   "features",
	Short: "List and manage feature flags",
	Long: `features lists all registered feature flags, their current values,
and how to override them via environment variables.

Feature flags allow runtime configuration of experimental or gated
capabilities without code changes or restarts (some changes may require
a daemon restart).

Override a flag via environment variable:
    RHO_FEATURE_<FLAG_NAME>=1 rho daemon start

Show a specific flag:
    rho features get <flag-name>`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 && args[0] == "get" {
			if len(args) < 2 {
				return fmt.Errorf("usage: rho features get <flag-name>")
			}
			f, ok := feature.Info(args[1])
			if !ok {
				return fmt.Errorf("unknown feature flag: %s", args[1])
			}
			fmt.Printf("%s\n", auditTint("Name:        ", textMuted)+auditTint(f.Name(), textPrimary))
			fmt.Printf("%s\n", auditTint("Default:     ", textMuted)+auditTint(fmt.Sprintf("%v", f.DefaultValue()), textPrimary))
			fmt.Printf("%s\n", auditTint("Current:     ", textMuted)+auditTint(fmt.Sprintf("%v", feature.EnabledByName(args[1])), textPrimary))
			fmt.Printf("%s\n", auditTint("Description: ", textMuted)+auditTint(f.Description(), textPrimary))
			envVar := "RHO_FEATURE_" + strings.ReplaceAll(strings.ToUpper(args[1]), "-", "_")
			fmt.Printf("%s\n", auditTint("Env var:     ", textMuted)+auditTint(envVar, textPrimary))
			return nil
		}

		flags := feature.List()
		names := make([]string, 0, len(flags))
		for name := range flags {
			names = append(names, name)
		}
		sort.Strings(names)

		fmt.Println(auditTint("Feature Flags:", rhoColor))
		fmt.Println()
		for _, name := range names {
			f, _ := feature.Info(name)
			val := flags[name]
			status := "DISABLED"
			var statusColor color.Color = textMuted
			if val {
				status = "ENABLED"
				statusColor = doneGreen
			}
			fmt.Printf("  %s = %v  [%s]\n", auditTint(name, textPrimary), val, auditTint(status, statusColor))
			if f != nil {
				fmt.Printf("%s\n", auditTint(fmt.Sprintf("    default: %v", f.DefaultValue()), textMuted))
				fmt.Printf("%s\n", auditTint("    description: "+f.Description(), textMuted))
				envVar := "RHO_FEATURE_" + strings.ReplaceAll(strings.ToUpper(name), "-", "_")
				fmt.Printf("%s\n", auditTint("    env: "+envVar, textMuted))
			}
			fmt.Println()
		}
		return nil
	},
}
