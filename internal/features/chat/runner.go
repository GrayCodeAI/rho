package chat

import (
	"context"
	"errors"

	"github.com/GrayCodeAI/rho/internal/engine"
)

// StreamSource is the provider/session boundary needed to run one assistant
// turn. The feature package does not depend on the concrete engine session.
type StreamSource interface {
	Stream(context.Context) (<-chan engine.StreamEvent, error)
}

// StreamChannel adapts RunStream to consumers that intentionally own their
// output loop (for example print and REPL modes). The error channel is
// buffered so consumers may stop after a terminal event without blocking the
// stream worker. Delivery is cancellation-aware so abandoning the output
// channel cannot strand the worker on an unbuffered send.
func StreamChannel(ctx context.Context, source StreamSource) (<-chan engine.StreamEvent, <-chan error) {
	events := make(chan engine.StreamEvent)
	errors := make(chan error, 1)
	go func() {
		err := RunStream(ctx, source, func(event engine.StreamEvent) {
			select {
			case events <- event:
			case <-ctx.Done():
			}
		})
		close(events)
		errors <- err
	}()
	return events, errors
}

// RunStream starts one source stream, applies chat stream policy, and emits
// domain events. Startup failures are returned; provider/runtime failures that
// arrive as stream events remain events so callers preserve their ordering.
func RunStream(ctx context.Context, source StreamSource, emit func(engine.StreamEvent)) error {
	if ctx == nil {
		return errors.New("chat: nil stream context")
	}
	if source == nil {
		return errors.New("chat: nil stream source")
	}
	if emit == nil {
		return errors.New("chat: nil stream emitter")
	}
	events, err := source.Stream(ctx)
	if err != nil {
		return err
	}
	if events == nil {
		return errors.New("chat: stream source returned nil channel")
	}
	PumpStreamEvents(events, emit)
	return nil
}
