package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	"github.com/GrayCodeAI/rho/internal/engine/safety"
	"github.com/GrayCodeAI/rho/internal/permissions"
	"github.com/GrayCodeAI/rho/internal/permissions/stableid"
	"github.com/spf13/cobra"
)

var (
	permissionsJSON        bool
	permissionsResetYes    bool
	permissionsInspectTool string
)

type permissionRuleOutput struct {
	ID         uint64 `json:"id"`
	Kind       string `json:"kind"`
	Identity   string `json:"identity"`
	Decision   string `json:"decision"`
	Generation uint64 `json:"generation"`
}

type permissionMutationOutput struct {
	Action    string `json:"action"`
	ID        uint64 `json:"id,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Identity  string `json:"identity,omitempty"`
	Decision  string `json:"decision,omitempty"`
	Removed   int    `json:"removed,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

type permissionInspectionOutput struct {
	Action       string   `json:"action"`
	Layer        string   `json:"layer"`
	Kind         string   `json:"kind"`
	Identity     string   `json:"identity"`
	Tool         string   `json:"tool"`
	Decision     string   `json:"decision"`
	Reason       string   `json:"reason"`
	Message      string   `json:"message,omitempty"`
	Risk         string   `json:"risk"`
	Capabilities []string `json:"capabilities,omitempty"`
}

func writePermissionJSON(cmd *cobra.Command, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(append(data, '\n'))
	return err
}

var permissionsCmd = &cobra.Command{
	Use:   "permissions",
	Short: "List and manage exact permission rules",
}

var permissionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List persisted exact permission rules",
	RunE: func(cmd *cobra.Command, _ []string) error {
		store := currentStableRuleStore()
		if err := store.Load(); err != nil {
			return err
		}
		rules := store.List()
		out := make([]permissionRuleOutput, 0, len(rules))
		for _, rule := range rules {
			out = append(out, permissionRuleOutput{
				ID: rule.ID, Kind: rule.Key.Kind.String(), Identity: rule.DisplayIdentity,
				Decision: rule.Decision.String(), Generation: rule.Generation,
			})
		}
		if permissionsJSON {
			return writePermissionJSON(cmd, out)
		}
		if len(out) == 0 {
			cmd.Println(auditTint("No persisted permission rules.", textMuted))
			return nil
		}
		for _, rule := range out {
			cmd.Printf("%d\t%s\t%s\t%s\t%d\n", rule.ID, rule.Decision, rule.Kind, rule.Identity, rule.Generation)
		}
		return nil
	},
}

var permissionsAddCmd = &cobra.Command{
	Use:   "add <allow|deny> <command|file|tool> <identity>",
	Short: "Persist an exact permission rule",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		decision, err := parsePermissionDecision(args[0])
		if err != nil {
			return err
		}
		kind, err := parsePermissionKind(args[1])
		if err != nil {
			return err
		}
		identity := strings.TrimSpace(args[2])
		if identity == "" {
			return fmt.Errorf("permission identity cannot be empty")
		}
		store := currentStableRuleStore()
		if err := store.Load(); err != nil {
			return err
		}
		id, ok := store.Remember(kind, stableid.CanonicalIdentity(kind, identity), identity, decision)
		if !ok {
			return fmt.Errorf("could not add permission rule")
		}
		if permissionsJSON {
			return writePermissionJSON(cmd, permissionMutationOutput{
				Action: "added", ID: id, Kind: kind.String(), Identity: identity, Decision: decision.String(),
			})
		}
		cmd.Printf("%s\n", auditTint(fmt.Sprintf("Permission rule %d saved.", id), doneGreen))
		return nil
	},
}

var permissionsRevokeCmd = &cobra.Command{
	Use:   "revoke <rule-id>",
	Short: "Revoke a persisted exact permission rule",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil || id == 0 {
			return fmt.Errorf("invalid permission rule ID %q", args[0])
		}
		store := currentStableRuleStore()
		if err := store.Load(); err != nil {
			return err
		}
		if !store.Revoke(id) {
			return fmt.Errorf("permission rule %d not found", id)
		}
		if permissionsJSON {
			return writePermissionJSON(cmd, permissionMutationOutput{Action: "revoked", ID: id})
		}
		cmd.Printf("%s\n", auditTint(fmt.Sprintf("Permission rule %d revoked.", id), textPrimary))
		return nil
	},
}

var permissionsResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Remove all persisted exact permission rules",
	RunE: func(cmd *cobra.Command, _ []string) error {
		store := currentStableRuleStore()
		if err := store.Load(); err != nil {
			return err
		}
		if len(store.List()) == 0 {
			if permissionsJSON {
				return writePermissionJSON(cmd, permissionMutationOutput{Action: "reset"})
			}
			cmd.Println(auditTint("No persisted permission rules.", textMuted))
			return nil
		}
		ok := permissionsResetYes
		if !ok {
			var err error
			ok, err = confirmDestructive("Remove all persisted permission rules?")
			if err != nil {
				return err
			}
		}
		if !ok {
			if permissionsJSON {
				return writePermissionJSON(cmd, permissionMutationOutput{Action: "reset", Cancelled: true})
			}
			cmd.Println(auditTint("Cancelled.", textMuted))
			return nil
		}
		removed := len(store.List())
		if !store.Reset() {
			return fmt.Errorf("permission rules changed before reset; refusing to continue")
		}
		if permissionsJSON {
			return writePermissionJSON(cmd, permissionMutationOutput{Action: "reset", Removed: removed})
		}
		cmd.Println(auditTint("Persisted permission rules reset.", doneGreen))
		return nil
	},
}

var permissionsInspectCmd = &cobra.Command{
	Use:   "inspect <command|file|tool> <identity>",
	Short: "Preview the permission-engine decision without executing anything",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		kind, err := parsePermissionKind(args[0])
		if err != nil {
			return err
		}
		identity := strings.TrimSpace(args[1])
		if identity == "" {
			return fmt.Errorf("permission identity cannot be empty")
		}
		result, err := inspectPermission(kind, identity, permissionsInspectTool)
		if err != nil {
			return err
		}
		if permissionsJSON {
			return writePermissionJSON(cmd, result)
		}
		cmd.Printf("%s %s (%s)\n", result.Decision, result.Identity, result.Kind)
		cmd.Printf("reason: %s\n", result.Reason)
		if result.Message != "" {
			cmd.Printf("message: %s\n", result.Message)
		}
		cmd.Printf("risk: %s\n", result.Risk)
		return nil
	},
}

// inspectPermission is deliberately built on safety.PermissionEngine's
// observational evaluator. It is the same resolver used before execution and
// never invokes a prompt, mutates remembered rules, or runs a tool.
func inspectPermission(kind stableid.Kind, identity, toolOverride string) (permissionInspectionOutput, error) {
	settings, err := rhoconfig.LoadSettingsWithOverride(settingsFlag)
	if err != nil {
		return permissionInspectionOutput{}, err
	}
	store := currentStableRuleStore()
	if err := store.Load(); err != nil {
		return permissionInspectionOutput{}, err
	}

	pe := safety.NewPermissionEngine()
	pe.ExactRules = store
	applyConfiguredPermissionRules(pe.Memory, settings, true)
	pe.SetNeverAllow(settings.NeverAllow)
	level := safety.AutonomySupervised
	if !settings.AutonomyExplicit {
		level = autonomyFromSettings(settings.Autonomy)
	}
	pe.Autonomy = level
	if dangerouslySkipPermissions {
		pe.Autonomy = safety.AutonomyYOLO
	}
	pe.Profile = safety.ProfileFromLevel(pe.Autonomy)
	pe.Profile.ApplyOverrides(settings.AutonomyOverrides)
	pe.DryRun = dryRunFlag

	toolName, args, err := inspectionToolCall(kind, identity, toolOverride)
	if err != nil {
		return permissionInspectionOutput{}, err
	}
	decision := pe.EvaluateTool(context.Background(), safety.ToolCallInfo{Name: toolName, Args: args})
	capabilities := make([]string, 0, len(decision.Capabilities))
	for _, capability := range decision.Capabilities {
		capabilities = append(capabilities, string(capability))
	}
	return permissionInspectionOutput{
		Action: "inspect", Layer: "permission_engine", Kind: kind.String(), Identity: identity, Tool: toolName,
		Decision: string(decision.Outcome), Reason: string(decision.Reason),
		Message: decision.Message, Risk: string(decision.Risk), Capabilities: capabilities,
	}, nil
}

