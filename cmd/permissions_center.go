package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine/safety"
	"github.com/GrayCodeAI/rho/internal/permissions/stableid"

	tea "charm.land/bubbletea/v2"
	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	"github.com/GrayCodeAI/rho/internal/engine"
)

func normalizePermissionTier(raw string) (safety.AutonomyLevel, string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "always_ask", "always-ask", "supervised", "ask":
		return safety.AutonomySupervised, "Always Ask", true
	case "scout", "basic", "read":
		return safety.AutonomyBasic, "Scout", true
	case "builder", "semi", "edit":
		return safety.AutonomySemi, "Builder", true
	case "operator", "full", "run":
		return safety.AutonomyFull, "Operator", true
	case "autonomous", "yolo", "auto":
		return safety.AutonomyYOLO, "Autonomous", true
	default:
		return 0, "", false
	}
}

func permissionTierSettingValue(level safety.AutonomyLevel) int {
	switch level {
	case safety.AutonomyBasic:
		return 1
	case safety.AutonomySemi:
		return 2
	case safety.AutonomyFull:
		return 3
	case safety.AutonomyYOLO:
		return 4
	default:
		return 0
	}
}

func effectivePermissionTier(sess *engine.Session) safety.AutonomyLevel {
	if sess == nil {
		return DefaultAutonomy
	}
	perms := sess.PermSvc()
	if perms == nil {
		return DefaultAutonomy
	}
	state := perms.RuntimeState()
	if state.Autonomy == 0 && !state.AutonomyExplicit {
		return DefaultAutonomy
	}
	return state.Autonomy
}

// specStageLabel returns the display label for the current spec workflow stage.
func specStageLabel(sess *engine.Session) string {
	return specStageDisplayName(currentSpecStage(sess))
}

// currentSpecStage returns the session's active spec stage, or
// SpecStageNone if the session (or its permission engine) isn't set up yet.
func currentSpecStage(sess *engine.Session) safety.SpecStage {
	if sess == nil || sess.PermSvc() == nil {
		return safety.SpecStageNone
	}
	return sess.PermSvc().SpecStage()
}

// currentDryRun returns whether the session's dry-run kill switch is
// active, or false if the session (or its permission engine) isn't set up
// yet — mirrors currentSpecStage's nil-safety, since PermSvc() can return
// nil for sessions built via a raw struct literal (e.g. in tests) rather
// than NewSession.
func currentDryRun(sess *engine.Session) bool {
	if sess == nil || sess.PermSvc() == nil {
		return false
	}
	return sess.PermSvc().RuntimeState().DryRun
}

// parseBypassFlags extracts and validates --scope, --for, and --reason from
// /autonomy bypass args. Invalid input must fail closed: silently turning a
// malformed expiry into a session-long bypass is unacceptable.
func parseBypassFlags(args []string) (scope []string, expires time.Time, reason string, err error) {
	seen := make(map[string]bool, 3)
	validScopes := map[string]bool{"bash": true, "network": true, "filesystem": true, "other": true}
	seenScopes := make(map[string]bool, len(validScopes))
	for _, a := range args {
		a = strings.TrimSpace(a)
		switch {
		case strings.HasPrefix(a, "--scope="):
			if seen["scope"] {
				return nil, time.Time{}, "", fmt.Errorf("--scope may be specified only once")
			}
			seen["scope"] = true
			v := strings.TrimPrefix(a, "--scope=")
			for _, s := range strings.Split(v, ",") {
				s = strings.TrimSpace(s)
				if s != "" {
					s = strings.ToLower(s)
					if !validScopes[s] {
						return nil, time.Time{}, "", fmt.Errorf("invalid bypass scope %q; valid scopes: bash, network, filesystem, other", s)
					}
					if seenScopes[s] {
						return nil, time.Time{}, "", fmt.Errorf("duplicate bypass scope %q", s)
					}
					seenScopes[s] = true
					scope = append(scope, s)
				}
			}
			if len(scope) == 0 {
				return nil, time.Time{}, "", fmt.Errorf("--scope requires at least one scope")
			}
		case strings.HasPrefix(a, "--for="):
			if seen["for"] {
				return nil, time.Time{}, "", fmt.Errorf("--for may be specified only once")
			}
			seen["for"] = true
			v := strings.TrimPrefix(a, "--for=")
			d, parseErr := time.ParseDuration(v)
			if parseErr != nil || d <= 0 {
				return nil, time.Time{}, "", fmt.Errorf("invalid --for duration %q; use a positive duration such as 5m", v)
			}
			expires = time.Now().Add(d)
		case strings.HasPrefix(a, "--reason="):
			if seen["reason"] {
				return nil, time.Time{}, "", fmt.Errorf("--reason may be specified only once")
			}
			seen["reason"] = true
			reason = strings.Trim(strings.TrimPrefix(a, "--reason="), `"`)
			reason = strings.TrimSpace(reason)
			if reason == "" {
				return nil, time.Time{}, "", fmt.Errorf("--reason requires a non-empty justification")
			}
		default:
			return nil, time.Time{}, "", fmt.Errorf("unknown bypass option %q; use --scope, --for, or --reason", a)
		}
	}
	if reason == "" {
		return nil, time.Time{}, "", fmt.Errorf("--reason is required when enabling bypass")
	}
	return scope, expires, reason, nil
}

