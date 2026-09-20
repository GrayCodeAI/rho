package engine

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine/safety"
	"github.com/GrayCodeAI/rho/internal/permissions"
	"github.com/GrayCodeAI/rho/internal/permissions/stableid"
)

func TestPermissionService_CheckTool(t *testing.T) {
	// Inject a permission engine whose CheckTool denies everything.
	// We avoid calling the real engine (which would need full tool/perm
	// state) — this test only checks the PermissionService delegation.
	s := NewPermissionService(nil)
	// The default engine may or may not allow Bash; replace with a
	// custom stub via a small inline trick: set Mode to a plan that
	// forces denial (not implemented in the engine, so use a
	// permissionFn that returns a specific deny). For now, just verify
	// the wrapper compiles and returns a (bool, string).
	granted, _ := s.CheckTool(context.Background(), safety.ToolCallInfo{Name: "Bash", Args: map[string]interface{}{"command": "ls"}})
	_ = granted
}

func TestPermissionService_UninitializedEvaluationFailsClosed(t *testing.T) {
	s := &PermissionService{}
	info := safety.ToolCallInfo{Name: "Write"}
	for name, decision := range map[string]safety.Decision{
		"structured": s.CheckToolDecision(context.Background(), info),
		"preview":    s.EvaluateTool(context.Background(), info),
		"snapshot":   s.CheckToolSnapshot(context.Background(), info, safety.PolicySnapshot{}),
	} {
		if decision.Outcome != safety.DecisionDeny || decision.Message != "permission service is unavailable" {
			t.Errorf("%s = %#v, want structured fail-closed denial", name, decision)
		}
	}
	if got := s.PolicySnapshot(); !reflect.DeepEqual(got, safety.PolicySnapshot{}) {
		t.Fatalf("uninitialized PolicySnapshot = %#v, want zero snapshot", got)
	}
	s.ApplyPolicySnapshot(safety.PolicySnapshot{})
}

func TestPermissionService_MarkToolUntrustedClosesDynamicTrustGap(t *testing.T) {
	s := NewPermissionService(nil)
	s.SetAutonomy(safety.AutonomyYOLO)
	s.SetKnownTools([]string{"plugin__late__publish"})
	s.MarkToolUntrusted("plugin__late__publish")

	d := s.CheckToolDecision(context.Background(), safety.ToolCallInfo{Name: "plugin__late__publish"})
	if d.Outcome != safety.DecisionDeny || d.Reason != safety.ReasonPromptUnavailable {
		t.Fatalf("dynamic external tool inherited implicit trust: %+v", d)
	}
}

func TestPermissionService_SetSpecStage(t *testing.T) {
	s := NewPermissionService(nil)
	stages := []safety.SpecStage{safety.SpecStageNone, safety.SpecStageSpecify, safety.SpecStagePlan, safety.SpecStageTasks, safety.SpecStageImplementing}
	for _, stage := range stages {
		s.SetSpecStage(stage)
		if s.SpecStage() != stage {
			t.Errorf("SetSpecStage(%v) then SpecStage() = %v", stage, s.SpecStage())
		}
	}
}

func TestPermissionService_AutonomyAndAllowedDirs(t *testing.T) {
	s := NewPermissionService(nil)
	s.SetAutonomy(safety.AutonomySupervised)
	dirs := []string{"/tmp", "/var/folders"}
	s.SetAllowedDirs(dirs)
	dirs[0] = "/changed"
	if s.RuntimeState().Autonomy != safety.AutonomySupervised {
		t.Errorf("Autonomy = %v, want AutonomySupervised", s.RuntimeState().Autonomy)
	}
	if len(s.AllowedDirs()) != 2 {
		t.Errorf("AllowedDirs len = %d, want 2", len(s.AllowedDirs()))
	}
	got := s.AllowedDirs()
	got[0] = "/changed-again"
	if s.AllowedDirs()[0] != "/tmp" {
		t.Fatalf("AllowedDirs returned an aliased slice: %v", s.AllowedDirs())
	}
}

