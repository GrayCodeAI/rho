package cmd

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine/safety"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	"github.com/GrayCodeAI/rho/internal/session"
	"github.com/GrayCodeAI/rho/internal/spec"
	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

// This file holds the Bubble Tea event loop for the chat TUI: the central
// Update message switch and the prompt arrow-key handler. Split out of
// chat.go so the model construction/lifecycle and the event loop live in
// separate, focused files.

// applyPromptArrowKey handles Up/Down in the prompt: slash menu navigation or input history.
// Returns true when the key was consumed so callers skip textarea/updateInput handling.
func (m *chatModel) applyPromptArrowKey(msg tea.KeyMsg) bool {
	if m.arrowBurstActive {
		// Only swallow further Up/Down here — they were already routed by
		// the burst-coalescing logic in Update(). Any other key (typing,
		// Escape, etc.) must still reach the input; arrowBurstActive is a
		// short-lived timing flag, not a general input lock.
		switch msg.Key().Code {
		case tea.KeyUp, tea.KeyDown:
			return true
		}
		return false
	}
	if m.uiFocus != focusPrompt || m.configOpen {
		return false
	}
	switch msg.Key().Code {
	case tea.KeyUp, tea.KeyDown:
	default:
		return false
	}
	sugs := m.slashSuggestionsFor(m.input.Value())
	if len(sugs) > 0 {
		switch msg.Key().Code {
		case tea.KeyUp:
			if m.slashSel <= 0 {
				m.slashSel = len(sugs) - 1
			} else {
				m.slashSel--
			}
		case tea.KeyDown:
			m.slashSel = (m.slashSel + 1) % len(sugs)
		}
		return true
	}
	switch msg.Key().Code {
	case tea.KeyUp:
		if value, ok := m.history.Up(m.input.Value()); ok {
			m.input.SetValue(value)
			m.input.CursorEnd()
		}
		return true
	case tea.KeyDown:
		if value, ok := m.history.Down(); ok {
			m.input.SetValue(value)
			m.input.CursorEnd()
		}
		return true
	}
	return false
}

func shouldReturnToPromptOnType(msg tea.KeyMsg) bool {
	if len(msg.Key().Text) == 0 {
		return false
	}
	if isMouseSequenceLeak(msg) {
		return false
	}
	return true
}

// quitModel performs the shared graceful-quit sequence used by every exit
// path (Ctrl+C twice, /quit, SIGINT as tea.InterruptMsg, SIGTERM/SIGHUP as
// tea.QuitMsg): cancel any in-flight stream, persist the session, stop
// background workers (watcher, parallel agents, background tasks), and mark
// the model as quitting so the final view can show the resume hint.
func (m *chatModel) quitModel() (tea.Model, tea.Cmd) {
	m.clearInteractivePrompts("rho is exiting")
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	saveInputHistory(m.history.Entries())
	m.saveSession()
	if m.watcherStop != nil {
		m.watcherStop()
	}
	if m.parallelCancel != nil {
		m.parallelCancel()
	}
	if m.bgCancel != nil {
		m.bgCancel()
	}
	m.quitting = true
	return m, tea.Quit
}

