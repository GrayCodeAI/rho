package safety

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	contracts "github.com/GrayCodeAI/rho/internal/contracts/policy"
	"github.com/GrayCodeAI/rho/internal/governance"
	"github.com/GrayCodeAI/rho/internal/hooks"
	"github.com/GrayCodeAI/rho/internal/observability/metrics"
	"github.com/GrayCodeAI/rho/internal/permissions"
	"github.com/GrayCodeAI/rho/internal/permissions/stableid"
	"github.com/GrayCodeAI/rho/internal/tool"
)

var reNeedsClarify = regexp.MustCompile(`\[NEEDS CLARIFICATION.*?\]`)

// SpecStage tracks position in the independent spec-driven-development
// workflow. It is orthogonal to AutonomyLevel: a session can be at any
// trust tier while at any spec stage — trust governs *how* a tool call is
// approved, spec stage governs *which* tools are relevant to call right now.
type SpecStage int

const (
	SpecStageNone SpecStage = iota // no active spec workflow
	SpecStageProposal
	SpecStageSpecify
	SpecStageDesign
	SpecStagePlan
	SpecStageTasks
	SpecStageImplementing
)

// PermissionEngine encapsulates all permission-checking logic.
// Extracted from Session to keep the god object lean.
type PermissionEngine struct {
	Memory           *PermissionMemory
	Classifier       *permissions.Classifier
	BypassKill       *permissions.BypassKillswitch
	Autonomy         AutonomyLevel
	AutonomyExplicit bool
	// Revision increments when policy configuration is replaced. It is
	// attached to decisions so audit consumers can correlate evaluations.
	Revision uint64
	Stage    SpecStage
	specDone specDone
	// DryRun is a global kill switch: when true, every tool call is denied
	// unconditionally, regardless of tier or spec stage. Replaces the old
	// PermissionModeDontAsk's hard-lockout role — that mode was otherwise
	// redundant with tier-based asking, but this orthogonal, always-deny
	// behavior (mainly for CI/headless "preview only, never execute" runs)
	// had no equivalent once Mode was removed.
	DryRun bool
	// SpecSlug is the directory name (under .rho/specs/) for the active
	// spec workflow, set by the Specify tool. Lives here (session-scoped,
	// via PermissionEngine) rather than as a package-level variable in
	// internal/tool, so concurrent sessions/sub-agents in the same process
	// never share or clobber each other's spec directory.
	SpecSlug string

	// Phase gates sequential task completion within the Implementing stage.
	// 0 means no phase gating (default); 1+ means the model should complete
	// Phase N before progressing to N+1.
	Phase    int
	Phases   int                     // total number of phases detected from tasks.md
	PromptFn func(PermissionRequest) // callback to ask user
	// Governance is the POLICY ∩ PROFILE ceiling. It is evaluated before
	// every other gate (hooks, spec stage, rules, autonomy, bypass) so no
	// agent state or user-granted bypass can loosen an administrator-set
	// ceiling. Nil means fail-open (no governance policy installed).
	Governance *governance.Engine
	// UnifiedGrants is the precedence-ordered view of session permission rules.
	// When non-nil it replaces the direct Memory.Check lookup.
	UnifiedGrants *permissions.UnifiedGrants
	// ExactRules is the optional persisted project rule store. It is kept
	// separate from session memory so durable rules are explicit and auditable.
	ExactRules *permissions.StableRuleStore
	// Profile is the per-flag autonomy profile consulted by the decision path.
	// When non-nil its NeedsPermission is the single autonomy policy path.
	Profile *AutonomyProfile
	// KnownTools is the registered execution surface. Built-in capability
	// metadata is still required for rich policy decisions, but registered
	// custom tools are legitimate and must not be mistaken for unregistered
	// tools merely because they have no static policy entry.
	KnownTools map[string]struct{}
	// UntrustedTools are registered external tools (currently MCP). They may
	// execute after an explicit remembered rule or user approval, but registry
	// membership and autonomy never implicitly authorize them.
	UntrustedTools map[string]struct{}
	// neverAllow is the personal hard-ceiling rule set (deny rules that even
	// YOLO + bypass cannot override). Parsed from settings.NeverAllow.
	neverAllow []string
	// neverAllowMu guards neverAllow; set via SetNeverAllow under the engine's
	// existing mutation points.
	neverAllowMu sync.RWMutex
	// specAllowTests enables safe test commands during the spec workflow.
	specAllowTests bool
	// Metrics records every decision for telemetry. Nil means no telemetry.
	Metrics *metrics.PermissionMetrics
	// auditLog is the in-memory ring buffer of recent decisions surfaced via
	// /autonomy audit. Nil disables the audit trail.
	auditLog *permissionAuditLog
}

// DecisionOutcome is the result of evaluating a tool request.
type DecisionOutcome string

const (
	DecisionAllow DecisionOutcome = "allow"
	DecisionAsk   DecisionOutcome = "ask"
	DecisionDeny  DecisionOutcome = "deny"
)

