// Package acp implements an Agent Client Protocol (ACP) server for rho, exposing
// the agent over newline-delimited JSON-RPC 2.0 on stdio so editors (e.g. Zed)
// can drive it. It mirrors the framing of internal/mcp and the session-driving
// pattern of internal/daemon.
//
// Scope (first cut): the core agent-side methods (initialize, session/new,
// session/prompt, session/cancel), streamed session/update notifications, and
// client-routed tool approvals via session/request_permission. File reads/writes
// use rho's local tools; client fs routing is intentionally out of scope.
package acp

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine/safety"

	"github.com/GrayCodeAI/rho/internal/attachment"
	"github.com/GrayCodeAI/rho/internal/engine"
	"github.com/GrayCodeAI/rho/internal/session"
	statussnapshot "github.com/GrayCodeAI/rho/internal/status"
)

// ProtocolVersion is the ACP protocol version this server implements.
const ProtocolVersion = 1

// SessionFactory creates a fresh, configured engine session for a new ACP session.
type SessionFactory func() (*engine.Session, error)

// Standard JSON-RPC 2.0 error codes.
const (
	errCodeParseError     = -32700
	errCodeInvalidRequest = -32600
	errCodeMethodNotFound = -32601
	errCodeInvalidParams  = -32602
	errCodeInternal       = -32603
)

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Server is an ACP server bound to a single stdio peer.
type Server struct {
	factory SessionFactory

	// store is the durable attachment service for inline image admission.
	// When nil, image prompt content is rejected as not advertised.
	store attachment.Store
	// imageCapable reports whether the deployment can admit inline image
	// prompts (store mounted and the model route supports image input).
	imageCapable bool

	mu       sync.Mutex
	sessions map[string]*acpSession
	// order tracks session creation order for FIFO eviction when the session
	// cap is exceeded (H11).
	order []string
	seq   int

	writeMu sync.Mutex
	w       io.Writer

	handlers sync.WaitGroup

	pendMu    sync.Mutex
	pending   map[int]chan rpcMessage
	nextReqID int
}

// maxACPSessions bounds how many sessions are kept alive at once. ACP is a
// long-lived stdio peer that can open many sessions; without a cap (and
// teardown on disconnect) the map grows unboundedly (H11).
const maxACPSessions = 64

type acpSession struct {
	sess   *engine.Session
	cancel context.CancelFunc
}

// NewServer creates an ACP server that builds sessions via factory.
func NewServer(factory SessionFactory) *Server {
	return &Server{
		factory:  factory,
		sessions: make(map[string]*acpSession),
		pending:  make(map[int]chan rpcMessage),
	}
}

// SetAttachmentStore mounts the durable attachment service used for inline
// image admission, and recomputes image-prompt capability from the store.
// modelSupportsImage gates the capability on the active model route.
func (s *Server) SetAttachmentStore(store attachment.Store, modelSupportsImage bool) {
	s.store = store
	s.imageCapable = SupportsAcpImagePrompts(store, modelSupportsImage)
}

// ServeStdio runs the server on stdin/stdout until ctx is cancelled or EOF.
func (s *Server) ServeStdio(ctx context.Context) error {
	return s.Serve(ctx, os.Stdin, os.Stdout)
}

// Serve runs the ACP server reading from r and writing to w. Each inbound
// request is dispatched on its own goroutine so the read loop stays free to
// receive responses to server-initiated requests (e.g. permission prompts)
// while a prompt is streaming.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	s.w = w
	defer s.teardown() // release every session on disconnect (H11)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 8*1024*1024)

	for {
		select {
		case <-ctx.Done():
			s.handlers.Wait()
			return ctx.Err()
		default:
		}
		if !scanner.Scan() {
			s.handlers.Wait() // let in-flight handlers finish writing
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("acp: read error: %w", err)
			}
			return nil // clean EOF
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			s.writeError(nil, errCodeParseError, "parse error")
			continue
		}

		// A message with no method but with a result/error is a response to a
		// server-initiated request (e.g. session/request_permission).
		if msg.Method == "" && (msg.Result != nil || msg.Error != nil) {
			s.routeResponse(msg)
			continue
		}

		// session/prompt is long-running and may issue server->client permission
		// requests mid-stream, so it runs on its own goroutine to keep the read
		// loop free to receive those responses. Other methods are quick and run
		// inline, which also preserves request ordering (e.g. session/new before
		// a session/prompt that references it).
		if msg.Method == "session/prompt" {
			s.handlers.Add(1)
			go func(m rpcMessage) {
				defer s.handlers.Done()
				s.handle(ctx, m)
			}(msg)
		} else {
			s.handle(ctx, msg)
		}
	}
}

