package cmd

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/GrayCodeAI/rho/internal/engine"
	"github.com/GrayCodeAI/rho/internal/engine/safety"
	"github.com/GrayCodeAI/rho/internal/permissions/stableid"
	"github.com/GrayCodeAI/rho/internal/tool"
	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

// maxPendingPrompts bounds memory and waiting tool calls when a provider emits
// a burst of interactive requests. Once the active prompt and this queue are
// full, new requests fail closed instead of creating unbounded pressure.
const maxPendingPrompts = 32

func (m chatModel) hasActiveInteractivePrompt() bool {
	return m.permReq != nil || m.approvalReq != nil || m.askReq != nil || m.credentialReq != nil
}

// Prompt handlers own the UI side of interactive tool approvals. The safety
// engine owns policy; this adapter only records the pending request, renders
// it, and resolves the request channel exactly once.

// resolvePermissionResponse is deliberately non-blocking. The engine may
// already have returned because its context or timeout fired while the TUI
// still had the prompt on screen. A second send must never freeze the UI.
func resolvePermissionResponse(req *safety.PermissionRequest, allowed bool) bool {
	if req == nil || req.Response == nil {
		return false
	}
	select {
	case req.Response <- allowed:
		return true
	default:
		return false
	}
}

func resolveAskUserResponse(req *askUserMsg, answer string) bool {
	if req == nil || req.response == nil {
		return false
	}
	select {
	case req.response <- answer:
		return true
	default:
		return false
	}
}

func resolveApprovalResponse(req *approvalAskMsg, response engine.ApprovalResponse) bool {
	if req == nil || req.response == nil {
		return false
	}
	select {
	case req.response <- response:
		return true
	default:
		return false
	}
}

func resolveCredentialResponse(req *credentialAskMsg, response tool.CredentialResponse) bool {
	if req == nil || req.response == nil {
		return false
	}
	select {
	case req.response <- response:
		return true
	default:
		return false
	}
}

func exactPermissionKind(toolName string) stableid.Kind {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "bash", "powershell", "power_shell":
		return stableid.KindCommand
	case "write", "edit", "notebookedit":
		return stableid.KindFileMutation
	default:
		return stableid.KindStructuredTool
	}
}

func rememberExactPermission(m chatModel, req *safety.PermissionRequest, decision stableid.Decision) (uint64, bool) {
	if m.session == nil {
		return 0, false
	}
	return rememberExactPermissionForSession(m.session, req, decision)
}

func rememberExactPermissionForSession(sess *engine.Session, req *safety.PermissionRequest, decision stableid.Decision) (uint64, bool) {
	if sess == nil || sess.PermSvc() == nil || req == nil {
		return 0, false
	}
	identity := strings.TrimSpace(req.Identity)
	if identity == "" {
		identity = strings.TrimSpace(req.Summary)
	}
	if identity == "" {
		return 0, false
	}
	kind := exactPermissionKind(req.ToolName)
	return sess.PermSvc().RememberExact(kind, stableid.CanonicalIdentity(kind, identity), identity, decision)
}

func exactPermissionReady(sess *engine.Session, req *safety.PermissionRequest) bool {
	if sess == nil || sess.PermSvc() == nil || req == nil || !sess.PermSvc().ExactRulesConfigured() {
		return false
	}
	return strings.TrimSpace(req.Identity) != "" || strings.TrimSpace(req.Summary) != ""
}

// rememberSessionPermission stores one literal action in ephemeral session
// memory. Session approval must never widen to a tool-wide rule: the user is
// approving the visible call, not every future call made by that tool.
func rememberSessionPermission(sess *engine.Session, req *safety.PermissionRequest, allow bool) bool {
	if sess == nil || sess.PermSvc() == nil || req == nil {
		return false
	}
	identity := strings.TrimSpace(req.Identity)
	if identity == "" {
		identity = strings.TrimSpace(req.Summary)
	}
	if identity == "" {
		return false
	}
	return sess.PermSvc().RememberSessionExact(req.ToolName, identity, allow)
}

