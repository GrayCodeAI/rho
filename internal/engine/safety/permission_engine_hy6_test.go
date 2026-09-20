package safety

import (
	"context"
	"testing"
	"time"
)

// TestCheckTool_BypassDoesNotGrantDestructiveCommands verifies the H6 fix:
// the bypass kill-switch must not allow destructive commands. The engine
// hard-denies them before the bypass branch, matching the tool layer's
// IsDestructiveCommand block.
func TestCheckTool_BypassDoesNotGrantDestructiveCommands(t *testing.T) {
	pe := NewPermissionEngine()
	if !pe.BypassKill.EnableScoped(nil, time.Time{}, "destructive-command test") {
		t.Fatal("failed to enable test bypass")
	}
	pe.Autonomy = AutonomyYOLO

	for _, cmd := range []string{"rm -rf /", "mkfs.ext4 /dev/sda", "dd if=/dev/zero of=/dev/sda"} {
		allowed, reason := pe.CheckTool(context.Background(), ToolCallInfo{Name: "Bash", Args: map[string]interface{}{"command": cmd}})
		if allowed {
			t.Errorf("bypass granted destructive command %q, want denied", cmd)
		}
		if reason == "" {
			t.Errorf("destructive command %q: expected a deny reason", cmd)
		}
	}
}

// TestCheckTool_BypassAllowsNonDestructive verifies the bypass kill-switch
// still allows non-destructive commands (its intended use).
func TestCheckTool_BypassAllowsNonDestructive(t *testing.T) {
	pe := NewPermissionEngine()
	if !pe.BypassKill.EnableScoped(nil, time.Time{}, "safe-command test") {
		t.Fatal("failed to enable test bypass")
	}

	allowed, reason := pe.CheckTool(context.Background(), ToolCallInfo{Name: "Bash", Args: map[string]interface{}{"command": "git status"}})
	if !allowed {
		t.Errorf("bypass should allow non-destructive command, got denied: %q", reason)
	}
}

// TestCheckTool_DestructiveBlockedWithoutBypass verifies destructive commands
// are denied even before the bypass branch when autonomy would otherwise allow.
func TestCheckTool_DestructiveBlockedByDefault(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Autonomy = AutonomyYOLO

	allowed, _ := pe.CheckTool(context.Background(), ToolCallInfo{Name: "Bash", Args: map[string]interface{}{"command": "rm -rf /"}})
	if allowed {
		t.Error("destructive command allowed at YOLO without bypass, want denied")
	}
}

func TestCheckTool_DestructivePowerShellBlockedByDefault(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Autonomy = AutonomyYOLO

	allowed, _ := pe.CheckTool(context.Background(), ToolCallInfo{
		Name: "PowerShell",
		Args: map[string]interface{}{"command": "rm -rf /"},
	})
	if allowed {
		t.Error("destructive PowerShell command allowed at YOLO, want denied")
	}
}

func TestCheckTool_DestructivePowerShellCmdletBlockedByDefault(t *testing.T) {
	pe := NewPermissionEngine()
	pe.Autonomy = AutonomyYOLO
	allowed, _ := pe.CheckTool(context.Background(), ToolCallInfo{
		Name: "PowerShell",
		Args: map[string]interface{}{"command": "Remove-Item -Recurse ./build"},
	})
	if allowed {
		t.Fatal("destructive PowerShell cmdlet allowed at YOLO, want denied")
	}
}