func (s *Server) handle(ctx context.Context, msg rpcMessage) {
	switch msg.Method {
	case "initialize":
		s.reply(msg.ID, map[string]any{
			"protocolVersion": ProtocolVersion,
			"agentCapabilities": map[string]any{
				"loadSession":  true,
				"listSessions": true,
				"promptCapabilities": map[string]any{
					"image": s.imageCapable,
					"audio": false,
				},
			},
			// Rho control-plane metadata for IDE clients that want it.
			"rhoCapabilities": map[string]any{
				"workModes": []string{"plan", "act", "review"},
				// Rho has no local OS/container sandbox runtime. These are the
				// only isolation choices the ACP surface can actually honor.
				"isolation":       []string{"none", "worktree"},
				"folderTrust":     true,
				"lazyTools":       true,
				"autoCommit":      true,
				"spawnController": true,
			},
		})
	case "session/new":
		s.handleSessionNew(msg)
	case "session/load":
		s.handleSessionLoad(msg)
	case "session/list":
		s.handleSessionList(msg)
	case "session/setMode":
		s.handleSetMode(msg)
	case "session/status":
		s.handleStatus(msg)
	case "session/prompt":
		s.handlePrompt(ctx, msg)
	case "session/cancel":
		s.handleCancel(msg)
	default:
		if len(msg.ID) > 0 {
			s.writeError(msg.ID, errCodeMethodNotFound, "method not found: "+msg.Method)
		}
	}
}

type setModeParams struct {
	SessionID string `json:"sessionId"`
	Mode      string `json:"mode"`
}

// handleSetMode switches the session's work mode (plan|act|review).
func (s *Server) handleSetMode(msg rpcMessage) {
	var p setModeParams
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		s.writeError(msg.ID, errCodeInvalidParams, "invalid params")
		return
	}
	as := s.lookupSession(p.SessionID)
	if as == nil {
		s.writeError(msg.ID, errCodeInvalidParams, "unknown sessionId")
		return
	}
	if err := as.sess.SetWorkMode(engine.WorkMode(p.Mode)); err != nil {
		s.writeError(msg.ID, errCodeInvalidParams, err.Error())
		return
	}
	s.reply(msg.ID, map[string]any{
		"sessionId": p.SessionID,
		"workMode":  string(as.sess.WorkMode()),
	})
}

type statusParams struct {
	SessionID string `json:"sessionId"`
}

// handleStatus returns the control-plane snapshot for a session.
func (s *Server) handleStatus(msg rpcMessage) {
	var p statusParams
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		s.writeError(msg.ID, errCodeInvalidParams, "invalid params")
		return
	}
	as := s.lookupSession(p.SessionID)
	if as == nil {
		s.writeError(msg.ID, errCodeInvalidParams, "unknown sessionId")
		return
	}
	snapshot := statussnapshot.New()
	snapshot.SessionID = p.SessionID
	snapshot.Workspace = statussnapshot.Workspace()
	snapshot.Provider = as.sess.Provider()
	snapshot.Model = as.sess.Model()
	snapshot.Permission.SecretRedacted = true
	snapshot.MCP.State = "client_supplied"
	snapshot.Skills.State = "session_visible"
	if entries, err := session.List(); err == nil {
		for _, entry := range entries {
			child, loadErr := session.Load(entry.ID)
			if loadErr != nil || child == nil || child.ParentSessionID != p.SessionID {
				continue
			}
			state := "persisted"
			if child.UpdatedAt.After(time.Now().Add(-5 * time.Minute)) {
				state = "active"
			}
			snapshot.Subagents = append(snapshot.Subagents, statussnapshot.SubagentStatus{
				ID: child.ID, ParentID: child.ParentSessionID, State: state,
				Model: child.Model, Mode: child.Name, Workspace: child.CWD,
			})
		}
	}
	s.reply(msg.ID, map[string]any{
		"sessionId":  p.SessionID,
		"workMode":   string(as.sess.WorkMode()),
		"autoCommit": as.sess.AutoCommit(),
		"messages":   as.sess.MessageCount(),
		"snapshot":   snapshot,
	})
}

