// Package config owns configuration command policy independent of TUI state.
package config

import (
	"fmt"
	"strings"
)

// Action identifies the configuration operation requested by slash input.
type Action string

const (
	ActionOpen      Action = "open"
	ActionProvider  Action = "provider"
	ActionModel     Action = "model"
	ActionKeys      Action = "keys"
	ActionRemoveKey Action = "remove-key"
	ActionGet       Action = "get"
	ActionSet       Action = "set"
)

// Command is a parsed configuration command.
type Command struct {
	Action Action
	Key    string
	Value  string
}

// UsageError is returned for a syntactically invalid configuration command.
type UsageError struct{ Text string }

func (e *UsageError) Error() string { return e.Text }

// ParseCommand classifies /config arguments while leaving side effects to the
// composition layer.
func ParseCommand(parts []string) (Command, error) {
	if len(parts) <= 1 {
		return Command{Action: ActionOpen}, nil
	}

	subcommand := strings.ToLower(strings.TrimSpace(parts[1]))
	switch subcommand {
	case "provider":
		if len(parts) >= 3 {
			return Command{Action: ActionProvider, Value: strings.TrimSpace(strings.Join(parts[2:], " "))}, nil
		}
		return Command{Action: ActionOpen}, nil
	case "model":
		if len(parts) >= 3 {
			return Command{Action: ActionModel, Value: strings.TrimSpace(strings.Join(parts[2:], " "))}, nil
		}
		return Command{Action: ActionOpen}, nil
	case "keys":
		if len(parts) > 2 {
			return Command{}, &UsageError{Text: "Usage: /config keys"}
		}
		return Command{Action: ActionKeys}, nil
	case "key":
		if len(parts) >= 3 && parts[2] == "remove" {
			if len(parts) > 3 {
				return Command{}, &UsageError{Text: "Usage: /config key remove"}
			}
			return Command{Action: ActionRemoveKey}, nil
		}
		return Command{}, &UsageError{Text: "Usage: /config key remove"}
	case "get":
		if len(parts) == 3 {
			return Command{Action: ActionGet, Key: parts[2]}, nil
		}
		return Command{}, &UsageError{Text: "Usage: /config get <key>"}
	case "set":
		if len(parts) >= 4 {
			return Command{Action: ActionSet, Key: parts[2], Value: strings.TrimSpace(strings.Join(parts[3:], " "))}, nil
		}
		return Command{}, &UsageError{Text: "Usage: /config set <key> <value>"}
	default:
		return Command{}, &UsageError{Text: fmt.Sprintf("Unknown /config action %q. Use: provider, model, keys, key remove, get, set", parts[1])}
	}
}

// NormalizeKey makes setting keys comparable across CLI spelling variants.
func NormalizeKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, "-", "")
	return strings.ReplaceAll(key, "_", "")
}

// UpdatedMessage formats the standard acknowledgement for /config set.
func UpdatedMessage(key, value string) string {
	return fmt.Sprintf("Updated %s = %s", key, value)
}