func (m chatModel) handlePermissionAsk(msg permissionAskMsg) (tea.Model, tea.Cmd) {
	// Tool execution can be concurrent. Never overwrite an active prompt: doing
	// so strands the older tool waiting on its response channel forever.
	if m.hasActiveInteractivePrompt() {
		if len(m.permQueue) >= maxPendingPrompts {
			resolvePermissionResponse(&msg.req, false)
			m.messages = append(m.messages, displayMsg{
				role:    "error",
				content: "Permission queue full — request denied; review the active prompt before continuing.",
			})
			m.viewDirty = true
			m.updateViewportContent()
			return m, nil
		}
		m.permQueue = append(m.permQueue, msg.req)
		m.messages = append(m.messages, displayMsg{
			role:    "system",
			content: fmt.Sprintf("Permission request queued (%d waiting)", len(m.permQueue)),
		})
		m.viewDirty = true
		m.updateViewportContent()
		return m, nil
	}
	return m.activatePermissionRequest(msg.req)
}

func (m chatModel) activatePermissionRequest(req safety.PermissionRequest) (tea.Model, tea.Cmd) {
	// Autonomous mode is policy-driven, not interactive. The safety engine has
	// already classified this request as requiring a permission callback; at
	// this tier the user explicitly chose for rho to resolve it without a
	// human-in-the-loop card.
	if m.session != nil && m.session.PermSvc() != nil && m.session.PermSvc().RuntimeState().Autonomy == safety.AutonomyYOLO {
		resolvePermissionResponse(&req, true)
		return m.activateNextPermissionRequest()
	}
	m.permReq = &req
	m.permReqSeq++
	m.promptGeneration++
	m.permTimeoutAt = time.Now().Add(interactivePromptTimeout)
	permBody := safety.FormatPermissionDisplay(req.ToolName, req.Summary)
	m.messages = append(m.messages, displayMsg{role: "permission", content: permBody, timeoutAt: m.permTimeoutAt})
	m.viewDirty = true
	m.updateViewportContent()
	return m, tea.Batch(permissionPromptTimeoutCmd(m.permReqSeq), promptCountdownTickCmd(m.promptGeneration))
}

func (m chatModel) activateNextPermissionRequest() (tea.Model, tea.Cmd) {
	return m.activateNextPrompt()
}

func (m *chatModel) clearPermissionRequests() {
	if m == nil {
		return
	}
	if m.permReq != nil {
		resolvePermissionResponse(m.permReq, false)
	}
	for i := range m.permQueue {
		resolvePermissionResponse(&m.permQueue[i], false)
	}
	m.permReq = nil
	m.permQueue = nil
	m.permTimeoutAt = time.Time{}
}

func (m chatModel) handlePermissionTimeout(msg permissionPromptTimeoutMsg) (tea.Model, tea.Cmd) {
	if m.permReq != nil && m.permReqSeq == msg.seq {
		resolvePermissionResponse(m.permReq, false)
		m.permReq = nil
		m.permTimeoutAt = time.Time{}
		m.messages = append(m.messages, displayMsg{role: "system", content: icons.Timer() + " Permission prompt timed out — denied."})
		m.viewDirty = true
		m.updateViewportContent()
		return m.activateNextPermissionRequest()
	}
	return m, nil
}

