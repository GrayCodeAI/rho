package safety

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/GrayCodeAI/rho/internal/permissions"
	"github.com/GrayCodeAI/rho/internal/permissions/stableid"
)

// TestCheckTool_SpecStageBlocksEvenYOLO verifies the core guarantee documented
// in permission_engine.go: the spec-stage gate is checked before autonomy, so
// no autonomy level (including YOLO) can bypass it while a spec workflow is
// mid-flight.
func TestCheckTool_SpecStageBlocksEvenYOLO(t *testing.T) {
	for _, stage := range []SpecStage{SpecStageProposal, SpecStageSpecify, SpecStageDesign, SpecStagePlan, SpecStageTasks} {
		pe := NewPermissionEngine()
		pe.Stage = stage
		pe.Autonomy = AutonomyYOLO

		allowed, reason := pe.CheckTool(context.Background(), ToolCallInfo{Name: "Write"})
		if allowed {
			t.Errorf("stage %v: YOLO autonomy bypassed spec gate for Write, want denied", stage)
		}
		if reason == "" {
			t.Errorf("stage %v: expected a deny reason", stage)
		}

		allowed, _ = pe.CheckTool(context.Background(), ToolCallInfo{Name: "Bash", Args: map[string]interface{}{"command": "rm -rf /"}})
		if allowed {
			t.Errorf("stage %v: YOLO autonomy bypassed spec gate for Bash, want denied", stage)
		}
	}
}

// TestCheckTool_SpecStageAllowsWorkflowAndReadTools verifies that while a
// spec workflow is active, the workflow's own tools and read-only tools are
// still allowed through without a user prompt.
func TestCheckTool_SpecStageAllowsWorkflowAndReadTools(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })

	constitutionDir := filepath.Join(tmpDir, ".rho", "specs", "test-spec")
	os.MkdirAll(constitutionDir, 0o700)
	os.WriteFile(filepath.Join(constitutionDir, "constitution.md"), []byte("## Constitution\n"), 0o600)

	pe := NewPermissionEngine()
	pe.Stage = SpecStageSpecify
	pe.SpecSlug = "test-spec"
	pe.Autonomy = AutonomySupervised

	for _, name := range []string{"Specify"} {
		allowed, reason := pe.CheckTool(context.Background(), ToolCallInfo{Name: name})
		if !allowed {
			t.Errorf("tool %q: expected allowed during spec stage (no slug), got denied: %q", name, reason)
		}
	}
	if allowed, reason := pe.CheckTool(context.Background(), ToolCallInfo{Name: "Plan"}); allowed || reason == "" {
		t.Fatalf("Plan should wait for Specify, allowed=%v reason=%q", allowed, reason)
	}
	pe.specDone = doneSpecify
	if allowed, reason := pe.CheckTool(context.Background(), ToolCallInfo{Name: "Plan"}); allowed || reason == "" {
		t.Fatalf("Plan should wait for both Specify and Design, allowed=%v reason=%q", allowed, reason)
	}
	pe.specDone = doneSpecify | doneDesign
	if allowed, reason := pe.CheckTool(context.Background(), ToolCallInfo{Name: "Plan"}); !allowed || reason != "" {
		t.Fatalf("Plan should be allowed when both Specify and Design done (gates checked post-write), allowed=%v reason=%q", allowed, reason)
	}
	if allowed, reason := pe.CheckTool(context.Background(), ToolCallInfo{Name: "Tasks"}); allowed || reason == "" {
		t.Fatalf("Tasks should wait for Plan, allowed=%v reason=%q", allowed, reason)
	}

	allowed, reason := pe.CheckTool(context.Background(), ToolCallInfo{Name: "Read"})
	if !allowed {
		t.Errorf("Read: expected allowed during spec stage (read-only), got denied: %q", reason)
	}
}

