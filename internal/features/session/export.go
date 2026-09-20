package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	store "github.com/GrayCodeAI/rho/internal/session"
	"github.com/GrayCodeAI/rho/internal/types"
)

// DisplayMessage is the presentation-neutral transcript shape used by the
// chat export formats that intentionally omit structured tool metadata.
type DisplayMessage struct {
	Role    string
	Content string
}

// ExportRequest describes a transcript without coupling the feature to a
// Bubble Tea model. RuntimeMessages preserve structured tool data for Markdown;
// DisplayMessages preserve the visible TUI transcript for JSON and text.
type ExportRequest struct {
	ID              string
	Model           string
	Provider        string
	RuntimeMessages []types.FluxMessage
	DisplayMessages []DisplayMessage
	Format          string
	Redact          bool
}

// WriteExport owns private export-directory and file-permission policy. The
// directory is injected so tests and frontends do not depend on global state.
func WriteExport(dir, id, ext string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	_ = os.Chmod(dir, 0o700) // #nosec G302 -- exports are private session data
	path := filepath.Join(dir, id+"."+ext)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// Render serializes a transcript as Markdown, JSON, or plain text.
func Render(req ExportRequest) ([]byte, error) {
	switch strings.ToLower(req.Format) {
	case "json":
		return renderJSON(req)
	case "txt", "text":
		return []byte(renderText(req)), nil
	default:
		return renderMarkdown(req)
	}
}

func renderMarkdown(req ExportRequest) ([]byte, error) {
	messages := store.FromRuntimeMessages(req.RuntimeMessages)
	if len(messages) == 0 {
		messages = make([]store.Message, 0, len(req.DisplayMessages))
		for _, msg := range req.DisplayMessages {
			if msg.Role == "user" || msg.Role == "assistant" || msg.Role == "system" {
				messages = append(messages, store.Message{Role: msg.Role, Content: msg.Content})
			}
		}
	}
	now := time.Now()
	return store.Export(&store.Session{
		ID: req.ID, Model: req.Model, Provider: req.Provider,
		Messages: messages, CreatedAt: now, UpdatedAt: now,
	}, "md", req.Redact)
}

func renderJSON(req ExportRequest) ([]byte, error) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type export struct {
		SessionID    string    `json:"session_id"`
		Model        string    `json:"model,omitempty"`
		Provider     string    `json:"provider,omitempty"`
		ExportedAt   string    `json:"exported_at"`
		MessageCount int       `json:"message_count"`
		Messages     []message `json:"messages"`
	}
	messages := make([]message, 0, len(req.DisplayMessages))
	for _, msg := range req.DisplayMessages {
		if msg.Role == "welcome" || msg.Content == "" {
			continue
		}
		messages = append(messages, message(msg))
	}
	return json.MarshalIndent(export{
		SessionID: req.ID, Model: req.Model, Provider: req.Provider,
		ExportedAt: time.Now().Format(time.RFC3339), MessageCount: len(messages), Messages: messages,
	}, "", "  ")
}

func renderText(req ExportRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Rho Session: %s\n", req.ID)
	fmt.Fprintf(&b, "Model: %s/%s\n", req.Provider, req.Model)
	fmt.Fprintf(&b, "Exported: %s\n\n", time.Now().Format(time.RFC3339))
	b.WriteString(strings.Repeat("=", 60) + "\n\n")
	for _, msg := range req.DisplayMessages {
		switch msg.Role {
		case "welcome":
			continue
		case "user":
			fmt.Fprintf(&b, "[User]\n%s\n\n", msg.Content)
		case "assistant":
			fmt.Fprintf(&b, "[Assistant]\n%s\n\n", msg.Content)
		case "system":
			fmt.Fprintf(&b, "[System]\n%s\n\n", msg.Content)
		case "error":
			fmt.Fprintf(&b, "[Error]\n%s\n\n", msg.Content)
		case "tool_use":
			fmt.Fprintf(&b, "[Tool: %s]\n\n", msg.Content)
		case "tool_result":
			fmt.Fprintf(&b, "[Tool Result]\n%s\n\n", msg.Content)
		default:
			if msg.Content != "" {
				b.WriteString(msg.Content + "\n\n")
			}
		}
	}
	return b.String()
}