func (m chatModel) handlePermissionResponse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.permReq == nil {
		return m, nil
	}
	switch msg.String() {
	case "y", "Y":
		req := m.permReq
		if !resolvePermissionResponse(req, true) {
			m.permReq = nil
			m.permTimeoutAt = time.Time{}
			m.messages = append(m.messages, displayMsg{role: "error", content: "Permission prompt expired; request was not allowed."})
			m.viewDirty = true
			m.updateViewportContent()
			return m.activateNextPermissionRequest()
		}
		m.permReq = nil
		m.permTimeoutAt = time.Time{}
		m.messages = append(m.messages, displayMsg{role: "system", content: icons.CheckBold() + " Allowed once"})
	case "n", "N", "esc", "escape":
		req := m.permReq
		if !resolvePermissionResponse(req, false) {
			m.permReq = nil
			m.permTimeoutAt = time.Time{}
			m.messages = append(m.messages, displayMsg{role: "error", content: "Permission prompt expired; denial was already resolved."})
			m.viewDirty = true
			m.updateViewportContent()
			return m.activateNextPermissionRequest()
		}
		m.permReq = nil
		m.permTimeoutAt = time.Time{}
		m.messages = append(m.messages, displayMsg{role: "system", content: icons.CloseThick() + " Denied once"})
	case "a", "A", "s", "S":
		req := m.permReq
		toolName := req.ToolName
		// Session allow is deliberately exact. Approving one shell command or
		// file path must never silently authorize every action for that tool.
		if !resolvePermissionResponse(req, true) {
			m.permReq = nil
			m.permTimeoutAt = time.Time{}
			m.messages = append(m.messages, displayMsg{role: "error", content: "Permission prompt expired; session rule was not saved."})
			m.viewDirty = true
			m.updateViewportContent()
			return m.activateNextPermissionRequest()
		}
		m.permReq = nil
		m.permTimeoutAt = time.Time{}
		if rememberSessionPermission(m.session, req, true) {
			m.messages = append(m.messages, displayMsg{role: "system", content: icons.CheckBold() + " Allowed for this session: " + toolName + " (this action only)"})
		} else {
			m.messages = append(m.messages, displayMsg{role: "error", content: "Allowed once; session rule was not saved."})
		}
	case "d", "D":
		req := m.permReq
		toolName := req.ToolName
		if !resolvePermissionResponse(req, false) {
			m.permReq = nil
			m.permTimeoutAt = time.Time{}
			m.messages = append(m.messages, displayMsg{role: "error", content: "Permission prompt expired; session rule was not saved."})
			m.viewDirty = true
			m.updateViewportContent()
			return m.activateNextPermissionRequest()
		}
		m.permReq = nil
		m.permTimeoutAt = time.Time{}
		if m.session != nil && m.session.PermSvc() != nil {
			_ = m.session.PermSvc().RememberSessionTool(toolName, false)
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: icons.CloseThick() + " Denied for this session: " + toolName + " (all actions for this tool)"})
	case "p", "P", "x", "X":
		req := m.permReq
		decision := stableid.Allow
		verb := "Allowed"
		if strings.EqualFold(msg.String(), "x") {
			decision = stableid.Deny
			verb = "Denied"
		}
		// A project decision is durable policy, not a one-shot approval. Do
		// not release the tool if the durable store is unavailable.
		if !exactPermissionReady(m.session, req) {
			resolvePermissionResponse(req, false)
			m.permReq = nil
			m.permTimeoutAt = time.Time{}
			m.messages = append(m.messages, displayMsg{role: "error", content: "Project permission storage unavailable; denied."})
			m.viewDirty = true
			m.updateViewportContent()
			return m.activateNextPermissionRequest()
		}
		// Persist an allow before releasing the waiting tool. Otherwise a
		// storage failure could execute an action the user intended to make
		// durable while the UI reports that the project rule was saved.
		id, ok := rememberExactPermission(m, req, decision)
		if decision == stableid.Allow && !ok {
			resolvePermissionResponse(req, false)
			m.permReq = nil
			m.permTimeoutAt = time.Time{}
			m.messages = append(m.messages, displayMsg{role: "error", content: "Project permission rule could not be saved; denied."})
			m.viewDirty = true
			m.updateViewportContent()
			return m.activateNextPermissionRequest()
		}
		if !resolvePermissionResponse(req, decision == stableid.Allow) {
			m.permReq = nil
			m.permTimeoutAt = time.Time{}
			message := "Permission prompt expired; exact rule was saved but the request was denied."
			if !ok {
				message = "Permission prompt expired; exact rule was not saved and the request was denied."
			}
			m.messages = append(m.messages, displayMsg{role: "error", content: message})
			m.viewDirty = true
			m.updateViewportContent()
			return m.activateNextPermissionRequest()
		}
		m.permReq = nil
		m.permTimeoutAt = time.Time{}
		if ok {
			m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("%s exact action for this project (#%d)", verb, id)})
		} else {
			content := "Denied once; exact permission rule was not saved."
			if decision == stableid.Allow {
				content = "Allowed once; exact permission rule was not saved."
			}
			m.messages = append(m.messages, displayMsg{role: "error", content: content})
		}
	}
	m.viewDirty = true
	m.updateViewportContent()
	return m.activateNextPermissionRequest()
}