// lookupSession returns the acpSession for id, or nil.
func (s *Server) lookupSession(id string) *acpSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

func (s *Server) handleSessionNew(msg rpcMessage) {
	sess, err := s.factory()
	if err != nil {
		s.writeError(msg.ID, errCodeInternal, "session creation failed: "+err.Error())
		return
	}

	s.mu.Lock()
	if len(s.sessions) >= maxACPSessions {
		s.evictOldestLocked()
	}
	s.seq++
	id := fmt.Sprintf("sess_%d", s.seq)
	s.sessions[id] = &acpSession{sess: sess}
	s.order = append(s.order, id)
	s.mu.Unlock()

	// Route tool-permission prompts to the client for this session.
	sess.SetPermissionFn(s.permissionFnFor(id))
	// Default product modes for IDE-driven sessions (same as chat).
	_ = sess.SetWorkMode(engine.WorkModeAct)

	s.reply(msg.ID, map[string]any{
		"sessionId": id,
		// Rho extensions (ignored by clients that only read sessionId).
		"rho": map[string]any{
			"workMode":   string(sess.WorkMode()),
			"autoCommit": sess.AutoCommit(),
		},
	})
}

type loadSessionParams struct {
	SessionID string `json:"sessionId"`
}

func (s *Server) handleSessionLoad(msg rpcMessage) {
	var p loadSessionParams
	if err := json.Unmarshal(msg.Params, &p); err != nil || p.SessionID == "" {
		s.writeError(msg.ID, errCodeInvalidParams, "invalid or missing sessionId")
		return
	}

	// 1. Load persisted session
	persisted, err := session.Load(p.SessionID)
	if err != nil {
		s.writeError(msg.ID, errCodeInvalidParams, fmt.Sprintf("session %q not found: %v", p.SessionID, err))
		return
	}

	// 2. Build new engine session
	sess, err := s.factory()
	if err != nil {
		s.writeError(msg.ID, errCodeInternal, "failed to construct session: "+err.Error())
		return
	}

	// 3. Populate messages
	for _, m := range persisted.Messages {
		sess.Persistence().AddMessage(m.Role, m.Content)
	}

	// 4. Register in active sessions
	s.mu.Lock()
	if len(s.sessions) >= maxACPSessions {
		s.evictOldestLocked()
	}
	s.sessions[p.SessionID] = &acpSession{sess: sess}
	s.order = append(s.order, p.SessionID)
	s.mu.Unlock()

	// Route tool-permission prompts to the client for this session.
	sess.SetPermissionFn(s.permissionFnFor(p.SessionID))
	_ = sess.SetWorkMode(engine.WorkModeAct)

	s.reply(msg.ID, map[string]any{
		"sessionId": p.SessionID,
		"model":     persisted.Model,
		"modes": map[string]any{
			"availableModes": []string{"plan", "act", "review"},
			"currentModeId":  string(sess.WorkMode()),
		},
		"messageCount": len(persisted.Messages),
		"status":       "ready",
	})
}

func (s *Server) handleSessionList(msg rpcMessage) {
	list, err := session.List()
	if err != nil {
		s.writeError(msg.ID, errCodeInternal, "failed to list sessions: "+err.Error())
		return
	}

	type sessionSummary struct {
		ID        string `json:"id"`
		Preview   string `json:"preview,omitempty"`
		CWD       string `json:"cwd,omitempty"`
		UpdatedAt string `json:"updatedAt,omitempty"`
	}

	summaries := make([]sessionSummary, 0, len(list))
	for _, e := range list {
		summaries = append(summaries, sessionSummary{
			ID:        e.ID,
			Preview:   e.Preview,
			CWD:       e.CWD,
			UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
		})
	}

	s.reply(msg.ID, map[string]any{
		"sessions": summaries,
	})
}

