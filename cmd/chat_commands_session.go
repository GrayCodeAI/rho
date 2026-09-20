package cmd

import (
	"fmt"
	"math/rand"
	"time"

	tea "charm.land/bubbletea/v2"

	sessionfeature "github.com/GrayCodeAI/rho/internal/features/session"
	workspacefeature "github.com/GrayCodeAI/rho/internal/features/workspace"
	"github.com/GrayCodeAI/rho/internal/session"
)

type sessionSaveResultMsg struct {
	id  string
	seq uint64
	err error
}

// saveSession persists the current session to disk.
func (m *chatModel) saveSession() {
	raw := m.session.RawMessages()
	if len(raw) == 0 {
		return
	}
	err := sessionfeature.Save(sessionfeature.Snapshot{
		ID: m.sessionID, Model: m.session.Model(), Provider: m.session.Provider(),
		Messages: raw, Created: time.Now(), Arc: m.session.Arc(),
	})
	// On successful save, WAL is no longer needed (session file has everything)
	if err == nil && m.wal != nil {
		if removeErr := m.wal.Remove(); removeErr != nil {
			m.recordWALError(removeErr)
		} else {
			m.wal = nil
		}
	} else if err != nil {
		m.recordWALError(err)
	}
}

// saveSessionCmd returns a background tea.Cmd that persists the session. It
// captures the messages and metadata up front so the write happens off the UI
// thread (large sessions would otherwise hitch the completion frame).
// Atomic tmp+rename in session.Save makes backgrounding safe. The WAL is
// removed only after a successful save, preserving the durability ordering.
func (m *chatModel) saveSessionCmd() tea.Cmd {
	if m == nil || m.session == nil || m.sessionID == "" {
		return nil
	}
	raw := m.session.RawMessages()
	if len(raw) == 0 {
		return nil
	}
	id, modelName, provider := m.sessionID, m.session.Model(), m.session.Provider()
	createdAt := time.Now()
	seq := m.walSeq
	arc := m.session.Arc()
	return func() tea.Msg {
		err := sessionfeature.Save(sessionfeature.Snapshot{
			ID: id, Model: modelName, Provider: provider,
			Messages: raw, Created: createdAt, Arc: arc,
		})
		return sessionSaveResultMsg{id: id, seq: seq, err: err}
	}
}

func formatQuitResumeMessage(sessionID string) string {
	return sessionfeature.QuitResumeMessage(sessionID)
}