func (m chatModel) handleAskUser(msg askUserMsg) (tea.Model, tea.Cmd) {
	if m.hasActiveInteractivePrompt() {
		if len(m.askQueue) >= maxPendingPrompts {
			resolveAskUserResponse(&msg, "")
			m.messages = append(m.messages, displayMsg{role: "error", content: "Question queue full — request denied; review the active prompt before continuing."})
			m.viewDirty = true
			m.updateViewportContent()
			return m, nil
		}
		m.askQueue = append(m.askQueue, msg)
		m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Question queued (%d waiting)", len(m.askQueue))})
		m.viewDirty = true
		m.updateViewportContent()
		return m, nil
	}
	return m.activateAskUserRequest(msg)
}

func (m chatModel) activateAskUserRequest(msg askUserMsg) (tea.Model, tea.Cmd) {
	m.askReq = &msg
	m.askReqSeq++
	m.promptGeneration++
	m.askTimeoutAt = time.Now().Add(interactivePromptTimeout)
	m.messages = append(m.messages, displayMsg{role: "question", content: msg.question, timeoutAt: m.askTimeoutAt})
	m.viewDirty = true
	m.input.Focus()
	m.input.SetValue("")
	m.updateViewportContent()
	return m, tea.Batch(askUserPromptTimeoutCmd(m.askReqSeq), promptCountdownTickCmd(m.promptGeneration))
}

func (m chatModel) handleApprovalAsk(msg approvalAskMsg) (tea.Model, tea.Cmd) {
	if m.hasActiveInteractivePrompt() {
		if len(m.approvalQueue) >= maxPendingPrompts {
			resolveApprovalResponse(&msg, engine.ApprovalReject)
			m.messages = append(m.messages, displayMsg{role: "error", content: "Approval queue full — request denied."})
			m.viewDirty = true
			m.updateViewportContent()
			return m, nil
		}
		m.approvalQueue = append(m.approvalQueue, msg)
		m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("High-risk approval queued (%d waiting)", len(m.approvalQueue))})
		m.viewDirty = true
		m.updateViewportContent()
		return m, nil
	}
	return m.activateApprovalRequest(msg)
}

func (m chatModel) activateApprovalRequest(req approvalAskMsg) (tea.Model, tea.Cmd) {
	m.approvalReq = &req
	m.approvalReqSeq++
	m.promptGeneration++
	m.approvalTimeoutAt = time.Now().Add(interactivePromptTimeout)
	summary := fmt.Sprintf("%s: %s", req.req.Category, req.req.Summary)
	m.messages = append(m.messages, displayMsg{role: "approval", content: summary, timeoutAt: m.approvalTimeoutAt})
	m.viewDirty = true
	m.updateViewportContent()
	return m, tea.Batch(approvalPromptTimeoutCmd(m.approvalReqSeq), promptCountdownTickCmd(m.promptGeneration))
}

func (m chatModel) handlePromptCountdownTick(msg promptCountdownTickMsg) (tea.Model, tea.Cmd) {
	if msg.generation != m.promptGeneration || (m.permReq == nil && m.approvalReq == nil && m.askReq == nil && m.credentialReq == nil) {
		return m, nil
	}
	if m.permTimeoutAt.IsZero() && m.approvalTimeoutAt.IsZero() && m.askTimeoutAt.IsZero() && m.credentialTimeoutAt.IsZero() {
		return m, nil
	}
	m.viewDirty = true
	m.updateViewportContent()
	return m, promptCountdownTickCmd(msg.generation)
}

func (m chatModel) activateNextApprovalRequest() (tea.Model, tea.Cmd) {
	return m.activateNextPrompt()
}

// activateNextPrompt serializes all blocking approval surfaces. Permission
// and high-risk approval requests arrive from different engine stages, but
// the user has one keyboard and one visible decision surface. High-risk
// approvals take priority once the current prompt is resolved.
func (m chatModel) activateNextPrompt() (tea.Model, tea.Cmd) {
	if m.hasActiveInteractivePrompt() {
		return m, nil
	}
	if len(m.approvalQueue) > 0 {
		req := m.approvalQueue[0]
		m.approvalQueue = m.approvalQueue[1:]
		return m.activateApprovalRequest(req)
	}
	if len(m.permQueue) > 0 {
		req := m.permQueue[0]
		m.permQueue = m.permQueue[1:]
		return m.activatePermissionRequest(req)
	}
	if len(m.credentialQueue) > 0 {
		req := m.credentialQueue[0]
		m.credentialQueue = m.credentialQueue[1:]
		return m.activateCredentialRequest(req)
	}
	if len(m.askQueue) > 0 {
		req := m.askQueue[0]
		m.askQueue = m.askQueue[1:]
		return m.activateAskUserRequest(req)
	}
	return m, nil
}

