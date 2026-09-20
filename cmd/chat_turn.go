package cmd

import chatfeature "github.com/GrayCodeAI/rho/internal/features/chat"

// turnHadThinkingOnly reports whether the latest user turn ended with internal
// reasoning visible but no assistant reply or tool activity. This is the TUI
// symptom of flux's ResponseErrorOnlyReasoning health check.
func turnHadThinkingOnly(messages []displayMsg) bool {
	return chatfeature.HadThinkingOnly(toFeatureMessages(messages))
}

// stripCurrentTurnThinking removes thinking messages from the latest user turn.
// Used when the engine retries after a reasoning-only response.
func stripCurrentTurnThinking(messages []displayMsg) []displayMsg {
	clean := chatfeature.StripCurrentTurnThinking(toFeatureMessages(messages))
	out := make([]displayMsg, 0, len(clean))
	for _, message := range clean {
		out = append(out, displayMsg{role: message.Role, content: message.Content})
	}
	return out
}

func toFeatureMessages(messages []displayMsg) []chatfeature.Message {
	projected := make([]chatfeature.Message, 0, len(messages))
	for _, message := range messages {
		projected = append(projected, chatfeature.Message{Role: message.role, Content: message.content})
	}
	return projected
}
