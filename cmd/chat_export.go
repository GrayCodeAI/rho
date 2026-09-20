package cmd

import (
	"fmt"
	"path/filepath"

	sessionfeature "github.com/GrayCodeAI/rho/internal/features/session"
	"github.com/GrayCodeAI/rho/internal/storage"
)

func chatExportRequest(m *chatModel, format string, redact bool) (sessionfeature.ExportRequest, error) {
	if m == nil || m.session == nil {
		return sessionfeature.ExportRequest{}, fmt.Errorf("no active session")
	}
	display := make([]sessionfeature.DisplayMessage, 0, len(m.messages))
	for _, msg := range m.messages {
		display = append(display, sessionfeature.DisplayMessage{Role: msg.role, Content: msg.content})
	}
	return sessionfeature.ExportRequest{
		ID: m.sessionID, Model: m.session.Model(), Provider: m.session.Provider(),
		RuntimeMessages: m.session.RawMessages(), DisplayMessages: display,
		Format: format, Redact: redact,
	}, nil
}

func writeRedactedChatMarkdownExport(m *chatModel) (string, error) {
	req, err := chatExportRequest(m, "md", true)
	if err != nil {
		return "", err
	}
	data, err := sessionfeature.Render(req)
	if err != nil {
		return "", err
	}
	return sessionfeature.WriteExport(filepath.Join(storage.StateDir(), "exports"), req.ID, "md", data)
}

// exportSession exports the current session in the specified format (md, json, or txt).
func exportSession(m *chatModel, format string) (string, error) {
	if format == "" {
		format = "md"
	}
	req, err := chatExportRequest(m, format, format == "md")
	if err != nil {
		return "", err
	}
	data, err := sessionfeature.Render(req)
	if err != nil {
		return "", err
	}
	ext := format
	if ext == "text" {
		ext = "txt"
	}
	return sessionfeature.WriteExport(filepath.Join(storage.StateDir(), "exports"), req.ID, ext, data)
}