func TestPermissionService_RuntimeStateIsCoherent(t *testing.T) {
	s := NewPermissionService(nil)
	s.SetAutonomy(safety.AutonomyFull)
	s.SetDryRun(true)

	state := s.RuntimeState()
	if state.Autonomy != safety.AutonomyFull {
		t.Fatalf("RuntimeState autonomy = %v, want full", state.Autonomy)
	}
	if !state.AutonomyExplicit {
		t.Fatal("RuntimeState autonomy is not marked explicit")
	}
	if !state.DryRun {
		t.Fatal("RuntimeState dry-run = false, want true")
	}
}

func TestPermissionService_ResetSpecIncrementsRevision(t *testing.T) {
	s := NewPermissionService(nil)
	s.SetSpecSlug("demo")
	s.SetSpecStage(safety.SpecStageImplementing)
	before := s.PolicySnapshot().Revision
	s.ResetSpec()
	if got := s.SpecSlug(); got != "" {
		t.Fatalf("SpecSlug after reset = %q, want empty", got)
	}
	if got := s.SpecStage(); got != safety.SpecStageNone {
		t.Fatalf("SpecStage after reset = %v, want none", got)
	}
	if s.PolicySnapshot().Revision <= before {
		t.Fatalf("ResetSpec revision = %d, want > %d", s.PolicySnapshot().Revision, before)
	}
}

func TestPermissionService_SessionMutationsAdvanceRevision(t *testing.T) {
	s := NewPermissionService(nil)
	before := s.PolicySnapshot().Revision

	if !s.RememberSessionExact("Bash", "git status", true) {
		t.Fatal("RememberSessionExact returned false")
	}
	afterExact := s.PolicySnapshot().Revision
	if afterExact <= before {
		t.Fatalf("exact session mutation revision = %d, want > %d", afterExact, before)
	}
	if got := s.Memory().CheckWithIdentity("Bash", "git status", "git status"); got == nil || !*got {
		t.Fatal("exact session allow was not applied")
	}

	if !s.RememberSessionTool("Write", false) {
		t.Fatal("RememberSessionTool returned false")
	}
	afterTool := s.PolicySnapshot().Revision
	if afterTool <= afterExact {
		t.Fatalf("tool session mutation revision = %d, want > %d", afterTool, afterExact)
	}
	if got := s.Memory().CheckWithIdentity("Write", "anything", "anything"); got == nil || *got {
		t.Fatal("tool session deny was not applied")
	}

	if !s.RememberSessionRule("Bash(git diff*)", true) {
		t.Fatal("RememberSessionRule returned false")
	}
	if s.PolicySnapshot().Revision <= afterTool {
		t.Fatal("parsed session rule did not advance revision")
	}
}

func TestPermissionService_PolicySettersAdvanceRevisionOnlyOnChange(t *testing.T) {
	s := NewPermissionService(nil)
	assertRevisionChanges := func(name string, mutate func()) {
		t.Helper()
		before := s.PolicySnapshot().Revision
		mutate()
		if got := s.PolicySnapshot().Revision; got <= before {
			t.Fatalf("%s revision = %d, want > %d", name, got, before)
		}
		before = s.PolicySnapshot().Revision
		mutate()
		if got := s.PolicySnapshot().Revision; got != before {
			t.Fatalf("%s no-op revision = %d, want %d", name, got, before)
		}
	}

	assertRevisionChanges("spec stage", func() { s.SetSpecStage(safety.SpecStagePlan) })
	assertRevisionChanges("dry run", func() { s.SetDryRun(true) })
	assertRevisionChanges("spec slug", func() { s.SetSpecSlug("demo") })
	assertRevisionChanges("never allow", func() { s.SetNeverAllow([]string{"Bash(rm -rf *)"}) })
	assertRevisionChanges("spec test policy", func() { s.SetSpecAllowTests(true) })
}

func TestPermissionService_ConcurrentPolicyUpdates(t *testing.T) {
	s := NewPermissionService(nil)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			s.SetAutonomy(safety.AutonomyLevel(i % int(safety.AutonomyYOLO+1)))
			s.SetAllowedDirs([]string{"/workspace", "/tmp"})
		}(i)
		go func() {
			defer wg.Done()
			_ = s.EvaluateTool(ctx, safety.ToolCallInfo{Name: "Read"})
			_, _ = s.CheckTool(ctx, safety.ToolCallInfo{Name: "Read"})
			_ = s.PolicySnapshot()
		}()
	}
	wg.Wait()
}