// evictOldestLocked removes the oldest session to keep memory bounded; the
// caller must hold s.mu. Any in-flight prompt is cancelled first.
func (s *Server) evictOldestLocked() {
	for len(s.order) > 0 {
		oldest := s.order[0]
		s.order = s.order[1:]
		if as, ok := s.sessions[oldest]; ok {
			delete(s.sessions, oldest)
			if as != nil && as.cancel != nil {
				as.cancel()
			}
			return
		}
	}
}

// teardown cancels and releases every session. It is called when Serve exits
// (disconnect or context cancellation).
func (s *Server) teardown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, as := range s.sessions {
		if as != nil && as.cancel != nil {
			as.cancel()
		}
		delete(s.sessions, id)
	}
	s.order = nil
}

type promptParams struct {
	SessionID string            `json:"sessionId"`
	Prompt    []AcpContentBlock `json:"prompt"`
}

func (s *Server) handlePrompt(ctx context.Context, msg rpcMessage) {
	var p promptParams
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		s.writeError(msg.ID, errCodeInvalidParams, "invalid params")
		return
	}
	s.mu.Lock()
	as, ok := s.sessions[p.SessionID]
	s.mu.Unlock()
	if !ok {
		s.writeError(msg.ID, errCodeInvalidParams, "unknown sessionId")
		return
	}

	turnCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	as.cancel = cancel
	s.mu.Unlock()
	defer cancel()

	// Admit the untrusted prompt into durable, ordered core content before
	// any user message is queued. Images are validated and durably committed
	// to the store first; the session spine is appended only after admission
	// returns, so no late message races a persisted image.
	content, err := AdmitAcpPrompt(turnCtx, s.store, p.Prompt, s.imageCapable, turnCtx.Done())
	if err != nil {
		if ce, ok := asContentError(err); ok {
			code := errCodeInternal
			if ce.Kind == FailureInvalid {
				code = errCodeInvalidParams
			}
			s.writeError(msg.ID, code, ce.Msg)
			return
		}
		s.writeError(msg.ID, errCodeInternal, "prompt admission failed: "+err.Error())
		return
	}

	// Append admitted content to the session spine. Consecutive text blocks
	// coalesce into a single user message (matching the text-only path);
	// image references are re-read and attached via the engine's multimodal
	// path, flushing pending text first so wire order is preserved.
	var pendingText string
	flushText := func() {
		if pendingText == "" {
			return
		}
		as.sess.AddUser(pendingText)
		pendingText = ""
	}
	for _, block := range content {
		switch {
		case block.Type == "text":
			pendingText += block.Text
		case block.Type == "image" && block.Attachment != nil && s.store != nil:
			flushText()
			stored, rerr := s.store.ReadImage(turnCtx, *block.Attachment)
			if rerr != nil {
				s.writeError(msg.ID, errCodeInternal, "prompt image unavailable: "+rerr.Error())
				return
			}
			as.sess.AddUserWithAttachment("", base64.StdEncoding.EncodeToString(stored.Data), string(stored.Ref.MediaType))
		}
	}
	flushText()

	events, err := as.sess.Stream(turnCtx)
	if err != nil {
		s.writeError(msg.ID, errCodeInternal, "stream failed: "+err.Error())
		return
	}

	stopReason := "end_turn"
	for ev := range events {
		switch ev.Type {
		case "content":
			s.sessionUpdate(p.SessionID, map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"type": "text", "text": ev.Content},
			})
		case "thinking":
			s.sessionUpdate(p.SessionID, map[string]any{
				"sessionUpdate": "agent_thought_chunk",
				"content":       map[string]any{"type": "text", "text": ev.Content},
			})
		case "tool_use":
			s.sessionUpdate(p.SessionID, map[string]any{
				"sessionUpdate": "tool_call",
				"toolCallId":    ev.ToolID,
				"title":         ev.ToolName,
				"status":        "in_progress",
			})
		case "tool_result":
			s.sessionUpdate(p.SessionID, map[string]any{
				"sessionUpdate": "tool_call_update",
				"toolCallId":    ev.ToolID,
				"status":        "completed",
				"content": []any{map[string]any{
					"type":    "content",
					"content": map[string]any{"type": "text", "text": ev.Content},
				}},
			})
		case "error":
			stopReason = "refusal"
			s.sessionUpdate(p.SessionID, map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"type": "text", "text": ev.Content},
			})
		case "done":
			// handled by channel close
		}
	}
	if turnCtx.Err() != nil {
		stopReason = "cancelled"
	}

	s.reply(msg.ID, map[string]any{"stopReason": stopReason})
}