// markOverridden returns " *" if the flag was explicitly overridden by the
// user, so the profile display can mark customized flags.
func markOverridden(profile *safety.AutonomyProfile, flag string) string {
	if profile != nil && profile.IsOverridden(flag) {
		return " *"
	}
	return ""
}

func autonomyCommandHelp() string {
	return "Permission Center\n" +
		"  /permission             Show active permissions and folder trust\n" +
		"  /permission mode        Choose permission mode\n" +
		"  /permission trust       Accept or reject this folder\n" +
		"  /permission reset       Restore safe defaults\n" +
		"\n" +
		"Autonomy controls: /autonomy"
}

// handlePolicyCommand is the focused permission entry point. The
// existing autonomy and trust handlers remain below it as compatibility
// implementations, but all policy changes still flow through their shared
// PermissionService and project-trust authority.
func (m *chatModel) handlePolicyCommand(args []string) (chatModel, tea.Cmd) {
	if len(args) == 0 {
		if m == nil {
			return chatModel{}, nil
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: autonomyCenterSummary(m) + "\n\n" + engine.ProjectTrust("").Detail()})
		return *m, nil
	}

	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "trust":
		trust := &trustSubcommand{}
		model, cmd := trust.Handle(m, args[1:], "")
		if next, ok := model.(*chatModel); ok && next != nil {
			return *next, cmd
		}
		return *m, cmd
	case "mode", "autonomy":
		if len(args) == 1 {
			return m.handleAutonomyCommand([]string{"/autonomy"})
		}
		return m.handleAutonomyCommand([]string{"/autonomy", "tier", args[1]})
	default:
		return m.handleAutonomyCommand(append([]string{"/autonomy"}, args...))
	}
}

func autonomyCenterSummary(m *chatModel) string {
	if m == nil || m.session == nil {
		return "Autonomy Center unavailable."
	}
	perms := m.session.PermSvc()
	if perms == nil {
		return "Autonomy Center unavailable: permission service is not initialized."
	}
	level := effectivePermissionTier(m.session)
	tier := autonomyTierName(level)
	allowCount, denyCount := activePermissionRuleCounts(m)
	exactAllow, exactDeny := exactPermissionRuleCounts(m)
	var b strings.Builder
	b.WriteString("Autonomy Center\n")
	b.WriteString(fmt.Sprintf("  Tier: %s\n", tier))
	b.WriteString(fmt.Sprintf("  Spec stage: %s\n", specStageLabel(m.session)))
	if currentDryRun(m.session) {
		b.WriteString("  Dry-run: ON — every tool call is being denied unconditionally\n")
	}
	if enabled, grant := perms.BypassState(); enabled {
		b.WriteString("  Bypass: ON — break-glass permission checks are relaxed")
		if grant != nil {
			if len(grant.Scope) == 0 {
				b.WriteString(" (scope: all")
			} else {
				b.WriteString(" (scope: " + strings.Join(grant.Scope, ","))
			}
			if grant.ExpiresAt.IsZero() {
				b.WriteString(", expires: session")
			} else {
				b.WriteString(", expires: " + grant.ExpiresAt.Format(time.RFC3339))
			}
			b.WriteString(")")
		}
		b.WriteByte('\n')
	} else {
		b.WriteString("  Bypass: OFF\n")
	}
	b.WriteString(fmt.Sprintf("  Rules: %d allow, %d deny", allowCount, denyCount))
	if exactAllow+exactDeny > 0 {
		b.WriteString(fmt.Sprintf("; %d persisted exact", exactAllow+exactDeny))
	}
	b.WriteByte('\n')
	b.WriteString(fmt.Sprintf("  Behavior: %s\n", autonomyTierDescription(level)))
	b.WriteString("\n")
	b.WriteString(autonomyCommandHelp())
	return strings.TrimRight(b.String(), "\n")
}

