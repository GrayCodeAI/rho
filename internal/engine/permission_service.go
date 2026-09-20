package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine/safety"
	"github.com/GrayCodeAI/rho/internal/eventlog"
	"github.com/GrayCodeAI/rho/internal/governance"
	"github.com/GrayCodeAI/rho/internal/observability/logger"
	"github.com/GrayCodeAI/rho/internal/permissions"
	"github.com/GrayCodeAI/rho/internal/permissions/stableid"
	"github.com/GrayCodeAI/rho/internal/permissions/turnrecovery"
	"github.com/GrayCodeAI/rho/internal/spec"
)

// PermissionService is the Session's view of the safety/approval layer.
// It owns the PermissionEngine, permission memory, autonomy level, approval
// gate, and AllowedDirs/permission function callbacks. Turn and budget limits
// live in LifecycleSvc().Limits() so there is one authority for execution caps.
//
// Extracted from Session in Phase 2 of the god-object decomposition
// (see docs/session-decomposition.md). Session delegates permission state and
// policy mutation here; this service is the single owner of that state.
type PermissionService struct {
	mu sync.RWMutex
	// perm is the underlying PermissionEngine. Always non-nil after
	// construction.
	perm *safety.PermissionEngine
	// allowedDirs is the list of directories the agent may write to.
	allowedDirs []string
	// permissionFn is the user-callback that prompts for approval.
	permissionFn func(safety.PermissionRequest)
	// approval is the human-in-the-loop gate for high-risk tool actions.
	approval *ApprovalGate
	// askUserFn is the fallback interactive approval callback.
	askUserFn func(question string) (string, error)
	// journal logs durable spec-workflow facts when non-nil.
	journal *eventlog.Log
	// log is the session logger.
	log *logger.Logger
	// recovery, when enabled, is the per-session opaque request-token
	// registry (ports fx TurnPermissionRecovery). When nil (the default)
	// the approval gate behaves exactly as before; when set, a denied
	// high-risk action returns an opaque permission_request_id and a later
	// identical call is denied again rather than re-prompted, unless the
	// exact token is escalted via EscalatePermission (single-use).
	recovery *turnrecovery.Recovery
	// exact, when configured, is the session's persisted store of exact,
	// stable-id permission rules (ports fx session_permission_state). Rules
	// here are addressable by a stable, monotonically increasing id (they
	// survive workspace changes) and can be remembered, listed, and revoked
	// by that id. When nil (the default) nothing changes; when set, callers
	// can RememberExact/RevokeExact/ListExact.
	exact *permissions.StableRuleStore
}

// PermissionView is the immutable presentation view used by status and
// permission UIs. It prevents callers from reaching into the live engine just
// to render rule counts or provenance.
type PermissionView struct {
	Grants     []permissions.Grant
	ExactRules []stableid.RuleSnap
}

// PermissionRuntimeState is the coherent runtime posture shown by the UI.
// Reading it once avoids mixing an old autonomy tier with a newer dry-run or
// explicitness bit during a redraw.
type PermissionRuntimeState struct {
	Autonomy         safety.AutonomyLevel
	AutonomyExplicit bool
	DryRun           bool
}

func (s *PermissionService) RuntimeState() PermissionRuntimeState {
	if s == nil {
		return PermissionRuntimeState{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.perm == nil {
		return PermissionRuntimeState{}
	}
	return PermissionRuntimeState{
		Autonomy: s.perm.Autonomy, AutonomyExplicit: s.perm.AutonomyExplicit,
		DryRun: s.perm.DryRun,
	}
}

// PolicyView returns a point-in-time view of remembered policy for display.
// Grants are copied; the exact-rule store remains internally synchronized.
func (s *PermissionService) PolicyView() PermissionView {
	if s == nil {
		return PermissionView{}
	}
	s.mu.RLock()
	pe := s.perm
	exact := s.exact
	if pe != nil && exact == nil {
		exact = pe.ExactRules
	}
	var grants []permissions.Grant
	var exactRules []stableid.RuleSnap
	if pe != nil && pe.UnifiedGrants != nil {
		grants = pe.UnifiedGrants.All(time.Now())
	} else if pe != nil && pe.Memory != nil {
		grants = pe.Memory.Grants()
	}
	if exact != nil {
		exactRules = exact.List()
	}
	s.mu.RUnlock()
	return PermissionView{Grants: grants, ExactRules: exactRules}
}

// NewPermissionService constructs a PermissionService with a fresh
// PermissionEngine. Tests can inject a custom engine via WithEngine.
func NewPermissionService(log *logger.Logger) *PermissionService {
	if log == nil {
		log = logger.Default()
	}
	pe := safety.NewPermissionEngine()
	s := &PermissionService{
		perm: pe,
		log:  log,
	}
	s.loadManagedGovernance()
	return s
}

// loadManagedGovernance attempts to install the administrator-set POLICY
// ceiling from the platform trust-root (see governance.ManagedPolicyPath).
// A missing file is the normal unmanaged case and leaves the engine
// fail-open; a malformed file is logged loudly so misconfiguration is
// visible without hard-failing every session.
func (s *PermissionService) loadManagedGovernance() {
	if s == nil || s.perm == nil || s.perm.Governance == nil {
		return
	}
	path := governance.ManagedPolicyPath()
	if path == "" {
		return
	}
	if err := s.perm.Governance.LoadPolicy(path); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			s.log.Error("governance: failed to load managed policy", map[string]interface{}{
				"path":  path,
				"error": err.Error(),
			})
		}
	}
}

