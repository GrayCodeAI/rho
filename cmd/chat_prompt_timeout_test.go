package cmd

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/GrayCodeAI/rho/internal/engine"
	"github.com/GrayCodeAI/rho/internal/engine/safety"
	"github.com/GrayCodeAI/rho/internal/tool"

	contracts "github.com/GrayCodeAI/rho/internal/contracts/policy"
)

func TestPermissionPromptTimeoutClearsStaleState(t *testing.T) {
	m := newTestChatModel()
	req := safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{
			ToolName: "Bash",
			Summary:  "run git status",
		},
		Response: make(chan bool, 1),
	}

	next, cmd := m.Update(permissionAskMsg{req: req})
	cm := requireChatModel(t, next)
	if cm.permReq == nil {
		t.Fatal("expected permission request to be active")
	}
	if cmd == nil {
		t.Fatal("expected timeout command for permission prompt")
	}
	seq := cm.permReqSeq

	next, _ = cm.Update(permissionPromptTimeoutMsg{seq: seq})
	cm = requireChatModel(t, next)
	if cm.permReq != nil {
		t.Fatal("expected timed-out permission request to be cleared")
	}
	if got := lastSystemMessage(cm.messages); !strings.Contains(got, "Permission prompt timed out") {
		t.Fatalf("unexpected timeout message: %q", got)
	}
}

func TestPromptCountdownIgnoresStaleGeneration(t *testing.T) {
	m := newTestChatModel()
	req := safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{ToolName: "Bash", Summary: "git status"},
		Response:          make(chan bool, 1),
	}
	next, _ := m.Update(permissionAskMsg{req: req})
	cm := requireChatModel(t, next)
	oldGeneration := cm.promptGeneration

	// Resolving the prompt advances the generation when a later prompt is
	// activated; a late tick from the old card must not restart a ticker loop.
	next, _ = cm.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	cm = requireChatModel(t, next)
	if cm.permReq != nil {
		t.Fatal("permission response should clear the active prompt")
	}
	if _, cmd := cm.Update(promptCountdownTickMsg{generation: oldGeneration}); cmd != nil {
		t.Fatal("stale countdown tick restarted after the prompt was resolved")
	}
}

func TestAskUserPromptTimeoutClearsStaleState(t *testing.T) {
	m := newTestChatModel()
	msg := askUserMsg{
		question: "Continue?",
		response: make(chan string, 1),
	}

	next, cmd := m.Update(msg)
	cm := requireChatModel(t, next)
	if cm.askReq == nil {
		t.Fatal("expected ask-user request to be active")
	}
	if cmd == nil {
		t.Fatal("expected timeout command for ask-user prompt")
	}
	seq := cm.askReqSeq

	next, _ = cm.Update(askUserPromptTimeoutMsg{seq: seq})
	cm = requireChatModel(t, next)
	if cm.askReq != nil {
		t.Fatal("expected timed-out ask-user request to be cleared")
	}
	if got := lastSystemMessage(cm.messages); !strings.Contains(got, "Question timed out") {
		t.Fatalf("unexpected timeout message: %q", got)
	}
}

func TestAskUserPromptEscDeniesAndClearsDeadline(t *testing.T) {
	m := newTestChatModel()
	response := make(chan string, 1)
	next, _ := m.Update(askUserMsg{question: "Continue?", response: response})
	cm := requireChatModel(t, next)
	if cm.askTimeoutAt.IsZero() {
		t.Fatal("expected ask-user prompt to expose a deadline")
	}

	next, _ = cm.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	cm = requireChatModel(t, next)
	if cm.askReq != nil || !cm.askTimeoutAt.IsZero() {
		t.Fatal("Esc should clear the active question and deadline")
	}
	select {
	case answer := <-response:
		if answer != "" {
			t.Fatalf("Esc response = %q, want empty denial", answer)
		}
	default:
		t.Fatal("Esc did not resolve the question response")
	}
}

func TestPromptTimeoutIgnoresNewerPrompt(t *testing.T) {
	m := newTestChatModel()

	next, _ := m.Update(askUserMsg{question: "First?", response: make(chan string, 1)})
	cm := requireChatModel(t, next)
	firstSeq := cm.askReqSeq

	next, _ = cm.Update(askUserMsg{question: "Second?", response: make(chan string, 1)})
	cm = requireChatModel(t, next)
	if cm.askReqSeq != firstSeq {
		t.Fatal("queued ask-user prompt must not replace the active prompt")
	}
	if len(cm.askQueue) != 1 {
		t.Fatalf("queued ask-user prompts = %d, want 1", len(cm.askQueue))
	}

	next, _ = cm.Update(askUserPromptTimeoutMsg{seq: firstSeq})
	cm = requireChatModel(t, next)
	if cm.askReq == nil {
		t.Fatal("timeout should activate the queued ask-user prompt")
	}
	if cm.askReqSeq == firstSeq {
		t.Fatal("queued ask-user prompt should receive a new sequence")
	}
}

