package cmd

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/GrayCodeAI/rho/internal/session"
)

func TestHistorySearchOverlaySelectsResultAndCloses(t *testing.T) {
	m := newTestChatModel()
	m.history.SetEntries([]string{"git status", "go test ./...", "git commit"})
	m.historySearchOpen = true
	m.historySearchInput = "git"
	m.applyHistorySearchFilter()

	next, _ := m.handleHistorySearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	cm := requireChatModel(t, next)
	if cm.historySearchOpen {
		t.Fatal("history search should close after selecting a result")
	}
	if cm.input.Value() == "" {
		t.Fatal("history search should copy the selected result into the prompt")
	}
	if cm.historySearchFiltered != nil || cm.historySearchInput != "" {
		t.Fatal("history search state should be cleared after selection")
	}
}

func TestSessionPickerOverlayDismissesAndClearsState(t *testing.T) {
	m := newTestChatModel()
	m.sessionPickerOpen = true
	m.sessionPickerInput = "rho"
	m.sessionPickerSel = 2
	m.sessionPickerEntries = make([]session.Entry, 1)
	m.sessionPickerFiltered = m.sessionPickerEntries

	next, _ := m.handleSessionPickerKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	cm := requireChatModel(t, next)
	if cm.sessionPickerOpen {
		t.Fatal("session picker should close on Escape")
	}
	if cm.sessionPickerInput != "" || cm.sessionPickerEntries != nil || cm.sessionPickerFiltered != nil || cm.sessionPickerSel != 0 {
		t.Fatal("session picker state should be cleared on dismissal")
	}
}