// DecisionReason is stable metadata for callers, telemetry, and tests. The
// human-readable message remains available through Decision.Message.
type DecisionReason string

const (
	ReasonDryRun            DecisionReason = "dry_run"
	ReasonHookDenied        DecisionReason = "hook_denied"
	ReasonSpecGate          DecisionReason = "spec_gate"
	ReasonRuleDenied        DecisionReason = "rule_denied"
	ReasonRuleAllowed       DecisionReason = "rule_allowed"
	ReasonGrantAllowed      DecisionReason = "grant_allowed"
	ReasonGrantDenied       DecisionReason = "grant_denied"
	ReasonAutonomy          DecisionReason = "autonomy"
	ReasonBypass            DecisionReason = "bypass"
	ReasonUserPrompt        DecisionReason = "user_prompt"
	ReasonPromptUnavailable DecisionReason = "prompt_unavailable"
	ReasonGovernance        DecisionReason = "governance"
)

// Decision is the structured result of a permission evaluation.
type Decision struct {
	Outcome      DecisionOutcome
	Reason       DecisionReason
	Message      string
	Capabilities []Capability
	Risk         RiskLevel
	Revision     uint64
}

// PolicySnapshot captures the scalar policy state for one tool evaluation.
// Callers can use it to ensure a request is evaluated consistently even when
// the live session settings change while the tool is running.
type PolicySnapshot struct {
	Autonomy         AutonomyLevel
	AutonomyExplicit bool
	Stage            SpecStage
	DryRun           bool
	SpecSlug         string
	// SpecDone is the serialized completion bitmask for the parallel
	// Specify/Design stages. It is intentionally an int at the snapshot
	// boundary because the bitmask type is private to this package.
	SpecDone       int
	SpecAllowTests bool
	Phase          int
	Phases         int
	Revision       uint64
	Rules          RuleSnapshot
	// ExactRules is the shared project-level exact-rule store. It is
	// internally synchronized and must remain attached to child sessions.
	ExactRules *permissions.StableRuleStore
	// NeverAllow is the user's hard permission ceiling. It must travel with
	// snapshots so child sessions and cross-goroutine evaluations cannot lose
	// a deny rule that autonomy and bypass are forbidden to override.
	NeverAllow  []string
	AllowedDirs []string
}

// Snapshot returns a copy of the engine's request-relevant scalar policy.
func (pe *PermissionEngine) Snapshot() PolicySnapshot {
	var rules RuleSnapshot
	if pe.Memory != nil {
		rules = pe.Memory.Snapshot()
	}
	return PolicySnapshot{
		Autonomy: pe.Autonomy, AutonomyExplicit: pe.AutonomyExplicit,
		Stage: pe.Stage, DryRun: pe.DryRun,
		SpecSlug: pe.SpecSlug, SpecDone: int(pe.specDone), SpecAllowTests: pe.specAllowTests,
		Phase: pe.Phase, Phases: pe.Phases, Revision: pe.Revision,
		Rules: rules, ExactRules: pe.ExactRules, NeverAllow: pe.NeverAllow(),
	}
}

// Copy returns an evaluation copy safe to construct without copying the
// engine's mutex. Concurrency-safe services (Memory, UnifiedGrants,
// Governance, Classifier, BypassKill, Metrics, and Profile) remain shared;
// scalar policy fields and the never-allow slice are copied. PromptFn is
// preserved.
func (pe *PermissionEngine) Copy() *PermissionEngine {
	if pe == nil {
		return nil
	}
	return pe.clone()
}

// clone copies the evaluation configuration without copying the mutex. The
// engine is intentionally composed of scalar policy state plus shared,
// concurrency-safe services; keeping this in one place prevents Copy,
// snapshot evaluation, and preview evaluation from drifting apart as new
// policy fields are added.
func (pe *PermissionEngine) clone() *PermissionEngine {
	if pe == nil {
		return nil
	}
	return &PermissionEngine{
		Autonomy:         pe.Autonomy,
		AutonomyExplicit: pe.AutonomyExplicit,
		Stage:            pe.Stage,
		DryRun:           pe.DryRun,
		SpecSlug:         pe.SpecSlug,
		specDone:         pe.specDone,
		Phase:            pe.Phase,
		Phases:           pe.Phases,
		Revision:         pe.Revision,
		Memory:           pe.Memory,
		ExactRules:       pe.ExactRules,
		UnifiedGrants:    pe.UnifiedGrants,
		Profile:          pe.Profile,
		KnownTools:       pe.KnownTools,
		UntrustedTools:   pe.UntrustedTools,
		Governance:       pe.Governance,
		Classifier:       pe.Classifier,
		BypassKill:       pe.BypassKill,
		PromptFn:         pe.PromptFn,
		Metrics:          pe.Metrics,
		auditLog:         pe.auditLog,
		specAllowTests:   pe.specAllowTests,
		neverAllow:       pe.NeverAllow(),
	}
}