// applyConfiguredPermissionRules is the one settings-to-memory translation
// used by session startup, the TUI permission center, and CLI inspection.
// Keeping this at one boundary prevents a preview or UI rebuild from drifting
// away from the rules used by the executor.
func applyConfiguredPermissionRules(mem *safety.PermissionMemory, settings rhoconfig.Settings, includeCLI bool) {
	if mem == nil {
		return
	}
	for _, spec := range settings.AutoAllow {
		mem.AllowSpec(spec)
	}
	for _, spec := range settings.AllowedTools {
		mem.AllowSpec(spec)
	}
	for _, spec := range settings.DisallowedTools {
		mem.DenySpec(spec)
	}
	if includeCLI {
		for _, spec := range parseToolListFromCLI(allowedToolsFlag) {
			mem.AllowSpec(spec)
		}
		for _, spec := range parseToolListFromCLI(disallowedToolsFlag) {
			mem.DenySpec(spec)
		}
	}
}

func inspectionToolCall(kind stableid.Kind, identity, toolOverride string) (string, map[string]interface{}, error) {
	switch kind {
	case stableid.KindCommand:
		toolName := "Bash"
		if strings.TrimSpace(toolOverride) != "" {
			toolName = permissions.CanonicalToolName(toolOverride)
			if toolName != "Bash" && toolName != "PowerShell" {
				return "", nil, fmt.Errorf("command inspection tool must be bash or powershell")
			}
		}
		return toolName, map[string]interface{}{"command": identity}, nil
	case stableid.KindFileMutation:
		if strings.TrimSpace(toolOverride) != "" {
			return "", nil, fmt.Errorf("--tool is only valid for command inspection")
		}
		return "Write", map[string]interface{}{"path": identity}, nil
	default:
		if strings.TrimSpace(toolOverride) != "" {
			return "", nil, fmt.Errorf("--tool is only valid for command inspection")
		}
		return identity, nil, nil
	}
}

func currentStableRuleStore() *permissions.StableRuleStore {
	projectDir, err := os.Getwd()
	if err != nil {
		projectDir = "."
	}
	return permissions.NewStableRuleStore(permissions.DefaultStableRulesPath(projectDir))
}

func parsePermissionDecision(raw string) (stableid.Decision, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "allow":
		return stableid.Allow, nil
	case "deny":
		return stableid.Deny, nil
	default:
		return stableid.Deny, fmt.Errorf("permission decision must be allow or deny")
	}
}

func parsePermissionKind(raw string) (stableid.Kind, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "command", "bash":
		return stableid.KindCommand, nil
	case "file", "file_mutation", "edit", "write":
		return stableid.KindFileMutation, nil
	case "tool", "structured_tool":
		return stableid.KindStructuredTool, nil
	default:
		return stableid.KindCommand, fmt.Errorf("permission kind must be command, file, or tool")
	}
}

func init() {
	// These policy flags must be valid on the permissions subcommands as well
	// as on the root agent command. Registering them only on root makes Cobra
	// display them in help while rejecting the actual invocation.
	permissionsCmd.PersistentFlags().StringVar(&settingsFlag, "settings", "", "JSON object or path to a settings override")
	permissionsCmd.PersistentFlags().StringArrayVar(&allowedToolsFlag, "allowed-tools", nil, "permission rules to allow")
	permissionsCmd.PersistentFlags().StringArrayVar(&disallowedToolsFlag, "disallowed-tools", nil, "permission rules to deny")
	permissionsCmd.PersistentFlags().BoolVar(&dangerouslySkipPermissions, "dangerously-skip-permissions", false, "skip normal permission prompts")
	permissionsCmd.PersistentFlags().BoolVar(&dryRunFlag, "dry-run", false, "deny every tool call (preview only)")
	permissionsListCmd.Flags().BoolVar(&permissionsJSON, "json", false, "output rules as JSON")
	permissionsAddCmd.Flags().BoolVar(&permissionsJSON, "json", false, "output result as JSON")
	permissionsRevokeCmd.Flags().BoolVar(&permissionsJSON, "json", false, "output result as JSON")
	permissionsResetCmd.Flags().BoolVar(&permissionsResetYes, "yes", false, "confirm reset without prompting")
	permissionsResetCmd.Flags().BoolVar(&permissionsJSON, "json", false, "output result as JSON")
	permissionsInspectCmd.Flags().BoolVar(&permissionsJSON, "json", false, "output result as JSON")
	permissionsInspectCmd.Flags().StringVar(&permissionsInspectTool, "tool", "", "command runtime to inspect: bash or powershell")
	permissionsCmd.AddCommand(permissionsListCmd, permissionsAddCmd, permissionsRevokeCmd, permissionsResetCmd, permissionsInspectCmd)
	rootCmd.AddCommand(permissionsCmd)
}
