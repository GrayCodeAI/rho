package cmd

import tea "charm.land/bubbletea/v2"

// handleHistorySearchKey owns keyboard input while reverse history search is
// open. The main event loop only needs to know whether this overlay consumed
// the message and return the resulting model.
func (m chatModel) handleHistorySearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "ctrl+g", "escape", "esc":
		m.closeHistorySearch()
	case "enter":
		if len(m.historySearchFiltered) > 0 && m.historySearchSel < len(m.historySearchFiltered) {
			m.input.SetValue(m.historySearchFiltered[m.historySearchSel])
			m.input.CursorEnd()
		}
		m.closeHistorySearch()
	case "up":
		if len(m.historySearchFiltered) > 0 {
			m.historySearchSel--
			if m.historySearchSel < 0 {
				m.historySearchSel = len(m.historySearchFiltered) - 1
			}
			m.viewDirty = true
		}
	case "down":
		if len(m.historySearchFiltered) > 0 {
			m.historySearchSel = (m.historySearchSel + 1) % len(m.historySearchFiltered)
			m.viewDirty = true
		}
	default:
		if msg.Key().Text != "" && msg.Key().Mod == 0 && msg.Key().Code == 0 {
			m.historySearchInput += msg.Key().Text
			m.applyHistorySearchFilter()
			m.viewDirty = true
		} else if msg.Key().Code == tea.KeyBackspace || msg.String() == "backspace" {
			if len(m.historySearchInput) > 0 {
				runes := []rune(m.historySearchInput)
				m.historySearchInput = string(runes[:len(runes)-1])
				m.applyHistorySearchFilter()
				m.viewDirty = true
			}
		}
	}
	m.updateViewportContent()
	return m, nil
}

func (m *chatModel) closeHistorySearch() {
	m.historySearchOpen = false
	m.historySearchInput = ""
	m.historySearchQuery = ""
	m.historySearchFiltered = nil
	m.historySearchSel = 0
	m.viewDirty = true
}

// handleSessionPickerKey owns keyboard input while the saved-session picker
// is open. Selecting a session deliberately returns the resume command from
// this boundary so Update does not need to know picker internals.
func (m chatModel) handleSessionPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "ctrl+g", "escape", "esc", "ctrl+s":
		m.closeSessionPicker()
	case "enter":
		if len(m.sessionPickerFiltered) > 0 && m.sessionPickerSel < len(m.sessionPickerFiltered) {
			selected := m.sessionPickerFiltered[m.sessionPickerSel]
			m.closeSessionPicker()
			return m.resumeSessionByID(selected.ID)
		}
	case "up":
		if len(m.sessionPickerFiltered) > 0 {
			m.sessionPickerSel--
			if m.sessionPickerSel < 0 {
				m.sessionPickerSel = len(m.sessionPickerFiltered) - 1
			}
			m.viewDirty = true
		}
	case "down":
		if len(m.sessionPickerFiltered) > 0 {
			m.sessionPickerSel = (m.sessionPickerSel + 1) % len(m.sessionPickerFiltered)
			m.viewDirty = true
		}
	default:
		if msg.Key().Text != "" && msg.Key().Mod == 0 && msg.Key().Code == 0 {
			m.sessionPickerInput += msg.Key().Text
			m.applySessionPickerFilter()
			m.viewDirty = true
		} else if msg.Key().Code == tea.KeyBackspace || msg.String() == "backspace" {
			if len(m.sessionPickerInput) > 0 {
				runes := []rune(m.sessionPickerInput)
				m.sessionPickerInput = string(runes[:len(runes)-1])
				m.applySessionPickerFilter()
				m.viewDirty = true
			}
		}
	}
	m.updateViewportContent()
	return m, nil
}

func (m *chatModel) closeSessionPicker() {
	m.sessionPickerOpen = false
	m.sessionPickerInput = ""
	m.sessionPickerEntries = nil
	m.sessionPickerFiltered = nil
	m.sessionPickerSel = 0
	m.viewDirty = true
}
