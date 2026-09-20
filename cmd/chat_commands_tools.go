package cmd

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	explainfeature "github.com/GrayCodeAI/rho/internal/features/explain"
	"github.com/GrayCodeAI/rho/internal/features/shellmode"
	skillsfeature "github.com/GrayCodeAI/rho/internal/features/skills"
	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

// explainCode traces a file/line back to the git commit and session that created it.
func explainCode(path string, line int) (string, error) {
	return explainfeature.Explain(path, line)
}

// handleShellEscape runs a shell command directly (triggered by ! prefix).
func (m *chatModel) handleShellEscape(command string) (tea.Model, tea.Cmd) {
	command = strings.TrimSpace(command)
	if command == "" {
		return m, nil
	}

	// Warn about destructive commands.
	if shellmode.IsDestructive(command) {
		m.messages = append(m.messages, displayMsg{role: "error", content: "Warning: potentially destructive command detected. Use with caution."})
	}

	m.messages = append(m.messages, displayMsg{role: "system", content: "$ " + command})
	m.viewDirty = true

	result := shellmode.ExecuteShell(context.Background(), command)
	display := shellmode.FormatInteractiveResult(result)

	if display.Output != "" {
		m.messages = append(m.messages, displayMsg{role: "tool_result", content: display.Output})
	}
	if display.ExitError != "" {
		m.messages = append(m.messages, displayMsg{role: "error", content: display.ExitError})
	}

	// Smart reroute: if command failed with NL markers, offer to send to AI
	if result.ExitCode != 0 && shellmode.RerouteCandidate(command, result.Stderr, result.ExitCode) {
		m.messages = append(m.messages, displayMsg{role: "system", content: icons.Refresh() + " Natural language detected in failed command — rerouting to AI..."})
		m.termCtx.MarkExitCode(result.ExitCode)
		query := m.termCtx.BuildContext(command)
		m.messages = append(m.messages, displayMsg{role: "user", content: command})
		m.session.AddUser(query)
		m.ghostText.SuggestExplicit(command) // suggest the original command for retry
		m.waiting = true
		m.autoScroll = true
		m.viewDirty = true
		m.partial.Reset()
		m.startStream()
		return m, nil
	}

	m.termCtx.MarkExitCode(result.ExitCode)
	m.viewDirty = true
	return m, nil
}

// handleNamespacedSkill handles /vendor:skill-name invocations.
func (m *chatModel) handleNamespacedSkill(cmd, fullText string) (tea.Model, tea.Cmd) {
	activation, err := skillsfeature.ResolveInstalledInvocation(cmd, fullText, m.activeSkills)
	if err != nil {
		m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
		return m, nil
	}

	if len(activation.Conflicts) > 0 {
		m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("%s Conflicts with active skill(s): %s", icons.Alert(), strings.Join(activation.Conflicts, ", "))})
	}

	m.activeSkills[activation.Skill.Name] = activation.Skill

	m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("%s Skill activated: %s", icons.Bolt(), activation.Skill.Name)})
	m.messages = append(m.messages, displayMsg{role: "user", content: activation.Arguments})
	m.session.AddUser(activation.Prompt)
	m.waiting = true
	m.autoScroll = true
	m.viewDirty = true
	m.partial.Reset()
	m.startStream()
	return m, nil
}
