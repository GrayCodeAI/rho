package cmd

import (
	"fmt"
	"math/rand/v2"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/GrayCodeAI/rho/internal/session"
)

// Stream-event handlers are kept separate from the main input router. They
// still live in cmd because they mutate the Bubble Tea model, but their
// ownership is explicit: provider output, turn state, and stream durability.

func (m chatModel) handleStreamChunk(msg streamChunkMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if m.compacting && !m.manualCompacting {
		m.compacting = false
		m.brailleSpinner.SetLabel(m.spinnerVerb)
	}
	m.turn.ObserveContent()
	chunk := string(msg)
	m.partial.WriteString(chunk)
	if m.turnOutputTokens == 0 {
		m.turnEstimatedOutputRunes += utf8.RuneCountInString(chunk)
	}
	if cmd := m.markPartialDirty(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if m.viewDirty {
		m.updateViewportContent()
	}
	return m, tea.Batch(cmds...)
}

func (m chatModel) handleStreamRenderTick() (tea.Model, tea.Cmd) {
	m.partialRenderPending = false
	if m.partialDirty {
		m.viewDirty = true
		m.partialDirty = false
		m.lastPartialRender = time.Now()
		m.updateViewportContent()
	}
	return m, nil
}

func (m chatModel) handleVoiceResult(msg voiceResultMsg) (tea.Model, tea.Cmd) {
	// The /voice subcommand reports back from a background goroutine; all model
	// mutation remains on the Bubble Tea goroutine.
	switch {
	case msg.err != "":
		m.messages = append(m.messages, displayMsg{role: "error", content: msg.err})
	case msg.info != "":
		m.messages = append(m.messages, displayMsg{role: "system", content: msg.info})
	case msg.transcript != "":
		m.input.SetValue(msg.transcript)
		m.input.CursorEnd()
		m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Voice input: %s", msg.transcript)})
	}
	m.viewDirty = true
	m.updateViewportContent()
	return m, nil
}

func (m chatModel) handleStreamRetry(msg streamRetryMsg) (tea.Model, tea.Cmd) {
	m.partial.Reset()
	m.turnEstimatedOutputRunes = 0
	m.messages = stripCurrentTurnThinking(m.messages)
	m.turn.Reset()
	m.messages = append(m.messages, displayMsg{role: "system", content: "↻ " + msg.content})
	m.viewDirty = true
	return m, nil
}

func (m chatModel) handleToolUse(msg toolUseMsg) (tea.Model, tea.Cmd) {
	m.turn.ObserveTool()
	if m.partial.Len() > 0 {
		m.messages = append(m.messages, displayMsg{role: "assistant", content: m.partial.String()})
		m.partial.Reset()
	}
	m.messages = append(m.messages, displayMsg{role: "tool_use", content: msg.name})
	m.toolStartTime = time.Now()
	m.viewDirty = true
	return m, nil
}

func (m chatModel) handleToolResult(msg toolResultMsg) (tea.Model, tea.Cmd) {
	m.turn.ObserveTool()
	// The preceding tool_use message renders the tool name as this block's
	// header, so the result contains only the returned content.
	m.messages = append(m.messages, displayMsg{role: "tool_result", content: msg.content})
	m.viewDirty = true
	// Persist completed tool results incrementally so a crash mid-turn does
	// not lose them before the final session save.
	m.ensureWAL()
	if m.wal != nil {
		m.walSeq++
		m.recordWALError(m.wal.Append(session.Message{Role: "tool_result", Content: msg.content}))
	}
	return m, nil
}

func (m chatModel) handleBlastRadius(msg blastRadiusMsg) (tea.Model, tea.Cmd) {
	m.messages = append(m.messages, displayMsg{role: "warning", content: msg.message})
	m.viewDirty = true
	return m, nil
}

func (m chatModel) handleSelectionResumed() (tea.Model, tea.Cmd) {
	// The terminal has been restored; redraw the viewport that was visible
	// before native selection mode took over.
	m.viewDirty = true
	m.updateViewportContent()
	return m, nil
}

func (m chatModel) handleStreamDone() (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if m.streamCancelled {
		m.streamCancelled = false
		m.waiting = false
		m.cancel = nil
		m.toolStartTime = time.Time{}
		m.viewDirty = true
	}
	if m.compacting {
		m.compacting = false
		m.brailleSpinner.SetLabel(m.spinnerVerb)
	}
	m.invalidateConnStatus()
	m.flushPartialDirty()
	if m.partial.Len() > 0 {
		content := sanitizeIdentity(m.partial.String())
		m.messages = append(m.messages, displayMsg{role: "assistant", content: content})
		m.ensureWAL()
		if m.wal != nil {
			m.walSeq++
			m.recordWALError(m.wal.Append(session.Message{Role: "assistant", Content: content}))
		}
		m.ghostText.Suggest(content)
		m.partial.Reset()
	} else if m.turn.ReasoningOnly() {
		m.messages = append(m.messages, displayMsg{
			role:    "error",
			content: friendlyError(fmt.Errorf("error_only_reasoning: model produced reasoning but no answer")),
		})
	}
	m.invalidateSlashSugCache()
	hadOutput := m.turn.HadAssistantOutput()
	wasCancelled := m.streamCancelled
	m.turn.Reset()
	m.clearInteractivePrompts("stream ended")
	m.waiting = false
	m.cancel = nil
	m.toolStartTime = time.Time{}
	m.viewDirty = true
	m.input.Focus()
	if saveCmd := m.saveSessionCmd(); saveCmd != nil {
		cmds = append(cmds, saveCmd)
	}
	m.trimOldMessages()
	if m.sleepCancel != nil {
		m.sleepCancel()
		m.sleepCancel = nil
	}
	ClearTabProgress()
	if m.backgrounded && !wasCancelled && hadOutput {
		sendTerminalNotification("rho", "Agent turn complete")
	}
	m.backgrounded = false
	m.notifiedComplete = false

	if len(m.messageQueue) > 0 {
		nextMsg := m.messageQueue[0]
		m.messageQueue = m.messageQueue[1:]
		m.messages = append(m.messages, displayMsg{role: "user", content: nextMsg})
		m.session.AddUser(nextMsg)
		m.waiting = true
		m.autoScroll = true
		m.viewDirty = true
		m.spinnerVerb = spinnerVerbs[rand.IntN(len(spinnerVerbs))] // #nosec G404 -- non-cryptographic use: non-cryptographic spinner selection
		m.brailleSpinner.SetLabel(m.spinnerVerb)
		m.turn.Reset()
		m.turnInputTokens = 0
		m.turnOutputTokens = 0
		m.turnEstimatedOutputRunes = 0
		m.startedAt = time.Now()
		m.partial.Reset()
		m.startStream()
		return m, tea.Batch(m.spinner.Tick, spinnerVerbTickCmd())
	}
	return m, tea.Batch(cmds...)
}
