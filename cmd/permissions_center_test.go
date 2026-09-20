package cmd

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine/safety"

	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	"github.com/GrayCodeAI/rho/internal/engine"
	"github.com/GrayCodeAI/rho/internal/permissions"
	"github.com/GrayCodeAI/rho/internal/permissions/stableid"
)

func TestNormalizePermissionTier(t *testing.T) {
	level, label, ok := normalizePermissionTier("always_ask")
	if !ok || level != safety.AutonomySupervised || label != "Always Ask" {
		t.Fatalf("always_ask = (%v, %q, %v)", level, label, ok)
	}
	level, label, ok = normalizePermissionTier("operator")
	if !ok || level != safety.AutonomyFull || label != "Operator" {
		t.Fatalf("operator = (%v, %q, %v)", level, label, ok)
	}
	level, label, ok = normalizePermissionTier("auto")
	if !ok || level != safety.AutonomyYOLO || label != "Autonomous" {
		t.Fatalf("auto = (%v, %q, %v)", level, label, ok)
	}
}

func TestParseBypassFlagsFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "missing reason", args: []string{"--scope=bash"}, wantErr: "--reason is required"},
		{name: "invalid duration", args: []string{"--for=forever", "--reason=debug"}, wantErr: "invalid --for duration"},
		{name: "negative duration", args: []string{"--for=-1m", "--reason=debug"}, wantErr: "invalid --for duration"},
		{name: "unknown scope", args: []string{"--scope=host", "--reason=debug"}, wantErr: "invalid bypass scope"},
		{name: "duplicate scope", args: []string{"--scope=bash,bash", "--reason=debug"}, wantErr: "duplicate bypass scope"},
		{name: "unknown option", args: []string{"--reason=debug", "--silent"}, wantErr: "unknown bypass option"},
		{name: "duplicate reason", args: []string{"--reason=one", "--reason=two"}, wantErr: "--reason may be specified only once"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := parseBypassFlags(tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("parseBypassFlags(%v) error = %v, want %q", tt.args, err, tt.wantErr)
			}
		})
	}

	scope, expires, reason, err := parseBypassFlags([]string{"--scope=bash,network", "--for=5m", "--reason=debugging"})
	if err != nil {
		t.Fatalf("valid bypass flags returned error: %v", err)
	}
	if !reflect.DeepEqual(scope, []string{"bash", "network"}) || expires.IsZero() || reason != "debugging" {
		t.Fatalf("valid bypass flags = scope=%v expires=%v reason=%q", scope, expires, reason)
	}
}

func TestEffectivePermissionRules(t *testing.T) {
	settings := rhoconfig.Settings{
		AutoAllow:       []string{"Read"},
		AllowedTools:    []string{"Bash(git:*)", "Read"},
		DisallowedTools: []string{"Bash(rm -rf *)"},
	}
	allow := effectiveAllowRules(settings)
	deny := effectiveDenyRules(settings)
	if len(allow) != 2 {
		t.Fatalf("allow len = %d, want 2 (%v)", len(allow), allow)
	}
	if len(deny) != 1 || deny[0] != "Bash(rm -rf *)" {
		t.Fatalf("deny = %v", deny)
	}
}

func TestAutonomyCenterSummary(t *testing.T) {
	sess := engine.NewSession("test", "test-model", "system", nil)
	sess.PermSvc().SetAutonomy(safety.AutonomySemi)
	sess.PermSvc().SetSpecStage(safety.SpecStageSpecify)
	model := &chatModel{
		session: sess,
		settings: rhoconfig.Settings{
			AllowedTools:    []string{"Bash(git:*)"},
			DisallowedTools: []string{"Bash(rm -rf *)"},
		},
	}
	out := autonomyCenterSummary(model)
	for _, fragment := range []string{"Autonomy Center", "Tier: Builder", "Spec stage: Specify", "Rules: 1 allow, 1 deny"} {
		if !strings.Contains(out, fragment) {
			t.Fatalf("summary %q missing %q", out, fragment)
		}
	}
}

func TestAutonomyCenterSummaryShowsBypassState(t *testing.T) {
	sess := engine.NewSession("test", "test-model", "system", nil)
	if !sess.PermSvc().EnableBypass([]string{"bash", "network"}, time.Now().Add(5*time.Minute), "diagnostics") {
		t.Fatal("failed to enable bypass")
	}
	out := autonomyCenterSummary(&chatModel{session: sess})
	for _, fragment := range []string{
		"Bypass: ON",
		"scope: bash,network",
		"expires:",
	} {
		if !strings.Contains(out, fragment) {
			t.Fatalf("summary %q missing %q", out, fragment)
		}
	}
}

