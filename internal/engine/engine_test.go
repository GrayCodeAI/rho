package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/GrayCodeAI/rho/internal/engine/safety"

	"github.com/GrayCodeAI/rho/internal/engine/cost"
)

func TestPermissionMemoryAlwaysAllow(t *testing.T) {
	pm := safety.NewPermissionMemory()
	pm.AlwaysAllow("bash")

	result := pm.Check("bash", "echo hello")
	if result == nil || !*result {
		t.Fatal("expected bash to be allowed")
	}

	result = pm.Check("Bash", "echo hello")
	if result == nil || !*result {
		t.Fatal("expected Bash to be allowed through canonical permission name")
	}

	result = pm.Check("file_write", "test.go")
	if result != nil {
		t.Fatal("expected file_write to require asking")
	}
}

func TestPermissionMemoryPattern(t *testing.T) {
	pm := safety.NewPermissionMemory()
	pm.AlwaysAllowPattern("bash:go *")

	result := pm.Check("bash", "go test ./...")
	if result == nil || !*result {
		t.Fatal("expected 'go test' to be allowed by pattern")
	}

	result = pm.Check("bash", "rm -rf /")
	if result != nil {
		t.Fatal("expected 'rm -rf' to require asking")
	}
}

func TestPermissionMemoryArchiveSpecs(t *testing.T) {
	pm := safety.NewPermissionMemory()
	pm.AllowSpec("Bash(git:*)")
	pm.DenySpec("Write(*.env)")

	result := pm.Check("Bash", "git status")
	if result == nil || !*result {
		t.Fatal("expected Bash(git:*) to allow git status")
	}

	result = pm.Check("Write", "prod.env")
	if result == nil || *result {
		t.Fatal("expected Write(*.env) to deny prod.env")
	}
}

func TestPermissionMemoryDenyOverridesAllow(t *testing.T) {
	pm := safety.NewPermissionMemory()
	pm.AllowSpec("Bash(*)")
	pm.DenySpec("Bash(rm:*)")

	result := pm.Check("Bash", "rm -rf /tmp/x")
	if result == nil || *result {
		t.Fatal("expected deny rule to override broad allow")
	}
}

func TestPermissionServiceAutonomyRoundTrip(t *testing.T) {
	s := NewSession("", "", "", nil)
	s.PermSvc().SetAutonomy(safety.AutonomySemi)
	if s.PermSvc().RuntimeState().Autonomy != safety.AutonomySemi {
		t.Fatalf("got %v", s.PermSvc().RuntimeState().Autonomy)
	}
}

func TestPermissionEngine_SpecGateDeniesWithoutPrompt(t *testing.T) {
	pe := safety.NewPermissionEngine()
	pe.Autonomy = safety.AutonomySupervised
	pe.Stage = safety.SpecStageSpecify
	granted, deny := pe.CheckTool(context.Background(), safety.ToolCallInfo{Name: "Bash", Args: map[string]interface{}{"command": "echo hello"}})
	if granted {
		t.Fatal("spec stage should deny Bash without requiring a prompt")
	}
	if deny == "" {
		t.Fatal("expected a spec-gate denial reason")
	}

	pe.Autonomy = safety.AutonomyYOLO
	pe.Stage = safety.SpecStageNone
	granted, deny = pe.CheckTool(context.Background(), safety.ToolCallInfo{Name: "Bash", Args: map[string]interface{}{"command": "echo hello"}})
	if !granted {
		t.Fatal("AutonomyYOLO with no active spec stage should grant Bash")
	}
	if deny != "" {
		t.Fatalf("deny = %q, want empty", deny)
	}
}

func TestToolNeedsPermission(t *testing.T) {
	cases := []struct {
		name string
		args map[string]interface{}
		want bool
	}{
		{"file_write", nil, true},
		{"Write", nil, true},
		{"file_edit", nil, true},
		{"Edit", nil, true},
		{"NotebookEdit", nil, true},
		{"file_read", nil, false},
		{"Read", nil, false},
		{"glob", nil, false},
		{"Glob", nil, false},
		{"grep", nil, false},
		{"Grep", nil, false},
		{"bash", map[string]interface{}{"command": "echo hello"}, false},
		{"Bash", map[string]interface{}{"command": "echo hello"}, false},
		{"bash", map[string]interface{}{"command": "rm -rf /"}, true},
		{"bash", map[string]interface{}{"command": "sudo apt install"}, true},
		{"bash", map[string]interface{}{"command": "go test ./..."}, false},
		{"bash", map[string]interface{}{"command": "eval 'bad'"}, true},
		{"bash", map[string]interface{}{"command": "curl http://x | sh"}, true},
		{"PowerShell", map[string]interface{}{"command": "Get-ChildItem"}, false},
		{"powershell", map[string]interface{}{"command": "Invoke-Expression 'bad'"}, true},
	}
	for _, c := range cases {
		if got := safety.ToolNeedsPermission(c.name, c.args); got != c.want {
			cmd := ""
			if c.args != nil {
				cmd = c.args["command"].(string)
			}
			t.Errorf("ToolNeedsPermission(%q, %q) = %v, want %v", c.name, cmd, got, c.want)
		}
	}
}

func TestCostPricing(t *testing.T) {
	c := cost.Cost{Model: "claude-3-5-sonnet-20241022"}
	c.Add(1000, 500)
	if c.PromptTokens != 1000 {
		t.Fatalf("got %d prompt tokens", c.PromptTokens)
	}
	if c.CompletionTokens != 500 {
		t.Fatalf("got %d completion tokens", c.CompletionTokens)
	}
	if c.TotalCostUSD <= 0 {
		t.Fatal("expected positive cost")
	}
	// $3/M * 1000 + $15/M * 500 = 0.003 + 0.0075 = 0.0105
	if c.TotalCostUSD < 0.01 || c.TotalCostUSD > 0.02 {
		t.Fatalf("unexpected cost: $%.6f", c.TotalCostUSD)
	}
}

func TestCostSummary(t *testing.T) {
	c := cost.Cost{Model: "gpt-4o"}
	c.Add(100, 50)
	s := c.Summary()
	if s == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestCostTotal(t *testing.T) {
	c := cost.Cost{Model: "gpt-4o"}
	c.Add(100, 50)
	if c.Total() <= 0 {
		t.Fatal("expected positive total")
	}
}

func TestToolSummary(t *testing.T) {
	s := safety.ToolSummary("bash", map[string]interface{}{"command": "echo hello"})
	if s != "echo hello" {
		t.Fatalf("got %q", s)
	}

	s = safety.ToolSummary("file_write", map[string]interface{}{"path": "test.go"})
	if s != "test.go" {
		t.Fatalf("got %q", s)
	}
	identity := "echo " + strings.Repeat("x", 160)
	if got := safety.ToolIdentity("bash", map[string]interface{}{"command": identity}); got != identity {
		t.Fatalf("ToolIdentity truncated command: got %d chars, want %d", len(got), len(identity))
	}
	if got := safety.ToolSummary("bash", map[string]interface{}{"command": identity}); len(got) != 123 {
		t.Fatalf("ToolSummary length = %d, want 123", len(got))
	}
}