// NewPermissionEngine creates a PermissionEngine with sensible defaults.
func NewPermissionEngine() *PermissionEngine {
	pe := &PermissionEngine{
		Memory:     NewPermissionMemory(),
		Classifier: permissions.NewClassifier(),
		BypassKill: permissions.NewBypassKillswitch(),
		Governance: governance.New(),
	}
	pe.UnifiedGrants = permissions.NewUnifiedGrants(pe.Memory)
	pe.Metrics = metrics.NewPermissionMetrics()
	pe.auditLog = newPermissionAuditLog(256)
	pe.Profile = ProfileFromLevel(pe.Autonomy)
	return pe
}

// CheckTool determines if a tool call is allowed, denied, or needs user prompt.
// Returns (granted bool, denyReason string).
// If the user must be asked, it blocks on PromptFn with a 5-minute timeout.
//
// Order (Year 0 PACK-04):
//  1. DryRun
//  2. PreToolUse decision hooks (can deny before autonomy short-circuits)
//  3. Spec-stage gate
//  4. Autonomy / bypass / classifier / auto-mode / memory / user prompt
func (pe *PermissionEngine) CheckTool(ctx context.Context, tc ToolCallInfo) (bool, string) {
	d := pe.CheckToolDecision(ctx, tc)
	return d.Outcome == DecisionAllow, d.Message
}

// CheckToolSnapshot evaluates a request using the supplied immutable scalar
// policy snapshot. Mutable rule stores and the prompt callback remain owned by
// the engine so remembered decisions and user approval keep their semantics.
func (pe *PermissionEngine) CheckToolSnapshot(ctx context.Context, tc ToolCallInfo, snapshot PolicySnapshot) Decision {
	// Build a fresh engine from the snapshot instead of copying *pe. Copying
	// would clone the sync.RWMutex; clone copies only the non-locking
	// configuration and the snapshot then replaces the mutable policy inputs.
	clone := pe.clone()
	clone.Autonomy = snapshot.Autonomy
	clone.AutonomyExplicit = snapshot.AutonomyExplicit
	clone.Stage = snapshot.Stage
	clone.DryRun = snapshot.DryRun
	clone.SpecSlug = snapshot.SpecSlug
	clone.specDone = specDone(snapshot.SpecDone)
	clone.specAllowTests = snapshot.SpecAllowTests
	clone.Phase = snapshot.Phase
	clone.Phases = snapshot.Phases
	clone.Revision = snapshot.Revision
	clone.Memory = NewPermissionMemoryFromSnapshot(snapshot.Rules)
	clone.neverAllow = append([]string(nil), snapshot.NeverAllow...)
	clone.ExactRules = snapshot.ExactRules
	// Rebuild UnifiedGrants so it wraps the snapshot's Memory (not the
	// original engine's live Memory, which may differ from the snapshot).
	if clone.UnifiedGrants != nil {
		clone.UnifiedGrants = permissions.NewUnifiedGrants(clone.Memory)
	}
	return clone.CheckToolDecision(ctx, tc)
}

// CheckToolDecision returns structured policy metadata while preserving the
// existing permission behavior and human-readable messages.
func (pe *PermissionEngine) CheckToolDecision(ctx context.Context, tc ToolCallInfo) Decision {
	d := pe.evaluateToolDecision(ctx, tc, true)
	return pe.decorateDecision(tc, d, true)
}

func (pe *PermissionEngine) decorateDecision(tc ToolCallInfo, d Decision, record bool) Decision {
	policy := ToolPolicyFor(tc.Name)
	d.Capabilities = policy.Capabilities
	d.Risk = ActionRisk(tc.Name, tc.Args)
	d.Revision = pe.Revision
	// Record telemetry + audit trail only for an execution check. Preview
	// evaluation must remain observational; otherwise a caller that renders a
	// policy preview changes the audit history and decision counters.
	if record && pe.Metrics != nil {
		outcome := string(d.Outcome)
		pe.Metrics.RecordDecision(outcome, string(d.Reason))
	}
	if record && pe.auditLog != nil {
		pe.auditLog.record(tc.Name, ToolSummary(tc.Name, tc.Args), d.Outcome, d.Reason)
	}
	return d
}

// EvaluateTool performs policy evaluation without waiting for UI approval.
// It returns DecisionAsk when the only remaining step is user approval.
// CheckToolDecision remains the compatibility API that performs the prompt.
func (pe *PermissionEngine) EvaluateTool(ctx context.Context, tc ToolCallInfo) Decision {
	// Build a fresh engine with PromptFn nil (so it returns Ask instead of
	// blocking). We avoid copying *pe to dodge the vet copylocks warning from
	// the embedded sync.RWMutex.
	clone := pe.clone()
	clone.PromptFn = nil
	d := clone.evaluateToolDecision(ctx, tc, false)
	d = clone.decorateDecision(tc, d, false)
	if d.Reason == ReasonPromptUnavailable {
		d.Outcome = DecisionAsk
		d.Reason = ReasonUserPrompt
		d.Message = "Permission approval required."
	}
	return d
}

