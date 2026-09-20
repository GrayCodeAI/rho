package commands

import (
	"strings"
)

// ParsedInput is the normalized command input consumed by a CLI dispatcher.
type ParsedInput struct {
	Text    string
	Parts   []string
	Command string
}

// Resolution is the pure routing decision made before a live command handler
// is consulted. Handler lookup remains a composition-root concern.
type Resolution struct {
	Parsed     ParsedInput
	Command    string
	Name       string
	IsSlash    bool
	Namespaced bool
}

// Resolve applies aliases and derives the canonical registry key without
// executing or looking up a command.
func Resolve(parsed ParsedInput, aliases map[string]string) Resolution {
	command := parsed.Command
	isSlash := strings.HasPrefix(command, "/")
	if isSlash {
		if target, ok := aliases[command]; ok {
			command = target
		}
	}
	return Resolution{
		Parsed:     parsed,
		Command:    command,
		Name:       strings.TrimPrefix(command, "/"),
		IsSlash:    isSlash,
		Namespaced: isSlash && strings.Contains(command, ":"),
	}
}

// Parse normalizes the supported help shorthand and tokenizes command input.
// It deliberately does not resolve aliases or execute commands.
func Parse(text string) ParsedInput {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)
	if lower == "?" || lower == "? help" || lower == "?help" || lower == "help" {
		text = "/help"
	} else if strings.HasPrefix(lower, "? ") {
		text = "/help " + strings.TrimPrefix(trimmed, "? ")
	}
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return ParsedInput{Text: text}
	}
	command := parts[0]
	if strings.HasPrefix(command, "/") {
		command = strings.ToLower(command)
	}
	return ParsedInput{Text: text, Parts: parts, Command: command}
}