// handleSessionCommand dispatches session-management slash commands.
func (m *chatModel) handleSessionCommand(cmd string, parts []string, text string) (tea.Model, tea.Cmd) {
	switch cmd {
	case "/quit", "/exit":
		// Re-enable system sleep before the canonical quit sequence.
		sessionfeature.StopBackgroundWork(sessionfeature.CleanupHooks{SleepCancel: m.sleepCancel})
		m.sleepCancel = nil
		return m.quitModel()

	case "/clear":
		if m.manualCompacting {
			return m.cancelManualCompact("Compaction cancelled.")
		}
		sessionfeature.StopBackgroundWork(sessionfeature.CleanupHooks{LoopCancel: m.loopCancel, ParallelCancel: m.parallelCancel})
		m.loopCancel = nil
		m.parallelCancel = nil
		m.messages = []displayMsg{{role: "system", content: "Conversation cleared."}}
		m.invalidateViewportCache()
		m.viewDirty = true
		m.autoScroll = false
		return m, nil

	case "/compact":
		if m.manualCompacting {
			return m.cancelManualCompact("Compaction cancelled.")
		}
		if m.waiting {
			m.messages = append(m.messages, displayMsg{role: "system", content: "Wait for the current response to finish, then run /compact."})
			m.viewDirty = true
			m.updateViewportContent()
			return m, nil
		}
		return m.startManualCompact()

	case "/diff":
		output, err := workspacefeature.DiffReport()
		if err != nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
			return m, nil
		}
		if output == "" {
			m.messages = append(m.messages, displayMsg{role: "system", content: "No changes detected."})
			return m, nil
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: output})
		return m, nil

	case "/history":
		report, found, err := sessionfeature.HistoryReport()
		if err != nil || !found {
			m.messages = append(m.messages, displayMsg{role: "system", content: "No saved sessions."})
			return m, nil
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: report})
		return m, nil

	case "/recover":
		candidates := sessionfeature.RecoveryCandidates()
		if len(candidates) == 0 {
			m.messages = append(m.messages, displayMsg{role: "system", content: "No interrupted sessions found."})
			return m, nil
		}
		if len(parts) >= 2 {
			// Resume specific session
			s, note, err := sessionfeature.Resume(parts[1])
			if err != nil {
				m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
				return m, nil
			}
			m.sessionID = s.ID
			m.invalidateViewportCache()
			m.messages = []displayMsg{{role: "welcome", content: m.welcomeCache}}
			hydrated := sessionfeature.Hydrate(s)
			for _, message := range hydrated.Display {
				m.messages = append(m.messages, displayMsg{role: message.Role, content: message.Content})
			}
			m.session.LoadMessages(hydrated.Runtime)
			m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Recovered: %s\nSession %s ready (%d msgs)", note, s.ID, len(s.Messages))})
			m.viewDirty = true
			m.autoScroll = false
			return m, nil
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: sessionfeature.RecoveryReport(candidates)})
		return m, nil

	case "/resume":
		if len(parts) < 2 {
			m.messages = append(m.messages, displayMsg{role: "error", content: "Usage: /resume <session-id>"})
			return m, nil
		}
		saved, err := sessionfeature.Load(parts[1])
		if err != nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
			return m, nil
		}
		m.sessionID = saved.ID
		m.invalidateViewportCache()
		m.messages = []displayMsg{{role: "welcome", content: m.welcomeCache}}
		hydrated := sessionfeature.Hydrate(saved)
		for _, message := range hydrated.Display {
			m.messages = append(m.messages, displayMsg{role: message.Role, content: message.Content})
		}
		m.session.LoadMessages(hydrated.Runtime)
		m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Resumed session %s", saved.ID)})
		m.viewDirty = true
		m.autoScroll = false
		return m, nil

	case "/fork":
		// If convodag is active, fork from the current head node
		if m.session.Persistence().Graph() != nil {
			headID := m.session.ConvoHead()
			if headID == "" {
				m.messages = append(m.messages, displayMsg{role: "error", content: "No conversation to fork from."})
				return m, nil
			}
			forkID, err := m.session.ForkConversation(headID)
			if err != nil {
				m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
				return m, nil
			}
			shortHead := headID
			if len(shortHead) > 8 {
				shortHead = shortHead[:8]
			}
			shortFork := forkID
			if len(shortFork) > 8 {
				shortFork = shortFork[:8]
			}
			m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Forked at %s → new branch %s\nYou can now take a different approach. Use /branches to see all branches.", shortHead, shortFork)})
			return m, nil
		}
		// Fallback: legacy session fork
		atIndex := len(m.session.RawMessages()) - 1
		atIndex = sessionfeature.ParseForkIndex(parts[1:], atIndex)
		if atIndex < 0 {
			m.messages = append(m.messages, displayMsg{role: "error", content: "No messages to fork from."})
			return m, nil
		}
		forked, err := sessionfeature.ForkAtMessage(m.sessionID, atIndex)
		if err != nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
			return m, nil
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Forked session %s from %s at index %d", forked.ID, m.sessionID, atIndex)})
		return m, nil

	case "/export":
		format := "md"
		if len(parts) >= 2 {
			parsedFormat, ok := sessionfeature.ParseExportFormat(parts[1])
			if !ok {
				m.messages = append(m.messages, displayMsg{
					role: "system",
					content: "Usage: /export [format]\n" +
						"  /export        — export as Markdown (default)\n" +
						"  /export md     — export as Markdown\n" +
						"  /export json   — export as structured JSON\n" +
						"  /export txt    — export as plain text",
				})
				return m, nil
			}
			format = parsedFormat
		}
		exportPath, err := exportSession(m, format)
		if err != nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
		} else {
			m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Exported to: %s", exportPath)})
		}
		return m, nil

	case "/share":
		exportPath, err := writeRedactedChatMarkdownExport(m)
		if err != nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
		} else {
			m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Session saved to: %s\nShare this file or paste its contents.", exportPath)})
		}
		return m, nil

	case "/rename":
		if len(parts) < 2 {
			m.messages = append(m.messages, displayMsg{role: "system", content: "Usage: /rename <new-session-name>"})
			return m, nil
		}
		newName := parts[1]
		if err := session.ValidateID(newName); err != nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: fmt.Sprintf("Invalid session name: %v", err)})
			return m, nil
		}
		if err := sessionfeature.Rename(m.sessionID, newName); err != nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
		} else {
			m.sessionID = newName
			m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Session renamed to: %s", newName)})
		}
		return m, nil

	case "/tag":
		if len(parts) < 2 {
			m.messages = append(m.messages, displayMsg{role: "system", content: "Usage: /tag <label>"})
			return m, nil
		}
		if err := sessionfeature.AddTag(m.sessionID, parts[1]); err != nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
		} else {
			m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Tagged: %s", parts[1])})
		}
		return m, nil

	case "/search":
		if len(parts) < 2 {
			m.messages = append(m.messages, displayMsg{role: "error", content: "Usage: /search <query>"})
			return m, nil
		}
		query := sessionfeature.SearchQuery(text)
		report, found, err := sessionfeature.SearchReport(query, 10)
		if err != nil || !found {
			m.messages = append(m.messages, displayMsg{role: "system", content: "No results found."})
			return m, nil
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: report})
		return m, nil

	case "/clean":
		days := sessionfeature.ParsePositiveDays(parts[1:], 30)
		removed, err := sessionfeature.CleanOld(time.Duration(days) * 24 * time.Hour)
		if err != nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
			return m, nil
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Cleaned %d sessions older than %d days.", removed, days)})
		return m, nil

	case "/compress":
		days := sessionfeature.ParsePositiveDays(parts[1:], 7)
		count, err := sessionfeature.CompressOld(time.Duration(days) * 24 * time.Hour)
		if err != nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
			return m, nil
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Compressed %d sessions older than %d days.", count, days)})
		return m, nil

	case "/rewind":
		if m.session.MessageCount() > 2 {
			if msgs, removed := sessionfeature.RemoveLastExchanges(m.session.RawMessages(), 1); removed > 0 {
				m.session.LoadMessages(msgs)
			}
			if len(m.messages) >= 2 {
				m.messages = m.messages[:len(m.messages)-2]
			}
			m.messages = append(m.messages, displayMsg{role: "system", content: "Rewound last exchange."})
		} else {
			m.messages = append(m.messages, displayMsg{role: "system", content: "Nothing to rewind."})
		}
		return m, nil

	case "/drop":
		if m.waiting {
			m.messages = append(m.messages, displayMsg{role: "system", content: "Wait for the current response to finish, then run /drop."})
			m.viewDirty = true
			m.updateViewportContent()
			return m, nil
		}
		sessionfeature.StopBackgroundWork(sessionfeature.CleanupHooks{
			LoopCancel: m.loopCancel, ParallelCancel: m.parallelCancel, SleepCancel: m.sleepCancel,
		})
		m.loopCancel = nil
		m.parallelCancel = nil
		m.sleepCancel = nil
		// Drop the last N exchanges (user+assistant pairs) from context.
		n, err := sessionfeature.ParsePositiveCount(parts[1:], 1)
		if err != nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: "Usage: /drop [N] (N must be a positive integer)"})
			return m, nil
		}
		if m.session.MessageCount() <= 2 {
			m.messages = append(m.messages, displayMsg{role: "system", content: "Nothing to drop."})
			return m, nil
		}
		dropped := 0
		if m.session.MessageCount() > 2 {
			if msgs, removed := sessionfeature.RemoveLastExchanges(m.session.RawMessages(), n); removed > 0 {
				m.session.LoadMessages(msgs)
				dropped = removed
			}
		}
		for i := 0; i < dropped && len(m.messages) >= 2; i++ {
			m.messages = m.messages[:len(m.messages)-2]
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Dropped %d exchange(s).", dropped)})
		m.invalidateViewportCache()
		m.viewDirty = true
		return m, nil

	case "/retry":
		if last, ok := m.history.Last(); ok {
			if m.session.MessageCount() > 2 {
				if msgs, removed := sessionfeature.RemoveLastExchanges(m.session.RawMessages(), 1); removed > 0 {
					m.session.LoadMessages(msgs)
				}
				if len(m.messages) >= 2 {
					m.messages = m.messages[:len(m.messages)-2]
				}
			}
			m.messages = append(m.messages, displayMsg{role: "user", content: last})
			m.session.AddUser(last)
			m.waiting = true
			m.autoScroll = true
			m.spinnerVerb = spinnerVerbs[rand.Intn(len(spinnerVerbs))] // #nosec G404 -- non-cryptographic use (random spinner verb selection)
			m.brailleSpinner.SetLabel(m.spinnerVerb)
			m.startStream()
			return m, nil
		}
		m.messages = append(m.messages, displayMsg{role: "error", content: "No previous message to retry."})
		return m, nil

	case "/new":
		m.saveSession()
		sessionfeature.StopBackgroundWork(sessionfeature.CleanupHooks{
			LoopCancel: m.loopCancel, ParallelCancel: m.parallelCancel,
			SleepCancel: m.sleepCancel, WatcherStop: m.watcherStop,
		})
		m.loopCancel = nil
		m.parallelCancel = nil
		m.sleepCancel = nil
		m.watcherStop = nil
		m.invalidateViewportCache()
		m.messages = []displayMsg{{role: "welcome", content: m.welcomeCache}}
		m.session.LoadMessages(nil)
		sid := genID()
		m.sessionID = sid
		if wal, err := session.NewWAL(sid); err == nil {
			m.wal = wal
		}
		m.termCtx.Reset()
		m.ghostText.Clear()
		m.messages = append(m.messages, displayMsg{role: "system", content: "New session started."})
		return m, nil

	case "/session":
		info := fmt.Sprintf("Session: %s\nModel: %s/%s\nSpec stage: %s\nMessages: %d\nTools: %d\n%s",
			m.sessionID, m.session.Provider(), m.session.Model(),
			specStageLabel(m.session), m.session.MessageCount(), len(m.registry.FluxTools()), m.session.CostValue().Summary())
		m.messages = append(m.messages, displayMsg{role: "system", content: info})
		return m, nil

	case "/snapshot":
		return m.handleSnapshot(text)

	case "/integrity":
		report, err := sessionfeature.IntegrityReport(m.sessionID)
		if err != nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: "Could not load current session: " + err.Error()})
			return m, nil
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: report})
		return m, nil
	}

	return m, nil
}