func (pe *PermissionEngine) evaluateToolDecision(ctx context.Context, tc ToolCallInfo, recordTelemetry bool) Decision {
	if pe.DryRun {
		return Decision{Outcome: DecisionDeny, Reason: ReasonDryRun, Message: "dry-run: tool execution disabled"}
	}

	// Governance ceiling — evaluated first so no later gate (hooks, spec
	// stage, remembered rules, autonomy, bypass kill-switch) can override an
	// administrator-set POLICY ∩ PROFILE decision. This is the un-disableable
	// org ceiling: deny in either layer denies regardless of what the agent
	// or user grants at lower layers.
	if pe.Governance != nil {
		if d := pe.Governance.Evaluate(tc.Name, ToolSummary(tc.Name, tc.Args)); !d.Allowed {
			if recordTelemetry && pe.Metrics != nil {
				pe.Metrics.RecordGovernanceDenial(tc.Name)
			}
			return Decision{Outcome: DecisionDeny, Reason: ReasonGovernance, Message: d.Reason}
		}
	}

	// Personal hard ceiling — evaluated before hooks and spec workflow gates.
	// A never-rule is the user's unconditional deny and must not be bypassed by
	// a workflow allow, autonomy tier, remembered grant, or break-glass switch.
	if denied, spec := pe.checkNeverAllow(tc.Name, ToolSummary(tc.Name, tc.Args)); denied {
		return Decision{Outcome: DecisionDeny, Reason: ReasonRuleDenied, Message: "denied by personal ceiling: " + spec}
	}

	toolName := canonicalToolName(tc.Name)

	// PreToolUse decision hooks — deny gate before autonomy. Hooks that
	// return allow/nil do not grant permission by themselves; they only
	// short-circuit when ActionDeny (or equivalent).
	if denied, reason := pe.checkPreToolHooks(tc); denied {
		return Decision{Outcome: DecisionDeny, Reason: ReasonHookDenied, Message: reason}
	}

	// Spec-stage gate — independent of trust tier, so no autonomy level can
	// bypass it. While a spec workflow is active and not yet approved for
	// implementation, only the workflow's own tools and reads may proceed.
	if pe.Stage != SpecStageNone && pe.Stage != SpecStageImplementing {
		switch toolName {
		case "Proposal", "Specify", "Design", "Plan", "Tasks", "AskUserQuestion", "SpecStatus", "SpecEdit", "SpecList", "SpecReset", "SpecConfig", "Clarify", "Analyze", "Checklist", "Constitution", "Converge":
			if !pe.specToolAllowed(toolName) {
				return Decision{Outcome: DecisionDeny, Reason: ReasonSpecGate, Message: pe.specStageReason(toolName)}
			}
			return Decision{Outcome: DecisionAllow, Reason: ReasonSpecGate}
		case "ApproveImplementation":
			if pe.Stage != SpecStageTasks {
				return Decision{Outcome: DecisionDeny, Reason: ReasonSpecGate, Message: "Spec stage active: ApproveImplementation is available only after Tasks completes."}
			}
			return pe.promptDecisionWithSummary(ctx, tc, specApprovalSummary(pe.SpecSlug))
		default:
			if tool.IsReadOnly(tc.Name) {
				return Decision{Outcome: DecisionAllow, Reason: ReasonSpecGate}
			}
			// When SpecAllowTests is enabled, safe test commands are permitted
			// during the spec workflow so the agent can verify its spec work.
			if pe.specAllowTests && tc.Name == "Bash" {
				if cmd, ok := tc.Args["command"].(string); ok && isSafeTestCommand(cmd) {
					return Decision{Outcome: DecisionAllow, Reason: ReasonSpecGate}
				}
			}
			return Decision{Outcome: DecisionDeny, Reason: ReasonSpecGate, Message: "Spec stage active: only spec workflow tools (and reads) are allowed until ApproveImplementation."}
		}
	}

	summary := ToolSummary(tc.Name, tc.Args)

	// Destructive commands are hard-blocked regardless of autonomy, rule
	// memory, or the bypass kill-switch (H6). The tool layer independently
	// rejects them (IsDestructiveCommand in BashTool.Execute), but failing
	// closed here too keeps the policy engine authoritative and prevents the
	// bypass from even appearing to grant destructive commands. Placed before
	// the rule/auto/autonomy allow paths so nothing can override it.
	if toolName == "Bash" || toolName == "PowerShell" {
		if cmd, ok := tc.Args["command"].(string); ok && (tool.IsDestructiveCommand(cmd) || (toolName == "PowerShell" && tool.IsPowerShellDestructive(cmd))) {
			return Decision{Outcome: DecisionDeny, Reason: ReasonRuleDenied, Message: "denied: destructive command is blocked even with bypass/autonomy enabled"}
		}
	}

	// Persisted exact rules are explicit user policy. They are checked before
	// broader session grants so a durable deny cannot be shadowed by a learned
	// allow, and a durable allow remains exact rather than becoming a glob.
	if pe.ExactRules != nil {
		if decision, found := pe.resolveExactRule(tc.Name, toolName, ToolIdentity(tc.Name, tc.Args), summary); found {
			if decision == stableid.Allow {
				return Decision{Outcome: DecisionAllow, Reason: ReasonGrantAllowed, Message: "Permission allowed by persisted exact rule."}
			}
			return Decision{Outcome: DecisionDeny, Reason: ReasonGrantDenied, Message: "Permission denied by persisted exact rule."}
		}
	}
	// Explicit remembered decisions are policy rules. They must be consulted
	// before autonomy can short-circuit the request, especially for deny rules.
	// When UnifiedGrants is wired up it provides one precedence-ordered lookup:
	// deny > allow, most
	// specific wins. Otherwise fall back to the legacy separate lookups so the
	// field can be rolled out gradually.
	if pe.UnifiedGrants != nil {
		if allowed, found := pe.UnifiedGrants.CheckWithIdentity(toolName, summary, ToolIdentity(tc.Name, tc.Args), time.Now()); found {
			if allowed {
				return Decision{Outcome: DecisionAllow, Reason: ReasonGrantAllowed}
			}
			return Decision{Outcome: DecisionDeny, Reason: ReasonGrantDenied, Message: "Permission denied (rule)."}
		}
	} else {
		var memoryDecision *bool
		if pe.Memory != nil {
			memoryDecision = pe.Memory.CheckWithIdentity(tc.Name, summary, ToolIdentity(tc.Name, tc.Args))
		}
		if memoryDecision != nil && !*memoryDecision {
			return Decision{Outcome: DecisionDeny, Reason: ReasonRuleDenied, Message: "Permission denied (rule)."}
		}
		if memoryDecision != nil && *memoryDecision {
			return Decision{Outcome: DecisionAllow, Reason: ReasonRuleAllowed}
		}
	}
	// External tools do not get implicit trust from being registered. An
	// explicit remembered rule above is still honored; otherwise they follow
	// the normal approval path even under YOLO or bypass mode.
	if pe.isUntrustedTool(tc.Name) {
		return pe.promptDecision(ctx, tc)
	}
	// Unknown/custom tools have no trusted capability declaration. Never let
	// autonomy or the bypass switch turn an unregistered tool into an implicit
	// allow; an explicit remembered rule may authorize it above, otherwise it
	// must go through the human approval path (or fail closed headlessly).
	if !pe.isRegisteredOrPolicyKnown(tc.Name) {
		return pe.promptDecision(ctx, tc)
	}

	// Keep the profile's level in sync with the engine's current Autonomy so
	// direct field assignments (e.g. in tests or legacy callers) take effect.
	if pe.Profile != nil && pe.Profile.Level != pe.Autonomy {
		overrides := pe.Profile.Overrides()
		pe.Profile = ProfileFromLevel(pe.Autonomy)
		// Re-apply user overrides after deriving the new tier defaults. A tier
		// change must not silently erase explicit per-flag policy.
		pe.Profile.ApplyOverrides(overrides)
	}

	isSafe := !ToolNeedsPermission(tc.Name, tc.Args)
	// The shell fast path is allowlisted, not merely “not suspicious”. This
	// keeps unknown Bash syntax and all PowerShell syntax out of unattended
	// execution while preserving the low-friction path for common read-only
	// commands at the Full tier.
	if toolName == "Bash" {
		isSafe = pe.Classifier != nil && pe.Classifier.Classify(summary) == "safe"
	} else if toolName == "PowerShell" {
		isSafe = false
	}
	// When a Profile is active (always, after NewPermissionEngine) it consults
	// The profile is normally initialized by NewPermissionEngine. Construct a
	// local default only for lightweight callers that build the engine directly.
	profile := pe.Profile
	if profile == nil {
		profile = ProfileFromLevel(pe.Autonomy)
	}
	if !profile.NeedsPermission(tc.Name, isSafe) {
		return Decision{Outcome: DecisionAllow, Reason: ReasonAutonomy}
	}
	if pe.BypassKill != nil && pe.BypassKill.IsEnabled() {
		// Audit bypass usage so there is a record of every tool call the
		// kill-switch approved (H6). Note the destructive-command hard-deny
		// above still applies: bypass cannot grant destructive commands.
		now := time.Now()
		grant := pe.BypassKill.Grant()
		scope := "all"
		if grant != nil {
			// Time-bound bypass: auto-expire.
			if grant.IsExpired(now) {
				if recordTelemetry {
					pe.BypassKill.Disable()
				}
				return pe.promptDecision(ctx, tc)
			}
			// Scoped bypass: only cover matching categories.
			cat := permissions.ToolCategory(tc.Name)
			if !grant.Covers(cat) {
				return pe.promptDecision(ctx, tc)
			}
			if len(grant.Scope) > 0 {
				scope = strings.Join(grant.Scope, ",")
			}
		}
		if recordTelemetry {
			slog.Warn("permission bypass used", "tool", tc.Name, "summary", summary, "scope", scope)
		}
		if recordTelemetry && pe.Metrics != nil {
			pe.Metrics.RecordBypass(scope)
		}
		return Decision{Outcome: DecisionAllow, Reason: ReasonBypass, Message: "bypass: permission checks bypassed"}
	}
	return pe.promptDecision(ctx, tc)
}