// WithEngine replaces the underlying PermissionEngine. Used by tests
// and by callers that want a pre-configured engine.
func (s *PermissionService) WithEngine(pe *safety.PermissionEngine) *PermissionService {
	if s == nil {
		return s
	}
	if pe == nil {
		pe = safety.NewPermissionEngine()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exact != nil {
		pe.ExactRules = s.exact
	}
	s.perm = pe
	return s
}

// Logger returns the logger used by permission decisions.
func (s *PermissionService) Logger() *logger.Logger {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.log
}

// SetLogger replaces the logger used by permission decisions.
func (s *PermissionService) SetLogger(l *logger.Logger) {
	if s == nil {
		return
	}
	if l == nil {
		l = logger.Default()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = l
}

// CheckTool is the central permission check. Returns (granted, denyMsg).
// The caller (engine/stream_tool_exec.go) handles the tool_result
// event emission and the post-call side effects.
func (s *PermissionService) CheckTool(ctx context.Context, info safety.ToolCallInfo) (bool, string) {
	if s == nil {
		return false, "permission service is unavailable"
	}
	// Evaluate a stable copy just like the structured and preview APIs. This
	// keeps autonomy, spec, dry-run, and hard-ceiling updates from racing with
	// an in-flight execution check while leaving the prompt callback and audit
	// sinks shared by reference.
	perm := s.engineCopy()
	if perm == nil {
		return false, "permission service is unavailable"
	}
	granted, denyMsg := perm.CheckTool(ctx, info)
	if !granted {
		if log := s.Logger(); log != nil {
			log.Warn("permission denied", map[string]interface{}{
				"tool":   info.Name,
				"reason": denyMsg,
			})
		}
	}
	return granted, denyMsg
}

// CheckToolDecision evaluates a request and exposes stable decision metadata.
func (s *PermissionService) CheckToolDecision(ctx context.Context, info safety.ToolCallInfo) safety.Decision {
	perm := s.engineCopy()
	if perm == nil {
		return unavailablePermissionDecision()
	}
	return perm.CheckToolDecision(ctx, info)
}

// EvaluateTool returns allow, ask, or deny without blocking on the UI.
func (s *PermissionService) EvaluateTool(ctx context.Context, info safety.ToolCallInfo) safety.Decision {
	perm := s.engineCopy()
	if perm == nil {
		return unavailablePermissionDecision()
	}
	return perm.EvaluateTool(ctx, info)
}

// PolicySnapshot returns the policy state used for a single request.
func (s *PermissionService) PolicySnapshot() safety.PolicySnapshot {
	if s == nil {
		return safety.PolicySnapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.perm == nil {
		return safety.PolicySnapshot{}
	}
	snapshot := s.perm.Snapshot()
	snapshot.AllowedDirs = append([]string(nil), s.allowedDirs...)
	return snapshot
}

// CheckToolSnapshot evaluates a request against a previously captured policy.
func (s *PermissionService) CheckToolSnapshot(ctx context.Context, info safety.ToolCallInfo, snapshot safety.PolicySnapshot) safety.Decision {
	perm := s.engineCopy()
	if perm == nil {
		return unavailablePermissionDecision()
	}
	return perm.CheckToolSnapshot(ctx, info, snapshot)
}

func unavailablePermissionDecision() safety.Decision {
	return safety.Decision{
		Outcome: safety.DecisionDeny,
		Message: "permission service is unavailable",
	}
}

// engineCopy returns a copy of the engine for cross-goroutine evaluation. The
// service lock is held only for the duration of the copy, so evaluation does
// not block policy updates or user prompts.
func (s *PermissionService) engineCopy() *safety.PermissionEngine {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.perm == nil {
		return nil
	}
	return s.perm.Copy()
}

// ApplyPolicySnapshot installs a bounded parent policy into a child service.
// The rule store is deep-copied so later parent changes cannot widen or alter
// an in-flight child policy.
func (s *PermissionService) ApplyPolicySnapshot(snapshot safety.PolicySnapshot) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perm == nil {
		return
	}
	s.perm.Autonomy = snapshot.Autonomy
	s.perm.AutonomyExplicit = snapshot.AutonomyExplicit
	s.perm.Stage = snapshot.Stage
	s.perm.DryRun = snapshot.DryRun
	s.perm.RestoreSpecWorkflow(snapshot.Stage, snapshot.SpecSlug, snapshot.SpecDone)
	s.perm.SetSpecAllowTests(snapshot.SpecAllowTests)
	s.perm.Phase = snapshot.Phase
	s.perm.Phases = snapshot.Phases
	s.perm.Revision = snapshot.Revision
	s.perm.SetNeverAllow(snapshot.NeverAllow)
	s.perm.Memory = safety.NewPermissionMemoryFromSnapshot(snapshot.Rules)
	if snapshot.ExactRules != nil {
		s.exact = snapshot.ExactRules
		s.perm.ExactRules = snapshot.ExactRules
	}
	// Rebuild UnifiedGrants so it wraps the snapshot's Memory (not the
	// service's original Memory, which is now stale).
	if s.perm.UnifiedGrants != nil {
		s.perm.UnifiedGrants = permissions.NewUnifiedGrants(s.perm.Memory)
	}
	s.allowedDirs = append([]string(nil), snapshot.AllowedDirs...)
}

// CheckApproval runs the human-in-the-loop gate on high-risk actions.
// Returns (approved, denyMsg). The caller handles tool_result emission.
// This is a thin wrapper around the engine's per-tool session.CheckApproval
// helper logic; the full implementation lives in
// internal/engine/safety/approval_gate.go (ApprovalGate) and is invoked
// via the Session.CheckApproval method (which has the full state). The
// service's own CheckApproval is a no-op when s.approval is nil so
// callers can use it as the canonical entry point.
func (s *PermissionService) CheckApproval(ctx context.Context, toolName string, args map[string]interface{}) (bool, string) {
	if s == nil {
		return false, "permission service is unavailable"
	}
	asked := false
	allowed, msg := s.checkApprovalGate(ctx, toolName, args, &asked)
	s.appendApprovalFact(toolName, args, allowed, msg, asked)
	return allowed, msg
}

// EnableTurnRecovery activates the opaque request-token escalation layer
// (ports fx's TurnPermissionRecovery). When enabled, a denied high-risk
// action returns an opaque permission_request_id; a later identical call is
// denied again instead of being re-prompted, and only the exact token
// presented via EscalatePermission can re-open it — and then only once.
func (s *PermissionService) EnableTurnRecovery() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recovery == nil {
		s.recovery = turnrecovery.New()
	}
}

// SetExactRuleStore installs a persisted exact, stable-id rule store (ports
// fx session_permission_state). Nil (the default) leaves the service
// unchanged.
func (s *PermissionService) SetExactRuleStore(store *permissions.StableRuleStore) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.exact = store
	if s.perm != nil {
		s.perm.ExactRules = store
	}
	s.mu.Unlock()
}

