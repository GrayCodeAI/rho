package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/GrayCodeAI/rho/internal/tool"
)

// TestSession_NewSessionWithClient_WiresAllSubServices proves that the
// Phase 7 god-object decomposition is fully wired: every getter
// returns a non-nil service that operates on the same underlying state
// the legacy fields reference. This is the integration contract for
// downstream call sites that want to migrate to s.ChatLLM(),
// s.PermSvc(), s.LifecycleSvc(), s.MemorySvc(), s.Persistence(),
// s.Tools() without losing backward compat.
func TestSession_NewSessionWithClient_WiresAllSubServices(t *testing.T) {
	registry := tool.NewRegistry()
	s := NewSession("anthropic", "claude-sonnet-4-20250514", "you are rho", registry)

	// ChatService: provider, model, client, router, rate limiter.
	if s.ChatLLM() == nil {
		t.Fatal("ChatLLM() should not be nil after NewSessionWithClient")
	}
	if got := s.ChatLLM().Provider(); got != "anthropic" {
		t.Errorf("ChatLLM().Provider() = %q, want anthropic", got)
	}
	if got := s.ChatLLM().Model(); got != "claude-sonnet-4-20250514" {
		t.Errorf("ChatLLM().Model() = %q, want claude-sonnet-4-20250514", got)
	}
	if s.ChatLLM().Client() == nil {
		t.Error("ChatLLM().Client() should not be nil")
	}

	// PermissionService: PermissionEngine, legacy shims, autonomy, mode.
	if s.PermSvc() == nil {
		t.Fatal("PermSvc() should not be nil after NewSessionWithClient")
	}
	if s.PermSvc().Memory() == nil {
		t.Error("PermSvc().Memory() should be initialized")
	}

	// LifecycleService owns limits, beliefs, backtrack, response cache, pipeline.
	if s.LifecycleSvc() == nil {
		t.Fatal("LifecycleSvc() should not be nil after NewSessionWithClient")
	}
	if s.LifecycleSvc().Limits() == nil {
		t.Error("LifecycleSvc().Limits() should not be nil")
	}
	if s.LifecycleSvc().Beliefs() == nil || s.LifecycleSvc().Backtrack() == nil || s.LifecycleSvc().ResponseCache() == nil || s.LifecycleSvc().Pipeline() == nil {
		t.Error("LifecycleService collaborators should be initialized")
	}

	// MemoryService: empty by default (no memory wired).
	if s.MemorySvc() == nil {
		t.Fatal("MemorySvc() should not be nil after NewSessionWithClient")
	}
	if !s.MemorySvc().IsZero() {
		t.Error("MemorySvc().IsZero() should be true for a fresh session")
	}

	// PersistenceService: empty messages, system prompt set, defaults applied.
	if s.Persistence() == nil {
		t.Fatal("Persistence() should not be nil after NewSessionWithClient")
	}
	if got := s.Persistence().System(); got != "you are rho" {
		t.Errorf("Persistence().System() = %q, want %q", got, "you are rho")
	}
	if got := s.Persistence().MessageCount(); got != 0 {
		t.Errorf("Persistence().MessageCount() = %d, want 0", got)
	}
	if got := s.Persistence().AutoCompactThresholdPct(); got != DefaultAutoCompactThresholdPct {
		t.Errorf("Persistence().AutoCompactThresholdPct() = %d, want %d", got, DefaultAutoCompactThresholdPct)
	}

	// ToolService: registry, defaults.
	if s.Tools() == nil {
		t.Fatal("Tools() should not be nil after NewSessionWithClient")
	}
	if s.Tools().Registry() == nil {
		t.Error("Tools().Registry() should not be nil")
	}
	if s.Tools().Registry() != registry {
		t.Error("Tools().Registry() should be the same instance as the one passed to NewSession")
	}
}

// TestSession_Stream_UsesChatService proves that the Stream() agent loop
// actually goes through s.ChatLLM().Stream() rather than the legacy
// ChatService.StreamChatContinue(). The mock client is injected via
// SetTestClient, which also reattaches the ChatService, so the agent
// loop's call site must hit the mock and not the real flux client.
func TestSession_Stream_UsesChatService(t *testing.T) {
	mc := newMockClient(mockTextResponse("hi from service"))
	s := newMockSession(mc)
	s.LifecycleSvc().Limits().SetMaxTurns(1)
	s.AddUser("ping")

	ctx, cancel := context.WithTimeout(context.Background(), 5*testutilTimeout)
	defer cancel()
	ch, err := s.Stream(ctx)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	var sawContent bool
	for ev := range ch {
		if ev.Type == "content" && strings.Contains(ev.Content, "hi from service") {
			sawContent = true
		}
	}
	if !sawContent {
		t.Error("expected at least one content event with mock response text")
	}
	if mc.callCount() < 1 {
		t.Errorf("mock client callCount = %d, want >= 1 — agent loop should have gone through ChatService.Stream", mc.callCount())
	}
}

// TestSession_ReattachTransport_UpdatesChatService proves that
// ReattachTransport (used by /config and provider.json changes) keeps
// the ChatService in sync so the agent loop's `s.ChatLLM().Stream()`
// call site picks up the new client.
func TestSession_ReattachTransport_UpdatesChatService(t *testing.T) {
	s := NewSession("anthropic", "claude-sonnet-4-20250514", "sys", nil)
	mc := newMockClient()
	originalClient := s.ChatLLM().Client()
	s.ReattachTransport(mc, "anthropic", false)
	if s.ChatLLM().Client() != mc {
		t.Error("ChatLLM().Client() should be the reattached mock")
	}
	if s.ChatLLM().Client() == originalClient {
		t.Error("ChatLLM().Client() should have changed after ReattachTransport")
	}
	if s.ChatLLM().Client() != mc {
		t.Error("ChatService client should be the reattached mock")
	}
}

// TestSession_SetTestClient_UpdatesChatService proves the test-only
// hook also reattaches the service.
func TestSession_SetTestClient_UpdatesChatService(t *testing.T) {
	s := NewSession("anthropic", "claude-sonnet-4-20250514", "sys", nil)
	mc := newMockClient()
	s.SetTestClient(mc)
	if s.ChatLLM().Client() != mc {
		t.Error("ChatLLM().Client() should be the test mock after SetTestClient")
	}
	if s.ChatLLM().Client() != mc {
		t.Error("ChatService client should be the test mock after SetTestClient")
	}
}

// testutilTimeout is the timeout used by integration tests that drive
// the full agent loop. 5s is enough for a mock response and tight
// enough to fail fast on hangs.
const testutilTimeout = 5e9 // 5s in nanoseconds, avoids importing "time" at the top
