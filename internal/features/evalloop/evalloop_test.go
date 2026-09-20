package evalloop

import (
	"context"
	"strings"
	"testing"

	"github.com/GrayCodeAI/rho/internal/engine"
	"github.com/GrayCodeAI/rho/internal/tool"
)

// TestSessionRuntimeSmoke drives the real engine agent loop end-to-end with a
// deterministic mock ChatClient (no external credentials), verifying the harness
// collects output, events, and a transcript snapshot. This is the CI smoke path
// for agent-runtime evaluation.
func TestSessionRuntimeSmoke(t *testing.T) {
	// Isolate cwd so session/storage wiring never touches the real workspace.
	t.Chdir(t.TempDir())

	client := engine.NewMockClientForTest()
	registry := tool.NewRegistry()
	cfg := DefaultConfig()
	cfg.MaxTurns = 2

	runtime := NewSessionRuntime(client, "eval", "mock-model", registry, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result, err := runtime.Run(ctx, ".", "summarize this repository")
	if err != nil {
		t.Fatalf("runtime run: %v", err)
	}

	if len(result.Events) == 0 {
		t.Fatal("expected at least one loop event")
	}
	if !strings.Contains(result.Output, "mock") {
		t.Fatalf("expected mock output, got %q", result.Output)
	}
	if len(result.Transcript) == 0 {
		t.Fatal("expected a transcript snapshot")
	}
	if result.Duration <= 0 {
		t.Fatal("expected a positive duration")
	}
}

// TestSessionRuntimeRequiresClient guards the nil-client invariant.
func TestSessionRuntimeRequiresClient(t *testing.T) {
	runtime := NewSessionRuntime(nil, "eval", "mock", nil, DefaultConfig())
	if _, err := runtime.Run(context.Background(), ".", "task"); err == nil {
		t.Fatal("nil client must error")
	}
}
