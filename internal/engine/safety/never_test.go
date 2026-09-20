package safety

import (
	"context"
	"testing"
	"time"
)

func TestNeverAllow_BlocksEvenAtYOLO(t *testing.T) {
	pe := NewPermissionEngine()
	pe.SetNeverAllow([]string{"Write(*.env)", "Bash(rm -rf *)"})
	pe.Autonomy = AutonomyYOLO

	// Write(*.env) should be blocked even at YOLO.
	d := pe.CheckToolDecision(context.Background(), ToolCallInfo{Name: "Write", Args: map[string]interface{}{"path": ".env"}})
	if d.Outcome != DecisionDeny {
		t.Fatalf("Write(*.env) should be denied at YOLO, got %#v", d)
	}

	// Bash(rm -rf *) should be blocked (also blocked by destructive hard-deny,
	// but never-rule fires first).
	d = pe.CheckToolDecision(context.Background(), ToolCallInfo{Name: "Bash", Args: map[string]interface{}{"command": "rm -rf /tmp"}})
	if d.Outcome != DecisionDeny {
		t.Fatalf("Bash(rm -rf *) should be denied, got %#v", d)
	}

	// A safe Write should still be allowed at YOLO.
	d = pe.CheckToolDecision(context.Background(), ToolCallInfo{Name: "Write", Args: map[string]interface{}{"path": "main.go"}})
	if d.Outcome != DecisionAllow {
		t.Fatalf("Write(main.go) should be allowed at YOLO, got %#v", d)
	}
}

func TestNeverAllow_BlockedByBypass(t *testing.T) {
	// Even with bypass enabled, never-rules must win.
	pe := NewPermissionEngine()
	pe.SetNeverAllow([]string{"Delete"})
	pe.Autonomy = AutonomyYOLO
	if !pe.BypassKill.EnableScoped(nil, time.Time{}, "never-rule test") {
		t.Fatal("failed to enable test bypass")
	}

	d := pe.CheckToolDecision(context.Background(), ToolCallInfo{Name: "Delete"})
	if d.Outcome != DecisionDeny {
		t.Fatalf("Delete should be denied even with bypass, got %#v", d)
	}
}

func TestNeverAllow_BlocksSpecStageWorkflowAllow(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Stage = SpecStagePlan
	pe.SetNeverAllow([]string{"Read(*)"})

	d := pe.CheckToolDecision(context.Background(), ToolCallInfo{Name: "Read", Args: map[string]interface{}{"path": "README.md"}})
	if d.Outcome != DecisionDeny || d.Reason != ReasonRuleDenied {
		t.Fatalf("never-rule during spec stage = %#v, want rule denial", d)
	}
}

func TestNeverAllow_WildcardTool(t *testing.T) {
	pe := NewPermissionEngine()
	pe.SetNeverAllow([]string{"*.env"}) // tool-wide wildcard via "*:*.env" syntax
	// parseRuleSpec("*.env") → tool="*.env", pattern="" → treated as tool match.
	// Use the explicit form instead.
	pe.SetNeverAllow([]string{"Write(*.env)", "Edit(*.env)"})
	pe.Autonomy = AutonomyFull

	d := pe.CheckToolDecision(context.Background(), ToolCallInfo{Name: "Edit", Args: map[string]interface{}{"path": "config.env"}})
	if d.Outcome != DecisionDeny {
		t.Fatalf("Edit(*.env) should be denied, got %#v", d)
	}
}

func TestNeverAllow_SetAndClear(t *testing.T) {
	pe := NewPermissionEngine()
	pe.SetNeverAllow([]string{"Write(*.env)"})
	if len(pe.NeverAllow()) != 1 {
		t.Fatal("expected 1 never rule")
	}
	pe.SetNeverAllow(nil)
	if len(pe.NeverAllow()) != 0 {
		t.Fatal("expected 0 never rules after clear")
	}
}
