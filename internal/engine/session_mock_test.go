package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine/safety"

	"github.com/GrayCodeAI/rho/internal/types"
)

func TestSession_PersistenceLazyInitIsSynchronized(t *testing.T) {
	t.Parallel()

	s := &Session{}
	const callers = 32
	services := make(chan *PersistenceService, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			services <- s.Persistence()
		}()
	}
	wg.Wait()
	close(services)

	var first *PersistenceService
	for service := range services {
		if service == nil {
			t.Fatal("Persistence returned nil")
		}
		if first == nil {
			first = service
			continue
		}
		if service != first {
			t.Fatal("concurrent lazy initialization created multiple persistence services")
		}
	}
}

func newMockSession(mc *mockClient) *Session {
	s := NewSession("", "mock-model", "You are a test assistant.", nil)
	// SetTestClient also reattaches the ChatService so the agent
	// loop's s.ChatLLM().Stream call site sees the mock.
	s.SetTestClient(mc)
	return s
}

// setMaxTurns is a test helper that sets the max turns through the sub-service.
func setMaxTurns(s *Session, turns int) {
	if s.LifecycleSvc() != nil {
		s.LifecycleSvc().Limits().SetMaxTurns(turns)
	}
}

func TestSession_AddUser(t *testing.T) {
	t.Parallel()
	mc := newMockClient()
	s := newMockSession(mc)

	s.AddUser("hello world")

	msgs := s.Persistence().RawMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("role = %q, want 'user'", msgs[0].Role)
	}
	if msgs[0].Content != "hello world" {
		t.Errorf("content = %q, want 'hello world'", msgs[0].Content)
	}
}

func TestSession_AddAssistant(t *testing.T) {
	t.Parallel()
	mc := newMockClient()
	s := newMockSession(mc)

	s.AddAssistant("here is my response")

	msgs := s.Persistence().RawMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" {
		t.Errorf("role = %q, want 'assistant'", msgs[0].Role)
	}
}

func TestSession_RawMessagesUsesPersistenceService(t *testing.T) {
	t.Parallel()
	mc := newMockClient()
	s := newMockSession(mc)

	s.AddUser("hello")
	s.AddAssistant("hi")

	msgs := s.RawMessages()
	if len(msgs) != 2 {
		t.Fatalf("RawMessages() length = %d, want 2", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Fatalf("RawMessages()[0] = %#v, want user hello", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "hi" {
		t.Fatalf("RawMessages()[1] = %#v, want assistant hi", msgs[1])
	}
}

func TestSession_LoadMessages(t *testing.T) {
	t.Parallel()
	mc := newMockClient()
	s := newMockSession(mc)

	msgs := []types.FluxMessage{
		{Role: "user", Content: "msg1"},
		{Role: "assistant", Content: "msg2"},
		{Role: "user", Content: "msg3"},
	}
	s.LoadMessages(msgs)

	if len(s.Persistence().RawMessages()) != 3 {
		t.Errorf("RawMessages() length = %d, want 3", len(s.Persistence().RawMessages()))
	}
}

func TestSession_Model(t *testing.T) {
	t.Parallel()
	mc := newMockClient()
	s := newMockSession(mc)

	if s.Model() != "mock-model" {
		t.Errorf("Model() = %q, want 'mock-model'", s.Model())
	}
}

func TestSession_Provider(t *testing.T) {
	t.Parallel()
	mc := newMockClient()
	s := newMockSession(mc)

	if s.Provider() != "" {
		t.Errorf("Provider() = %q, want empty", s.Provider())
	}
}

func TestSession_Cost(t *testing.T) {
	t.Parallel()
	mc := newMockClient()
	s := newMockSession(mc)

	s.Cost.Add(100, 50)
	if s.Cost.Total() == 0 {
		t.Error("Cost.Total() should be > 0 after Add")
	}
}

func TestSession_MaxTurns(t *testing.T) {
	t.Parallel()
	mc := newMockClient()
	s := newMockSession(mc)
	setMaxTurns(s, 5)

	if s.LifecycleSvc().Limits().MaxTurns() != 5 {
		t.Errorf("MaxTurns = %d, want 5", s.LifecycleSvc().Limits().MaxTurns())
	}
}

func TestSession_Chat_MockResponse(t *testing.T) {
	mc := newMockClient(mockTextResponse("hello from LLM"))
	s := newMockSession(mc)
	s.AddUser("hi")

	// Call Chat directly
	resp, err := mc.Chat(context.TODO(), s.Persistence().RawMessages(), types.ChatOptions{})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.Content != "hello from LLM" {
		t.Errorf("response = %q, want 'hello from LLM'", resp.Content)
	}
	if mc.callCount() != 1 {
		t.Errorf("callCount = %d, want 1", mc.callCount())
	}
}

func TestSession_SetAutonomy(t *testing.T) {
	mc := newMockClient()
	s := newMockSession(mc)

	s.PermSvc().SetAutonomy(safety.AutonomyYOLO)
	if s.PermSvc().RuntimeState().Autonomy != safety.AutonomyYOLO {
		t.Errorf("SetAutonomy did not take effect, got %v", s.PermSvc().RuntimeState().Autonomy)
	}
}

func TestSession_Metrics(t *testing.T) {
	t.Parallel()
	mc := newMockClient()
	s := newMockSession(mc)

	if s.Metrics() == nil {
		t.Error("Metrics() should not be nil")
	}
}

func TestSession_Stream_MockEndTurn(t *testing.T) {
	mc := newMockClient(mockTextResponse("Hello! I can help with that."))
	s := newMockSession(mc)
	s.LifecycleSvc().Limits().SetMaxTurns(1)
	s.AddUser("hi there")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := s.Stream(ctx)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) == 0 {
		t.Fatal("expected at least one stream event")
	}

	if mc.callCount() < 1 {
		t.Error("expected at least one LLM call")
	}
}

func TestSession_Stream_MultiTurn(t *testing.T) {
	mc := newMockClient(
		mockTextResponse("First response"),
		mockTextResponse("Second response"),
	)
	s := newMockSession(mc)
	s.LifecycleSvc().Limits().SetMaxTurns(2)
	s.AddUser("hello")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := s.Stream(ctx)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) == 0 {
		t.Fatal("expected stream events")
	}
}
