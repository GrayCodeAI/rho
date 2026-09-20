package cmd

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	configfeature "github.com/GrayCodeAI/rho/internal/features/config"
)

// handleConfigCommand handles /config policy through the feature parser and
// keeps only persistence, session synchronization, and TUI transitions here.
func (m *chatModel) handleConfigCommand(parts []string, text string) (tea.Model, tea.Cmd) {
	command, err := configfeature.ParseCommand(parts)
	if err != nil {
		m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
		return m, nil
	}

	switch command.Action {
	case configfeature.ActionProvider:
		if err := rhoconfig.SetGlobalSetting("provider", command.Value); err != nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
			return m, nil
		}
		m.syncSessionSelection()
		modelCacheMu.RLock()
		cached, cacheHit := modelCache[m.session.Provider()]
		modelCacheMu.RUnlock()
		if cacheHit && len(cached) > 0 {
			m.session.SetModel(cached[0].ID)
			_ = rhoconfig.SetGlobalSetting("model", cached[0].ID)
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Provider set to: %s\nModel: %s\nSaved in flux (provider.json).", command.Value, m.session.Model())})
		return m, nil

	case configfeature.ActionModel:
		value := command.Value
		known := configModelChoices(m.configModelOptions, false)
		if len(known) > 0 {
			found := false
			for i, name := range known {
				if strings.EqualFold(name, value) || strings.EqualFold(m.configModelOptions[i].ID, value) {
					value = m.configModelOptions[i].ID
					found = true
					break
				}
			}
			if !found {
				m.messages = append(m.messages, displayMsg{role: "error", content: "Unknown model: " + value + "\nUse /model to browse available models."})
				return m, nil
			}
		}
		if err := rhoconfig.SetGlobalSetting("model", value); err != nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
			return m, nil
		}
		m.syncSessionSelection()
		m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Model switched to: %s\nSaved in flux (provider.json).", m.session.Model())})
		return m, nil

	case configfeature.ActionKeys:
		m.messages = append(m.messages, displayMsg{role: "system", content: apiKeyConfigSummary()})
		return m, nil

	case configfeature.ActionRemoveKey:
		return m.openConfigRemoveKeyPanel()

	case configfeature.ActionGet:
		settings, err := loadEffectiveSettings()
		if err != nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
			return m, nil
		}
		value, ok := rhoconfig.SettingValue(settings, command.Key)
		if !ok {
			m.messages = append(m.messages, displayMsg{role: "error", content: fmt.Sprintf("Unsupported setting key %q", command.Key)})
			return m, nil
		}
		if strings.TrimSpace(value) == "" {
			value = "(empty)"
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("%s = %s", command.Key, value)})
		return m, nil

	case configfeature.ActionSet:
		if err := rhoconfig.SetGlobalSetting(command.Key, command.Value); err != nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
			return m, nil
		}
		switch configfeature.NormalizeKey(command.Key) {
		case "model", "provider":
			m.syncSessionSelection()
		}
		m.messages = append(m.messages, displayMsg{role: "system", content: configfeature.UpdatedMessage(command.Key, command.Value)})
		return m, nil
	}

	settings, err := loadEffectiveSettings()
	if err != nil {
		m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
		return m, nil
	}
	m.settings = settings
	next, teaCmd := m.openConfigPanel()
	*m = next
	return m, teaCmd
}
