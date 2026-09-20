package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine/safety"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/GrayCodeAI/rho/internal/engine"
)

func TestRenderStatusBar_SignatureExists(t *testing.T) {
	var _ func(*chatModel, int) []string = renderStatusBar
}

func TestRenderStatusBar_TwoLineAtWidth120(t *testing.T) {
	m := &chatModel{session: engine.NewSession("", "test-model", "system", nil)}
	m.session.PermSvc().SetDryRun(true)
	m.session.CostValue().PromptTokens = 1000
	m.session.CostValue().CompletionTokens = 500
	m.sessionStartedAt = time.Now().Add(-5 * time.Minute)
	lines := renderStatusBar(m, 120)
	if len(lines) < 2 {
		t.Fatalf("expected 2 lines at width 120, got %d", len(lines))
	}
}

func TestRenderStatusBar_SingleLineAtWidth80(t *testing.T) {
	m := &chatModel{session: &engine.Session{}}
	m.session.CostValue().PromptTokens = 1000
	m.session.CostValue().CompletionTokens = 500
	lines := renderStatusBar(m, 80)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line at width 80, got %d", len(lines))
	}
}

func TestRenderStatusBarSecondaryRight_OmitsDuplicatedAutonomy(t *testing.T) {
	sess := engine.NewSession("", "test-model", "system", nil)
	sess.PermSvc().SetAutonomy(safety.AutonomyBasic)

	if got := renderStatusBarSecondaryRight(&chatModel{session: sess}); strings.Contains(got, "Scout") {
		t.Fatalf("secondary status = %q, want autonomy shown only in top footer", got)
	}
}

func TestRenderStatusBarSecondaryRight_OmitsDuplicatedCost(t *testing.T) {
	sess := engine.NewSession("", "test-model", "system", nil)
	sess.CostValue().TotalCostUSD = 1.25

	if got := renderStatusBarSecondaryRight(&chatModel{session: sess}); strings.Contains(got, "$1.250") {
		t.Fatalf("secondary status = %q, want cost shown only in primary footer", got)
	}
}

func TestRenderStatusBar_HidesModelSessionAndShortcut(t *testing.T) {
	m := &chatModel{
		session:          engine.NewSession("", "test-model", "system", nil),
		sessionID:        "cc6f7764abcdef",
		statusLeftVal:    "~/repo",
		statusLeftBranch: "main",
	}

	for _, width := range []int{80, 120} {
		got := strings.Join(renderStatusBar(m, width), "\n")
		for _, hidden := range []string{"test-model", "#cc6f7764", "ctrl+K"} {
			if strings.Contains(got, hidden) {
				t.Errorf("width %d status = %q, want %q hidden", width, got, hidden)
			}
		}
	}
}

func TestRenderStatusBarRight_IncludesTokensLabel(t *testing.T) {
	m := &chatModel{session: &engine.Session{}}
	m.session.CostValue().PromptTokens = 1200
	m.session.CostValue().CompletionTokens = 300
	got := renderStatusBarRight(m)
	if !strings.Contains(got, "tokens") {
		t.Fatalf("expected tokens label in footer right, got %q", got)
	}
	if !strings.Contains(got, "[db]") {
		t.Fatalf("expected bullet prefix, got %q", got)
	}
}

func TestRenderStatusBarRight_OmitsDuplicatedContext(t *testing.T) {
	session := engine.NewSession("", "test-model", "system", nil)
	session.RecordAPIUsage(36_000, 100)
	session.SetContextWindowCached(262_000)

	got := renderStatusBarPrimaryRight(&chatModel{session: session})
	if strings.Contains(got, "ctx") {
		t.Fatalf("status footer = %q, want context shown only in the connection row", got)
	}
}

func TestRenderStatusBarPrimaryLeft_UsesCachedState(t *testing.T) {
	m := &chatModel{statusLeftVal: "~/repo", statusLeftBranch: "main"}
	got := renderStatusBarPrimaryLeft(m)
	if !strings.Contains(got, "~/repo") {
		t.Fatalf("status left = %q, want cached cwd", got)
	}
	if !strings.Contains(got, "main") {
		t.Fatalf("status left = %q, want cached branch", got)
	}
}

func TestShortenHomePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got := shortenHomePath(home + "/project/rho")
	if got != "~/project/rho" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatSessionDuration(t *testing.T) {
	tests := []struct {
		d        time.Duration
		expected string
	}{
		{30 * time.Second, "30s"},
		{3*time.Minute + 2*time.Second, "3m 2s"},
		{90 * time.Minute, "1h 30m"},
	}
	for _, tt := range tests {
		if got := formatSessionDuration(tt.d); got != tt.expected {
			t.Errorf("duration %v: got %q want %q", tt.d, got, tt.expected)
		}
	}
}

func TestFormatTokenCountWithCommas(t *testing.T) {
	if got := formatTokenCountWithCommas(14442); got != "14,442 tokens" {
		t.Fatalf("got %q", got)
	}
}

func TestLayoutFooterRow_NoWrap(t *testing.T) {
	left := "left"
	right := "right"
	result := layoutFooterRow(left, right, 20)
	if lipgloss.Width(result) > 20 {
		t.Fatalf("expected visual width <= 20, got %q", result)
	}
	if !strings.HasPrefix(result, left) || !strings.HasSuffix(result, right) {
		t.Fatalf("unexpected layout: %q", result)
	}
}

func TestLayoutFooterRow_TruncatesOverflow(t *testing.T) {
	long := strings.Repeat("x", 80)
	result := layoutFooterRow("ok", long, 40)
	if lipgloss.Width(result) > 40 {
		t.Fatalf("footer row overflow: width=%d", lipgloss.Width(result))
	}
}