// RememberExact records an exact stable-id permission rule and returns its
// stable id. ok is false when the identity is invalid, the store is full, or
// no store is configured (fx invalid/full outcomes). The surviving id is
// stable across workspace changes and can be used with RevokeExact.
func (s *PermissionService) RememberExact(kind stableid.Kind, canonical, displayIdent string, decision stableid.Decision) (uint64, bool) {
	if s == nil {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exact == nil {
		return 0, false
	}
	return s.exact.Remember(kind, canonical, displayIdent, decision)
}

// RevokeExact removes the exact rule with the given stable id. false when no
// such rule exists (fx stale outcome).
func (s *PermissionService) RevokeExact(id uint64) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exact == nil {
		return false
	}
	return s.exact.Revoke(id)
}

// ListExact returns the configured exact rules ordered by stable id.
func (s *PermissionService) ListExact() []stableid.RuleSnap {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.exact == nil {
		return nil
	}
	return s.exact.List()
}

// ExactRulesConfigured reports whether durable exact-action rules are
// available for this session. It lets callers fail closed before approving a
// project-scoped prompt that cannot actually be persisted.
func (s *PermissionService) ExactRulesConfigured() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.exact != nil
}

// EscalatePermission re-opens a previously denied high-risk action by
// presenting the exact opaque permission_request_id returned in the denial.
// Generic text can never authorize; only the exact current-turn token binds,
// and the grant is consumed once at the next identical call's execution.
// Returns false when the id is not a pending denial or is not 64 hex digits.
func (s *PermissionService) EscalatePermission(requestID string) bool {
	if s == nil || len(requestID) != 64 {
		return false
	}
	s.mu.RLock()
	recovery := s.recovery
	s.mu.RUnlock()
	if recovery == nil {
		return false
	}
	var id turnrecovery.ID
	for i := range 32 {
		v, err := strconv.ParseUint(requestID[i*2:i*2+2], 16, 8)
		if err != nil {
			return false
		}
		id[i] = byte(v)
	}
	if _, ok := recovery.DeniedCall(id); !ok {
		return false
	}
	return recovery.RememberApproval(id, turnrecovery.Approval{
		Authority:     "escalation",
		HumanApproval: true,
	})
}

