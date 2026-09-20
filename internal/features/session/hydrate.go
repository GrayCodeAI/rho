package session

import (
	store "github.com/GrayCodeAI/rho/internal/session"
	"github.com/GrayCodeAI/rho/internal/types"
)

// Hydrated contains both representations needed when resuming a session.
type Hydrated struct {
	Runtime []types.FluxMessage
	Display []DisplayMessage
}

// Hydrate converts one durable session once, keeping runtime and display
// projections consistent across TUI resume paths.
func Hydrate(saved *store.Session) Hydrated {
	if saved == nil {
		return Hydrated{}
	}
	result := Hydrated{Runtime: store.ToRuntimeMessages(saved.Messages)}
	for _, message := range saved.Messages {
		if message.Role == "user" || message.Role == "assistant" {
			result.Display = append(result.Display, DisplayMessage{Role: message.Role, Content: message.Content})
		}
	}
	return result
}
