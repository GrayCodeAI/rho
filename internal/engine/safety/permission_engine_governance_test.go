package safety

import (
	"context"
	"testing"
	"time"

	"github.com/GrayCodeAI/rho/internal/governance"
)

// govPolicy builds a policy layer with the given capabilities.
func govPolicy(caps ...governance.Capability) *governance.Layer {
	l, err := governance.BuildProfile("policy", governance.Document{
		Version:      1,
		FailClosed:   true,
		Capabilities: caps,
	})
	if err != nil {
		panic(err)
	}
	return l
}

func TestGovernanceCeilingOverridesBypass(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Governance = governance.New()
	pe.Governance.SetPolicy(govPolicy(
		governance.Capability{Scope: "bash", Action: governance.ActionDeny},
	))
	if !pe.BypassKill.EnableScoped(nil, time.Time{}, "governance test") {
		t.Fatal("failed to enable test bypass")
	}

	d := pe.CheckToolDecision(context.Background(), ToolCallInfo{
		Name: "Bash",
		Args: map[string]interface{}{"command": "rm -rf /"},
	})
	if d.Outcome != DecisionDeny || d.Reason != ReasonGovernance {
		t.Fatalf("governance deny must override bypass: %+v", d)
	}
}

func TestGovernanceCeilingOverridesAutonomyAndRules(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Governance = governance.New()
	pe.Governance.SetPolicy(govPolicy(
		governance.Capability{Scope: "bash", Action: governance.ActionDeny},
	))
	pe.Autonomy = AutonomyYOLO
	pe.Memory.AlwaysAllow("Bash")

	d := pe.CheckToolDecision(context.Background(), ToolCallInfo{
		Name: "Bash",
		Args: map[string]interface{}{"command": "echo hi"},
	})
	if d.Outcome != DecisionDeny || d.Reason != ReasonGovernance {
		t.Fatalf("governance deny must override autonomy+rule allow: %+v", d)
	}
}

func TestGovernanceUnconfiguredIsFailOpen(t *testing.T) {
	pe := NewPermissionEngine()
	d := pe.CheckToolDecision(context.Background(), ToolCallInfo{
		Name: "Bash",
		Args: map[string]interface{}{"command": "echo hi"},
	})
	if d.Reason == ReasonGovernance {
		t.Fatalf("unconfigured governance must not interfere: %+v", d)
	}
}
