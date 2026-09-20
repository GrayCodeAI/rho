package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteExportCreatesPrivateFile(t *testing.T) {
	path, err := WriteExport(filepath.Join(t.TempDir(), "exports"), "session-1", "md", []byte("hello"))
	if err != nil {
		t.Fatalf("WriteExport() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "hello" {
		t.Fatalf("export contents = %q, error = %v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("export permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestRenderJSONUsesVisibleTranscript(t *testing.T) {
	data, err := Render(ExportRequest{
		ID: "session-1", Model: "model", Provider: "provider", Format: "json",
		DisplayMessages: []DisplayMessage{
			{Role: "welcome", Content: "hidden"},
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "world"},
		},
	})
	if err != nil {
		t.Fatalf("Render JSON: %v", err)
	}
	var got struct {
		MessageCount int `json:"message_count"`
		Messages     []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if got.MessageCount != 2 || len(got.Messages) != 2 || got.Messages[0].Role != "user" {
		t.Fatalf("unexpected export: %#v", got)
	}
}

func TestRenderTextPreservesRoleLabels(t *testing.T) {
	data, err := Render(ExportRequest{
		ID: "session-1", Model: "model", Provider: "provider", Format: "txt",
		DisplayMessages: []DisplayMessage{{Role: "user", Content: "hello"}, {Role: "tool_result", Content: "done"}},
	})
	if err != nil {
		t.Fatalf("Render text: %v", err)
	}
	text := string(data)
	for _, want := range []string{"Rho Session: session-1", "[User]", "hello", "[Tool Result]", "done"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text export missing %q: %q", want, text)
		}
	}
}