// permissionViewFor is the single nil-safe lookup used by permission-center
// presentation code. The UI receives a snapshot instead of the live engine.
func permissionViewFor(m *chatModel) engine.PermissionView {
	if m == nil || m.session == nil || m.session.PermSvc() == nil {
		return engine.PermissionView{}
	}
	return m.session.PermSvc().PolicyView()
}

func activePermissionRuleCounts(m *chatModel) (allow, deny int) {
	view := permissionViewFor(m)
	if view.Grants != nil {
		for _, grant := range view.Grants {
			if grant.Allow {
				allow++
			} else {
				deny++
			}
		}
		if allow != 0 || deny != 0 {
			return allow, deny
		}
	}
	if m != nil {
		return len(effectiveAllowRules(m.settings)), len(effectiveDenyRules(m.settings))
	}
	return 0, 0
}

func permissionRulesSummary(m *chatModel) string {
	if m == nil {
		return "No active permission state."
	}
	var b strings.Builder
	b.WriteString("Permission Rules\n")

	// Show unified grants from the engine's session store.
	// when available, with source labels. Persisted exact rules are appended
	// separately because they intentionally do not participate in glob matching.
	view := permissionViewFor(m)
	if view.Grants != nil {
		grants := view.Grants
		var allows, denies []string
		for _, g := range grants {
			label := g.Tool + "(" + g.Pattern + ") [" + g.Source.String() + "]"
			if g.Exact {
				label += " [exact]"
			}
			if g.Label != "" {
				label += " (" + g.Label + ")"
			}
			if g.Allow {
				allows = append(allows, label)
			} else {
				denies = append(denies, label)
			}
		}
		appendPermissionRuleSections(&b, allows, denies)
		appendExactPermissionRules(&b, view.ExactRules)
		return strings.TrimRight(b.String(), "\n")
	}

	// Fallback: settings-based rules.
	allowRules := effectiveAllowRules(m.settings)
	denyRules := effectiveDenyRules(m.settings)
	appendPermissionRuleSections(&b, allowRules, denyRules)
	appendExactPermissionRules(&b, view.ExactRules)
	return strings.TrimRight(b.String(), "\n")
}

func appendPermissionRuleSections(b *strings.Builder, allows, denies []string) {
	if b == nil {
		return
	}
	if len(allows) == 0 {
		b.WriteString("  Allow: none\n")
	} else {
		b.WriteString("  Allow:\n")
		for _, rule := range allows {
			b.WriteString("    - " + rule + "\n")
		}
	}
	if len(denies) == 0 {
		b.WriteString("  Deny: none\n")
	} else {
		b.WriteString("  Deny:\n")
		for _, rule := range denies {
			b.WriteString("    - " + rule + "\n")
		}
	}
}

func exactPermissionRuleCounts(m *chatModel) (allow, deny int) {
	for _, rule := range permissionViewFor(m).ExactRules {
		if rule.Decision == stableid.Allow {
			allow++
		} else {
			deny++
		}
	}
	return allow, deny
}

func appendExactPermissionRules(b *strings.Builder, rules []stableid.RuleSnap) {
	if b == nil || len(rules) == 0 {
		return
	}
	b.WriteString("  Persisted exact rules:\n")
	for _, rule := range rules {
		fmt.Fprintf(b, "    - #%d %s %s: %s\n", rule.ID, rule.Decision.String(), rule.Key.Kind.String(), rule.DisplayIdentity)
	}
}

func effectiveAllowRules(settings rhoconfig.Settings) []string {
	var rules []string
	rules = append(rules, settings.AutoAllow...)
	rules = append(rules, settings.AllowedTools...)
	rules = append(rules, parseToolListFromCLI(allowedToolsFlag)...)
	return dedupeStrings(rules)
}

func effectiveDenyRules(settings rhoconfig.Settings) []string {
	rules := append([]string{}, settings.DisallowedTools...)
	rules = append(rules, parseToolListFromCLI(disallowedToolsFlag)...)
	return dedupeStrings(rules)
}

