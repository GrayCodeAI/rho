package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine"
	"github.com/GrayCodeAI/rho/internal/engine/safety"
	"github.com/GrayCodeAI/rho/internal/permissions"
	"github.com/GrayCodeAI/rho/internal/permissions/stableid"

	tea "charm.land/bubbletea/v2"
	contracts "github.com/GrayCodeAI/rho/internal/contracts/policy"
)

func TestHighRiskApprovalUsesKeyboardActions(t *testing.T) {
	m := newTestChatModel()
	response := make(chan engine.ApprovalResponse, 1)
	next, _ := m.Update(approvalAskMsg{
		req:      engine.ApprovalRequest{ToolName: "Bash", Category: engine.ApprovalNetwork, Summary: "curl https://example.com"},
		response: response,
	})
	cm := requireChatModel(t, next)
	if cm.approvalReq == nil {
		t.Fatal("expected active high-risk approval")
	}

	next, _ = cm.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	cm = requireChatModel(t, next)
	if cm.approvalReq != nil {
		t.Fatal("expected approval request to clear")
	}
	select {
	case got := <-response:
		if got != engine.ApprovalApproveForSession {
			t.Fatalf("approval response = %v, want session approval", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for approval response")
	}
}

func TestPermissionAlwaysAllowDoesNotNilDeref(t *testing.T) {
	m := newTestChatModel()
	req := safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{
			ToolName: "Bash",
			Summary:  "git -C /tmp status",
			Identity: "git -C /tmp status",
		},
		Response: make(chan bool, 1),
	}

	next, _ := m.Update(permissionAskMsg{req: req})
	cm := requireChatModel(t, next)
	if cm.permReq == nil {
		t.Fatal("expected active permission request")
	}

	next, _ = cm.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	cm = requireChatModel(t, next)
	if cm.permReq != nil {
		t.Fatal("expected permission request cleared after always-allow")
	}
	select {
	case allowed := <-req.Response:
		if !allowed {
			t.Fatal("expected always-allow to approve the request")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for permission response")
	}
	if got := lastSystemMessage(cm.messages); !strings.Contains(got, "Allowed for this session: Bash") {
		t.Fatalf("unexpected session-allow message: %q", got)
	}
	decision := cm.session.PermSvc().Memory().Check("Bash", "git -C /tmp status")
	if decision == nil || !*decision {
		t.Fatal("expected exact session allow rule to be recorded")
	}
	if decision := cm.session.PermSvc().Memory().Check("Bash", "rm -rf /"); decision != nil {
		t.Fatalf("session allow unexpectedly authorized a different command: %v", *decision)
	}
}

func TestPermissionSessionAllowUsesCanonicalIdentity(t *testing.T) {
	m := newTestChatModel()
	req := safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{
			ToolName: "Write",
			Summary:  "README.md",
			Identity: "README.md",
		},
		Response: make(chan bool, 1),
	}
	next, _ := m.Update(permissionAskMsg{req: req})
	cm := requireChatModel(t, next)
	next, _ = cm.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	cm = requireChatModel(t, next)
	if got := cm.session.PermSvc().Memory().Check("Write", "README.md"); got == nil || !*got {
		t.Fatal("expected the approved file identity to be allowed")
	}
	if got := cm.session.PermSvc().Memory().Check("Write", ".env"); got != nil {
		t.Fatalf("session allow widened from one path to another: %v", *got)
	}
}

func TestStalePermissionKeyDoesNotLearnRule(t *testing.T) {
	m := newTestChatModel()
	req := safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{
			ToolName: "Bash",
			Summary:  "git status",
			Identity: "git status",
		},
		// No receiver models an engine that already stopped waiting.
		Response: make(chan bool),
	}
	next, _ := m.Update(permissionAskMsg{req: req})
	cm := requireChatModel(t, next)
	next, _ = cm.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	cm = requireChatModel(t, next)
	if got := cm.session.PermSvc().Memory().Check("Bash", "git status"); got != nil {
		t.Fatalf("stale session prompt learned a rule: %v", *got)
	}
	if got := lastSystemMessage(cm.messages); !strings.Contains(got, "expired") {
		t.Fatalf("expected stale-prompt diagnostic, got %q", got)
	}
}

func TestPermissionAllowOnceDoesNotLearnRule(t *testing.T) {
	m := newTestChatModel()
	req := safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{
			ToolName: "Bash",
			Summary:  "git status",
		},
		Response: make(chan bool, 1),
	}

	next, _ := m.Update(permissionAskMsg{req: req})
	cm := requireChatModel(t, next)
	next, _ = cm.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	cm = requireChatModel(t, next)
	select {
	case allowed := <-req.Response:
		if !allowed {
			t.Fatal("expected allow-once to approve the request")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for allow-once response")
	}
	if got := cm.session.PermSvc().Memory().Check("Bash", "another command"); got != nil {
		t.Fatalf("allow once unexpectedly created a remembered rule: %v", *got)
	}
	if got := lastSystemMessage(cm.messages); !strings.Contains(got, "Allowed once") {
		t.Fatalf("unexpected allow-once message: %q", got)
	}
}