// TestCheckTool_ApproveImplementationAlwaysPrompts verifies that
// ApproveImplementation is never auto-allowed by autonomy tier, bypass-kill,
// or auto-mode — it always calls PromptFn, even at YOLO.
func TestCheckTool_ApproveImplementationAlwaysPrompts(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Stage = SpecStageTasks
	pe.Autonomy = AutonomyYOLO
	if !pe.BypassKill.EnableScoped(nil, time.Time{}, "permission test") {
		t.Fatal("failed to enable test bypass")
	}

	promptCalled := false
	pe.PromptFn = func(req PermissionRequest) {
		promptCalled = true
		req.Response <- true
	}

	allowed, reason := pe.CheckTool(context.Background(), ToolCallInfo{Name: "ApproveImplementation"})
	if !promptCalled {
		t.Fatal("expected PromptFn to be called for ApproveImplementation despite YOLO autonomy and bypass-kill")
	}
	if !allowed {
		t.Errorf("expected approval after user said yes, got denied: %q", reason)
	}
}

// TestCheckTool_ApproveImplementationDeniedByUser verifies a user rejecting
// the ApproveImplementation prompt keeps the spec gate closed.
func TestCheckTool_ApproveImplementationDeniedByUser(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Stage = SpecStagePlan
	pe.Autonomy = AutonomyYOLO
	pe.PromptFn = func(req PermissionRequest) {
		req.Response <- false
	}

	allowed, reason := pe.CheckTool(context.Background(), ToolCallInfo{Name: "ApproveImplementation"})
	if allowed {
		t.Error("expected ApproveImplementation to be denied when user says no")
	}
	if reason == "" {
		t.Error("expected a deny reason")
	}
}

// TestCheckTool_SpecStageImplementingUsesAutonomy verifies the gate opens
// once Stage transitions to Implementing: ordinary autonomy-tier logic
// governs tool calls again, independent of spec stage.
func TestCheckTool_SpecStageImplementingUsesAutonomy(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Stage = SpecStageImplementing
	pe.Autonomy = AutonomyYOLO

	allowed, reason := pe.CheckTool(context.Background(), ToolCallInfo{Name: "Write"})
	if !allowed {
		t.Errorf("expected Write allowed at YOLO once Implementing, got denied: %q", reason)
	}
}

func TestPermissionEngine_StructuredDecisionIncludesStableReason(t *testing.T) {
	pe := NewPermissionEngine()
	pe.DryRun = true
	d := pe.CheckToolDecision(context.Background(), ToolCallInfo{Name: "Read"})
	if d.Outcome != DecisionDeny || d.Reason != ReasonDryRun {
		t.Fatalf("decision = %#v, want deny/dry_run", d)
	}
	if d.Message == "" {
		t.Fatal("structured decision should retain human-readable message")
	}
}

func TestPermissionEngine_UsesPersistedExactRules(t *testing.T) {
	store := permissions.NewStableRuleStore(filepath.Join(t.TempDir(), "stable-rules.json"))
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Remember(stableid.KindCommand, "command\x00git status", "git status", stableid.Allow); !ok {
		t.Fatal("remember allow rule")
	}
	if _, ok := store.Remember(stableid.KindCommand, "command\x00git push", "git push", stableid.Deny); !ok {
		t.Fatal("remember deny rule")
	}

	pe := NewPermissionEngine()
	pe.ExactRules = store
	if d := pe.CheckToolDecision(context.Background(), ToolCallInfo{Name: "Bash", Args: map[string]interface{}{"command": "git status"}}); d.Outcome != DecisionAllow {
		t.Fatalf("exact allow decision = %#v", d)
	}
	if d := pe.CheckToolDecision(context.Background(), ToolCallInfo{Name: "Bash", Args: map[string]interface{}{"command": "git push"}}); d.Outcome != DecisionDeny {
		t.Fatalf("exact deny decision = %#v", d)
	}
	if d := pe.CheckToolDecision(context.Background(), ToolCallInfo{Name: "Bash", Args: map[string]interface{}{"command": "git commit -am change"}}); d.Reason == ReasonGrantAllowed {
		t.Fatal("unmatched exact rule must not grant a different command")
	}
}

func TestPermissionEngine_SnapshotIsStableAfterLivePolicyChange(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Autonomy = AutonomyYOLO
	snapshot := pe.Snapshot()
	pe.DryRun = true
	d := pe.CheckToolSnapshot(context.Background(), ToolCallInfo{Name: "Read"}, snapshot)
	if d.Outcome != DecisionAllow {
		t.Fatalf("snapshot decision = %#v, want allow from captured policy", d)
	}
}