// denyHighRiskAction registers a denied high-risk call in the recovery
// registry (when enabled) and appends its opaque token to the message.
func denyHighRiskAction(recovery *turnrecovery.Recovery, cat ApprovalCategory, toolName string, args map[string]interface{}, baseMsg string) string {
	if recovery == nil {
		return baseMsg
	}
	id, _ := recovery.RememberAutoDenial(".", approvalRecoveryCall(toolName, args))
	return baseMsg + " permission_request_id: " + id.Hash()
}

// approvalRecoveryCall builds the exact tool-call identity for the recovery
// registry. args is marshaled deterministically (json.Marshal sorts map keys).
func approvalRecoveryCall(toolName string, args map[string]interface{}) turnrecovery.ToolCall {
	b, err := json.Marshal(args)
	if err != nil {
		b = []byte("{}")
	}
	return turnrecovery.ToolCall{Name: toolName, ArgumentsJSON: string(b)}
}

func (s *PermissionService) checkApprovalGate(ctx context.Context, toolName string, args map[string]interface{}, asked *bool) (bool, string) {
	s.mu.RLock()
	g := s.approval
	recovery := s.recovery
	askUserFn := s.askUserFn
	journal := s.journal
	autonomy := safety.AutonomySupervised
	if s.perm != nil {
		autonomy = s.perm.Autonomy
	}
	s.mu.RUnlock()
	if g == nil || !g.Enabled {
		return true, ""
	}
	cat, risky := g.classifyAction(toolName, args)
	if !risky || !g.categoryEnabled(cat) {
		return true, ""
	}
	if autonomy <= g.MaxAutoApprove {
		return true, ""
	}
	// Atomic check-and-consume: session-wide approval or remaining N-count.
	// tryConsumeApproval holds the lock across both checks so concurrent
	// high-risk tool calls cannot double-spend a session or N-count approval.
	if g.tryConsumeApproval(cat) {
		return true, ""
	}
	// Opaque request-token escalation (fx TurnPermissionRecovery). When the
	// recovery registry is enabled, a denied action is bound to an opaque
	// permission_request_id; only presenting that exact id (EscalatePermission)
	// can re-open the action, and then only once. This prevents a model from
	// re-invoking an identical call to re-enter the prompt after a denial.
	if recovery != nil {
		call := approvalRecoveryCall(toolName, args)
		// Live single-use revalidation: the exact call was escalated, consume it now.
		if appr, ok := recovery.TakeApproval(call); ok && appr.HumanApproval {
			return true, ""
		}
		// A still-pending, unapproved denial is denied again — no re-prompt.
		if recovery.PreservedOutcome(".", call) {
			id, _ := recovery.RememberAutoDenial(".", call)
			return false, "Action denied by human approval gate (" + string(cat) + "). permission_request_id: " + id.Hash()
		}
	}
	req := ApprovalRequest{
		ToolName: canonicalToolName(toolName),
		Category: cat,
		Summary:  approvalSummary(toolName, args),
		Args:     args,
	}
	if asked != nil {
		*asked = true
	}
	askedReq := eventlog.ApprovalAskedFact{
		Tool:     req.ToolName,
		Category: string(cat),
		Question: "Approve high-risk action [" + string(cat) + "]: " + req.Summary + "?",
	}
	if journal != nil {
		journal.AppendApprovalAsked(askedReq)
	}
	if g.Waterfall != nil {
		resp, denyMsg := g.Waterfall.Decide(ctx, req)
		switch resp {
		case ApprovalApproveForSession:
			g.sessionApprove(cat)
			return true, ""
		case ApprovalApproveForN:
			n := req.N
			if n <= 0 {
				n = 5
			}
			g.nApprove(cat, n)
			return true, ""
		case ApprovalApprove:
			return true, ""
		default:
			return false, denyHighRiskAction(recovery, cat, toolName, args, denyMsg)
		}
	}
	if g.ConfirmFn != nil {
		switch g.ConfirmFn(req) {
		case ApprovalApproveForSession:
			g.sessionApprove(cat)
			return true, ""
		case ApprovalApproveForN:
			// Default N=5 when the typed response carries no count.
			n := req.N
			if n <= 0 {
				n = 5
			}
			g.nApprove(cat, n)
			return true, ""
		case ApprovalApprove:
			return true, ""
		default:
			return false, denyHighRiskAction(recovery, cat, toolName, args, "Action denied by human approval gate ("+string(cat)+").")
		}
	}
	if askUserFn != nil {
		ans, err := askUserFn("Approve high-risk action [" + string(cat) + "]: " + req.Summary + "? (yes/no/session/N)")
		if err != nil {
			return false, denyHighRiskAction(recovery, cat, toolName, args, "Action denied by human approval gate ("+string(cat)+").")
		}
		lower := strings.ToLower(strings.TrimSpace(ans))
		switch lower {
		case "session", "s", "approve-session", "yes-session":
			g.sessionApprove(cat)
			return true, ""
		default:
			// "10" or "5x" style: approve for N.
			if n, ok := parseApprovalCount(lower); ok {
				g.nApprove(cat, n)
				return true, ""
			}
			if isAffirmative(ans) {
				return true, ""
			}
			return false, denyHighRiskAction(recovery, cat, toolName, args, "Action denied by human approval gate ("+string(cat)+").")
		}
	}
	return false, fmt.Sprintf("High-risk action requires approval but no confirmation handler is configured (%q).", cat)
}

