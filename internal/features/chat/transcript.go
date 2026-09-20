// Package chat contains chat-domain policies that do not depend on Bubble Tea.
package chat

import (
	"strings"
)

// Message is the small transcript projection needed by chat policies. UI
// models may keep richer state, but the domain does not need to know about it.
type Message struct {
	Role    string
	Content string
}

// PlainTranscript renders messages for terminal selection and clipboard use.
// Presentation-only messages are intentionally omitted.
func PlainTranscript(messages []Message, partial string) string {
	var b strings.Builder
	for _, message := range messages {
		line, ok := PlainTranscriptLine(message)
		if !ok {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n\n")
	}
	if partial != "" {
		b.WriteString("rho: ")
		b.WriteString(partial)
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// PlainTranscriptLine returns a copyable line and whether the message should
// be included in a plain transcript.
func PlainTranscriptLine(message Message) (string, bool) {
	content := strings.TrimSpace(message.Content)
	if content == "" {
		return "", false
	}
	switch message.Role {
	case "welcome", "usage", "setup_complete":
		return "", false
	case "user":
		return "You: " + content, true
	case "assistant":
		return "rho: " + content, true
	case "error":
		return "Error: " + content, true
	case "system":
		return content, true
	case "thinking":
		return "thinking: " + content, true
	case "tool_use":
		return "tool: " + content, true
	case "tool_result":
		return content, true
	case "permission":
		return "permission: " + content, true
	case "question":
		return content, true
	default:
		return content, true
	}
}

// HadThinkingOnly reports whether the latest user turn ended with thinking
// messages but no assistant answer or tool activity.
func HadThinkingOnly(messages []Message) bool {
	if len(messages) == 0 {
		return false
	}
	var sawThinking, sawAssistant, sawTool bool
	for i := len(messages) - 1; i >= 0; i-- {
		switch messages[i].Role {
		case "user":
			return sawThinking && !sawAssistant && !sawTool
		case "thinking":
			sawThinking = true
		case "assistant":
			sawAssistant = true
		case "tool_use", "tool_result":
			sawTool = true
		}
	}
	return false
}

// StripCurrentTurnThinking removes thinking messages after the latest user
// message. It preserves all other transcript entries and ordering.
func StripCurrentTurnThinking(messages []Message) []Message {
	lastUser := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUser = i
			break
		}
	}
	if lastUser < 0 {
		return append([]Message(nil), messages...)
	}
	out := make([]Message, 0, len(messages))
	for i, message := range messages {
		if i > lastUser && message.Role == "thinking" {
			continue
		}
		out = append(out, message)
	}
	return out
}