func TestPermissionEngine_SnapshotCapturesRememberedRules(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Memory.AlwaysDeny("Write")
	snapshot := pe.Snapshot()
	pe.Memory.Reset()
	d := pe.CheckToolSnapshot(context.Background(), ToolCallInfo{Name: "Write"}, snapshot)
	if d.Outcome != DecisionDeny || (d.Reason != ReasonRuleDenied && d.Reason != ReasonGrantDenied) {
		t.Fatalf("snapshot decision = %#v, want remembered deny", d)
	}
}

func TestPermissionMemory_ExactRulesDoNotInterpretGlobs(t *testing.T) {
	pm := NewPermissionMemory()
	pm.AlwaysAllowExact("Bash", "echo *")

	if got := pm.Check("Bash", "echo *"); got == nil || !*got {
		t.Fatal("literal exact action was not allowed")
	}
	if got := pm.Check("Bash", "echo hello"); got != nil {
		t.Fatalf("literal wildcard expanded into a glob: %v", *got)
	}

	clone := NewPermissionMemoryFromSnapshot(pm.Snapshot())
	if got := clone.Check("Bash", "echo *"); got == nil || !*got {
		t.Fatal("exact rule was lost in snapshot")
	}
	if got := clone.Check("Bash", "echo hello"); got != nil {
		t.Fatalf("snapshot exact rule expanded into a glob: %v", *got)
	}
}

func TestPermissionMemory_ZeroValueIsUsable(t *testing.T) {
	var pm PermissionMemory
	pm.AlwaysAllow("Read")
	pm.AlwaysAllowExact("Bash", "git status")
	pm.AlwaysDenyExact("Write", ".env")

	if got := pm.Check("Read", "anything"); got == nil || !*got {
		t.Fatal("zero-value memory did not record a tool-wide allow")
	}
	if got := pm.CheckWithIdentity("Bash", "git status", "git status"); got == nil || !*got {
		t.Fatal("zero-value memory did not record an exact allow")
	}
	if got := pm.CheckWithIdentity("Write", ".env", ".env"); got == nil || *got {
		t.Fatal("zero-value memory did not record an exact deny")
	}
}

func TestPermissionMemory_ExactRulesUseFullIdentityWhenSummaryIsTruncated(t *testing.T) {
	identity := "git status " + strings.Repeat("x", 200)
	pm := NewPermissionMemory()
	pm.AlwaysAllowExact("Bash", identity)
	summary := identity[:120] + "..."

	if got := pm.CheckWithIdentity("Bash", summary, identity); got == nil || !*got {
		t.Fatal("exact memory grant did not use the complete identity")
	}
	grants := permissions.NewUnifiedGrants(pm)
	allowed, found := grants.CheckWithIdentity("Bash", summary, identity, time.Now())
	if !found || !allowed {
		t.Fatalf("exact unified grant did not use the complete identity: allowed=%v found=%v", allowed, found)
	}
	if allowed, found := grants.CheckWithIdentity("Bash", summary, identity+"-different", time.Now()); found || allowed {
		t.Fatalf("exact unified grant matched a different identity: allowed=%v found=%v", allowed, found)
	}
}

func TestPermissionEngine_AliasUsesCanonicalUnifiedGrant(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Autonomy = AutonomyYOLO
	pe.Memory.AllowSpec("Bash(echo *)")

	decision := pe.EvaluateTool(context.Background(), ToolCallInfo{
		Name: "bash",
		Args: map[string]interface{}{"command": "echo hello"},
	})
	if decision.Outcome != DecisionAllow || decision.Reason != ReasonGrantAllowed {
		t.Fatalf("alias decision = %#v, want grant allow", decision)
	}
}

func TestPermissionMemory_GrantsAreDeterministicallyOrdered(t *testing.T) {
	pm := NewPermissionMemory()
	pm.AlwaysAllow("Write")
	pm.AlwaysAllow("Bash")
	pm.AlwaysDenyExact("Bash", "rm -rf /")
	pm.AlwaysAllowExact("Bash", "git status")

	first := pm.Grants()
	second := pm.Grants()
	if len(first) != len(second) {
		t.Fatalf("grant lengths differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("grant order changed at %d: first=%#v second=%#v", i, first[i], second[i])
		}
	}
	for i := 1; i < len(first); i++ {
		if first[i-1].Tool > first[i].Tool || (first[i-1].Tool == first[i].Tool && first[i-1].Pattern > first[i].Pattern) {
			t.Fatalf("grants are not sorted: %#v", first)
		}
	}
}