func (m *chatModel) clearApprovalRequests() {
	if m == nil {
		return
	}
	if m.approvalReq != nil {
		resolveApprovalResponse(m.approvalReq, engine.ApprovalReject)
	}
	for i := range m.approvalQueue {
		resolveApprovalResponse(&m.approvalQueue[i], engine.ApprovalReject)
	}
	m.approvalReq = nil
	m.approvalQueue = nil
	m.approvalTimeoutAt = time.Time{}
}

func (m *chatModel) clearDeferredPrompts() {
	if m == nil {
		return
	}
	for i := range m.askQueue {
		resolveAskUserResponse(&m.askQueue[i], "")
	}
	for i := range m.credentialQueue {
		resolveCredentialResponse(&m.credentialQueue[i], tool.CredentialResponse{Approved: false, Reason: "stream ended"})
	}
	m.askQueue = nil
	m.credentialQueue = nil
}

// clearInteractivePrompts rejects every prompt channel before the UI can
// disappear. This is used by both stream teardown and quit; leaving a channel
// unresolved would strand a tool until its independent timeout fires.
func (m *chatModel) clearInteractivePrompts(reason string) {
	if m == nil {
		return
	}
	m.clearPermissionRequests()
	m.clearApprovalRequests()
	m.clearDeferredPrompts()
	if m.askReq != nil {
		resolveAskUserResponse(m.askReq, "")
		m.askReq = nil
	}
	if m.credentialReq != nil {
		resolveCredentialResponse(m.credentialReq, tool.CredentialResponse{Approved: false, Reason: reason})
		m.credentialReq = nil
	}
	m.permTimeoutAt = time.Time{}
	m.approvalTimeoutAt = time.Time{}
	m.askTimeoutAt = time.Time{}
	m.credentialTimeoutAt = time.Time{}
}

func (m chatModel) handleApprovalTimeout(msg approvalPromptTimeoutMsg) (tea.Model, tea.Cmd) {
	if m.approvalReq != nil && m.approvalReqSeq == msg.seq {
		resolveApprovalResponse(m.approvalReq, engine.ApprovalReject)
		m.approvalReq = nil
		m.approvalTimeoutAt = time.Time{}
		m.messages = append(m.messages, displayMsg{role: "system", content: icons.Timer() + " High-risk approval timed out — denied."})
		m.viewDirty = true
		m.updateViewportContent()
		return m.activateNextApprovalRequest()
	}
	return m, nil
}

func (m chatModel) handleApprovalResponse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.approvalReq == nil {
		return m, nil
	}
	response := engine.ApprovalReject
	label := "Denied once"
	switch msg.String() {
	case "y", "Y":
		response, label = engine.ApprovalApprove, "Approved once"
	case "s", "S", "a", "A":
		response, label = engine.ApprovalApproveForSession, "Approved for this category this session"
	case "5":
		response, label = engine.ApprovalApproveForN, "Approved for the next 5 actions"
	case "n", "N", "esc", "escape":
	default:
		return m, nil
	}
	if !resolveApprovalResponse(m.approvalReq, response) {
		m.approvalReq = nil
		m.approvalTimeoutAt = time.Time{}
		m.messages = append(m.messages, displayMsg{role: "error", content: "High-risk approval prompt expired; request was not resolved."})
		m.viewDirty = true
		m.updateViewportContent()
		return m.activateNextApprovalRequest()
	}
	m.approvalReq = nil
	m.approvalTimeoutAt = time.Time{}
	m.messages = append(m.messages, displayMsg{role: "system", content: label})
	m.viewDirty = true
	m.updateViewportContent()
	return m.activateNextApprovalRequest()
}

