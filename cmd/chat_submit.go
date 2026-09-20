package cmd

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine/safety"

	tea "charm.land/bubbletea/v2"

	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	"github.com/GrayCodeAI/rho/internal/engine"
	"github.com/GrayCodeAI/rho/internal/features/shellmode"
	"github.com/GrayCodeAI/rho/internal/session"
	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

// submitUserMessage handles Enter on a non-empty prompt (slash commands, shell, or agent turn).
func (m chatModel) submitUserMessage() (chatModel, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	// A pending YOLO confirmation consumes the next submitted input: exact
	// (case-insensitive) match of the token confirms, anything else cancels.
	if m.pendingYOLOConfirm {
		m.pendingYOLOConfirm = false
		m.input.Reset()
		m.viewDirty = true
		if strings.EqualFold(text, yoloConfirmToken) && m.session != nil {
			m.session.PermSvc().SetAutonomy(safety.AutonomyYOLO)
			m.settings.Autonomy = permissionTierSettingValue(safety.AutonomyYOLO)
			m.settings.AutonomyExplicit = true
			m.messages = append(m.messages, displayMsg{role: "system", content: formatAutonomyTierMessage(safety.AutonomyYOLO) + " — enabled. " + icons.CloseThick() + " You will not be prompted for permission."})
		} else {
			m.messages = append(m.messages, displayMsg{role: "system", content: "Autonomy change cancelled — stayed on the previous tier."})
		}
		m.updateViewportContent()
		return m, nil
	}
	if sugs := m.slashSuggestionsFor(text); len(sugs) > 0 {
		if m.slashSel < 0 || m.slashSel >= len(sugs) {
			m.slashSel = 0
		}
		m.input.SetValue(applySlashSuggestion(sugs[m.slashSel]))
		m.input.CursorEnd()
		return m, nil
	}
	m.pushHistory(text)
	m.input.Reset()
	if strings.HasPrefix(text, "/") {
		result, cmd := m.handleCommand(text)
		if cm, ok := result.(chatModel); ok {
			m = cm
		}
		m.viewDirty = true
		m.updateViewportContent()
		return m, cmd
	}
	if strings.HasPrefix(text, "!") {
		m.termCtx.MarkCommand(text[1:])
		result, cmd := m.handleShellEscape(text[1:])
		if cm, ok := result.(chatModel); ok {
			return cm, cmd
		}
		return m, cmd
	}
	classification := m.modeManager.ClassifyWithMode(text)
	if classification == shellmode.ClassShell && !strings.HasPrefix(text, "!") {
		m.termCtx.MarkCommand(text)
		result, cmd := m.handleShellEscape(text)
		if cm, ok := result.(chatModel); ok {
			return cm, cmd
		}
		return m, cmd
	}
	if setup := rhoconfig.EvaluateSetupCached(context.Background()); setup.NeedsSetup {
		hint := setup.Hint
		if hint == "" {
			hint = "Complete setup in /config (keychain + model)."
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: hint})
		m.viewDirty = true
		m.updateViewportContent()
		return m, nil
	}
	if err := m.ensureSessionReadyForChat(); err != nil {
		m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
		m.viewDirty = true
		m.updateViewportContent()
		return m, nil
	}
	text = m.handleMentions(text)
	userDisplay := text
	text = m.termCtx.BuildContext(text)
	scale := engine.ClassifyScale(text)
	behavior := engine.GetBehavior(scale)
	_ = behavior
	if lessons := m.selfImprover.ForPrompt(5); lessons != "" {
		m.session.AppendSystemContext(lessons)
	}
	if soul := m.codingSoul.ForPrompt(); soul != "" {
		m.session.AppendSystemContext(soul)
	}
	cwd, _ := os.Getwd()
	if hints := m.hintsLoader.LoadHints(cwd); hints != "" {
		m.session.AppendSystemContext(hints)
	}
	m.messages = append(m.messages, displayMsg{role: "user", content: userDisplay})

	if imgPath := extractImagePath(text); imgPath != "" {
		if att, err := ReadImageFile(imgPath); err == nil {
			if m.session.AddUserWithAttachment(text, att.Base64, att.MIMEType) {
				m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("%s Attached image: %s", icons.Image(), filepath.Base(imgPath))})
			} else {
				m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("%s Model %q has no vision support — image %s sent as text-only note.", icons.Alert(), m.session.Model(), filepath.Base(imgPath))})
			}
		} else {
			m.session.AddUser(text)
		}
	} else if pdfPath := extractPDFPath(text); pdfPath != "" {
		if extracted, err := ReadPDFText(pdfPath); err == nil && strings.TrimSpace(extracted) != "" {
			m.session.AddUserWithDocumentText(text, filepath.Base(pdfPath), extracted)
			m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("%s Extracted text from PDF: %s", icons.FileDocument(), filepath.Base(pdfPath))})
		} else {
			m.session.AddUser(text)
			m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("%s Could not extract text from PDF: %s", icons.Alert(), filepath.Base(pdfPath))})
		}
	} else {
		m.session.AddUser(text)
	}
	m.ensureWAL()
	if m.wal != nil {
		m.walSeq++
		m.recordWALError(m.wal.Append(session.Message{Role: "user", Content: text}))
	}
	m.turn.Reset()
	m.waiting = true
	m.autoScroll = true
	m.viewDirty = true
	m.spinnerVerb = spinnerVerbs[rand.Intn(len(spinnerVerbs))] // #nosec G404 -- non-cryptographic use (random spinner verb selection)
	m.brailleSpinner.SetLabel(m.spinnerVerb)
	m.turnInputTokens = 0
	m.turnOutputTokens = 0
	m.turnEstimatedOutputRunes = 0
	m.startedAt = time.Now()
	m.partial.Reset()
	m.startStream()
	return m, tea.Batch(m.spinner.Tick, spinnerVerbTickCmd())
}