func TestPermissionEngine_TierChangePreservesProfileOverrides(t *testing.T) {
	pe := NewPermissionEngine()
	if !pe.Profile.Override("auto_network", false) {
		t.Fatal("expected auto_network to be a supported profile override")
	}

	pe.Autonomy = AutonomyFull
	d := pe.EvaluateTool(context.Background(), ToolCallInfo{Name: "WebFetch", Args: map[string]interface{}{"url": "https://example.com"}})
	if d.Outcome != DecisionAsk {
		t.Fatalf("tier change lost explicit network override: decision = %#v, want ask", d)
	}
	if !pe.Profile.IsOverridden("auto_network") {
		t.Fatal("tier change lost the profile override marker")
	}
}

func TestPermissionEngine_FullOnlyAutoRunsAllowlistedShellCommands(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Autonomy = AutonomyFull

	if d := pe.EvaluateTool(context.Background(), ToolCallInfo{Name: "Bash", Args: map[string]interface{}{"command": "git status"}}); d.Outcome != DecisionAllow {
		t.Fatalf("allowlisted Bash command = %#v, want allow", d)
	}
	if d := pe.EvaluateTool(context.Background(), ToolCallInfo{Name: "Bash", Args: map[string]interface{}{"command": "python -c 'print(1)'"}}); d.Outcome != DecisionAsk {
		t.Fatalf("unknown Bash command = %#v, want ask", d)
	}
}

func TestPermissionEngine_PowerShellDoesNotUseBashFastPath(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Autonomy = AutonomyFull
	d := pe.EvaluateTool(context.Background(), ToolCallInfo{Name: "PowerShell", Args: map[string]interface{}{"command": "Get-ChildItem"}})
	if d.Outcome != DecisionAsk {
		t.Fatalf("PowerShell command = %#v, want ask", d)
	}
}

func TestPermissionEngine_EvaluateToolReturnsAskWithoutBlocking(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Autonomy = AutonomySupervised
	d := pe.EvaluateTool(context.Background(), ToolCallInfo{Name: "Write"})
	if d.Outcome != DecisionAsk || d.Reason != ReasonUserPrompt {
		t.Fatalf("decision = %#v, want ask/user_prompt", d)
	}
}

func TestPermissionEngine_EvaluateToolDoesNotRecordPreview(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Autonomy = AutonomySupervised
	metricsBefore := pe.PermissionMetrics().Snapshot()
	auditBefore := len(pe.AuditLog().Recent(0))

	d := pe.EvaluateTool(context.Background(), ToolCallInfo{Name: "Write", Args: map[string]interface{}{"path": "README.md"}})
	if d.Outcome != DecisionAsk {
		t.Fatalf("preview decision = %#v, want ask", d)
	}
	if got := pe.PermissionMetrics().Snapshot(); !reflect.DeepEqual(got, metricsBefore) {
		t.Fatalf("preview changed metrics: before=%v after=%v", metricsBefore, got)
	}
	if got := len(pe.AuditLog().Recent(0)); got != auditBefore {
		t.Fatalf("preview changed audit log length: before=%d after=%d", auditBefore, got)
	}
}

// TestCheckTool_SpecStageNoneIgnoresGate verifies that outside of any spec
// workflow (Stage == SpecStageNone), the spec gate does not apply at all and
// autonomy-tier logic governs directly.
func TestCheckTool_SpecStageNoneIgnoresGate(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Stage = SpecStageNone
	pe.Autonomy = AutonomySupervised

	promptCalled := false
	pe.PromptFn = func(req PermissionRequest) {
		promptCalled = true
		req.Response <- true
	}

	allowed, reason := pe.CheckTool(context.Background(), ToolCallInfo{Name: "Write"})
	if !promptCalled {
		t.Fatal("expected supervised autonomy to prompt for Write when no spec workflow is active")
	}
	if !allowed {
		t.Errorf("expected allowed after user approval, got denied: %q", reason)
	}
}

