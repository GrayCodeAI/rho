package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestPowerShellTool_EmptyCommand(t *testing.T) {
	ps := PowerShellTool{}
	_, err := ps.Execute(context.Background(), json.RawMessage(`{"command":""}`))
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestPowerShellTool_DestructiveBlocked(t *testing.T) {
	ps := PowerShellTool{}
	_, err := ps.Execute(context.Background(), json.RawMessage(`{"command":"rm -rf /"}`))
	if err == nil || err.Error() != "command blocked: contains a destructive pattern" {
		t.Fatalf("expected destructive block error, got: %v", err)
	}
}

func TestPowerShellTool_DestructiveCmdletBlocked(t *testing.T) {
	ps := PowerShellTool{}
	_, err := ps.Execute(context.Background(), json.RawMessage(`{"command":"Remove-Item -Recurse ./build"}`))
	if err == nil || err.Error() != "command blocked: contains a destructive pattern" {
		t.Fatalf("expected destructive cmdlet block, got: %v", err)
	}
}

func TestPowerShellTool_SuspiciousBlocked(t *testing.T) {
	ps := PowerShellTool{}
	_, err := ps.Execute(context.Background(), json.RawMessage(`{"command":"eval bad"}`))
	if err == nil || err.Error() != "command blocked: flagged as suspicious" {
		t.Fatalf("expected suspicious block error, got: %v", err)
	}
}

func TestPowerShellTool_InvocationBlocked(t *testing.T) {
	ps := PowerShellTool{}
	_, err := ps.Execute(context.Background(), json.RawMessage(`{"command":"Invoke-Expression 'Get-ChildItem'"}`))
	if err == nil || err.Error() != "command blocked: flagged as suspicious" {
		t.Fatalf("expected PowerShell code-evaluation block, got: %v", err)
	}
}

func TestPowerShellTool_Name(t *testing.T) {
	ps := PowerShellTool{}
	if ps.Name() != "PowerShell" {
		t.Fatalf("expected PowerShell, got %s", ps.Name())
	}
}
