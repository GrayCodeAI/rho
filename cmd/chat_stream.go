package cmd

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/GrayCodeAI/rho/internal/engine"
	chatfeature "github.com/GrayCodeAI/rho/internal/features/chat"
)

// Streaming and prompt command functions extracted from chat.go

func (m *chatModel) startPromptCommand(display, prompt string) (tea.Model, tea.Cmd) {
	m.messages = append(m.messages, displayMsg{role: "user", content: display})
	m.session.AddUser(prompt)
	m.turn.Reset()
	m.waiting = true
	m.viewDirty = true
	m.partial.Reset()
	// Prevent system sleep during long-running agent turns.
	m.sleepCancel = preventSleep()
	// Show indeterminate progress in the terminal tab until the turn completes.
	SetTabProgress(-1)
	m.startStream()
	return m, nil
}

func dispatchStreamEvent(ref *progRef, ev engine.StreamEvent) bool {
	switch ev.Type {
	case "content":
		ref.Send(streamChunkMsg(ev.Content))
	case "thinking":
		ref.Send(thinkingMsg(ev.Content))
	case "tool_use":
		ref.Send(toolUseMsg{name: ev.ToolName, id: ev.ToolID})
	case "tool_result":
		ref.Send(toolResultMsg{name: ev.ToolName, content: ev.Content})
	case "blast_radius":
		ref.Send(blastRadiusMsg{message: ev.Content})
	case "compact_start":
		ref.Send(compactStartMsg{})
	case "compact":
		ref.Send(compactMsg{
			strategy:     ev.Content,
			tokensBefore: ev.TokensBefore,
			tokensAfter:  ev.TokensAfter,
		})
	case "usage":
		if ev.Usage != nil {
			ref.Send(usageUpdateMsg{usage: ev.Usage})
		}
	case "retry":
		ref.Send(streamRetryMsg{content: ev.Content})
	case "error":
		ref.Send(streamErrMsg{err: fmt.Errorf("%s", ev.Content)})
		return true
	case "done":
		ref.Send(streamDoneMsg{})
		return true
	}
	return false
}

func shouldFlushStreamChunkBuffer(s string) bool {
	return chatfeature.ShouldFlushStreamChunkBuffer(s)
}

func (m *chatModel) startStream() {
	m.turnEstimatedOutputRunes = 0
	if m.testStreamStarter != nil {
		m.testStreamStarter()
		return
	}
	m.streamCancelled = false
	m.syncSessionSelection()
	// Cancel any prior in-flight stream first: overwriting m.cancel would
	// leak the previous stream's goroutine and leave it running against an
	// orphaned context.
	if m.cancel != nil {
		m.cancel()
	}
	sess := m.session
	ref := m.ref
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go func() {
		err := chatfeature.RunStream(ctx, sess, func(event engine.StreamEvent) {
			dispatchStreamEvent(ref, event)
		})
		if err != nil {
			ref.Send(streamErrMsg{err: err})
		}
	}()
}