func TestAutonomyCenterSummaryHandlesUninitializedSession(t *testing.T) {
	out := autonomyCenterSummary(&chatModel{session: &engine.Session{}})
	if !strings.Contains(out, "permission service is not initialized") {
		t.Fatalf("summary = %q, want an initialization error", out)
	}
}

func TestPermissionRulesSummaryIncludesPersistedExactRules(t *testing.T) {
	sess := engine.NewSession("", "test-model", "you are helpful", nil)
	store := permissions.NewStableRuleStore(t.TempDir() + "/stable-rules.json")
	if _, ok := store.Remember(stableid.KindCommand, "git status", "git status", stableid.Allow); !ok {
		t.Fatal("failed to create persisted allow rule")
	}
	if _, ok := store.Remember(stableid.KindCommand, "git push", "git push", stableid.Deny); !ok {
		t.Fatal("failed to create persisted deny rule")
	}
	sess.PermSvc().SetExactRuleStore(store)

	out := permissionRulesSummary(&chatModel{session: sess})
	for _, fragment := range []string{
		"Persisted exact rules:",
		"#1 allow command: git status",
		"#2 deny command: git push",
	} {
		if !strings.Contains(out, fragment) {
			t.Fatalf("summary %q missing %q", out, fragment)
		}
	}
}

func TestPermissionRulesSummaryMarksSessionExactRules(t *testing.T) {
	sess := engine.NewSession("", "test-model", "you are helpful", nil)
	sess.PermSvc().Memory().AlwaysAllowExact("Bash", "git status")

	out := permissionRulesSummary(&chatModel{session: sess})
	if !strings.Contains(out, "Bash(git status)") || !strings.Contains(out, "[exact]") {
		t.Fatalf("session exact rule was not identified in summary: %q", out)
	}
}

func TestPermissionTierSettingValue(t *testing.T) {
	cases := []struct {
		level safety.AutonomyLevel
		want  int
	}{
		{safety.AutonomySupervised, 0},
		{safety.AutonomyBasic, 1},
		{safety.AutonomySemi, 2},
		{safety.AutonomyFull, 3},
		{safety.AutonomyYOLO, 4},
	}
	for _, c := range cases {
		if got := permissionTierSettingValue(c.level); got != c.want {
			t.Errorf("permissionTierSettingValue(%v) = %d, want %d", c.level, got, c.want)
		}
	}
}

// TestEffectivePermissionTier_ReadsThroughRealSession exercises
// effectivePermissionTier against a session built via engine.NewSession +
// PermSvc().SetAutonomy, the actual production wiring — unlike
// TestAutonomyCenterSummary's `&engine.Session{Perm: perm}` literal, whose
// Perm field is a distinct, unwired pointer from the perms service that
// PermSvc() actually reads (session.go:106 perms vs session.go:112 Perm).
// That literal's assignment is dead for this codepath and only "worked"
// because AutonomySemi happens to equal DefaultAutonomy.
func TestEffectivePermissionTier_ReadsThroughRealSession(t *testing.T) {
	if got := effectivePermissionTier(nil); got != DefaultAutonomy {
		t.Fatalf("nil session: got %v, want default %v", got, DefaultAutonomy)
	}

	sess := engine.NewSession("", "test-model", "you are helpful", nil)
	if got := effectivePermissionTier(sess); got != DefaultAutonomy {
		t.Fatalf("unset autonomy: got %v, want default %v", got, DefaultAutonomy)
	}

	sess.PermSvc().SetAutonomy(safety.AutonomyFull)
	if got := effectivePermissionTier(sess); got != safety.AutonomyFull {
		t.Fatalf("explicit Full: got %v, want %v", got, safety.AutonomyFull)
	}
	sess.PermSvc().SetAutonomy(safety.AutonomySupervised)
	if got := effectivePermissionTier(sess); got != safety.AutonomySupervised {
		t.Fatalf("explicit Supervised: got %v, want %v", got, safety.AutonomySupervised)
	}
}

func TestAutonomyTierDescriptions(t *testing.T) {
	seen := make(map[string]bool)
	for _, level := range []safety.AutonomyLevel{
		safety.AutonomyBasic, safety.AutonomySemi, safety.AutonomyFull, safety.AutonomyYOLO,
	} {
		summary := autonomyTierDescription(level)
		if summary == "" {
			t.Errorf("level %v: empty summary", level)
		}
		if seen[summary] {
			t.Errorf("level %v: summary %q duplicates another tier's", level, summary)
		}
		seen[summary] = true
	}
}