func (pe *PermissionEngine) isUntrustedTool(name string) bool {
	if pe == nil || pe.UntrustedTools == nil {
		return false
	}
	if _, ok := pe.UntrustedTools[strings.TrimSpace(name)]; ok {
		return true
	}
	_, ok := pe.UntrustedTools[canonicalToolName(name)]
	return ok
}

func (pe *PermissionEngine) isRegisteredOrPolicyKnown(name string) bool {
	if isKnownTool(name) {
		return true
	}
	if pe == nil || pe.KnownTools == nil {
		return false
	}
	if _, ok := pe.KnownTools[canonicalToolName(name)]; ok {
		return true
	}
	_, ok := pe.KnownTools[strings.TrimSpace(name)]
	return ok
}

func (pe *PermissionEngine) resolveExactRule(rawName, toolName, identity, summary string) (stableid.Decision, bool) {
	kind := stableid.KindStructuredTool
	switch toolName {
	case "Bash", "PowerShell":
		kind = stableid.KindCommand
	case "Write", "Edit", "NotebookEdit":
		kind = stableid.KindFileMutation
	}
	canonical := kind.String() + "\x00" + identity
	if decision, ok := pe.ExactRules.Resolve(kind, canonical); ok {
		return decision, true
	}
	// Accept the pre-prefix form used by the original exact-rule API so an
	// upgrade does not silently discard a user's existing project policy.
	if decision, ok := pe.ExactRules.Resolve(kind, identity); ok {
		return decision, true
	}
	if rawName != toolName {
		if decision, ok := pe.ExactRules.Resolve(stableid.KindStructuredTool, rawName); ok {
			return decision, true
		}
	}
	return stableid.Deny, false
}