// TestCheckTool_DryRunOverridesEverything verifies DryRun denies
// unconditionally, even during spec stages that would otherwise allow the
// tool through.
func TestCheckTool_DryRunOverridesEverything(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Stage = SpecStageSpecify
	pe.DryRun = true

	allowed, reason := pe.CheckTool(context.Background(), ToolCallInfo{Name: "Specify"})
	if allowed {
		t.Error("expected DryRun to deny even a spec-stage-allowed tool")
	}
	if reason == "" {
		t.Error("expected a deny reason")
	}
}

func TestCheckTool_ExplicitDenyOverridesAutonomy(t *testing.T) {
	for _, tier := range []AutonomyLevel{AutonomyBasic, AutonomySemi, AutonomyFull, AutonomyYOLO} {
		pe := NewPermissionEngine()
		pe.Autonomy = tier
		pe.Memory.AlwaysDeny("Write")
		allowed, reason := pe.CheckTool(context.Background(), ToolCallInfo{Name: "Write", Args: map[string]interface{}{"file_path": "x.txt"}})
		if allowed || reason != "Permission denied (rule)." {
			t.Fatalf("tier %v: allowed=%v reason=%q", tier, allowed, reason)
		}
	}
	pe := NewPermissionEngine()
	pe.Autonomy = AutonomyYOLO
	pe.Memory.AlwaysAllowPattern("Write:x.txt")
	pe.Memory.AlwaysDenyPattern("Write:x.txt")
	if allowed, reason := pe.CheckTool(context.Background(), ToolCallInfo{Name: "Write", Args: map[string]interface{}{"file_path": "x.txt"}}); allowed || reason != "Permission denied (rule)." {
		t.Fatalf("explicit deny did not beat auto-allow: allowed=%v reason=%q", allowed, reason)
	}
}

func TestCheckTool_UnknownToolFailsClosedInYOLOAndBypass(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Autonomy = AutonomyYOLO

	d := pe.CheckToolDecision(context.Background(), ToolCallInfo{Name: "plugin_future_tool"})
	if d.Outcome != DecisionDeny || d.Reason != ReasonPromptUnavailable {
		t.Fatalf("unknown tool in YOLO should fail closed without a prompt: %+v", d)
	}

	if !pe.BypassKill.EnableScoped(nil, time.Time{}, "unknown-tool test") {
		t.Fatal("failed to enable test bypass")
	}
	d = pe.CheckToolDecision(context.Background(), ToolCallInfo{Name: "plugin_future_tool"})
	if d.Outcome != DecisionDeny || d.Reason != ReasonPromptUnavailable {
		t.Fatalf("unknown tool must not be granted by bypass: %+v", d)
	}
}

func TestCheckTool_RegisteredExternalToolRequiresExplicitApproval(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Autonomy = AutonomyYOLO
	pe.KnownTools = map[string]struct{}{"mcp__server__publish": {}}
	pe.UntrustedTools = map[string]struct{}{"mcp__server__publish": {}}

	d := pe.CheckToolDecision(context.Background(), ToolCallInfo{Name: "mcp__server__publish"})
	if d.Outcome != DecisionDeny || d.Reason != ReasonPromptUnavailable {
		t.Fatalf("registered external tool should not inherit YOLO: %+v", d)
	}

	pe.Memory.AlwaysAllowExact("mcp__server__publish", "mcp__server__publish")
	d = pe.CheckToolDecision(context.Background(), ToolCallInfo{Name: "mcp__server__publish"})
	if d.Outcome != DecisionAllow {
		t.Fatalf("explicit remembered rule should authorize external tool: %+v", d)
	}
}

func TestCheckTool_SpecWorkflowRequiresOrderButAllowsSupportTools(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Stage = SpecStageSpecify
	for _, name := range []string{"AskUserQuestion", "SpecStatus", "SpecEdit", "SpecList", "SpecConfig", "Clarify"} {
		allowed, reason := pe.CheckTool(context.Background(), ToolCallInfo{Name: name})
		if !allowed {
			t.Errorf("support tool %q denied during spec stage: %q", name, reason)
		}
	}
	if allowed, _ := pe.CheckTool(context.Background(), ToolCallInfo{Name: "Tasks"}); allowed {
		t.Fatal("Tasks must not skip the Plan stage")
	}
	if allowed, _ := pe.CheckTool(context.Background(), ToolCallInfo{Name: "ApproveImplementation"}); allowed {
		t.Fatal("ApproveImplementation must not skip the Tasks stage")
	}
}
