// Package skills owns skill activation policy independent of the TUI.
package skills

import (
	"fmt"
	"strings"

	"github.com/GrayCodeAI/rho/internal/plugin"
)

// Activation is the resolved work needed to activate a skill in a session.
type Activation struct {
	Skill     plugin.SmartSkill
	Arguments string
	Prompt    string
	Conflicts []string
}

// ResolveInvocation finds an invocation in the supplied installed skills and
// builds the prompt without mutating session state.
func ResolveInvocation(invoke, fullText string, active map[string]plugin.SmartSkill, installed []plugin.SmartSkill) (*Activation, error) {
	var matched *plugin.SmartSkill
	for i := range installed {
		skill := &installed[i]
		if skill.Invoke == invoke || "/rho:"+skill.Name == invoke {
			matched = skill
			break
		}
	}
	if matched == nil {
		return nil, fmt.Errorf("skill not found: %s", invoke)
	}

	arguments := strings.TrimSpace(strings.TrimPrefix(fullText, invoke))
	prompt := fmt.Sprintf("[Skill: %s activated]\n%s", matched.Name, matched.Content)
	if arguments != "" {
		prompt = fmt.Sprintf("[Skill: %s]\n%s\n\n[User request]: %s", matched.Name, matched.Content, arguments)
	}
	return &Activation{
		Skill:     *matched,
		Arguments: arguments,
		Prompt:    prompt,
		Conflicts: plugin.ResolveChainConflicts(*matched, active),
	}, nil
}

// ResolveInstalledInvocation loads installed skills and resolves an
// invocation for normal CLI use.
func ResolveInstalledInvocation(invoke, fullText string, active map[string]plugin.SmartSkill) (*Activation, error) {
	return ResolveInvocation(invoke, fullText, active, plugin.LoadSmartSkills(plugin.DefaultSkillDirs()))
}