// SetNeverAllow replaces the personal hard-ceiling rule set. Each spec has the
// same format as PermissionMemory.AllowSpec: "Bash(rm -rf *)", "Write(*.env)",
// or "Delete" (tool-wide).
func (pe *PermissionEngine) SetNeverAllow(specs []string) {
	pe.neverAllowMu.Lock()
	defer pe.neverAllowMu.Unlock()
	pe.neverAllow = append([]string(nil), specs...)
}

// NeverAllow returns a copy of the current never-allow rules.
func (pe *PermissionEngine) NeverAllow() []string {
	pe.neverAllowMu.RLock()
	defer pe.neverAllowMu.RUnlock()
	return append([]string(nil), pe.neverAllow...)
}

// checkNeverAllow reports whether a tool call matches a never-rule. Returns
// (denied, matchedSpec).
func (pe *PermissionEngine) checkNeverAllow(toolName, summary string) (bool, string) {
	pe.neverAllowMu.RLock()
	rules := pe.neverAllow
	pe.neverAllowMu.RUnlock()

	canon := canonicalToolName(toolName)
	for _, spec := range rules {
		tool, pattern := parseRuleSpec(spec)
		if tool != "*" && tool != canon {
			continue
		}
		// Empty pattern means tool-wide (e.g. "Delete" blocks all Delete calls).
		if pattern == "" || matchRulePattern(pattern, summary) {
			return true, spec
		}
	}
	return false, ""
}

// SetSpecAllowTests enables or disables safe test commands during the spec
// workflow.
func (pe *PermissionEngine) SetSpecAllowTests(allow bool) {
	pe.specAllowTests = allow
}

// isSafeTestCommand reports whether a Bash command is a safe test invocation
// (read-only, non-destructive). Used to gate test runs during spec stage.
func isSafeTestCommand(cmd string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(cmd))
	safePrefixes := []string{
		"go test", "npm test", "npm run test", "pytest", "cargo test",
		"mix test", "bun test", "deno test", "npx jest", "npx vitest",
		"make test", "bundle exec rspec",
	}
	for _, prefix := range safePrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

// AuditLog returns the engine's audit log, or nil if disabled.
func (pe *PermissionEngine) AuditLog() *permissionAuditLog {
	return pe.auditLog
}

// PermissionMetrics returns the engine's metrics, or nil if disabled.
func (pe *PermissionEngine) PermissionMetrics() *metrics.PermissionMetrics {
	return pe.Metrics
}

func (pe *PermissionEngine) specToolAllowed(toolName string) bool {
	switch toolName {
	case "Proposal":
		return pe.Stage == SpecStageNone || pe.Stage == SpecStageProposal
	case "Specify", "Design":
		if pe.SpecSlug != "" && !pe.constitutionExists() {
			return false
		}
		return pe.Stage >= SpecStageProposal && pe.Stage < SpecStagePlan
	case "Plan":
		return pe.specDone&(doneSpecify|doneDesign) == doneSpecify|doneDesign
	case "Tasks":
		return pe.Stage == SpecStagePlan
	default:
		return true
	}
}