func TestResetPermissionCenter(t *testing.T) {
	sess := engine.NewSession("", "test-model", "you are helpful", nil)
	sess.PermSvc().SetAutonomy(safety.AutonomyYOLO)
	sess.PermSvc().SetSpecStage(safety.SpecStageTasks)
	sess.PermSvc().SetDryRun(true)
	if !sess.PermSvc().EnableBypass([]string{"bash"}, time.Now().Add(time.Minute), "test reset") {
		t.Fatal("failed to enable bypass for reset test")
	}
	store := permissions.NewStableRuleStore(t.TempDir() + "/stable-rules.json")
	if _, ok := store.Remember(stableid.KindCommand, "git status", "git status", stableid.Allow); !ok {
		t.Fatal("failed to create persisted rule")
	}
	sess.PermSvc().SetExactRuleStore(store)
	model := &chatModel{
		session: sess,
		settings: rhoconfig.Settings{
			Autonomy:        permissionTierSettingValue(safety.AutonomyYOLO),
			AutoAllow:       []string{"Read"},
			AllowedTools:    []string{"Bash(git:*)"},
			DisallowedTools: []string{"Bash(rm -rf *)"},
		},
	}

	resetPermissionCenter(model)

	if got := sess.PermSvc().RuntimeState().Autonomy; got != DefaultAutonomy {
		t.Errorf("autonomy = %v, want default %v", got, DefaultAutonomy)
	}
	if got := sess.PermSvc().SpecStage(); got != safety.SpecStageNone {
		t.Errorf("spec stage = %v, want None", got)
	}
	if currentDryRun(sess) {
		t.Error("dry-run should be cleared")
	}
	if enabled, _ := sess.PermSvc().BypassState(); enabled {
		t.Error("bypass should be disabled by reset")
	}
	if model.settings.AutoAllow != nil || model.settings.AllowedTools != nil || model.settings.DisallowedTools != nil {
		t.Error("rule lists should be cleared")
	}
	if got := store.List(); len(got) != 1 {
		t.Fatalf("persisted exact rules = %d, want 1 (session reset must not delete durable policy)", len(got))
	}
}

func TestAutonomyCommandHandlesUninitializedPermissionService(t *testing.T) {
	model := &chatModel{session: &engine.Session{}}
	updated, _ := model.handleAutonomyCommand([]string{"autonomy", "allow", "Write(*.md)"})
	if len(updated.messages) == 0 || !strings.Contains(updated.messages[len(updated.messages)-1].content, "Permission service unavailable") {
		t.Fatalf("expected a safe unavailable message, got %+v", updated.messages)
	}
}

func TestHandleAutonomyCommand_Tier(t *testing.T) {
	sess := engine.NewSession("", "test-model", "you are helpful", nil)
	model := &chatModel{session: sess}

	updated, _ := model.handleAutonomyCommand([]string{"autonomy", "tier", "operator"})
	if got := updated.session.PermSvc().RuntimeState().Autonomy; got != safety.AutonomyFull {
		t.Fatalf("after tier operator: autonomy = %v, want Full", got)
	}
	if updated.settings.Autonomy != permissionTierSettingValue(safety.AutonomyFull) {
		t.Fatalf("settings.Autonomy = %d, want %d", updated.settings.Autonomy, permissionTierSettingValue(safety.AutonomyFull))
	}

	updated, _ = updated.handleAutonomyCommand([]string{"autonomy", "tier", "not-a-tier"})
	last := updated.messages[len(updated.messages)-1]
	if last.role != "error" {
		t.Fatalf("invalid tier: expected error message, got role %q", last.role)
	}
	if got := updated.session.PermSvc().RuntimeState().Autonomy; got != safety.AutonomyFull {
		t.Fatalf("invalid tier should not change autonomy: got %v, want Full unchanged", got)
	}

	updated, _ = updated.handleAutonomyCommand([]string{"autonomy", "tier"})
	last = updated.messages[len(updated.messages)-1]
	if last.role != "error" || !strings.Contains(last.content, "Usage:") {
		t.Fatalf("missing tier arg: expected usage error, got %+v", last)
	}
}

func TestHandleAutonomyCommand_RuleChangePreservesSessionDecisions(t *testing.T) {
	sess := engine.NewSession("", "test-model", "you are helpful", nil)
	mem := sess.PermSvc().Memory()
	mem.AlwaysAllowPattern("Bash:git status")
	model := &chatModel{session: sess}

	updated, _ := model.handleAutonomyCommand([]string{"autonomy", "allow", "Write(*.md)"})
	if got := mem.Check("Bash", "git status"); got == nil || !*got {
		t.Fatal("adding an explicit allow rule erased the existing session allow")
	}
	if got := mem.Check("Write", "README.md"); got == nil || !*got {
		t.Fatal("explicit allow rule was not applied to the live session")
	}

	updated, _ = updated.handleAutonomyCommand([]string{"autonomy", "deny", "Bash(git push)"})
	if got := mem.Check("Bash", "git status"); got == nil || !*got {
		t.Fatal("adding an explicit deny rule erased the existing session allow")
	}
	if got := mem.Check("Bash", "git push"); got == nil || *got {
		t.Fatal("explicit deny rule was not applied to the live session")
	}
}
