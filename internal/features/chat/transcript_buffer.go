package chat

import "fmt"

// TrimMessages bounds a display transcript while preserving a leading message
// with preserveRole (normally the welcome banner). It returns the trimmed
// transcript and the number of original messages removed.
func TrimMessages(messages []Message, threshold, maximum int, preserveRole string) ([]Message, int) {
	if len(messages) <= threshold {
		return messages, 0
	}
	remove := len(messages) - maximum
	if remove <= 0 {
		return messages, 0
	}
	start := 0
	if len(messages) > 0 && messages[0].Role == preserveRole {
		start = 1
	}
	if start+remove >= len(messages) {
		return messages, 0
	}
	trimmed := make([]Message, 0, len(messages)-remove+1)
	trimmed = append(trimmed, messages[:start]...)
	trimmed = append(trimmed, Message{
		Role:    "system",
		Content: fmt.Sprintf("... %d earlier messages trimmed (use /export to save full history)", remove),
	})
	trimmed = append(trimmed, messages[start+remove:]...)
	return trimmed, remove
}