func TestPermissionService_ConcurrentApprovalUpdates(t *testing.T) {
	s := NewPermissionService(nil)
	s.SetAutonomy(safety.AutonomyFull)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			s.SetApproval(&ApprovalGate{
				Enabled: true,
				ConfirmFn: func(ApprovalRequest) ApprovalResponse {
					return ApprovalReject
				},
			})
		}()
		go func() {
			defer wg.Done()
			_, _ = s.CheckApproval(context.Background(), "WebFetch", map[string]interface{}{"url": "https://example.com"})
		}()
	}
	wg.Wait()
}

func TestPermissionService_ApprovalEnabledDoesNotExposeGate(t *testing.T) {
	s := NewPermissionService(nil)
	if s.ApprovalEnabled() {
		t.Fatal("approval should be disabled by default")
	}
	s.SetApproval(&ApprovalGate{Enabled: true})
	if !s.ApprovalEnabled() {
		t.Fatal("approval should be enabled after installing an enabled gate")
	}
	s.SetApproval(&ApprovalGate{Enabled: false})
	if s.ApprovalEnabled() {
		t.Fatal("disabled gate should not report approval enabled")
	}
}

func TestPermissionService_PolicyViewIsPresentationSnapshot(t *testing.T) {
	s := NewPermissionService(nil)
	s.Memory().AlwaysAllowPattern("Bash:git status")
	store := newExactStore(t)
	s.SetExactRuleStore(store)
	if _, ok := s.RememberExact(stableid.KindCommand, "command\x00git status", "git status", stableid.Allow); !ok {
		t.Fatal("failed to add exact rule")
	}
	view := s.PolicyView()
	if len(view.Grants) == 0 {
		t.Fatal("policy view omitted the remembered grant")
	}
	view.Grants[0].Tool = "mutated"
	if got := s.PolicyView().Grants[0].Tool; got == "mutated" {
		t.Fatal("policy view exposed mutable engine grant storage")
	}
	if len(view.ExactRules) != 1 {
		t.Fatalf("policy view exact rules = %d, want 1", len(view.ExactRules))
	}
	view.ExactRules[0].DisplayIdentity = "mutated"
	if got := s.PolicyView().ExactRules[0].DisplayIdentity; got == "mutated" {
		t.Fatal("policy view exposed mutable exact-rule storage")
	}
}

func TestPermissionService_SetMemoryRewiresUnifiedGrants(t *testing.T) {
	s := NewPermissionService(nil)
	s.Memory().AlwaysAllow("Bash")
	if got := len(s.PolicyView().Grants); got != 1 {
		t.Fatalf("initial grant count = %d, want 1", got)
	}

	replacement := safety.NewPermissionMemory()
	s.SetMemory(replacement)
	if got := len(s.PolicyView().Grants); got != 0 {
		t.Fatalf("stale grant count after replacement = %d, want 0", got)
	}
	replacement.AlwaysDeny("Bash")
	view := s.PolicyView()
	if len(view.Grants) != 1 || view.Grants[0].Allow {
		t.Fatalf("replacement grant not visible through unified view: %+v", view.Grants)
	}
}

func TestPermissionService_ConcurrentEngineReplacementAndLogging(t *testing.T) {
	s := NewPermissionService(nil)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(4)
		go func() {
			defer wg.Done()
			s.WithEngine(nil)
		}()
		go func() {
			defer wg.Done()
			s.SetLogger(nil)
		}()
		go func() {
			defer wg.Done()
			_ = s.PolicySnapshot()
		}()
		go func() {
			defer wg.Done()
			_, _ = s.CheckTool(context.Background(), safety.ToolCallInfo{Name: "Read"})
		}()
	}
	wg.Wait()
}

