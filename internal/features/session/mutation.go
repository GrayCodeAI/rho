package session

import "github.com/GrayCodeAI/rho/internal/types"

// RemoveLastExchanges removes up to count user/assistant exchanges using the
// same role-aware rule as the live engine session. Tool messages are skipped;
// truncation occurs at the point where the pair is found. The returned count
// is the number of complete exchanges removed.
func RemoveLastExchanges(messages []types.FluxMessage, count int) ([]types.FluxMessage, int) {
	if count <= 0 {
		return messages, 0
	}
	out := messages
	removedExchanges := 0
	for removedExchanges < count && len(out) >= 2 {
		removed := 0
		for i := len(out) - 1; i >= 0 && removed < 2; i-- {
			role := out[i].Role
			if role != "user" && role != "assistant" {
				continue
			}
			removed++
			out = out[:i]
		}
		if removed < 2 {
			return messages, removedExchanges
		}
		removedExchanges++
	}
	return out, removedExchanges
}