func (s *PermissionService) appendApprovalFact(toolName string, args map[string]interface{}, allowed bool, msg string, asked bool) {
	if s == nil {
		return
	}
	s.mu.RLock()
	journal := s.journal
	g := s.approval
	s.mu.RUnlock()
	if journal == nil {
		return
	}
	category := ""
	risky := false
	if g != nil {
		if cat, r := g.classifyAction(toolName, args); r {
			category = string(cat)
			risky = true
		}
	}
	tool := canonicalToolName(toolName)
	journal.AppendPermission(eventlog.PermissionFact{
		Tool:     tool,
		Category: category,
		Allowed:  allowed,
		Message:  msg,
	})
	if asked {
		journal.AppendApprovalDecided(eventlog.ApprovalDecidedFact{
			Tool:     tool,
			Category: category,
			Allowed:  allowed,
			Message:  msg,
		})
		journal.AppendApprovalPolicy(eventlog.ApprovalPolicyFact{
			Category: category,
			Covered:  risky,
		})
	}
}

// SetAllowedDirs sets the directories the agent may write to.
func (s *PermissionService) SetAllowedDirs(dirs []string) {
	if s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.allowedDirs = append([]string(nil), dirs...)
	}
}

// SetAutonomy sets the agent's autonomy level and rebuilds the per-flag
// profile from that level, preserving any user overrides. Writes directly to
// the underlying PermissionEngine — the same field CheckTool reads — rather
// than a separate shadow field, so the change actually takes effect.
func (s *PermissionService) SetAutonomy(level safety.AutonomyLevel) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perm == nil {
		return
	}
	s.perm.Autonomy = level
	s.perm.AutonomyExplicit = true
	s.perm.Revision++
	// Rebuild profile from the new level, then re-apply overrides so the
	// user's per-flag tweaks survive a tier change.
	if s.perm.Profile != nil {
		overrides := s.perm.Profile.Overrides()
		s.perm.Profile = safety.ProfileFromLevel(level)
		s.perm.Profile.ApplyOverrides(overrides)
	}
}