func (pe *PermissionEngine) constitutionExists() bool {
	if pe.SpecSlug == "" {
		return false
	}
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	path := filepath.Join(cwd, ".rho", "specs", pe.SpecSlug, "constitution.md")
	_, err = os.Stat(path)
	return err == nil
}

func (pe *PermissionEngine) phaseGatesPass() bool {
	if pe.SpecSlug == "" {
		return false
	}
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	planPath := filepath.Join(cwd, ".rho", "specs", pe.SpecSlug, "plan.md")
	data, err := os.ReadFile(planPath)
	if err != nil {
		return false
	}
	content := strings.ToLower(string(data))
	hasSimplicity := strings.Contains(content, "simplicity") || strings.Contains(content, "≤3") || strings.Contains(content, "<=3")
	hasAntiAbstraction := strings.Contains(content, "anti-abstraction") || strings.Contains(content, "framework directly")
	hasIntegrationFirst := strings.Contains(content, "integration-first") || strings.Contains(content, "contract")
	hasComplexityTracking := strings.Contains(content, "complexity tracking") || strings.Contains(content, "justification")
	return hasSimplicity && hasAntiAbstraction && hasIntegrationFirst && hasComplexityTracking
}

func (pe *PermissionEngine) unresolvedClarifications() int {
	if pe.SpecSlug == "" {
		return 0
	}
	cwd, err := os.Getwd()
	if err != nil {
		return 0
	}
	specPath := filepath.Join(cwd, ".rho", "specs", pe.SpecSlug, "spec.md")
	data, err := os.ReadFile(specPath)
	if err != nil {
		return 0
	}
	matches := reNeedsClarify.FindAllString(string(data), -1)
	return len(matches)
}

func (pe *PermissionEngine) specStageReason(toolName string) string {
	switch toolName {
	case "Proposal":
		return "Proposal is only available when no spec workflow is active."
	case "Specify", "Design":
		if pe.SpecSlug != "" && !pe.constitutionExists() {
			return "Constitution required: call Constitution tool with action='init' before Specify/Design."
		}
		return "Spec stage active: Specify/Design require Proposal and must complete before Plan."
	case "Plan":
		if pe.specDone&(doneSpecify|doneDesign) != doneSpecify|doneDesign {
			return "Spec stage active: Plan requires both Specify and Design to be complete."
		}
		if pe.unresolvedClarifications() > 0 {
			return fmt.Sprintf("Spec stage active: resolve %d [NEEDS CLARIFICATION] marker(s) before advancing to Plan.", pe.unresolvedClarifications())
		}
		return "Spec stage active: Plan phase gates not documented."
	case "Tasks":
		return "Spec stage active: Tasks is available only after Plan completes."
	default:
		return "Spec stage active: tool is not available at the current stage."
	}
}

// checkPreToolHooks runs decision hooks for PreToolUse / pre_tool.
// Returns (denied, reason).
func (pe *PermissionEngine) checkPreToolHooks(tc ToolCallInfo) (bool, string) {
	data := map[string]interface{}{
		"tool":    tc.Name,
		"tool_id": tc.ID,
		"args":    tc.Args,
	}
	// Matchers accepting PreToolUse or pre_tool both match via CanonicalEvent.
	d := hooks.ExecuteDecisionHooks(string(hooks.EventPreTool), data)
	if d == nil {
		return false, ""
	}
	switch d.Action {
	case hooks.ActionDeny:
		msg := d.Message
		if msg == "" {
			msg = d.Reason
		}
		if msg == "" {
			msg = "Permission denied (PreToolUse hook)."
		}
		return true, msg
	default:
		// allow / modify / instruct: do not grant; continue pipeline.
		// Modify of args is applied later by stream layer if needed.
		return false, ""
	}
}

func (pe *PermissionEngine) promptDecision(ctx context.Context, tc ToolCallInfo) Decision {
	return pe.promptDecisionWithSummary(ctx, tc, ToolSummary(tc.Name, tc.Args))
}

func (pe *PermissionEngine) promptDecisionWithSummary(ctx context.Context, tc ToolCallInfo, summary string) Decision {
	if pe.PromptFn == nil {
		return Decision{Outcome: DecisionDeny, Reason: ReasonPromptUnavailable, Message: "Permission prompt unavailable."}
	}
	resp := make(chan bool, 1)
	pe.PromptFn(PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{
			ToolName: tc.Name,
			ToolID:   tc.ID,
			Summary:  summary,
			Identity: ToolIdentity(tc.Name, tc.Args),
		},
		Response: resp,
	})
	select {
	case allowed := <-resp:
		if !allowed {
			return Decision{Outcome: DecisionDeny, Reason: ReasonUserPrompt, Message: "Permission denied by user."}
		}
		return Decision{Outcome: DecisionAllow, Reason: ReasonUserPrompt}
	case <-ctx.Done():
		return Decision{Outcome: DecisionDeny, Reason: ReasonUserPrompt, Message: "Permission prompt cancelled."}
	case <-time.After(5 * time.Minute):
		return Decision{Outcome: DecisionDeny, Reason: ReasonUserPrompt, Message: "Permission prompt timed out."}
	}
}

