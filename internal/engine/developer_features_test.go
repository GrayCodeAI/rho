package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine/compact"
)

func TestBackgroundRunner_Delegate(t *testing.T) {
	br := NewBackgroundRunner()
	id := br.Delegate(context.Background(), "research Go generics", func(_ context.Context, prompt string) (string, error) {
		time.Sleep(50 * time.Millisecond)
		return "Generics were added in Go 1.18", nil
	})
	if id == "" {
		t.Fatal("expected non-empty ID")
	}
	if br.PendingCount() != 1 {
		t.Errorf("expected 1 pending, got %d", br.PendingCount())
	}

	// Wait for completion
	time.Sleep(100 * time.Millisecond)

	task := br.Collect(id)
	if task == nil {
		t.Fatal("expected completed task")
	}
	if task.Status != "done" {
		t.Errorf("status = %q, want done", task.Status)
	}
	if task.Result == "" {
		t.Error("expected non-empty result")
	}
}

func TestBackgroundRunner_CollectWhileRunning(t *testing.T) {
	br := NewBackgroundRunner()
	br.Delegate(context.Background(), "slow task", func(_ context.Context, _ string) (string, error) {
		time.Sleep(500 * time.Millisecond)
		return "done", nil
	})
	// Collect immediately — should return nil (still running)
	task := br.Collect("bg-1")
	if task != nil {
		t.Error("expected nil for running task")
	}
}

func TestCompactionTrigger(t *testing.T) {
	ct := compact.NewCompactionTrigger(100000)
	if ct.ShouldCompact(50000) {
		t.Error("50% should not trigger at 75% threshold")
	}
	if !ct.ShouldCompact(80000) {
		t.Error("80% should trigger at 75% threshold")
	}
	ct.MarkCompacted()
	if ct.ShouldCompact(80000) {
		t.Error("should not trigger immediately after compaction (min interval)")
	}
}

func TestLargeResponseHandler_Small(t *testing.T) {
	h := NewLargeResponseHandler()
	cr := h.Process("short content")
	if cr.TotalPages != 1 {
		t.Errorf("expected 1 page, got %d", cr.TotalPages)
	}
}

func TestLargeResponseHandler_Large(t *testing.T) {
	h := &LargeResponseHandler{MaxChunkSize: 100, OverlapLines: 1}
	// Generate content larger than 100 chars
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, "This is a line of content that takes up space in the output buffer.")
	}
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	cr := h.Process(content)
	if cr.TotalPages <= 1 {
		t.Errorf("expected multiple pages, got %d", cr.TotalPages)
	}
	page1 := cr.FormatPage(1)
	if page1 == "" {
		t.Error("expected non-empty page 1")
	}
	if !hasSubstr(page1, "[Page 1/") {
		t.Error("expected page header")
	}
}

func hasSubstr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || strings.Contains(s, sub))
}