// ApplyAutonomyOverrides merges per-flag overrides onto the active profile.
// Unknown flag names are ignored. The profile is rebuilt from the current
// level first so overrides are applied consistently.
func (s *PermissionService) ApplyAutonomyOverrides(overrides map[string]bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perm == nil {
		return
	}
	if s.perm.Profile == nil {
		s.perm.Profile = safety.ProfileFromLevel(s.perm.Autonomy)
	}
	s.perm.Profile.ApplyOverrides(overrides)
	s.perm.Revision++
}

// AutonomyProfile returns a copy of the active profile's override set (for
// display/persistence). Returns nil if no profile is active.
func (s *PermissionService) AutonomyProfile() *safety.AutonomyProfile {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.perm == nil {
		return nil
	}
	if s.perm.Profile == nil {
		return nil
	}
	return s.perm.Profile.Clone()
}

// SetSpecStage sets the independent spec-workflow stage. Also writes
// directly to the engine, same reasoning as SetAutonomy.
func (s *PermissionService) SetSpecStage(stage safety.SpecStage) {
	if s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.perm == nil {
			return
		}
		if s.perm.Stage != stage {
			s.perm.Stage = stage
			s.perm.Revision++
		}
	}
}

// SetDryRun toggles the global kill switch: when true, every tool call is
// denied unconditionally, regardless of tier or spec stage.
func (s *PermissionService) SetDryRun(dryRun bool) {
	if s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.perm == nil {
			return
		}
		if s.perm.DryRun != dryRun {
			s.perm.DryRun = dryRun
			s.perm.Revision++
		}
	}
}

// SetApproval replaces the ApprovalGate.
func (s *PermissionService) SetApproval(a *ApprovalGate) {
	if s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.approval = a
	}
}

// SetAskUserFn sets the fallback interactive approval callback.
func (s *PermissionService) SetAskUserFn(fn func(question string) (string, error)) {
	if s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.askUserFn = fn
	}
}

func (s *PermissionService) AskUserFn() func(string) (string, error) {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.askUserFn
}

// ApprovalEnabled reports whether the human-in-the-loop gate is active.
// Callers do not receive the mutable gate; decisions remain inside the
// permission service's synchronized boundary.
func (s *PermissionService) ApprovalEnabled() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.approval != nil && s.approval.Enabled
}

// SpecSlug returns the active specification identifier.
func (s *PermissionService) SpecSlug() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.perm == nil {
		return ""
	}
	return s.perm.SpecSlug
}

// SetSpecSlug updates the active specification identifier.
func (s *PermissionService) SetSpecSlug(slug string) {
	if s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.perm == nil {
			return
		}
		if s.perm.SpecSlug != slug {
			s.perm.SpecSlug = slug
			s.perm.Revision++
		}
	}
}

// SetPermissionFn replaces the user-callback.
func (s *PermissionService) SetPermissionFn(fn func(safety.PermissionRequest)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perm == nil {
		return
	}
	s.permissionFn = fn
	s.perm.PromptFn = fn
}

// PermissionFn returns the configured approval callback for sub-agent
// construction and legacy integrations.
func (s *PermissionService) PermissionFn() func(safety.PermissionRequest) {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.permissionFn
}

// AllowedDirs returns the write-allowlist.
func (s *PermissionService) AllowedDirs() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.allowedDirs...)
}

// SpecStage returns the active spec-workflow stage.
func (s *PermissionService) SpecStage() safety.SpecStage {
	if s == nil {
		return safety.SpecStageNone
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.perm == nil {
		return safety.SpecStageNone
	}
	return s.perm.Stage
}

// SpecPhaseProgress returns the current and total implementation phases.
func (s *PermissionService) SpecPhaseProgress() (current, total int) {
	if s == nil {
		return 0, 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.perm == nil {
		return 0, 0
	}
	return s.perm.Phase, s.perm.Phases
}

// AdvanceSpecStage records the next spec workflow transition through the
// permission service instead of exposing the underlying engine to callers.
func (s *PermissionService) AdvanceSpecStage(toolName string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.perm == nil {
		s.mu.Unlock()
		return
	}
	s.perm.AdvanceSpecStage(toolName)
	journal := s.journal
	stage := spec.StringFromStageEnum(int(s.perm.Stage))
	slug := s.perm.SpecSlug
	s.mu.Unlock()
	if journal != nil {
		journal.AppendSpec(stage, slug)
	}
}

// SetJournal attaches the append-only event spine used for durable spec facts.
func (s *PermissionService) SetJournal(j *eventlog.Log) {
	if s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.journal = j
	}
}

// ResetSpec clears the active spec workflow.
func (s *PermissionService) ResetSpec() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perm == nil {
		return
	}
	s.perm.ResetSpecWorkflow()
}