func dedupeStrings(values []string) []string {
	var out []string
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func rebuildSessionPermissionRules(sess *engine.Session, settings rhoconfig.Settings) {
	if sess == nil {
		return
	}
	perm := sess.PermSvc()
	if perm == nil {
		return
	}
	allowSpecs := append(append([]string{}, settings.AutoAllow...), settings.AllowedTools...)
	allowSpecs = append(allowSpecs, parseToolListFromCLI(allowedToolsFlag)...)
	denySpecs := append([]string{}, settings.DisallowedTools...)
	denySpecs = append(denySpecs, parseToolListFromCLI(disallowedToolsFlag)...)
	_ = perm.ReplaceSessionRules(allowSpecs, denySpecs)
}

// updateAutonomyRule applies the shared session-rule mutation for both
// /autonomy allow and /autonomy deny. Keeping parsing, deduplication, and the
// live-memory update together prevents the two commands from drifting apart.
func (m *chatModel) updateAutonomyRule(parts []string, allow bool) {
	verb := "allow"
	setting := &m.settings.AllowedTools
	usage := "Usage: /autonomy allow <rule>  e.g. /autonomy allow Bash(git:*)"
	if !allow {
		verb = "deny"
		setting = &m.settings.DisallowedTools
		usage = "Usage: /autonomy deny <rule>  e.g. /autonomy deny Bash(rm -rf *)"
	}
	if len(parts) < 3 {
		m.messages = append(m.messages, displayMsg{role: "error", content: usage})
		return
	}
	specs := parseToolListFromCLI([]string{strings.Join(parts[2:], " ")})
	if len(specs) == 0 {
		m.messages = append(m.messages, displayMsg{role: "error", content: "No valid " + verb + " rule provided."})
		return
	}
	*setting = dedupeStrings(append(*setting, specs...))
	for _, spec := range specs {
		_ = m.session.PermSvc().RememberSessionRule(spec, allow)
	}
	label := strings.ToUpper(verb[:1]) + verb[1:]
	m.messages = append(m.messages, displayMsg{role: "system", content: label + " rules updated.\n" + permissionRulesSummary(m)})
}

func savePermissionSettings(scope string, settings rhoconfig.Settings, level safety.AutonomyLevel) (string, error) {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		scope = "global"
	}
	settings.Autonomy = permissionTierSettingValue(level)
	settings.AutonomyExplicit = true
	settings.AllowedTools = dedupeStrings(settings.AllowedTools)
	settings.DisallowedTools = dedupeStrings(settings.DisallowedTools)

	switch scope {
	case "project":
		return "", fmt.Errorf("project-local settings writes are disabled; use scope \"global\" or an explicit --settings file")
	case "global":
		target := rhoconfig.LoadGlobalSettings()
		target.AutoAllow = append([]string{}, settings.AutoAllow...)
		target.AllowedTools = append([]string{}, settings.AllowedTools...)
		target.DisallowedTools = append([]string{}, settings.DisallowedTools...)
		target.Autonomy = settings.Autonomy
		target.AutonomyExplicit = true
		if err := rhoconfig.SaveGlobal(target); err != nil {
			return "", err
		}
		return "user settings", nil
	default:
		return "", fmt.Errorf("valid save scopes: project, global")
	}
}

func resetPermissionCenter(m *chatModel) {
	if m == nil || m.session == nil {
		return
	}
	perm := m.session.PermSvc()
	if perm == nil {
		return
	}
	perm.SetAutonomy(DefaultAutonomy)
	m.settings.Autonomy = permissionTierSettingValue(DefaultAutonomy)
	m.settings.AutonomyExplicit = true
	m.settings.AutoAllow = nil
	m.settings.AllowedTools = nil
	m.settings.DisallowedTools = nil
	perm.SetSpecStage(safety.SpecStageNone)
	perm.SetDryRun(false)
	// Reset is a complete safety reset: a break-glass bypass must not survive
	// while the UI reports that autonomy has returned to defaults.
	perm.DisableBypass()
	rebuildSessionPermissionRules(m.session, m.settings)
}