func TestStreamErrClearsInteractivePromptState(t *testing.T) {
	m := newTestChatModel()
	req := safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{
			ToolName: "Bash",
			Summary:  "run git status",
		},
		Response: make(chan bool, 1),
	}
	next, _ := m.Update(permissionAskMsg{req: req})
	cm := requireChatModel(t, next)
	next, _ = cm.Update(askUserMsg{question: "Continue?", response: make(chan string, 1)})
	cm = requireChatModel(t, next)

	next, _ = cm.Update(streamErrMsg{err: errNoInteractivePromptInput})
	cm = requireChatModel(t, next)
	if cm.permReq != nil || cm.askReq != nil {
		t.Fatal("stream error should clear stale interactive prompt state")
	}
}

func TestBlockingPromptsDoNotOverwriteEachOther(t *testing.T) {
	m := newTestChatModel()
	permission := safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{ToolName: "Bash", Summary: "git status"},
		Response:          make(chan bool, 1),
	}
	credential := credentialAskMsg{
		req:      tool.CredentialRequest{Name: "git", Credential: "gitconfig"},
		response: make(chan tool.CredentialResponse, 1),
	}

	next, _ := m.Update(askUserMsg{question: "First?", response: make(chan string, 1)})
	cm := requireChatModel(t, next)
	next, _ = cm.Update(permissionAskMsg{req: permission})
	cm = requireChatModel(t, next)
	next, _ = cm.Update(credential)
	cm = requireChatModel(t, next)

	if cm.askReq == nil || len(cm.permQueue) != 1 || len(cm.credentialQueue) != 1 {
		t.Fatalf("prompts were not queued behind the active question: ask=%v permission=%d credential=%d", cm.askReq != nil, len(cm.permQueue), len(cm.credentialQueue))
	}

	next, _ = cm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	cm = requireChatModel(t, next)
	if cm.permReq == nil || len(cm.credentialQueue) != 1 {
		t.Fatalf("permission should activate before credential: permission=%v credential=%d", cm.permReq != nil, len(cm.credentialQueue))
	}

	next, _ = cm.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	cm = requireChatModel(t, next)
	if cm.credentialReq == nil {
		t.Fatal("credential prompt should activate after permission is resolved")
	}
}

func TestCredentialPromptEscDeniesImmediately(t *testing.T) {
	m := newTestChatModel()
	response := make(chan tool.CredentialResponse, 1)
	next, _ := m.Update(credentialAskMsg{
		req:      tool.CredentialRequest{Name: "git", Credential: "gitconfig"},
		response: response,
	})
	cm := requireChatModel(t, next)
	if cm.credentialReq == nil {
		t.Fatal("expected credential request to be active")
	}

	next, _ = cm.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	cm = requireChatModel(t, next)
	if cm.credentialReq != nil || !cm.credentialTimeoutAt.IsZero() {
		t.Fatal("Esc should clear the credential request and deadline")
	}
	select {
	case result := <-response:
		if result.Approved || result.Reason != "denied by user" {
			t.Fatalf("Esc response = %+v, want user denial", result)
		}
	default:
		t.Fatal("Esc did not resolve the credential response")
	}
}

func TestClearInteractivePromptsFailsClosedImmediately(t *testing.T) {
	m := newTestChatModel()
	askResponse := make(chan string, 1)
	credentialResponse := make(chan tool.CredentialResponse, 1)
	permissionResponse := make(chan bool, 1)

	m.askReq = &askUserMsg{response: askResponse}
	m.askQueue = []askUserMsg{{response: make(chan string, 1)}}
	m.credentialReq = &credentialAskMsg{response: credentialResponse}
	m.credentialQueue = []credentialAskMsg{{response: make(chan tool.CredentialResponse, 1)}}
	m.permReq = &safety.PermissionRequest{Response: permissionResponse}

	m.clearInteractivePrompts("rho is exiting")

	if m.askReq != nil || m.credentialReq != nil || m.permReq != nil || len(m.askQueue) != 0 || len(m.credentialQueue) != 0 {
		t.Fatal("interactive prompts were not fully cleared")
	}
	select {
	case <-askResponse:
	default:
		t.Fatal("active question was not resolved")
	}
	select {
	case response := <-credentialResponse:
		if response.Approved || response.Reason != "rho is exiting" {
			t.Fatalf("unexpected credential shutdown response: %+v", response)
		}
	default:
		t.Fatal("active credential request was not resolved")
	}
	select {
	case <-permissionResponse:
	default:
		t.Fatal("active permission request was not resolved")
	}
}