func (m chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if _, isMouse := msg.(tea.MouseMsg); !isMouse {
		if changed, prCmd := m.refreshStatusBarLeft(false); changed {
			m.viewDirty = true
			if prCmd != nil {
				cmds = append(cmds, prCmd)
			}
		}
	}

	switch msg := msg.(type) {
	case sessionSaveResultMsg:
		return m.updateSessionSaveResult(msg)
	case tea.FocusMsg:
		return m.updateFocusMsg(msg)
	case tea.BlurMsg:
		return m.updateBlurMsg(msg)
	case tea.InterruptMsg:
		return m.updateInterruptMsg(msg)
	case tea.QuitMsg:
		return m.updateQuitMsg(msg)
	case promptKeepAliveMsg:
		return m.updatePromptKeepAliveMsg(msg)
	case statusLeftPRsMsg:
		return m.updateStatusLeftPRsMsg(msg)
	case eyeBlinkTickMsg:
		return m.updateEyeBlinkTickMsg(msg)
	case eyeFrameNextMsg:
		return m.updateEyeFrameNextMsg(msg)
	case tea.MouseMsg:
		if m.mouseEnabled() {
			if m.configOpen {
				if next, handled := m.handleConfigMouse(msg); handled {
					next.viewDirty = true
					next.updateViewportContent()
					return next, nil
				}
			}
			if msg.Mouse().Button == tea.MouseWheelUp || msg.Mouse().Button == tea.MouseWheelDown {
				m.trackMousePosition(msg)
				cmds = append(cmds, m.applyMouseScroll(msg))
				m.sanitizeInputIfNeeded()
				m = m.syncViewportMouseWheel().withSyncedLayout()
				if m.syncInputLayout() {
					m.updateViewportContent()
				}
				if focus := m.ensurePromptInputFocus(); focus != nil {
					cmds = append(cmds, focus)
				}
			} else {
				// Motion events (?1003): track pointer only — avoid layout/sanitize/focus per move.
				m.trackMousePosition(msg)
			}
		}
		return m, tea.Batch(cmds...)

	case processArrowTickMsg:
		return m.updateProcessArrowTickMsg(msg)
	case tea.PasteMsg:
		return m.updatePasteMsg(msg)
	case tea.KeyMsg:
		s := msg.String()
		if (s == "up" || s == "down") && !m.processingGenuineArrow {
			now := time.Now()
			dt := now.Sub(m.lastArrowTime)
			m.lastArrowTime = now
			m.arrowSeq++
			seq := m.arrowSeq

			if dt < 30*time.Millisecond {
				m.arrowBurstActive = true
				if m.pendingArrow != nil {
					pMsg := *m.pendingArrow
					m.pendingArrow = nil
					m.processingGenuineArrow = true
					next, cmd := m.Update(pMsg)
					if nextModel, ok := next.(chatModel); ok {
						m = nextModel
					}
					m.processingGenuineArrow = false
					if cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
				// Proceed to process `msg` immediately (fall through with m.arrowBurstActive = true).
				// Arm a trailing tick so that if this is the last keypress of
				// the burst, arrowBurstActive still gets cleared once things
				// go quiet — otherwise it would stay stuck true forever.
				cmds = append(cmds, tea.Tick(30*time.Millisecond, func(t time.Time) tea.Msg {
					return processArrowTickMsg{seq: seq}
				}))
			} else {
				m.arrowBurstActive = false
				m.pendingArrow = &msg
				return m, tea.Tick(30*time.Millisecond, func(t time.Time) tea.Msg {
					return processArrowTickMsg{seq: seq}
				})
			}
		} else {
			if m.pendingArrow != nil && !m.processingGenuineArrow {
				pMsg := *m.pendingArrow
				m.pendingArrow = nil
				m.arrowBurstActive = false
				m.processingGenuineArrow = true
				next, cmd := m.Update(pMsg)
				if nextModel, ok := next.(chatModel); ok {
					m = nextModel
				}
				m.processingGenuineArrow = false
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}

		if isCopyToClipboardKey(msg) {
			return m.handleCopyShortcut()
		}
		if isMouseSequenceLeak(msg) {
			if m.configOpen {
				if next, handled := m.handleConfigMouseLeak(msg); handled {
					next.viewDirty = true
					next.updateViewportContent()
					return next, nil
				}
			}
			if handled, cmd := m.tryScrollFromMouseLeak(msg); handled {
				m.sanitizeInputIfNeeded()
				if focus := m.ensurePromptInputFocus(); focus != nil {
					return m, tea.Batch(cmd, focus)
				}
				return m, cmd
			}
			m.sanitizeInputIfNeeded()
			if focus := m.ensurePromptInputFocus(); focus != nil {
				return m, focus
			}
			return m, nil
		}
		// Input history search (Ctrl+R) — intercept all input when open.
		if m.historySearchOpen {
			return m.handleHistorySearchKey(msg)
		}

		// Session picker (Ctrl+S) — intercept all input when open.
		if m.sessionPickerOpen {
			return m.handleSessionPickerKey(msg)
		}

		// Command palette (Ctrl+K) — intercept all input when open.
		// The palette owns Ctrl+K while open so the shortcut toggles it cleanly.
		if m.commandPalette != nil && m.commandPalette.IsOpen() {
			action, handled, paletteCmd := m.commandPalette.Update(msg)
			if handled {
				if action != "" {
					// Execute the selected command
					m.commandPalette.Close()
					result, _ := m.handleCommand(action)
					if cm, ok := result.(chatModel); ok {
						m = cm
					}
					m.viewDirty = true
					m.updateViewportContent()
				}
				return m, paletteCmd
			}
		}

		// Autonomy tier picker (/autonomy) — intercept all input when open
		if m.autonomyPicker != nil && m.autonomyPicker.IsOpen() {
			chosen, handled := m.autonomyPicker.Update(msg)
			if handled {
				if chosen != nil && m.session != nil {
					// YOLO ("Autonomous") is unattended mode: require a typed
					// confirmation instead of a single Enter, so a stray key
					// cannot silently drop the session into never-ask.
					if chosen.Level == safety.AutonomyYOLO {
						m.pendingYOLOConfirm = true
						m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Autonomy tier → %s — this enables unattended mode (never prompts for permission). Type %s then Enter to confirm, or type anything else to cancel.", chosen.Name, yoloConfirmToken)})
						m.viewDirty = true
						m.updateViewportContent()
						return m, nil
					}
					m.session.PermSvc().SetAutonomy(chosen.Level)
					m.settings.Autonomy = permissionTierSettingValue(chosen.Level)
					m.settings.AutonomyExplicit = true
					m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Autonomy tier → %s\nBehavior: %s", chosen.Name, chosen.Description)})
				}
				m.viewDirty = true
				m.updateViewportContent()
				return m, nil
			}
		}

		// Theme picker (/theme) — intercept all input when open
		if m.themePicker != nil && m.themePicker.IsOpen() {
			chosen, handled := m.themePicker.Update(msg)
			if handled {
				if chosen != nil {
					if err := rhoconfig.SetGlobalSetting("theme", chosen.Name); err != nil {
						m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
					} else {
						// Apply immediately — repaints with full palette on next frame.
						ApplyTheme(chosen.Name)
						m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("%s Theme set to: %s", icons.CheckBold(), chosen.Name)})
					}
				}
				m.viewDirty = true
				m.updateViewportContent()
				return m, nil
			}
		}

		// Spec workflow picker (/spec) — intercept all input when open
		if m.specPicker != nil && m.specPicker.IsOpen() {
			chosen, handled := m.specPicker.Update(msg)
			if handled {
				if chosen != nil && m.session != nil {
					switch chosen.Action {
					case specActionStart:
						m.session.PermSvc().SetSpecStage(safety.SpecStageProposal)
						m.messages = append(m.messages, displayMsg{role: "system", content: "Spec workflow started — Write/Edit/Bash are gated. Start with Proposal, then Specify + Design (parallel), then Plan, Tasks, and ApproveImplementation."})
					case specActionStatus:
						m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Spec stage: %s", specStageLabel(m.session))})
					case specActionEdit:
						m.messages = append(m.messages, displayMsg{role: "system", content: "Use the SpecEdit tool to modify spec artifacts (spec.md, plan.md, tasks.md). You can apply deltas or replace content entirely."})
					case specActionResume:
						stage := currentSpecStage(m.session)
						stageName := specStageDisplayName(stage)
						m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Resuming from %s — continue working through the spec workflow.", stageName)})
					case specActionArchive:
						m.messages = append(m.messages, displayMsg{role: "system", content: "Archive a completed spec. The agent will use the ArchiveSpec tool to archive the spec when implementation is complete."})
					case specActionConfigure:
						cfg := spec.LoadSpecConfig()
						msg := "Spec configuration:\n" + cfg.Format()
						msg += "\n\nUse `/spec config set <field> <value>` to change settings."
						msg += "\nThe agent can also use `SpecConfig` tool to read/update."
						m.messages = append(m.messages, displayMsg{role: "system", content: msg})
					case specActionReset:
						m.session.PermSvc().ResetSpec()
						m.messages = append(m.messages, displayMsg{role: "system", content: "Spec workflow reset — Write/Edit/Bash follow the trust tier again."})
					}
				}
				m.viewDirty = true
				m.updateViewportContent()
				return m, nil
			}
		}

		if m.manualCompacting {
			if isCompactCancelKey(msg) {
				return m.cancelManualCompact("Compaction cancelled.")
			}
			if msg.String() == "enter" {
				return m, nil
			}
			// Allow typing in the input while compaction runs (Esc cancels).
		}

		if m.inScrollbackFocus() {
			switch msg.Key().Code {
			case tea.KeyTab:
				return m.cycleUIFocus()
			case tea.KeyEsc:
				m.uiFocus = focusPrompt
				m.viewDirty = true
				return m, m.input.Focus()
			case tea.KeyEnter:
				// Toggle expansion of the tool result nearest the viewport center.
				if idx := m.toolResultIndexAtViewportCenter(m.width); idx >= 0 {
					m.toolResultExpanded[idx] = !m.toolResultExpanded[idx]
					m.invalidateViewportCache()
					m.viewDirty = true
					m.updateViewportContent()
				}
				return m, nil
			}
			// 'c' copies the code block nearest to viewport center.
			if msg.String() == "c" {
				content, ok := m.codeBlockAtViewportCenter()
				if !ok {
					m.messages = append(m.messages, displayMsg{
						role:    "system",
						content: "No code block found at viewport center. Scroll to a code block and press 'c' again.",
					})
					m.viewDirty = true
					m.updateViewportContent()
					return m, nil
				}
				result := copyToClipboard(content)
				lineCount := strings.Count(content, "\n") + 1
				if result.FallbackPath != "" {
					// Clipboard unavailable — saved to file.
					m.messages = append(m.messages, displayMsg{
						role:    "system",
						content: fmt.Sprintf("Clipboard unavailable — saved code block (%d lines) to %s", lineCount, result.FallbackPath),
					})
				} else {
					// Show a brief highlight message with line count.
					m.messages = append(m.messages, displayMsg{
						role:    "system",
						content: fmt.Sprintf("%s Copied code block (%d lines) to clipboard.", icons.CheckBold(), lineCount),
					})
				}
				m.viewDirty = true
				m.updateViewportContent()
				return m, nil
			}
			if shouldReturnToPromptOnType(msg) {
				m.uiFocus = focusPrompt
				m.viewDirty = true
				cmds = append(cmds, m.input.Focus())
				cmds = append(cmds, m.updateInput(msg))
				m.updateViewportContent()
				return m, tea.Batch(cmds...)
			}
			if scrolled, cmd := m.applyViewportScroll(msg); scrolled {
				return m, cmd
			}
			if m.routeKeyToViewport(msg) {
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				if m.viewport.AtBottom() {
					m.autoScroll = true
				} else {
					m.autoScroll = false
				}
				return m, cmd
			}
			return m, nil
		}

		if scrolled, cmd := m.applyViewportScroll(msg); scrolled {
			return m, tea.Batch(append(cmds, cmd)...)
		}

		// Permission prompt active — handle allow/deny scope keys.
		if m.approvalReq != nil {
			return m.handleApprovalResponse(msg)
		}

		if m.permReq != nil {
			return m.handlePermissionResponse(msg)
		}

		// Credential prompt active — handle y/n
		if m.credentialReq != nil {
			return m.handleCredentialResponse(msg)
		}

		// AskUser prompt active — Enter submits answer
		if m.askReq != nil {
			if msg.String() == "esc" || msg.String() == "escape" {
				resolveAskUserResponse(m.askReq, "")
				m.askReq = nil
				m.askTimeoutAt = time.Time{}
				m.messages = append(m.messages, displayMsg{role: "system", content: icons.CloseThick() + " Question denied."})
				m.viewDirty = true
				m.updateViewportContent()
				return m.activateNextPrompt()
			}
			if msg.String() == "enter" {
				answer := strings.TrimSpace(m.input.Value())
				m.input.Reset()
				m.messages = append(m.messages, displayMsg{role: "user", content: answer})
				resolveAskUserResponse(m.askReq, answer)
				m.askReq = nil
				m.askTimeoutAt = time.Time{}
				m.viewDirty = true
				m.updateViewportContent()
				return m.activateNextPrompt()
			}
			return m, m.updateInput(msg)
		}
		if m.waiting {
			if msg.String() == "ctrl+c" {
				// First Ctrl+C cancels stream, second quits
				if m.cancel != nil {
					m.lastCtrlC = time.Now()
					m.cancel()
					m.cancel = nil
					m.streamCancelled = true
					m.messages = append(m.messages, displayMsg{role: "system", content: icons.Stop() + " Cancelled."})
					if m.partial.Len() > 0 {
						m.messages = append(m.messages, displayMsg{role: "assistant", content: m.partial.String()})
						m.partial.Reset()
					}
					m.waiting = false
					m.input.Focus()
					m.viewDirty = true
					m.updateViewportContent()
					return m, nil
				}
				return m.quitModel()
			}
			if msg.String() == "escape" {
				if m.cancel != nil {
					m.cancel()
					m.cancel = nil
					m.streamCancelled = true
					m.messages = append(m.messages, displayMsg{role: "system", content: icons.Stop() + " Cancelled."})
					if m.partial.Len() > 0 {
						m.messages = append(m.messages, displayMsg{role: "assistant", content: m.partial.String()})
						m.partial.Reset()
					}
					m.waiting = false
					m.input.Focus()
				}
				m.viewDirty = true
				m.updateViewportContent()
				return m, nil
			}
			// Queue message on Enter while agent is working
			if msg.String() == "enter" {
				text := strings.TrimSpace(m.input.Value())
				if text != "" {
					m.pushHistory(text)
					m.enqueueMessage(text)
					m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("%s Queued: %s", icons.Mail(), text)})
					m.input.Reset()
					m.viewDirty = true
					m.updateViewportContent()
				}
				return m, nil
			}
			if m.applyPromptArrowKey(msg) {
				return m, tea.Batch(cmds...)
			}
			return m, m.updateInput(msg)
		}
		if m.configOpen {
			switch msg.String() {
			case "ctrl+c":
				if time.Since(m.lastCtrlC) < 1*time.Second {
					return m.quitModel()
				}
				m.lastCtrlC = time.Now()
				m.messages = append(m.messages, displayMsg{role: "system", content: quitAgainMsg})
				m.viewDirty = true
				m.updateViewportContent()
				return m, nil
			default:
				next, cmd := m.handleConfigKey(msg)
				next.viewDirty = true
				next.updateViewportContent()
				return next, cmd
			}
		}

		// Handle modifier key combos (ctrl+a, ctrl+k, etc.) via string matching.
		switch msg.Key().Mod {
		case 0: // no modifier
		default:
			// modifier combos: check the keystroke string
			keyText := msg.String()
			if keyText != "" {
				switch keyText {
				case "ctrl+a":
					m.hudOpen = !m.hudOpen
					if m.hudOpen {
						m.hudData = m.collectHUDData()
					}
					m.viewDirty = true
					m.updateViewportContent()
					return m, nil
				case "ctrl+k", "ctrl+p":
					if m.commandPalette == nil {
						m.commandPalette = NewCommandPaletteWithRuntime(m.width, m.pluginRuntime)
					}
					m.commandPalette.Open()
					m.viewDirty = true
					m.updateViewportContent()
					return m, nil
				case "ctrl+r":
					// Reverse-i-search: only when input is empty and not waiting.
					if strings.TrimSpace(m.input.Value()) == "" && !m.waiting {
						m.historySearchOpen = true
						m.historySearchInput = ""
						m.historySearchQuery = ""
						m.historySearchFiltered = nil
						m.historySearchSel = 0
						m.applyHistorySearchFilter()
						m.viewDirty = true
						m.updateViewportContent()
					}
					return m, nil
				case "ctrl+s":
					// Session picker: fuzzy search through saved sessions.
					if !m.waiting {
						m.sessionPickerOpen = true
						m.sessionPickerInput = ""
						m.sessionPickerEntries, _ = session.List()
						m.sessionPickerFiltered = m.sessionPickerEntries
						m.sessionPickerSel = 0
						m.viewDirty = true
						m.updateViewportContent()
					}
					return m, nil
				case "?":
					// Quick help — show contextual help summary in chat.
					m.messages = append(m.messages, displayMsg{role: "system", content: "Quick help:\n  /start           — guided setup (trust, mode, branch)\n  /mode plan|act   — research vs build\n  /isolation       — workspace and permission scope\n  /help            — list all commands\n  /help <topic>    — detailed help (e.g., /help /commit)\n  ctrl+K           — command palette\n  ctrl+L           — cycle autonomy tiers\n  ctrl+N           — switch model\n  ctrl+R           — search input history\n  ?                — show this help\n  Type / to see slash commands, or ask a question to get started."})
					m.viewDirty = true
					m.updateViewportContent()
					return m, nil
				case "ctrl+n":
					models := configModelChoices(m.configModelOptions, false)
					if len(models) > 1 {
						current := m.session.Model()
						idx := 0
						for i, md := range models {
							if md == current {
								idx = (i + 1) % len(models)
							}
						}
						m.session.SetModel(models[idx])
						m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Model → %s", models[idx])})
					}
					m.viewDirty = true
					m.updateViewportContent()
					return m, nil
				case "ctrl+l":
					// Expire a stale Supervised-confirmation prompt.
					if m.supervisedPending && time.Since(m.supervisedPendingAt) > 1500*time.Millisecond {
						m.supervisedPending = false
					}
					current := m.session.PermSvc().RuntimeState().Autonomy
					// Guard landing on Supervised: when the cycle would reach it
					// (current is YOLO), require a second Ctrl+L within 1.5s. This
					// prevents accidental max-friction while keeping it one deliberate
					// gesture away.
					if isSupervisedPending(current) && !m.supervisedPending {
						m.supervisedPending = true
						m.supervisedPendingAt = time.Now()
						m.messages = append(m.messages, displayMsg{
							role:    "warning",
							content: "Ctrl+L again within 1.5s to confirm Always Ask (max friction), or wait to skip.",
						})
						m.viewDirty = true
						m.updateViewportContent()
						return m, nil
					}
					var nextTier safety.AutonomyLevel
					if m.supervisedPending && isSupervisedPending(current) {
						nextTier = nextAutonomyTierIncludingSupervised(current)
					} else {
						nextTier = nextAutonomyTier(current)
					}
					if current == 0 || autonomyTierIndex(current) < 0 {
						nextTier = DefaultAutonomy
					}
					m.supervisedPending = false
					m.session.PermSvc().SetAutonomy(nextTier)
					m.settings.AutonomyExplicit = true
					m.invalidateConnStatus()
					m.messages = append(m.messages, displayMsg{
						role:    "warning",
						content: formatAutonomyTierMessage(nextTier) + "  ·  Ctrl+L to change",
					})
					m.viewDirty = true
					m.updateViewportContent()
					return m, nil
				case "ctrl+c":
					if time.Since(m.lastCtrlC) < 1*time.Second {
						return m.quitModel()
					}
					m.lastCtrlC = time.Now()
					m.messages = append(m.messages, displayMsg{role: "system", content: quitAgainMsg})
					m.viewDirty = true
					m.updateViewportContent()
					return m, nil
				case "shift+tab":
					if m.specPicker == nil {
						m.specPicker = NewSpecPicker(m.width)
					}
					m.specPicker.Open(currentSpecStage(m.session))
					m.viewDirty = true
					m.updateViewportContent()
					return m, nil
				}
			}
		}

		// Main key dispatch (special keys via code, runes via text)
		switch msg.Key().Code {
		case tea.KeyTab:
			// Accept ghost text suggestion if active and input is empty
			if m.ghostText.Active() && strings.TrimSpace(m.input.Value()) == "" {
				accepted := m.ghostText.Accept()
				m.input.SetValue(accepted)
				m.input.CursorEnd()
				return m, nil
			}
			sugs := m.slashSuggestionsFor(m.input.Value())
			if len(sugs) > 0 {
				if m.slashSel < 0 || m.slashSel >= len(sugs) {
					m.slashSel = 0
				}
				m.input.SetValue(applySlashSuggestion(sugs[m.slashSel]))
				m.input.CursorEnd()
				return m, nil
			}
			return m.cycleUIFocus()
		case tea.KeyUp, tea.KeyDown:
			if m.applyPromptArrowKey(msg) {
				return m, tea.Batch(cmds...)
			}
		case tea.KeyEsc:
			if m.inScrollbackFocus() {
				return m.cycleUIFocus()
			}
			if m.waiting {
				return m, nil
			}
			if len(m.slashSuggestionsFor(m.input.Value())) > 0 {
				m.slashSel = 0
				return m, nil
			}
		case tea.KeyEnter:
			return m.submitUserMessage()
		}

	case platformContextIndexMsg:
		updatePlatformContextCache(msg)
		m.invalidateConnStatus()
		m.viewDirty = true
		return m, nil

	case modelsFetchedMsg:
		m.configSaving = false
		if msg.err != nil {
			if m.configOpen {
				m.configNotice = sanitizeConfigNotice(rhoconfig.FormatConfigProviderError(msg.provider, msg.err))
				m.viewDirty = true
				m.updateViewportContent()
			}
			return m, nil
		}
		if len(msg.options) > 0 {
			m.configModelOptions = msg.options
			if msg.provider != "" {
				modelCacheMu.Lock()
				modelCache[msg.provider] = msg.options
				modelCacheMu.Unlock()
			}
			if m.configOpen && strings.Contains(m.configNotice, "Loading") {
				m.configNotice = ""
			}
		} else if m.configOpen {
			m.configNotice = rhoconfig.CatalogEmptyHint(context.Background())
		}
		if m.session != nil && msg.provider != "" {
			gw, _ := m.sessionGatewayModel()
			if gw == "" {
				gw = msg.provider
			}
			if strings.TrimSpace(gw) == strings.TrimSpace(msg.provider) {
				applyLiveModelMetadata(m.session, gw, m.session.Model())
			}
		}
		m.invalidateConnStatus()
		m.viewDirty = true
		if m.configOpen {
			if m.configTab == configTabModels {
				m = m.focusConfigActiveModelSelection()
			}
			m.updateViewportContent()
		}
		return m, nil

	case pluginRuntimeReadyMsg:
		if msg.runtime != nil {
			m.pluginRuntime = msg.runtime
			if m.commandPalette != nil {
				m.commandPalette.RefreshRuntime(msg.runtime)
			}
			m.rebuildWelcomeCache(m.blinkClosed)
			m.viewDirty = true
			m.updateViewportContent()
		}
		return m, nil

	case startupWarmMsg:
		if strings.TrimSpace(msg.statusLeftVal) != "" {
			m.statusLeftKey = msg.statusLeftKey
			m.statusLeftVal = msg.statusLeftVal
			m.statusLeftBranch = msg.statusLeftBranch
			m.statusLeftAt = time.Now()
		}
		if msg.connStatusKey != "" || msg.connStatusVal != "" {
			m.connStatusKey = msg.connStatusKey
			m.connStatusVal = msg.connStatusVal
		}
		m.welcomeSetupState = msg.welcomeSetup
		m.welcomeAgentsOK = msg.welcomeAgentsOK
		m.rebuildWelcomeCache(m.blinkClosed)
		m.viewDirty = true
		m.updateViewportContent()
		return m, nil

	case systemPromptContextReadyMsg:
		if contextBlock := strings.TrimSpace(msg.context); contextBlock != "" {
			m.deferredSystemContext = contextBlock
			m.deferredSystemContextReady = true
			if m.session != nil && !m.deferredSystemContextApplied {
				m.session.AppendSystemContext(contextBlock)
				m.deferredSystemContextApplied = true
			}
		}
		return m, nil

	case configApplyCredentialsMsg:
		next, cmd := m.handleConfigApplyCredentialsMsg(msg)
		if m.configOpen {
			next.viewDirty = true
			next.updateViewportContent()
		}
		return next, cmd

	case configGatewayRefreshMsg:
		next := m.handleConfigGatewayRefreshMsg(msg)
		if m.configOpen {
			next.viewDirty = true
			next.updateViewportContent()
		}
		return next, nil

	case configRemoveCredentialMsg:
		next, cmd := m.handleConfigRemoveCredentialMsg(msg)
		if m.configOpen {
			next.viewDirty = true
			next.updateViewportContent()
		}
		return next, cmd

	case loopTickMsg:
		if !m.waiting {
			result, cmd := m.handleCommand(msg.command)
			m.viewDirty = true
			m.updateViewportContent()
			return result, cmd
		}
		return m, nil

	case streamChunkMsg:
		return m.handleStreamChunk(msg)

	case streamRenderTickMsg:
		return m.handleStreamRenderTick()

	case thinkingMsg:
		m.turn.ObserveThinking()
		return m, nil

	case voiceResultMsg:
		return m.handleVoiceResult(msg)

	case streamRetryMsg:
		return m.handleStreamRetry(msg)

	case toolUseMsg:
		return m.handleToolUse(msg)

	case toolResultMsg:
		return m.handleToolResult(msg)

	case blastRadiusMsg:
		return m.handleBlastRadius(msg)

	case selectionResumedMsg:
		// Returned from enterSelectionMode. The terminal has been
		// restored; just trigger a redraw so the viewport reflects the
		// state that was visible before selection.
		return m.handleSelectionResumed()

	case permissionAskMsg:
		return m.handlePermissionAsk(msg)

	case permissionPromptTimeoutMsg:
		return m.handlePermissionTimeout(msg)

	case promptCountdownTickMsg:
		return m.handlePromptCountdownTick(msg)

	case approvalAskMsg:
		return m.handleApprovalAsk(msg)

	case approvalPromptTimeoutMsg:
		return m.handleApprovalTimeout(msg)

	case askUserMsg:
		return m.handleAskUser(msg)

	case askUserPromptTimeoutMsg:
		return m.handleAskUserTimeout(msg)

	case credentialAskMsg:
		return m.handleCredentialAsk(msg)

	case credentialPromptTimeoutMsg:
		return m.handleCredentialTimeout(msg)

	case usageUpdateMsg:
		if msg.usage != nil {
			m.turnInputTokens += msg.usage.PromptTokens
			m.turnOutputTokens += msg.usage.CompletionTokens
			m.invalidateConnStatus()
			m.viewDirty = true
		}

	case compactTickMsg:
		if m.manualCompacting {
			if m.brailleSpinner != nil {
				m.brailleSpinner.Tick()
			}
			m.viewDirty = true
			m.updateViewportContent()
			localCmds := []tea.Cmd{compactTickCmd()}
			if !m.input.Focused() {
				localCmds = append(localCmds, m.input.Focus())
			}
			return m, tea.Batch(localCmds...)
		}

	case compactDoneMsg:
		return m.finishManualCompact(msg)

	case compactStartMsg:
		if !m.manualCompacting {
			m.compacting = true
			m.brailleSpinner.SetLabel("Compacting context")
			m.viewDirty = true
		}

	case compactMsg:
		m.compacting = false
		m.brailleSpinner.SetLabel(m.spinnerVerb)
		line := fmt.Sprintf(
			"Context compacted (%s): ~%s → ~%s tokens",
			msg.strategy,
			formatRhoTokenCount(msg.tokensBefore),
			formatRhoTokenCount(msg.tokensAfter),
		)
		m.messages = append(m.messages, displayMsg{role: "system", content: line})
		m.invalidateConnStatus()
		m.viewDirty = true

	case streamDoneMsg:
		return m.handleStreamDone()

	case streamErrMsg:
		m.messages = append(m.messages, displayMsg{role: "error", content: friendlyError(msg.err)})
		m.partial.Reset()
		// Resolve any pending permission/approval/askUser prompts to unblock
		// waiting goroutines. All interactive gates fail closed on stream errors.
		m.clearInteractivePrompts("stream ended")
		m.waiting = false
		m.cancel = nil
		m.toolStartTime = time.Time{}
		m.viewDirty = true
		m.input.Focus()

	case spinnerVerbTickMsg:
		if !m.waiting {
			return m, tea.Batch(cmds...)
		}
		cmds = append(cmds, spinnerVerbTickCmd())
		if strings.TrimSpace(m.partial.String()) == "" {
			m.spinnerVerb = spinnerVerbs[rand.IntN(len(spinnerVerbs))] // #nosec G404 -- non-cryptographic use (random spinner verb selection)
			m.brailleSpinner.SetLabel(m.spinnerVerb)
			m.viewDirty = true
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(msg.Width - 4)
		m.configInput.SetWidth(msg.Width - 4)
		m.invalidateInputLayoutCache()
		m.rebuildWelcomeCache(false)
		m.viewDirty = true
		m.refreshInputLayoutIfNeeded()
		m = m.withSyncedLayout()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.waiting {
			if strings.TrimSpace(m.partial.String()) == "" {
				m.brailleSpinner.Tick()
				if m.startedAt.IsZero() {
					m.startedAt = time.Now()
				}
				m.viewDirty = true
			}
			m.displayInTok += (float64(m.tokenInputTarget()) - m.displayInTok) * 0.25
			m.displayOutTok += (float64(m.tokenOutputTarget()) - m.displayOutTok) * 0.25
		}

		if cmd != nil {
			cmds = append(cmds, cmd)
		} else if m.waiting {
			cmds = append(cmds, m.spinner.Tick)
		}
		if !m.waiting {
			return m, tea.Batch(cmds...)
		}
	}

	if !m.waiting && m.uiFocus == focusPrompt {
		// Clear ghost text when user starts typing
		if m.ghostText.Active() && m.input.Value() != "" {
			m.ghostText.Clear()
		}
		// Vim mode key interception (operates on full textarea value)
		if m.vim != nil && m.vim.IsEnabled() {
			if keyMsg, ok := msg.(tea.KeyMsg); ok {
				text := m.input.Value()
				// textarea doesn't expose cursor column; use text length as approximation
				cursor := len(text)
				newText, newCursor, consumed := m.vim.HandleKey(keyMsg, text, cursor)
				if consumed {
					if newText != text {
						m.input.SetValue(newText)
					}
					m.input.SetCursorColumn(newCursor)
				}
				if consumed && m.vim.Mode == VimNormal {
					return m, tea.Batch(cmds...)
				}
			}
		}
		if shouldForwardToInput(msg) {
			cmds = append(cmds, m.updateInput(msg))
		}
	}
	if m.uiFocus == focusPrompt && !m.input.Focused() {
		cmds = append(cmds, m.input.Focus())
	}

	layoutChanged := m.refreshInputLayoutIfNeeded()
	if layoutChanged {
		m = m.withSyncedLayout()
	}
	if m.viewDirty || layoutChanged {
		m.updateViewportContent()
	}

	return m, tea.Batch(cmds...)
}