type cancelParams struct {
	SessionID string `json:"sessionId"`
}

func (s *Server) handleCancel(msg rpcMessage) {
	var p cancelParams
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return // notification: no response
	}
	s.mu.Lock()
	if as, ok := s.sessions[p.SessionID]; ok && as.cancel != nil {
		as.cancel()
	}
	s.mu.Unlock()
}

// permissionFnFor returns a PermissionFn that asks the ACP client to approve a
// tool call via session/request_permission, falling back to denial on timeout.
func (s *Server) permissionFnFor(sessionID string) func(safety.PermissionRequest) {
	return func(req safety.PermissionRequest) {
		params := map[string]any{
			"sessionId": sessionID,
			"toolCall": map[string]any{
				"toolCallId": req.ToolID,
				"title":      req.Summary,
			},
			"options": []any{
				map[string]any{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
				map[string]any{"optionId": "reject", "name": "Reject", "kind": "reject_once"},
			},
		}
		allowed := s.requestPermission(params)
		if req.Response != nil {
			req.Response <- allowed
		}
	}
}

type permissionResult struct {
	Outcome struct {
		Outcome  string `json:"outcome"`
		OptionID string `json:"optionId"`
	} `json:"outcome"`
}

// requestPermission issues a server->client session/request_permission request
// and blocks for the reply. Returns true only on an explicit "allow" selection.
func (s *Server) requestPermission(params map[string]any) bool {
	resp, ok := s.call("session/request_permission", params, 5*time.Minute)
	if !ok || resp.Error != nil || resp.Result == nil {
		return false
	}
	var pr permissionResult
	if err := json.Unmarshal(resp.Result, &pr); err != nil {
		return false
	}
	return pr.Outcome.Outcome == "selected" && pr.Outcome.OptionID == "allow"
}

// call sends a server-initiated JSON-RPC request and waits for the response.
func (s *Server) call(method string, params any, timeout time.Duration) (rpcMessage, bool) {
	s.pendMu.Lock()
	s.nextReqID++
	id := s.nextReqID
	ch := make(chan rpcMessage, 1)
	s.pending[id] = ch
	s.pendMu.Unlock()

	defer func() {
		s.pendMu.Lock()
		delete(s.pending, id)
		s.pendMu.Unlock()
	}()

	rawID, _ := json.Marshal(id)
	s.writeMessage(rpcMessage{JSONRPC: "2.0", ID: rawID, Method: method, Params: mustRaw(params)})

	select {
	case resp := <-ch:
		return resp, true
	case <-time.After(timeout):
		return rpcMessage{}, false
	}
}

func (s *Server) routeResponse(msg rpcMessage) {
	var id int
	if err := json.Unmarshal(msg.ID, &id); err != nil {
		return
	}
	s.pendMu.Lock()
	ch, ok := s.pending[id]
	s.pendMu.Unlock()
	if ok {
		ch <- msg
	}
}

// ---- JSON-RPC write helpers ----

func (s *Server) reply(id json.RawMessage, result any) {
	if len(id) == 0 {
		return // notification: no reply expected
	}
	s.writeMessage(rpcMessage{JSONRPC: "2.0", ID: id, Result: mustRaw(result)})
}

func (s *Server) writeError(id json.RawMessage, code int, message string) {
	s.writeMessage(rpcMessage{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}})
}

func (s *Server) sessionUpdate(sessionID string, update map[string]any) {
	s.writeMessage(rpcMessage{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params:  mustRaw(map[string]any{"sessionId": sessionID, "update": update}),
	})
}

func (s *Server) writeMessage(m rpcMessage) {
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, _ = s.w.Write(append(b, '\n'))
}

func mustRaw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}