// detectPhases counts numbered phase sections in tasks.md.
func detectPhases(slug string) int {
	if slug == "" {
		return 0
	}
	cwd, err := os.Getwd()
	if err != nil {
		return 0
	}
	tasksPath := filepath.Join(cwd, ".rho", "specs", slug, "tasks.md")
	data, err := os.ReadFile(tasksPath) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return 0
	}
	return len(rePhaseSection.FindAllString(string(data), -1))
}

var rePhaseSection = regexp.MustCompile(`(?m)^## \d+\.`)

// specApprovalSummary reads spec.md/plan.md/tasks.md from the active spec's
// directory and builds a short preview for the ApproveImplementation
// prompt, so approving isn't a blind yes/no — the user sees what they're
// actually signing off on. Falls back to a plain name if the slug is empty
// or the files can't be read (e.g. deleted after being written).
func specApprovalSummary(slug string) string {
	phases := detectPhases(slug)
	if slug == "" {
		return "ApproveImplementation"
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "ApproveImplementation"
	}
	dir := filepath.Join(cwd, ".rho", "specs", slug)

	var b strings.Builder
	for _, f := range []string{"proposal.md", "spec.md", "design.md", "plan.md", "tasks.md"} {
		content, err := os.ReadFile(filepath.Join(dir, f)) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
		if err != nil {
			continue
		}
		preview := strings.TrimSpace(string(content))
		const maxPreview = 300
		if len(preview) > maxPreview {
			preview = preview[:maxPreview] + "..."
		}
		fmt.Fprintf(&b, "## %s\n%s\n\n", f, preview)
	}
	if phases > 0 {
		fmt.Fprintf(&b, "Phases: %d\n", phases)
	}
	if b.Len() == 0 {
		return "ApproveImplementation"
	}
	return strings.TrimSpace(b.String())
}

// AdvancePhase increments the phase gate when the model completes the
// current phase's tasks and calls AdvancePhase.
func (pe *PermissionEngine) AdvancePhase() {
	if pe.Phase < pe.Phases {
		pe.Phase++
	}
}

// PhaseProgress returns a summary of phase completion for display.
func (pe *PermissionEngine) PhaseProgress() string {
	if pe.Phases <= 0 {
		return ""
	}
	return fmt.Sprintf("Phase %d/%d", pe.Phase, pe.Phases)
}

// AdvanceSpecStage updates Stage based on which spec-workflow tool the
// model just executed successfully. Called by stream_tool_exec.go — plays
// the same role ApplyToolState played for the old Plan Mode.
func (pe *PermissionEngine) AdvanceSpecStage(name string) {
	if canonicalToolName(name) == "SpecReset" {
		pe.Stage = SpecStageNone
		pe.SpecSlug = ""
		pe.specDone = 0
		pe.Phase = 0
		pe.Phases = 0
		pe.Revision++
		return
	}
	w := SpecWorkflow{Stage: pe.Stage, Slug: pe.SpecSlug, Done: pe.specDone}
	if err := w.Transition(name, pe.SpecSlug); err != nil {
		return
	}
	pe.Stage, pe.SpecSlug, pe.specDone = w.Stage, w.Slug, w.Done
	pe.Revision++
	if canonicalToolName(name) == "ApproveImplementation" {
		pe.Phase = 1
		pe.Phases = detectPhases(pe.SpecSlug)
	}
	if canonicalToolName(name) == "Plan" {
		if pe.unresolvedClarifications() > 0 {
			pe.Stage = SpecStageDesign
		} else if !pe.phaseGatesPass() {
			pe.Stage = SpecStageDesign
		}
	}
}

// RestoreSpecWorkflow installs the serialized workflow state carried by a
// parent policy snapshot. The completion mask is private to this package, so
// callers provide its stable integer representation.
func (pe *PermissionEngine) RestoreSpecWorkflow(stage SpecStage, slug string, doneMask int) {
	if pe == nil {
		return
	}
	pe.Stage = stage
	pe.SpecSlug = slug
	pe.specDone = specDone(doneMask)
}

// ResetSpecWorkflow clears every spec-workflow field, including the parallel
// stage completion mask that must not leak into a later workflow.
func (pe *PermissionEngine) ResetSpecWorkflow() {
	if pe == nil {
		return
	}
	pe.Stage = SpecStageNone
	pe.SpecSlug = ""
	pe.specDone = 0
	pe.Phase = 0
	pe.Phases = 0
	pe.Revision++
}

// ToolCallInfo is a minimal struct for permission checking.
type ToolCallInfo struct {
	Name string
	ID   string
	Args map[string]interface{}
}
