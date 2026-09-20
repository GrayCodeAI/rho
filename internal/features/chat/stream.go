package chat

import (
	"strings"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine"
)

const StreamChunkCoalesceInterval = 16 * time.Millisecond

// PumpStreamEvents coalesces small content events while preserving all
// non-content events and their ordering. It stops after terminal events and
// emits a synthetic done event if the producer closes without one.
func PumpStreamEvents(ch <-chan engine.StreamEvent, emit func(engine.StreamEvent)) {
	if emit == nil {
		return
	}
	var buf strings.Builder
	firstContent := true
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		emit(engine.StreamEvent{Type: "content", Content: buf.String()})
		buf.Reset()
	}

	var timer *time.Timer
	var timerC <-chan time.Time
	stopTimer := func() {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer = nil
		timerC = nil
	}
	startTimer := func() {
		if timer != nil {
			return
		}
		timer = time.NewTimer(StreamChunkCoalesceInterval)
		timerC = timer.C
	}

	for {
		select {
		case <-timerC:
			flush()
			timer = nil
			timerC = nil
		case event, ok := <-ch:
			if !ok {
				stopTimer()
				flush()
				emit(engine.StreamEvent{Type: "done"})
				return
			}
			if event.Type == "content" {
				if firstContent {
					firstContent = false
					emit(event)
					continue
				}
				buf.WriteString(event.Content)
				if ShouldFlushStreamChunkBuffer(buf.String()) {
					stopTimer()
					flush()
				} else {
					startTimer()
				}
				continue
			}
			stopTimer()
			flush()
			emit(event)
			if event.Type == "error" || event.Type == "done" {
				return
			}
		}
	}
}

func ShouldFlushStreamChunkBuffer(content string) bool {
	if len(content) >= 512 {
		return true
	}
	return strings.ContainsAny(content, "\n.!?;:")
}
