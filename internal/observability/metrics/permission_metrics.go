// Package metrics — permission decision telemetry.
//
// PermissionMetrics tracks every decision the permission engine makes, so
// operators can answer "how often does governance override autonomy?", "how
// many times was bypass used this session?", and "what's the current autonomy
// level?". The counters are atomic and safe to increment from the engine's
// hot path without locking.
package metrics

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// PermissionMetrics holds atomic counters for permission decisions.
type PermissionMetrics struct {
	mu sync.RWMutex // protects the dynamically keyed bypass/governance maps
	// decisions counts outcomes by reason label (allow/deny/ask).
	decisions map[string]*int64
	// bypass counts bypass activations, keyed by scope.
	bypass map[string]*int64
	// governanceDenials counts POLICY-ceiling denials, keyed by tool.
	governanceDenials map[string]*int64
	// autonomyLevel is a gauge (current autonomy level 0-4).
	autonomyLevel int64
}

// NewPermissionMetrics creates a PermissionMetrics with pre-allocated counters
// for the known decision reasons.
func NewPermissionMetrics() *PermissionMetrics {
	pm := &PermissionMetrics{
		decisions:         make(map[string]*int64),
		bypass:            make(map[string]*int64),
		governanceDenials: make(map[string]*int64),
	}
	// Pre-allocate the common keys so Increment is allocation-free at runtime.
	for _, reason := range []string{"allow", "deny", "ask"} {
		v := int64(0)
		pm.decisions[reason] = &v
	}
	return pm
}

// RecordDecision increments the counter for an outcome ("allow"/"deny"/"ask").
// reason is the DecisionReason string from the engine (e.g. "autonomy",
// "grant_denied", "governance"). Labels are bounded by what the engine emits.
func (pm *PermissionMetrics) RecordDecision(outcome, reason string) {
	if v, ok := pm.decisions[outcome]; ok {
		atomic.AddInt64(v, 1)
	}
	// Also bucket by reason so dashboards can break down "allow" into
	// autonomy vs grant vs classifier etc.
	if v, ok := pm.decisions[reason]; ok {
		atomic.AddInt64(v, 1)
	}
}

// RecordBypass increments the bypass counter for a scope (e.g. "bash",
// "network", "all"). The scope is the bypass grant's scope or "all" when
// unbounded.
func (pm *PermissionMetrics) RecordBypass(scope string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if v, ok := pm.bypass[scope]; ok {
		atomic.AddInt64(v, 1)
		return
	}
	v := int64(1)
	pm.bypass[scope] = &v
}

// RecordGovernanceDenial increments the governance-ceiling denial counter for
// a tool name.
func (pm *PermissionMetrics) RecordGovernanceDenial(tool string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if v, ok := pm.governanceDenials[tool]; ok {
		atomic.AddInt64(v, 1)
		return
	}
	v := int64(1)
	pm.governanceDenials[tool] = &v
}

// SetAutonomyLevel updates the current-autonomy-level gauge.
func (pm *PermissionMetrics) SetAutonomyLevel(level int) {
	atomic.StoreInt64(&pm.autonomyLevel, int64(level))
}

// AutonomyLevel returns the current autonomy level gauge value.
func (pm *PermissionMetrics) AutonomyLevel() int {
	return int(atomic.LoadInt64(&pm.autonomyLevel))
}

// Snapshot returns a flat map of every counter for export / display.
func (pm *PermissionMetrics) Snapshot() map[string]int64 {
	out := make(map[string]int64)
	for k, v := range pm.decisions {
		out["decision."+k] = atomic.LoadInt64(v)
	}
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	for k, v := range pm.bypass {
		out["bypass."+k] = atomic.LoadInt64(v)
	}
	for k, v := range pm.governanceDenials {
		out["governance_denial."+k] = atomic.LoadInt64(v)
	}
	out["autonomy_level"] = atomic.LoadInt64(&pm.autonomyLevel)
	return out
}

// Format returns a human-readable summary for the /autonomy audit command.
func (pm *PermissionMetrics) Format() string {
	out := "Permission Metrics\n"
	out += fmt.Sprintf("  Autonomy level: %d\n", pm.AutonomyLevel())
	out += "  Decisions:\n"
	for k, v := range pm.decisions {
		if v := atomic.LoadInt64(v); v > 0 {
			out += fmt.Sprintf("    %s: %d\n", k, v)
		}
	}
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if len(pm.bypass) > 0 {
		out += "  Bypass activations:\n"
		for k, v := range pm.bypass {
			out += fmt.Sprintf("    %s: %d\n", k, atomic.LoadInt64(v))
		}
	}
	if len(pm.governanceDenials) > 0 {
		out += "  Governance denials:\n"
		for k, v := range pm.governanceDenials {
			out += fmt.Sprintf("    %s: %d\n", k, atomic.LoadInt64(v))
		}
	}
	return out
}