func (m chatModel) handleAskUserTimeout(msg askUserPromptTimeoutMsg) (tea.Model, tea.Cmd) {
	if m.askReq != nil && m.askReqSeq == msg.seq {
		resolveAskUserResponse(m.askReq, "")
		m.askReq = nil
		m.askTimeoutAt = time.Time{}
		m.messages = append(m.messages, displayMsg{role: "system", content: icons.Timer() + " Question timed out."})
		m.viewDirty = true
		m.updateViewportContent()
		model, cmd := m.activateNextPrompt()
		return model, tea.Batch(cmd, m.input.Focus())
	}
	return m, nil
}

func (m chatModel) handleCredentialAsk(msg credentialAskMsg) (tea.Model, tea.Cmd) {
	if m.hasActiveInteractivePrompt() {
		if len(m.credentialQueue) >= maxPendingPrompts {
			resolveCredentialResponse(&msg, tool.CredentialResponse{Approved: false, Reason: "credential queue full"})
			m.messages = append(m.messages, displayMsg{role: "error", content: "Credential queue full — request denied; review the active prompt before continuing."})
			m.viewDirty = true
			m.updateViewportContent()
			return m, nil
		}
		m.credentialQueue = append(m.credentialQueue, msg)
		m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Credential request queued (%d waiting)", len(m.credentialQueue))})
		m.viewDirty = true
		m.updateViewportContent()
		return m, nil
	}
	return m.activateCredentialRequest(msg)
}

func (m chatModel) activateCredentialRequest(msg credentialAskMsg) (tea.Model, tea.Cmd) {
	m.credentialReq = &msg
	m.credentialReqSeq++
	m.promptGeneration++
	m.credentialTimeoutAt = time.Now().Add(interactivePromptTimeout)
	prompt := fmt.Sprintf("AI wants to access %s (%s): %s", msg.req.Name, msg.req.Credential, msg.req.Reason)
	m.messages = append(m.messages, displayMsg{role: "credential", content: prompt, timeoutAt: m.credentialTimeoutAt})
	m.viewDirty = true
	m.updateViewportContent()
	return m, tea.Batch(credentialPromptTimeoutCmd(m.credentialReqSeq), promptCountdownTickCmd(m.promptGeneration))
}

func (m chatModel) handleCredentialTimeout(msg credentialPromptTimeoutMsg) (tea.Model, tea.Cmd) {
	if m.credentialReq != nil && m.credentialReqSeq == msg.seq {
		resolveCredentialResponse(m.credentialReq, tool.CredentialResponse{Approved: false, Reason: "timed out"})
		m.credentialReq = nil
		m.credentialTimeoutAt = time.Time{}
		m.messages = append(m.messages, displayMsg{role: "system", content: icons.Timer() + " Credential request timed out — denied."})
		m.viewDirty = true
		m.updateViewportContent()
		return m.activateNextPrompt()
	}
	return m, nil
}

func (m chatModel) handleCredentialResponse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.credentialReq == nil {
		return m, nil
	}
	switch msg.String() {
	case "y", "Y":
		req := m.credentialReq
		if !resolveCredentialResponse(req, tool.CredentialResponse{Approved: true}) {
			m.credentialReq = nil
			m.credentialTimeoutAt = time.Time{}
			m.messages = append(m.messages, displayMsg{role: "error", content: "Credential prompt expired; access was not granted."})
			m.viewDirty = true
			m.updateViewportContent()
			return m.activateNextPrompt()
		}
		m.credentialReq = nil
		m.credentialTimeoutAt = time.Time{}
		m.messages = append(m.messages, displayMsg{role: "system", content: icons.CheckBold() + " Credential access granted: " + req.req.Name})
	case "n", "N", "esc", "escape":
		req := m.credentialReq
		if !resolveCredentialResponse(req, tool.CredentialResponse{Approved: false, Reason: "denied by user"}) {
			m.credentialReq = nil
			m.credentialTimeoutAt = time.Time{}
			m.messages = append(m.messages, displayMsg{role: "error", content: "Credential prompt expired; denial was already resolved."})
			m.viewDirty = true
			m.updateViewportContent()
			return m.activateNextPrompt()
		}
		m.credentialReq = nil
		m.credentialTimeoutAt = time.Time{}
		m.messages = append(m.messages, displayMsg{role: "system", content: icons.CloseThick() + " Credential access denied: " + req.req.Name})
	}
	m.viewDirty = true
	m.updateViewportContent()
	return m.activateNextPrompt()
}