// Memory returns the engine's session permission memory. Mutations are safe
// through the memory's own synchronization; policy evaluation takes a stable
// engine copy before matching rules.
func (s *PermissionService) Memory() *safety.PermissionMemory {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.perm == nil {
		return nil
	}
	return s.perm.Memory
}

// SetMemory replaces the session's permission-memory policy store.
func (s *PermissionService) SetMemory(m *safety.PermissionMemory) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perm == nil {
		return
	}
	if s.perm.Memory == m {
		return
	}
	s.perm.Memory = m
	// Memory is the canonical session-rule source. Rebuild the derived grant
	// view at the same mutation point so policy display/evaluation cannot keep
	// consulting a replaced store.
	if s.perm.UnifiedGrants != nil {
		s.perm.UnifiedGrants = permissions.NewUnifiedGrants(m)
	}
	s.perm.Revision++
}

// SetKnownTools wires the registered execution surface into permission
// evaluation. Custom registered tools may not have static capability metadata,
// but unregistered tool names must still fail closed.
func (s *PermissionService) SetKnownTools(names []string) {
	if s == nil {
		return
	}
	known := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			known[strings.TrimSpace(name)] = struct{}{}
		}
	}
	s.mu.Lock()
	if s.perm != nil {
		s.perm.KnownTools = known
		s.perm.Revision++
	}
	s.mu.Unlock()
}

// SetUntrustedTools marks registered external tools that require explicit
// approval. This is separate from the execution registry because registry
// membership alone does not describe side effects.
func (s *PermissionService) SetUntrustedTools(names []string) {
	if s == nil {
		return
	}
	untrusted := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			untrusted[name] = struct{}{}
		}
	}
	s.mu.Lock()
	if s.perm != nil {
		s.perm.UntrustedTools = untrusted
		s.perm.Revision++
	}
	s.mu.Unlock()
}

// MarkToolUntrusted records an external tool discovered after session startup.
// Dynamic plugin activation can add tools after the initial registry snapshot,
// so the execution boundary must be able to extend this set atomically.
func (s *PermissionService) MarkToolUntrusted(name string) {
	if s == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perm == nil {
		return
	}
	if _, exists := s.perm.UntrustedTools[name]; exists {
		return
	}
	// Permission checks run against engine copies outside this service lock.
	// Replace the map instead of mutating it so in-flight evaluators retain an
	// immutable snapshot and cannot race with late tool registration.
	untrusted := make(map[string]struct{}, len(s.perm.UntrustedTools)+1)
	for toolName := range s.perm.UntrustedTools {
		untrusted[toolName] = struct{}{}
	}
	untrusted[name] = struct{}{}
	s.perm.UntrustedTools = untrusted
	s.perm.Revision++
}

// ReplaceSessionRules atomically replaces ephemeral allow/deny rules and
// keeps the derived grant view attached to the same canonical memory store.
// Durable exact rules are intentionally untouched.
func (s *PermissionService) ReplaceSessionRules(allowSpecs, denySpecs []string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perm == nil {
		return false
	}
	if s.perm.Memory == nil {
		s.perm.Memory = safety.NewPermissionMemory()
	}
	mem := s.perm.Memory
	if s.perm.UnifiedGrants == nil {
		s.perm.UnifiedGrants = permissions.NewUnifiedGrants(mem)
	}
	mem.Reset()
	for _, spec := range allowSpecs {
		mem.AllowSpec(spec)
	}
	for _, spec := range denySpecs {
		mem.DenySpec(spec)
	}
	s.perm.Revision++
	return true
}

// mutateSessionMemory applies one ephemeral policy mutation while holding the
// service lock, then advances the engine revision with that mutation. Keeping
// the mutation and revision together prevents stale policy views and gives
// callers a single synchronization boundary for session rules.
func (s *PermissionService) mutateSessionMemory(mutate func(*safety.PermissionMemory)) bool {
	if s == nil || mutate == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perm == nil || s.perm.Memory == nil {
		return false
	}
	mutate(s.perm.Memory)
	s.perm.Revision++
	return true
}