func TestPermissionAlwaysDenyDoesNotNilDeref(t *testing.T) {
	m := newTestChatModel()
	req := safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{
			ToolName: "Bash",
			Summary:  "rm -rf /",
		},
		Response: make(chan bool, 1),
	}

	next, _ := m.Update(permissionAskMsg{req: req})
	cm := requireChatModel(t, next)

	next, _ = cm.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	cm = requireChatModel(t, next)
	if cm.permReq != nil {
		t.Fatal("expected permission request cleared after always-deny")
	}
	select {
	case allowed := <-req.Response:
		if allowed {
			t.Fatal("expected always-deny to reject the request")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for permission response")
	}
	if got := lastSystemMessage(cm.messages); !strings.Contains(got, "Denied for this session: Bash") {
		t.Fatalf("unexpected session-deny message: %q", got)
	}
}

func TestPermissionEscapeDeniesOnce(t *testing.T) {
	m := newTestChatModel()
	req := safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{
			ToolName: "Write",
			Summary:  "update README.md",
		},
		Response: make(chan bool, 1),
	}

	next, _ := m.Update(permissionAskMsg{req: req})
	cm := requireChatModel(t, next)
	next, _ = cm.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	cm = requireChatModel(t, next)
	if cm.permReq != nil {
		t.Fatal("escape should clear the permission request")
	}
	select {
	case allowed := <-req.Response:
		if allowed {
			t.Fatal("escape should deny the request")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for escape response")
	}
	if got := lastSystemMessage(cm.messages); !strings.Contains(got, "Denied once") {
		t.Fatalf("unexpected escape message: %q", got)
	}
}

func TestPermissionPersistExactAction(t *testing.T) {
	m := newTestChatModel()
	store := permissions.NewStableRuleStore(t.TempDir() + "/stable-rules.json")
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	m.session.PermSvc().SetExactRuleStore(store)
	identity := "git status --porcelain " + strings.Repeat("x", 140)
	req := safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{ToolName: "Bash", Summary: identity[:120] + "...", Identity: identity},
		Response:          make(chan bool, 1),
	}
	next, _ := m.Update(permissionAskMsg{req: req})
	cm := requireChatModel(t, next)
	next, _ = cm.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	cm = requireChatModel(t, next)
	if cm.permReq != nil {
		t.Fatal("expected exact permission prompt to close")
	}
	select {
	case allowed := <-req.Response:
		if !allowed {
			t.Fatal("expected exact action to be allowed")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for exact permission response")
	}
	rules := store.List()
	if len(rules) != 1 || rules[0].DisplayIdentity != identity {
		t.Fatalf("unexpected persisted exact rules: %+v", rules)
	}
	if got := lastSystemMessage(cm.messages); !strings.Contains(got, "Allowed exact action") {
		t.Fatalf("unexpected exact-rule message: %q", got)
	}
}

func TestPermissionPersistPowerShellUsesCommandRuleKind(t *testing.T) {
	m := newTestChatModel()
	store := permissions.NewStableRuleStore(t.TempDir() + "/stable-rules.json")
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	m.session.PermSvc().SetExactRuleStore(store)
	identity := "Get-ChildItem -Path ."
	req := safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{ToolName: "PowerShell", Summary: identity, Identity: identity},
		Response:          make(chan bool, 1),
	}
	next, _ := m.Update(permissionAskMsg{req: req})
	cm := requireChatModel(t, next)
	next, _ = cm.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	cm = requireChatModel(t, next)
	if cm.permReq != nil {
		t.Fatal("expected exact PowerShell permission prompt to close")
	}
	if allowed := <-req.Response; !allowed {
		t.Fatal("expected exact PowerShell action to be allowed")
	}
	rules := store.List()
	if len(rules) != 1 || rules[0].Key.Kind != stableid.KindCommand {
		t.Fatalf("PowerShell rule kind = %+v, want command", rules)
	}

	pe := safety.NewPermissionEngine()
	pe.ExactRules = store
	d := pe.CheckToolDecision(context.Background(), safety.ToolCallInfo{
		Name: "PowerShell", Args: map[string]interface{}{"command": identity},
	})
	if d.Outcome != safety.DecisionAllow {
		t.Fatalf("persisted PowerShell exact rule decision = %#v, want allow", d)
	}
}

func TestPermissionPersistExactActionFailsClosedWithoutStore(t *testing.T) {
	m := newTestChatModel()
	req := safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{ToolName: "Bash", Summary: "git status", Identity: "git status"},
		Response:          make(chan bool, 1),
	}
	next, _ := m.Update(permissionAskMsg{req: req})
	cm := requireChatModel(t, next)
	next, _ = cm.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	cm = requireChatModel(t, next)
	select {
	case allowed := <-req.Response:
		if allowed {
			t.Fatal("project approval must deny when durable rule storage is unavailable")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for fail-closed project response")
	}
	if got := lastSystemMessage(cm.messages); !strings.Contains(got, "storage unavailable") {
		t.Fatalf("unexpected fail-closed message: %q", got)
	}
}