func TestPermissionService_EngineReplacementPreservesExactRules(t *testing.T) {
	s := NewPermissionService(nil)
	store := permissions.NewStableRuleStore(t.TempDir() + "/stable-rules.json")
	if _, ok := store.Remember(stableid.KindFileMutation, "README.md", "README.md", stableid.Allow); !ok {
		t.Fatal("failed to create exact rule")
	}
	s.SetExactRuleStore(store)
	s.WithEngine(safety.NewPermissionEngine())

	decision := s.EvaluateTool(context.Background(), safety.ToolCallInfo{
		Name: "Write",
		Args: map[string]interface{}{"path": "README.md"},
	})
	if decision.Outcome != safety.DecisionAllow {
		t.Fatalf("engine replacement detached exact rule: %#v", decision)
	}
}

func TestPermissionService_PolicySnapshotPreservesExactRules(t *testing.T) {
	parent := NewPermissionService(nil)
	store := permissions.NewStableRuleStore(t.TempDir() + "/stable-rules.json")
	if _, ok := store.Remember(stableid.KindFileMutation, "config.env", "config.env", stableid.Deny); !ok {
		t.Fatal("failed to create exact deny rule")
	}
	parent.SetExactRuleStore(store)

	child := NewPermissionService(nil)
	child.ApplyPolicySnapshot(parent.PolicySnapshot())
	decision := child.CheckToolDecision(context.Background(), safety.ToolCallInfo{
		Name: "Write",
		Args: map[string]interface{}{"path": "config.env"},
	})
	if decision.Outcome != safety.DecisionDeny {
		t.Fatalf("child policy snapshot lost exact deny rule: %#v", decision)
	}
}

func TestPermissionService_ApplyPolicySnapshotCopiesRulesAndScopes(t *testing.T) {
	parent := NewPermissionService(nil)
	parent.Memory().AlwaysDeny("Write")
	parent.SetNeverAllow([]string{"Bash(git push *)"})
	parent.SetAllowedDirs([]string{"/workspace"})
	snapshot := parent.PolicySnapshot()
	child := NewPermissionService(nil)
	child.ApplyPolicySnapshot(snapshot)
	snapshot.AllowedDirs[0] = "/changed"
	if child.AllowedDirs()[0] != "/workspace" {
		t.Fatalf("child allowed dirs changed through snapshot alias: %v", child.AllowedDirs())
	}
	allowed, reason := child.CheckTool(context.Background(), safety.ToolCallInfo{Name: "Write"})
	if allowed || reason != "Permission denied (rule)." {
		t.Fatalf("child did not inherit deny rule: allowed=%v reason=%q", allowed, reason)
	}
	decision := child.CheckToolDecision(context.Background(), safety.ToolCallInfo{
		Name: "Bash", Args: map[string]interface{}{"command": "git push origin main"},
	})
	if decision.Outcome != safety.DecisionDeny {
		t.Fatalf("child lost never-allow ceiling: %#v", decision)
	}
}

func TestPermissionService_ApplyPolicySnapshotPreservesSpecProgress(t *testing.T) {
	parent := NewPermissionService(nil)
	parent.SetSpecSlug("demo")
	parent.AdvanceSpecStage("Proposal")
	parent.AdvanceSpecStage("Specify")
	parent.AdvanceSpecStage("Design")

	child := NewPermissionService(nil)
	child.ApplyPolicySnapshot(parent.PolicySnapshot())
	decision := child.EvaluateTool(context.Background(), safety.ToolCallInfo{Name: "Plan"})
	if decision.Outcome != safety.DecisionAllow {
		t.Fatalf("child lost completed Specify/Design stages: %#v", decision)
	}
}

func TestPermissionService_ResetSpecClearsCompletionState(t *testing.T) {
	s := NewPermissionService(nil)
	s.SetSpecSlug("demo")
	s.AdvanceSpecStage("Proposal")
	s.AdvanceSpecStage("Specify")
	s.AdvanceSpecStage("Design")
	s.ResetSpec()
	s.SetSpecSlug("new-demo")
	s.AdvanceSpecStage("Proposal")

	decision := s.EvaluateTool(context.Background(), safety.ToolCallInfo{Name: "Plan"})
	if decision.Outcome == safety.DecisionAllow {
		t.Fatalf("Plan remained allowed after spec reset: %#v", decision)
	}
}