// RememberSessionExact records one literal action in ephemeral session
// policy. The service owns the mutation so UI surfaces do not reach through
// to the live permission engine.
func (s *PermissionService) RememberSessionExact(toolName, identity string, allow bool) bool {
	return s.mutateSessionMemory(func(mem *safety.PermissionMemory) {
		if allow {
			mem.AlwaysAllowExact(toolName, identity)
		} else {
			mem.AlwaysDenyExact(toolName, identity)
		}
	})
}

// RememberSessionTool records a session-wide tool decision. This is reserved
// for explicit tool-wide choices; ordinary approval uses RememberSessionExact.
func (s *PermissionService) RememberSessionTool(toolName string, allow bool) bool {
	return s.mutateSessionMemory(func(mem *safety.PermissionMemory) {
		if allow {
			mem.AlwaysAllow(toolName)
		} else {
			mem.AlwaysDeny(toolName)
		}
	})
}

// RememberSessionRule adds a parsed session rule such as Bash(git:*).
func (s *PermissionService) RememberSessionRule(rule string, allow bool) bool {
	return s.mutateSessionMemory(func(mem *safety.PermissionMemory) {
		if allow {
			mem.AllowSpec(rule)
		} else {
			mem.DenySpec(rule)
		}
	})
}

// BypassState returns a point-in-time view of the break-glass permission
// switch. The grant is copied by the underlying switch and is safe for the
// caller to inspect or retain.
func (s *PermissionService) BypassState() (bool, *permissions.BypassGrant) {
	bypass := s.bypassSwitch()
	if bypass == nil {
		return false, nil
	}
	if !bypass.IsEnabled() {
		return false, nil
	}
	grant := bypass.Grant()
	if grant != nil && grant.IsExpired(time.Now()) {
		// Expiry is fail-closed even when no tool call has arrived to trigger
		// the evaluator's lazy cleanup. This keeps status/UI and execution
		// semantics consistent while the session is idle.
		bypass.Disable()
		return false, nil
	}
	return true, grant
}

// EnableBypass enables the scoped break-glass permission switch.
func (s *PermissionService) EnableBypass(scope []string, expiresAt time.Time, reason string) bool {
	reason = strings.TrimSpace(reason)
	if reason == "" || (!expiresAt.IsZero() && !expiresAt.After(time.Now())) {
		return false
	}
	bypass := s.bypassSwitch()
	if bypass == nil {
		return false
	}
	return bypass.EnableScoped(append([]string(nil), scope...), expiresAt, reason)
}

// DisableBypass turns off the break-glass permission switch.
func (s *PermissionService) DisableBypass() bool {
	bypass := s.bypassSwitch()
	if bypass == nil {
		return false
	}
	bypass.Disable()
	return true
}

func (s *PermissionService) bypassSwitch() *permissions.BypassKillswitch {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.perm == nil {
		return nil
	}
	return s.perm.BypassKill
}

// SetNeverAllow replaces the personal hard-ceiling rule set on the engine.
func (s *PermissionService) SetNeverAllow(specs []string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perm == nil {
		return
	}
	if slices.Equal(s.perm.NeverAllow(), specs) {
		return
	}
	s.perm.SetNeverAllow(specs)
	s.perm.Revision++
}

// NeverAllow returns a copy of the current never-allow rules.
func (s *PermissionService) NeverAllow() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.perm == nil {
		return nil
	}
	return s.perm.NeverAllow()
}

// SetSpecAllowTests enables or disables safe test commands during spec stage.
func (s *PermissionService) SetSpecAllowTests(allow bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perm == nil {
		return
	}
	if s.perm.Snapshot().SpecAllowTests == allow {
		return
	}
	s.perm.SetSpecAllowTests(allow)
	s.perm.Revision++
}

// AuditLog returns a formatted audit trail of recent permission decisions,
// or a message if the audit log is disabled.
func (s *PermissionService) AuditLog() string {
	if s == nil {
		return "Audit log unavailable."
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.perm == nil {
		return "Audit log unavailable."
	}
	if s.perm.AuditLog() == nil {
		return "Audit log disabled."
	}
	return s.perm.AuditLog().Format(50)
}

// PermissionMetrics returns a formatted metrics summary, or a message if
// metrics are disabled.
func (s *PermissionService) PermissionMetrics() string {
	if s == nil {
		return "Metrics unavailable."
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.perm == nil {
		return "Metrics unavailable."
	}
	if s.perm.PermissionMetrics() == nil {
		return "Metrics disabled."
	}
	return s.perm.PermissionMetrics().Format()
}
