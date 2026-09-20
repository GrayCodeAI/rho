package cmd

import (
	"testing"

	"github.com/GrayCodeAI/rho/internal/permissions"
	"github.com/GrayCodeAI/rho/internal/permissions/stableid"
)

func TestInspectionToolCall_SelectsPowerShellRuntime(t *testing.T) {
	name, args, err := inspectionToolCall(stableid.KindCommand, "Get-ChildItem", "power_shell")
	if err != nil {
		t.Fatalf("inspectionToolCall returned error: %v", err)
	}
	if name != "PowerShell" {
		t.Fatalf("tool name = %q, want PowerShell", name)
	}
	if got := args["command"]; got != "Get-ChildItem" {
		t.Fatalf("command = %v, want Get-ChildItem", got)
	}
}

func TestInspectionToolCallRejectsWrongRuntimeForFile(t *testing.T) {
	_, _, err := inspectionToolCall(stableid.KindFileMutation, "README.md", "powershell")
	if err == nil {
		t.Fatal("expected --tool to be rejected for file inspection")
	}
}

func TestInspectionToolCallCanonicalizesRuntimeAlias(t *testing.T) {
	if got := permissions.CanonicalToolName("powershell"); got != "PowerShell" {
		t.Fatalf("canonical tool = %q, want PowerShell", got)
	}
}