func (m *chatModel) handleAutonomyCommand(parts []string) (chatModel, tea.Cmd) {
	if m == nil {
		return chatModel{}, nil
	}
	if m.session == nil {
		m.messages = append(m.messages, displayMsg{role: "error", content: "No active session."})
		return *m, nil
	}
	if m.session.PermSvc() == nil {
		m.messages = append(m.messages, displayMsg{role: "error", content: "Permission service unavailable."})
		return *m, nil
	}
	if len(parts) == 1 {
		if m.autonomyPicker == nil {
			m.autonomyPicker = NewAutonomyPicker(m.width)
		}
		m.autonomyPicker.Open(effectivePermissionTier(m.session))
		return *m, nil
	}

	switch strings.ToLower(strings.TrimSpace(parts[1])) {
	case "help", "status":
		m.messages = append(m.messages, displayMsg{role: "system", content: autonomyCenterSummary(m)})
	case "tier":
		if len(parts) < 3 {
			m.messages = append(m.messages, displayMsg{role: "error", content: "Usage: /autonomy tier <scout|builder|operator|autonomous>"})
			return *m, nil
		}
		level, label, ok := normalizePermissionTier(parts[2])
		if !ok {
			m.messages = append(m.messages, displayMsg{role: "error", content: "Valid tiers: scout, builder, operator, autonomous"})
			return *m, nil
		}
		m.session.PermSvc().SetAutonomy(level)
		m.settings.Autonomy = permissionTierSettingValue(level)
		m.settings.AutonomyExplicit = true
		m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Autonomy tier → %s\nBehavior: %s", label, autonomyTierDescription(level))})
	case "dry-run":
		if len(parts) < 3 {
			state := "off"
			if currentDryRun(m.session) {
				state = "on"
			}
			m.messages = append(m.messages, displayMsg{role: "system", content: "Dry-run: " + state + "\nUsage: /autonomy dry-run <on|off>"})
			return *m, nil
		}
		switch strings.ToLower(strings.TrimSpace(parts[2])) {
		case "on", "true", "1":
			m.session.PermSvc().SetDryRun(true)
			m.messages = append(m.messages, displayMsg{role: "system", content: "Dry-run → on. Every tool call will be denied unconditionally, regardless of tier or spec stage."})
		case "off", "false", "0":
			m.session.PermSvc().SetDryRun(false)
			m.messages = append(m.messages, displayMsg{role: "system", content: "Dry-run → off. Normal tier/spec-gate rules apply again."})
		default:
			m.messages = append(m.messages, displayMsg{role: "error", content: "Usage: /autonomy dry-run <on|off>"})
		}
	case "allow":
		m.updateAutonomyRule(parts, true)
	case "deny":
		m.updateAutonomyRule(parts, false)
	case "grants":
		if len(parts) > 2 && strings.EqualFold(strings.TrimSpace(parts[2]), "cleanup") {
			if m.session != nil && m.session.PermSvc() != nil {
				// Reset clears all learned + user rules; rebuild from settings.
				rebuildSessionPermissionRules(m.session, m.settings)
				m.messages = append(m.messages, displayMsg{role: "system", content: "Grants cleaned up. Active rules rebuilt from settings.\n" + permissionRulesSummary(m)})
				return *m, nil
			}
			m.messages = append(m.messages, displayMsg{role: "error", content: "No active permission state."})
			return *m, nil
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: "Usage: /autonomy grants cleanup — rebuild active rules from settings (clears learned grants)"})
	case "rules":
		if len(parts) > 2 && strings.EqualFold(strings.TrimSpace(parts[2]), "clear") {
			m.settings.AutoAllow = nil
			m.settings.AllowedTools = nil
			m.settings.DisallowedTools = nil
			m.settings.NeverAllow = nil
			rebuildSessionPermissionRules(m.session, m.settings)
			if m.session != nil && m.session.PermSvc() != nil {
				m.session.PermSvc().SetNeverAllow(nil)
			}
			m.messages = append(m.messages, displayMsg{role: "system", content: "Autonomy rules cleared for the current session."})
			return *m, nil
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: permissionRulesSummary(m)})
	case "save":
		scope := ""
		if len(parts) > 2 {
			scope = parts[2]
		}
		path, err := savePermissionSettings(scope, m.settings, effectivePermissionTier(m.session))
		if err != nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: fmt.Sprintf("Save failed: %v", err)})
			return *m, nil
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: "Autonomy policy saved to " + path})
	case "bypass":
		if m.session == nil || m.session.PermSvc() == nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: "No active session."})
			return *m, nil
		}
		if len(parts) < 3 {
			enabled, grant := m.session.PermSvc().BypassState()
			state := "off"
			if enabled {
				state = "on"
				if grant != nil && len(grant.Scope) > 0 {
					state += " (scope: " + strings.Join(grant.Scope, ",") + ")"
					if !grant.ExpiresAt.IsZero() {
						state += " (expires: " + grant.ExpiresAt.Format("15:04:05") + ")"
					}
				}
			}
			m.messages = append(m.messages, displayMsg{role: "system", content: "Bypass: " + state + "\nUsage: /autonomy bypass <on|off> --reason=\"debugging\" [--scope=bash,network] [--for=5m]"})
			return *m, nil
		}
		switch strings.ToLower(strings.TrimSpace(parts[2])) {
		case "on", "true", "1":
			scope, expires, reason, err := parseBypassFlags(parts[3:])
			if err != nil {
				m.messages = append(m.messages, displayMsg{role: "error", content: "Invalid bypass options: " + err.Error()})
				return *m, nil
			}
			if !m.session.PermSvc().EnableBypass(scope, expires, reason) {
				m.messages = append(m.messages, displayMsg{role: "error", content: "Unable to enable bypass: no permission service."})
				return *m, nil
			}
			scopeLabel := "all"
			if len(scope) > 0 {
				scopeLabel = strings.Join(scope, ",")
			}
			m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Bypass → on (scope: %s, reason: %s). Use with care.", scopeLabel, reason)})
		case "off", "false", "0":
			if !m.session.PermSvc().DisableBypass() {
				m.messages = append(m.messages, displayMsg{role: "error", content: "Unable to disable bypass: no permission service."})
				return *m, nil
			}
			m.messages = append(m.messages, displayMsg{role: "system", content: "Bypass → off. Normal permission checks resume."})
		default:
			m.messages = append(m.messages, displayMsg{role: "error", content: "Usage: /autonomy bypass <on|off> --reason=\"...\" [--scope=...] [--for=...]"})
		}
	case "profile":
		if m.session == nil || m.session.PermSvc() == nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: "No active session."})
			return *m, nil
		}
		if len(parts) < 3 {
			// Show current profile flags.
			profile := m.session.PermSvc().AutonomyProfile()
			if profile == nil {
				m.messages = append(m.messages, displayMsg{role: "system", content: "No active profile."})
				return *m, nil
			}
			var b strings.Builder
			b.WriteString("Autonomy Profile\n")
			b.WriteString(fmt.Sprintf("  Level: %s\n", profile.Level.String()))
			b.WriteString(fmt.Sprintf("  auto_continue:    %v\n", profile.AutoContinue))
			b.WriteString(fmt.Sprintf("  auto_apply_edits:  %v\n", profile.AutoApplyEdits))
			b.WriteString(fmt.Sprintf("  auto_execute_bash: %v%s\n", profile.AutoExecuteBash, markOverridden(profile, "autoexecutebash")))
			b.WriteString(fmt.Sprintf("  auto_commit:       %v\n", profile.AutoCommit))
			b.WriteString(fmt.Sprintf("  auto_network:      %v%s\n", profile.AutoNetwork, markOverridden(profile, "autonetwork")))
			b.WriteString("\nUsage: /autonomy profile <flag>=<on|off>\n  e.g. /autonomy profile auto_execute_bash=off")
			m.messages = append(m.messages, displayMsg{role: "system", content: b.String()})
			return *m, nil
		}
		// Parse flag=value.
		flagSet := strings.Join(parts[2:], " ")
		idx := strings.Index(flagSet, "=")
		if idx < 0 {
			m.messages = append(m.messages, displayMsg{role: "error", content: "Usage: /autonomy profile <flag>=<on|off>"})
			return *m, nil
		}
		flagName := strings.TrimSpace(flagSet[:idx])
		flagVal := strings.ToLower(strings.TrimSpace(flagSet[idx+1:]))
		val := flagVal == "on" || flagVal == "true" || flagVal == "1"
		before := m.session.PermSvc().AutonomyProfile()
		if before == nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: fmt.Sprintf("Unknown flag %q. Valid: auto_continue, auto_apply_edits, auto_execute_bash, auto_commit, auto_network", flagName)})
			return *m, nil
		}
		if !before.Override(flagName, val) {
			m.messages = append(m.messages, displayMsg{role: "error", content: fmt.Sprintf("Unknown flag %q. Valid: auto_continue, auto_apply_edits, auto_execute_bash, auto_commit, auto_network", flagName)})
			return *m, nil
		}
		m.session.PermSvc().ApplyAutonomyOverrides(before.Overrides())
		m.settings.AutonomyOverrides = before.Overrides()
		m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Profile updated: %s=%v\n%s", flagName, val, func() string {
			p := m.session.PermSvc().AutonomyProfile()
			if p == nil {
				return ""
			}
			return fmt.Sprintf("  auto_execute_bash=%v auto_network=%v", p.AutoExecuteBash, p.AutoNetwork)
		}())})
	case "spec-tests":
		if len(parts) < 3 {
			state := "off"
			if m.settings.SpecAllowTests {
				state = "on"
			}
			m.messages = append(m.messages, displayMsg{role: "system", content: "Spec-stage test allowance: " + state + "\nUsage: /autonomy spec-tests <on|off>\n  When on, safe test commands (go test, npm test, pytest, etc.) are permitted during the spec workflow."})
			return *m, nil
		}
		switch strings.ToLower(strings.TrimSpace(parts[2])) {
		case "on", "true", "1":
			m.settings.SpecAllowTests = true
			m.session.PermSvc().SetSpecAllowTests(true)
			m.messages = append(m.messages, displayMsg{role: "system", content: "Spec-stage test allowance → on"})
		case "off", "false", "0":
			m.settings.SpecAllowTests = false
			m.session.PermSvc().SetSpecAllowTests(false)
			m.messages = append(m.messages, displayMsg{role: "system", content: "Spec-stage test allowance → off"})
		default:
			m.messages = append(m.messages, displayMsg{role: "error", content: "Usage: /autonomy spec-tests <on|off>"})
		}
	case "never":
		if m.session == nil || m.session.PermSvc() == nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: "No active session."})
			return *m, nil
		}
		if len(parts) < 3 {
			never := m.session.PermSvc().NeverAllow()
			var b strings.Builder
			b.WriteString("Personal Hard Ceiling (never rules)\n")
			if len(never) == 0 {
				b.WriteString("  none — YOLO can do anything. Add one with /autonomy never <rule>\n")
			} else {
				for _, r := range never {
					b.WriteString("  - " + r + "\n")
				}
			}
			b.WriteString("\nUsage: /autonomy never <rule>   e.g. /autonomy never Write(*.env)\n")
			b.WriteString("       /autonomy never clear")
			m.messages = append(m.messages, displayMsg{role: "system", content: b.String()})
			return *m, nil
		}
		if strings.EqualFold(strings.TrimSpace(parts[2]), "clear") {
			m.settings.NeverAllow = nil
			m.session.PermSvc().SetNeverAllow(nil)
			m.messages = append(m.messages, displayMsg{role: "system", content: "Never rules cleared."})
			return *m, nil
		}
		specs := parseToolListFromCLI([]string{strings.Join(parts[2:], " ")})
		if len(specs) == 0 {
			m.messages = append(m.messages, displayMsg{role: "error", content: "No valid never rule provided."})
			return *m, nil
		}
		m.settings.NeverAllow = append(m.settings.NeverAllow, specs...)
		m.session.PermSvc().SetNeverAllow(m.settings.NeverAllow)
		var nb strings.Builder
		nb.WriteString("Never rule added. Even YOLO will be blocked.\n")
		for _, r := range m.settings.NeverAllow {
			nb.WriteString("  - " + r + "\n")
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: nb.String()})
	case "audit":
		if m.session == nil || m.session.PermSvc() == nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: "No active session."})
			return *m, nil
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: m.session.PermSvc().AuditLog()})
	case "metrics":
		if m.session == nil || m.session.PermSvc() == nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: "No active session."})
			return *m, nil
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: m.session.PermSvc().PermissionMetrics()})
	case "reset":
		resetPermissionCenter(m)
		m.messages = append(m.messages, displayMsg{role: "system", content: "Session autonomy reset to defaults. Persisted exact rules were kept; use `rho permissions reset` to remove them with confirmation.\n" + autonomyCenterSummary(m)})
	default:
		m.messages = append(m.messages, displayMsg{role: "system", content: autonomyCommandHelp()})
	}
	return *m, nil
}