func TestStreamErrClearsHighRiskApprovalState(t *testing.T) {
	m := newTestChatModel()
	response := make(chan engine.ApprovalResponse, 1)
	next, _ := m.Update(approvalAskMsg{
		req:      engine.ApprovalRequest{ToolName: "WebFetch", Category: engine.ApprovalNetwork, Summary: "https://example.com"},
		response: response,
	})
	cm := requireChatModel(t, next)
	if cm.approvalReq == nil {
		t.Fatal("expected high-risk approval to be active")
	}

	next, _ = cm.Update(streamErrMsg{err: errNoInteractivePromptInput})
	cm = requireChatModel(t, next)
	if cm.approvalReq != nil || len(cm.approvalQueue) != 0 {
		t.Fatal("stream error should clear high-risk approval state")
	}
	select {
	case got := <-response:
		if got != engine.ApprovalReject {
			t.Fatalf("stream-error response = %v, want rejection", got)
		}
	default:
		t.Fatal("stream error did not resolve the approval request")
	}
}

func TestLatePermissionResolutionDoesNotBlockUI(t *testing.T) {
	m := newTestChatModel()
	req := safety.PermissionRequest{
		PermissionRequest: contracts.PermissionRequest{
			ToolName: "Bash",
			Summary:  "git status",
		},
		Response: make(chan bool, 1),
	}

	// Simulate the engine timing out/cancelling first. The channel is full,
	// but the TUI may still receive the stale prompt message afterward.
	req.Response <- true
	next, _ := m.Update(permissionAskMsg{req: req})
	cm := requireChatModel(t, next)
	next, _ = cm.Update(streamErrMsg{err: errNoInteractivePromptInput})
	cm = requireChatModel(t, next)
	if cm.permReq != nil {
		t.Fatal("stream error should clear the stale permission prompt")
	}
	select {
	case got := <-req.Response:
		if !got {
			t.Fatal("late resolution should not overwrite the already-resolved response")
		}
	default:
		t.Fatal("expected the original response to remain available")
	}
}

func TestLateAskUserResolutionDoesNotBlockUI(t *testing.T) {
	req := &askUserMsg{response: make(chan string, 1)}
	req.response <- "already resolved"
	if resolveAskUserResponse(req, "late answer") {
		t.Fatal("late ask-user resolution should not report success")
	}
	if got := <-req.response; got != "already resolved" {
		t.Fatalf("late resolution overwrote existing answer: %q", got)
	}
}

func TestLateCredentialResolutionDoesNotBlockUI(t *testing.T) {
	req := &credentialAskMsg{response: make(chan tool.CredentialResponse, 1)}
	req.response <- tool.CredentialResponse{Approved: true}
	if resolveCredentialResponse(req, tool.CredentialResponse{Approved: false, Reason: "late"}) {
		t.Fatal("late credential resolution should not report success")
	}
	got := <-req.response
	if !got.Approved || got.Reason != "" {
		t.Fatalf("late resolution overwrote existing credential response: %+v", got)
	}
}

func TestStreamErrClearsCredentialPrompt(t *testing.T) {
	m := newTestChatModel()
	response := make(chan tool.CredentialResponse, 1)
	next, _ := m.Update(credentialAskMsg{
		req:      tool.CredentialRequest{Name: "git", Credential: "gitconfig"},
		response: response,
	})
	cm := requireChatModel(t, next)
	next, _ = cm.Update(streamErrMsg{err: errNoInteractivePromptInput})
	cm = requireChatModel(t, next)
	if cm.credentialReq != nil {
		t.Fatal("stream error should clear stale credential prompt")
	}
	select {
	case got := <-response:
		if got.Approved || got.Reason != "stream ended" {
			t.Fatalf("unexpected stream-end credential response: %+v", got)
		}
	default:
		t.Fatal("stream error should resolve the credential request")
	}
}
