package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine"
	"github.com/GrayCodeAI/rho/internal/session"
	"github.com/GrayCodeAI/rho/internal/tool"
)

// testFactory builds a session backed by the engine's canned mock chat client,
// so prompts complete without a real provider.
func testFactory() (*engine.Session, error) {
	registry := tool.NewRegistry(tool.FileReadTool{})
	s := engine.NewSession("test", "mock-model", "test", registry)
	s.SetTestClient(engine.NewMockClientForTest())
	return s, nil
}

// runServer drives Serve with the given input lines and returns all output
// messages it wrote.
func runServer(t *testing.T, factory SessionFactory, inputLines []string) []rpcMessage {
	t.Helper()
	in := strings.NewReader(strings.Join(inputLines, "\n") + "\n")
	pr, pw := io.Pipe()

	srv := NewServer(factory)
	// The deadline is generous on purpose: a prompt turn resolves model
	// metadata through the provider catalog, which can take several seconds
	// on a cold cache (and longer under -race). A tight deadline cancels the
	// turn mid-stream and surfaces as a spurious "cancelled" stopReason.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = srv.Serve(ctx, in, pw)
		_ = pw.Close()
	}()

	var msgs []rpcMessage
	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var m rpcMessage
		if err := json.Unmarshal(line, &m); err == nil {
			msgs = append(msgs, m)
		}
	}
	wg.Wait()
	return msgs
}

func TestACP_InitializeAndPrompt(t *testing.T) {
	lines := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`,
		`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"sess_1","prompt":[{"type":"text","text":"hello"}]}}`,
	}
	msgs := runServer(t, testFactory, lines)

	var gotInit, gotNew, gotUpdate, gotPromptResult bool
	for _, m := range msgs {
		switch {
		case hasID(m, 1):
			gotInit = true
			var r struct {
				ProtocolVersion int `json:"protocolVersion"`
				RhoCapabilities struct {
					Isolation []string `json:"isolation"`
				} `json:"rhoCapabilities"`
			}
			_ = json.Unmarshal(m.Result, &r)
			if r.ProtocolVersion != ProtocolVersion {
				t.Errorf("initialize protocolVersion = %d, want %d", r.ProtocolVersion, ProtocolVersion)
			}
			if strings.Join(r.RhoCapabilities.Isolation, ",") != "none,worktree" {
				t.Errorf("initialize isolation = %v, want [none worktree]", r.RhoCapabilities.Isolation)
			}
		case hasID(m, 2):
			gotNew = true
			var r struct {
				SessionID string `json:"sessionId"`
			}
			_ = json.Unmarshal(m.Result, &r)
			if r.SessionID == "" {
				t.Error("session/new returned empty sessionId")
			}
		case m.Method == "session/update":
			gotUpdate = true
		case hasID(m, 3):
			gotPromptResult = true
			var r struct {
				StopReason string `json:"stopReason"`
			}
			_ = json.Unmarshal(m.Result, &r)
			if r.StopReason != "end_turn" {
				t.Errorf("prompt stopReason = %q, want end_turn", r.StopReason)
			}
		}
	}
	if !gotInit || !gotNew || !gotUpdate || !gotPromptResult {
		t.Errorf("missing responses: init=%v new=%v update=%v promptResult=%v", gotInit, gotNew, gotUpdate, gotPromptResult)
	}
}

func TestACP_UnknownMethod(t *testing.T) {
	msgs := runServer(t, testFactory, []string{
		`{"jsonrpc":"2.0","id":1,"method":"does/not/exist","params":{}}`,
	})
	if len(msgs) != 1 || msgs[0].Error == nil || msgs[0].Error.Code != errCodeMethodNotFound {
		t.Fatalf("expected method-not-found error, got %+v", msgs)
	}
}

func TestACP_PromptUnknownSession(t *testing.T) {
	msgs := runServer(t, testFactory, []string{
		`{"jsonrpc":"2.0","id":1,"method":"session/prompt","params":{"sessionId":"nope","prompt":[{"type":"text","text":"hi"}]}}`,
	})
	if len(msgs) != 1 || msgs[0].Error == nil || msgs[0].Error.Code != errCodeInvalidParams {
		t.Fatalf("expected invalid-params error for unknown session, got %+v", msgs)
	}
}

func TestACP_ParseError(t *testing.T) {
	msgs := runServer(t, testFactory, []string{`{not valid json`})
	if len(msgs) != 1 || msgs[0].Error == nil || msgs[0].Error.Code != errCodeParseError {
		t.Fatalf("expected parse error, got %+v", msgs)
	}
}

func TestACP_SessionLoad(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("RHO_SESSIONS_DIR", tempDir)

	// Create and persist a session
	sessID := "acp-load-test-1"
	prior := &session.Session{
		ID:    sessID,
		Model: "mock-model",
		Name:  "Test Session 1",
		Messages: []session.Message{
			{Role: "user", Content: "Hello from prior session"},
			{Role: "assistant", Content: "Hello! How can I help you?"},
		},
	}
	if err := session.Save(prior); err != nil {
		t.Fatalf("session.Save failed: %v", err)
	}

	lines := []string{
		`{"jsonrpc":"2.0","id":1,"method":"session/load","params":{"sessionId":"` + sessID + `"}}`,
	}
	msgs := runServer(t, testFactory, lines)

	if len(msgs) != 1 {
		t.Fatalf("expected 1 response, got %d", len(msgs))
	}
	if msgs[0].Error != nil {
		t.Fatalf("unexpected rpc error: %+v", msgs[0].Error)
	}

	var r struct {
		SessionID    string `json:"sessionId"`
		Model        string `json:"model"`
		MessageCount int    `json:"messageCount"`
		Status       string `json:"status"`
	}
	if err := json.Unmarshal(msgs[0].Result, &r); err != nil {
		t.Fatalf("failed to unmarshal load result: %v", err)
	}
	if r.SessionID != sessID {
		t.Errorf("got sessionId %q, want %q", r.SessionID, sessID)
	}
	if r.MessageCount != 2 {
		t.Errorf("got messageCount %d, want 2", r.MessageCount)
	}
	if r.Status != "ready" {
		t.Errorf("got status %q, want ready", r.Status)
	}
}

func TestACP_SessionList(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("RHO_SESSIONS_DIR", tempDir)

	prior := &session.Session{
		ID:    "acp-list-test-1",
		Model: "mock-model",
		Name:  "List Test Session",
		Messages: []session.Message{
			{Role: "user", Content: "First prompt"},
		},
	}
	if err := session.Save(prior); err != nil {
		t.Fatalf("session.Save failed: %v", err)
	}

	lines := []string{
		`{"jsonrpc":"2.0","id":1,"method":"session/list","params":{}}`,
	}
	msgs := runServer(t, testFactory, lines)

	if len(msgs) != 1 {
		t.Fatalf("expected 1 response, got %d", len(msgs))
	}
	if msgs[0].Error != nil {
		t.Fatalf("unexpected rpc error: %+v", msgs[0].Error)
	}

	var r struct {
		Sessions []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(msgs[0].Result, &r); err != nil {
		t.Fatalf("failed to unmarshal list result: %v", err)
	}
	if len(r.Sessions) == 0 {
		t.Fatalf("expected at least 1 session in list")
	}
	if r.Sessions[0].ID != "acp-list-test-1" {
		t.Errorf("got session ID %q, want acp-list-test-1", r.Sessions[0].ID)
	}
}

func hasID(m rpcMessage, id int) bool {
	if len(m.ID) == 0 {
		return false
	}
	var got int
	if err := json.Unmarshal(m.ID, &got); err != nil {
		return false
	}
	return got == id
}