func TestPermissionRequestsQueueAndAdvance(t *testing.T) {
	m := newTestChatModel()
	first := safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{ToolName: "Bash", Summary: "git status"},
		Response:          make(chan bool, 1),
	}
	second := safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{ToolName: "Write", Summary: "README.md"},
		Response:          make(chan bool, 1),
	}

	next, _ := m.Update(permissionAskMsg{req: first})
	cm := requireChatModel(t, next)
	next, _ = cm.Update(permissionAskMsg{req: second})
	cm = requireChatModel(t, next)
	if cm.permReq == nil || cm.permReq.ToolName != "Bash" {
		t.Fatalf("active permission was overwritten: %+v", cm.permReq)
	}
	if len(cm.permQueue) != 1 || cm.permQueue[0].ToolName != "Write" {
		t.Fatalf("expected one queued permission, got %+v", cm.permQueue)
	}

	next, _ = cm.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	cm = requireChatModel(t, next)
	if cm.permReq == nil || cm.permReq.ToolName != "Write" || len(cm.permQueue) != 0 {
		t.Fatalf("next permission was not activated: active=%+v queue=%d", cm.permReq, len(cm.permQueue))
	}
	select {
	case allowed := <-first.Response:
		if !allowed {
			t.Fatal("first permission should be allowed")
		}
	default:
		t.Fatal("first permission was not resolved")
	}
	select {
	case <-second.Response:
		t.Fatal("queued permission resolved before it was reviewed")
	default:
	}

	next, _ = cm.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	cm = requireChatModel(t, next)
	if cm.permReq != nil {
		t.Fatal("expected permission queue to be empty")
	}
	select {
	case allowed := <-second.Response:
		if allowed {
			t.Fatal("second permission should be denied")
		}
	default:
		t.Fatal("second permission was not resolved")
	}
}

func TestPermissionAndHighRiskPromptsAreSerialized(t *testing.T) {
	m := newTestChatModel()
	permissionResponse := make(chan bool, 1)
	next, _ := m.Update(permissionAskMsg{req: safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{ToolName: "Write", Summary: "README.md", Identity: "README.md"},
		Response:          permissionResponse,
	}})
	cm := requireChatModel(t, next)
	if cm.permReq == nil {
		t.Fatal("expected permission prompt to be active")
	}

	approvalResponse := make(chan engine.ApprovalResponse, 1)
	next, _ = cm.Update(approvalAskMsg{
		req:      engine.ApprovalRequest{ToolName: "Bash", Category: engine.ApprovalNetwork, Summary: "curl https://example.com"},
		response: approvalResponse,
	})
	cm = requireChatModel(t, next)
	if cm.permReq == nil || cm.approvalReq != nil || len(cm.approvalQueue) != 1 {
		t.Fatalf("prompts were not serialized: permission=%v approval=%v queue=%d", cm.permReq != nil, cm.approvalReq != nil, len(cm.approvalQueue))
	}

	next, _ = cm.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	cm = requireChatModel(t, next)
	if cm.permReq != nil || cm.approvalReq == nil {
		t.Fatal("queued high-risk approval was not activated after permission response")
	}
	if got := <-permissionResponse; !got {
		t.Fatal("permission response should be allowed")
	}
}

func TestPermissionRequestQueueFailsClosedAtCapacity(t *testing.T) {
	m := newTestChatModel()
	active := safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{ToolName: "Bash", Summary: "git status"},
		Response:          make(chan bool, 1),
	}
	next, _ := m.Update(permissionAskMsg{req: active})
	m = requireChatModel(t, next)

	for i := 0; i < maxPendingPrompts; i++ {
		req := safety.PermissionRequest{
			PermissionRequest: contracts.PermissionRequest{ToolName: "Write", Summary: "file.txt"},
			Response:          make(chan bool, 1),
		}
		next, _ = m.Update(permissionAskMsg{req: req})
		m = requireChatModel(t, next)
	}
	if len(m.permQueue) != maxPendingPrompts {
		t.Fatalf("queue length = %d, want %d", len(m.permQueue), maxPendingPrompts)
	}

	overflow := safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{ToolName: "Edit", Summary: "overflow.txt"},
		Response:          make(chan bool, 1),
	}
	next, _ = m.Update(permissionAskMsg{req: overflow})
	m = requireChatModel(t, next)
	if len(m.permQueue) != maxPendingPrompts {
		t.Fatalf("overflow changed queue length to %d", len(m.permQueue))
	}
	select {
	case allowed := <-overflow.Response:
		if allowed {
			t.Fatal("overflow request was allowed")
		}
	default:
		t.Fatal("overflow request was not denied immediately")
	}
}
