package cmd

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
)

func TestChatScrollbarVisible_WhenContentOverflows(t *testing.T) {
	m := chatModel{
		viewport:     viewportWithSize(80, 20),
		contentLines: 200,
		autoScroll:   true,
	}
	if !m.chatScrollbarVisible() {
		t.Fatal("expected scrollable when content exceeds viewport height")
	}
}

func TestChatScrollbarVisible_NotWhenContentFits(t *testing.T) {
	m := chatModel{
		viewport:     viewportWithSize(80, 20),
		contentLines: 10,
	}
	if m.chatScrollbarVisible() {
		t.Fatal("expected not scrollable when content fits")
	}
}

func TestChatViewportWidth_WithScrollbar(t *testing.T) {
	m := chatModel{
		viewport:     viewportWithSize(80, 20),
		contentLines: 200,
		width:        80,
	}
	if w := m.chatViewportWidth(80); w != 79 {
		t.Fatalf("expected width 79 when scrollbar visible, got %d", w)
	}
}

func TestChatViewportWidth_NoScrollbar(t *testing.T) {
	m := chatModel{
		viewport:     viewportWithSize(80, 20),
		contentLines: 10,
		width:        80,
	}
	if w := m.chatViewportWidth(80); w != 80 {
		t.Fatalf("expected full width 80 when scrollbar hidden, got %d", w)
	}
}

func TestChatViewportWidth_NarrowTerminalClampsWithoutJump(t *testing.T) {
	m := chatModel{viewport: viewportWithSize(8, 4), contentLines: 100}
	if got := m.chatViewportWidth(8); got != 7 {
		t.Fatalf("narrow overflowing viewport width = %d, want 7", got)
	}
	if got := m.chatViewportWidth(1); got != 1 {
		t.Fatalf("single-column viewport width = %d, want 1", got)
	}
}

func TestRenderChatPaneUsesFullWidthWithoutSessionSidebar(t *testing.T) {
	m := chatModel{
		viewport:     viewportWithSize(120, 4),
		contentLines: 2,
		width:        120,
	}
	m.viewport.SetContent("hello\nworld")

	got := m.renderChatPane()
	if strings.Contains(got, "SESSION") || strings.Contains(got, "CONTEXT") {
		t.Fatalf("chat pane still renders the removed session sidebar: %q", got)
	}
	if !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Fatalf("chat pane lost conversation content: %q", got)
	}
}

func TestRenderScrollbar_TopAndBottom(t *testing.T) {
	m := chatModel{
		viewport:     viewportWithSize(80, 10),
		contentLines: 100,
	}
	m.viewport.SetContent(strings.Repeat("line\n", 100))
	m.viewport.SetYOffset(0)
	sb := m.renderScrollbar()
	lines := strings.Split(sb, "\n")
	if len(lines) != 10 {
		t.Fatalf("expected 10 scrollbar rows, got %d", len(lines))
	}
	if !strings.Contains(lines[0], scrollbarThumbGlyph) {
		t.Fatalf("expected thumb at top row when YOffset=0, got %q", lines[0])
	}

	m.viewport.SetYOffset(90) // max offset
	sb = m.renderScrollbar()
	lines = strings.Split(sb, "\n")
	if !strings.Contains(lines[9], scrollbarThumbGlyph) {
		t.Fatalf("expected thumb at bottom row when YOffset=max, got %q", lines[9])
	}
}

func TestRenderScrollbar_SlightOverflowUsesLargeThumb(t *testing.T) {
	m := chatModel{
		viewport:     viewportWithSize(80, 10),
		contentLines: 11,
	}
	sb := m.renderScrollbar()
	lines := strings.Split(sb, "\n")
	thumbRows := 0
	for _, line := range lines {
		if strings.Contains(line, scrollbarThumbGlyph) {
			thumbRows++
		}
	}
	if thumbRows < 8 {
		t.Fatalf("expected large thumb for slight overflow, got %d thumb rows", thumbRows)
	}
}

func TestRenderScrollbar_UsesTrackOutsideThumb(t *testing.T) {
	m := chatModel{viewport: viewportWithSize(80, 10), contentLines: 100}
	lines := strings.Split(m.renderScrollbar(), "\n")
	if !strings.Contains(lines[5], scrollbarTrackGlyph) {
		t.Fatalf("expected track glyph outside thumb, got %q", lines[5])
	}
}

func TestRenderChatPane_PaddedWidth(t *testing.T) {
	m := chatModel{
		viewport:     viewportWithSize(19, 4),
		contentLines: 100,
		width:        20,
	}
	m.viewport.SetContent("line 1\nshort\nlonger line here\nend")
	pane := m.renderChatPane()
	lines := strings.Split(pane, "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines in chat pane, got %d", len(lines))
	}
	// With width 20 and 1 column scrollbar, viewport width is 19.
	// Each line should be padded to width 19 plus 1 column scrollbar = 20 visual width.
	for i, line := range lines {
		if w := visibleWidth(line); w != 20 {
			t.Errorf("line %d has visual width %d, want 20: %q", i, w, line)
		}
	}
}

func viewportWithSize(width, height int) viewport.Model {
	return viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))
}