func TestPermissionService_CopiedEvaluationsPreserveNeverAllow(t *testing.T) {
	s := NewPermissionService(nil)
	s.SetNeverAllow([]string{"Write(*.env)"})
	s.SetAutonomy(safety.AutonomyYOLO)
	call := safety.ToolCallInfo{Name: "Write", Args: map[string]interface{}{"path": ".env"}}

	for name, decision := range map[string]safety.Decision{
		"structured": s.CheckToolDecision(context.Background(), call),
		"preview":    s.EvaluateTool(context.Background(), call),
	} {
		if decision.Outcome != safety.DecisionDeny {
			t.Errorf("%s evaluation lost never-allow ceiling: %#v", name, decision)
		}
	}
}

func TestPermissionService_CheckApproval_NoGate(t *testing.T) {
	s := NewPermissionService(nil)
	approved, _ := s.CheckApproval(context.Background(), "Bash", map[string]interface{}{})
	if !approved {
		t.Error("expected approved when no gate is set")
	}
}

func TestPermissionService_NewReturnsReadyEngine(t *testing.T) {
	s := NewPermissionService(nil)
	if s.Memory() == nil {
		t.Error("NewPermissionService should produce a non-nil permission memory")
	}
}

func TestPermissionService_SetPermissionFn(t *testing.T) {
	s := NewPermissionService(nil)
	called := false
	s.SetPermissionFn(func(req safety.PermissionRequest) {
		called = true
	})
	if s.perm == nil || s.perm.PromptFn == nil {
		t.Error("SetPermissionFn should have set the engine's PromptFn")
	}
	// Call directly to verify.
	s.perm.PromptFn(safety.PermissionRequest{})
	if !called {
		t.Error("PromptFn was not called")
	}
}

func TestPermissionService_BypassOperationsHaveSingleAuthority(t *testing.T) {
	s := NewPermissionService(nil)
	expires := time.Now().Add(time.Minute)
	if !s.EnableBypass([]string{"bash"}, expires, "debug") {
		t.Fatal("EnableBypass reported no permission engine")
	}
	enabled, grant := s.BypassState()
	if !enabled || grant == nil {
		t.Fatalf("BypassState = enabled:%v grant:%#v, want scoped grant", enabled, grant)
	}
	if len(grant.Scope) != 1 || grant.Scope[0] != "bash" || grant.Reason != "debug" {
		t.Fatalf("unexpected bypass grant: %#v", grant)
	}
	if !s.DisableBypass() {
		t.Fatal("DisableBypass reported no permission engine")
	}
	if enabled, _ := s.BypassState(); enabled {
		t.Fatal("bypass remained enabled after DisableBypass")
	}
}

func TestPermissionService_BypassRejectsMissingOrExpiredJustification(t *testing.T) {
	s := NewPermissionService(nil)
	if s.EnableBypass(nil, time.Time{}, "") {
		t.Fatal("bypass enabled without a justification")
	}
	if enabled, _ := s.BypassState(); enabled {
		t.Fatal("invalid bypass request changed bypass state")
	}
	if s.EnableBypass(nil, time.Now().Add(-time.Second), "debug") {
		t.Fatal("bypass enabled with an expired timestamp")
	}
	if enabled, _ := s.BypassState(); enabled {
		t.Fatal("expired bypass request changed bypass state")
	}
}

func TestPermissionService_BypassStateExpiresLazily(t *testing.T) {
	s := NewPermissionService(nil)
	if !s.EnableBypass([]string{"bash"}, time.Now().Add(20*time.Millisecond), "stale") {
		t.Fatal("failed to seed time-bound bypass")
	}
	time.Sleep(30 * time.Millisecond)
	if enabled, grant := s.BypassState(); enabled || grant != nil {
		t.Fatalf("expired bypass state = enabled=%v grant=%#v, want disabled", enabled, grant)
	}
	if enabled, grant := s.BypassState(); enabled || grant != nil {
		t.Fatalf("expired bypass remained after cleanup: enabled=%v grant=%#v", enabled, grant)
	}
}
