package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine"
)

var errorsSentinel = errors.New("startup failure")

type testStreamSource struct {
	events     []engine.StreamEvent
	err        error
	nilChannel bool
}

func (s testStreamSource) Stream(context.Context) (<-chan engine.StreamEvent, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.nilChannel {
		return nil, nil
	}
	ch := make(chan engine.StreamEvent, len(s.events))
	for _, event := range s.events {
		ch <- event
	}
	close(ch)
	return ch, nil
}

func TestRunStreamRunsSourceThroughPolicy(t *testing.T) {
	var got []engine.StreamEvent
	err := RunStream(context.Background(), testStreamSource{events: []engine.StreamEvent{{Type: "content", Content: "ok"}}}, func(event engine.StreamEvent) {
		got = append(got, event)
	})
	if err != nil {
		t.Fatalf("RunStream() error = %v", err)
	}
	if len(got) != 2 || got[0].Content != "ok" || got[1].Type != "done" {
		t.Fatalf("events = %#v", got)
	}
}

func TestRunStreamReturnsStartupError(t *testing.T) {
	want := errors.New("provider unavailable")
	if err := RunStream(context.Background(), testStreamSource{err: want}, func(engine.StreamEvent) {}); !errors.Is(err, want) {
		t.Fatalf("RunStream() error = %v, want %v", err, want)
	}
}

func TestRunStreamRejectsMissingDependencies(t *testing.T) {
	if err := RunStream(nil, testStreamSource{}, func(engine.StreamEvent) {}); err == nil {
		t.Fatal("expected nil context error")
	}
	if err := RunStream(context.Background(), nil, func(engine.StreamEvent) {}); err == nil {
		t.Fatal("expected nil source error")
	}
	if err := RunStream(context.Background(), testStreamSource{}, nil); err == nil {
		t.Fatal("expected nil emitter error")
	}
	if err := RunStream(context.Background(), testStreamSource{nilChannel: true}, func(engine.StreamEvent) {}); err == nil {
		t.Fatal("expected nil channel error")
	}
}

func TestStreamChannelDeliversEventsAndStartupErrors(t *testing.T) {
	events, errors := StreamChannel(context.Background(), testStreamSource{events: []engine.StreamEvent{{Type: "content", Content: "ok"}}})
	var got []engine.StreamEvent
	for event := range events {
		got = append(got, event)
	}
	if err := <-errors; err != nil || len(got) != 2 || got[1].Type != "done" {
		t.Fatalf("events=%#v error=%v", got, err)
	}

	_, errors = StreamChannel(context.Background(), testStreamSource{err: errorsSentinel})
	if err := <-errors; err != errorsSentinel {
		t.Fatalf("startup error = %v, want %v", err, errorsSentinel)
	}
}

func TestStreamChannelCancellationDoesNotStrandWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	largeStream := make([]engine.StreamEvent, 128)
	for i := range largeStream {
		largeStream[i] = engine.StreamEvent{Type: "content", Content: "discarded"}
	}
	_, errors := StreamChannel(ctx, testStreamSource{events: largeStream})

	select {
	case err := <-errors:
		if err != nil {
			t.Fatalf("StreamChannel() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stream worker remained blocked after cancellation")
	}
}
