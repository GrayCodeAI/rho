package cmd

import (
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/viewport"
)

func TestRenderDisplayMessage_ErrorLabelIsCapitalized(t *testing.T) {
	got := renderDisplayMessage(
		displayMsg{role: "error", content: "Rate limited by the API provider."},
		0,
		nil,
		80,
		nil,
	)
	if !strings.Contains(got, "Error: Rate limited by the API provider.") {
		t.Fatalf("renderDisplayMessage() = %q, want capitalized Error label", got)
	}
	if strings.Contains(got, "error: Rate limited by the API provider.") {
		t.Fatalf("renderDisplayMessage() = %q, contains lowercase error label", got)
	}
}

func TestRenderDisplayMessage_QuestionUsesPromptCard(t *testing.T) {
	got := renderDisplayMessage(
		displayMsg{role: "question", content: "Continue?", timeoutAt: time.Now().Add(time.Minute)},
		0,
		nil,
		80,
		nil,
	)
	if !strings.Contains(got, "Input required") || !strings.Contains(got, "Enter") {
		t.Fatalf("question prompt was not rendered as an actionable card: %q", got)
	}
}

func TestAssembleViewportContent_IncrementalMatchesFullRebuild(t *testing.T) {
	msgs := []displayMsg{
		{role: "user", content: "hello"},
		{role: "assistant", content: "hi **there**"},
		{role: "tool_use", content: "Read"},
		{role: "tool_result", content: "[Read] file contents"},
	}

	m := &chatModel{messages: msgs, width: 80}
	got := m.assembleViewportContent(80)

	fresh := &chatModel{messages: msgs, width: 80}
	want := fresh.assembleViewportContent(80)
	if got != want {
		t.Fatal("initial assemble should be deterministic")
	}

	// Append a message — should match a full rebuild.
	m.messages = append(m.messages, displayMsg{role: "user", content: "follow up"})
	incremental := m.assembleViewportContent(80)

	fresh = &chatModel{messages: m.messages, width: 80}
	full := fresh.assembleViewportContent(80)
	if incremental != full {
		t.Fatal("incremental append should match full rebuild")
	}
	if m.vpRenderedMsgs != len(m.messages) {
		t.Fatalf("expected all messages cached, got %d want %d", m.vpRenderedMsgs, len(m.messages))
	}
}

func TestAssembleViewportContent_ThinkingInPlaceUpdate(t *testing.T) {
	m := &chatModel{
		messages: []displayMsg{{role: "thinking", content: "part"}},
		width:    80,
	}
	first := m.assembleViewportContent(80)

	m.messages[0].content = "part two"
	second := m.assembleViewportContent(80)
	if first == second {
		t.Fatal("thinking growth should change rendered output")
	}

	fresh := &chatModel{messages: m.messages, width: 80}
	full := fresh.assembleViewportContent(80)
	if second != full {
		t.Fatal("in-place thinking update should match full rebuild")
	}
}

func TestAssembleViewportContent_StreamTailNotCached(t *testing.T) {
	m := &chatModel{
		messages: []displayMsg{{role: "user", content: "go"}},
		waiting:  true,
		partial:  &strings.Builder{},
		width:    80,
	}
	m.partial.WriteString("streaming")

	stable := m.assembleViewportContent(80)
	stablePrefix := m.vpStableContent

	m.partial.WriteString(" more")
	withMore := m.assembleViewportContent(80)

	if m.vpStableContent != stablePrefix {
		t.Fatal("stream tail changes should not mutate stable cache")
	}
	if withMore == stable {
		t.Fatal("stream tail growth should change assembled output")
	}
	if !strings.Contains(withMore, "streaming more") {
		t.Fatal("expected updated partial in output")
	}
}

func TestAssembleViewportContent_WidthChangeRebuilds(t *testing.T) {
	m := &chatModel{
		messages: []displayMsg{{role: "user", content: "hello world"}},
		width:    80,
	}
	_ = m.assembleViewportContent(80)
	if m.vpRenderWidth != 80 {
		t.Fatalf("expected width 80 cached, got %d", m.vpRenderWidth)
	}

	_ = m.assembleViewportContent(60)
	if m.vpRenderWidth != 60 {
		t.Fatalf("expected width 60 after resize, got %d", m.vpRenderWidth)
	}
}

func TestUpdateViewportContent_RewrapsAfterScrollbarGutterAppears(t *testing.T) {
	content, fullWidth, narrowWidth, fullWidthLines, narrowWidthLines := findWidthSensitiveViewportScenario(t)
	viewportHeight := fullWidthLines - 1
	if viewportHeight < 1 {
		viewportHeight = 1
	}

	m := &chatModel{
		messages: []displayMsg{{role: "system", content: content}},
		viewport: viewport.New(viewport.WithWidth(fullWidth), viewport.WithHeight(viewportHeight)),
		width:    fullWidth,
	}
	m.viewDirty = true

	m.updateViewportContent()

	if !m.chatHasOverflow() {
		t.Fatalf(
			"expected overflow after re-wrapping for scrollbar width (viewportHeight=%d fullWidthLines=%d narrowWidthLines=%d contentLines=%d viewportWidth=%d)",
			viewportHeight,
			fullWidthLines,
			narrowWidthLines,
			m.contentLines,
			m.viewport.Width(),
		)
	}
	if !m.chatScrollbarVisible() {
		t.Fatal("expected scrollbar to become visible after re-wrap")
	}
	if m.viewport.Width() != narrowWidth {
		t.Fatalf("expected viewport width %d with scrollbar gutter, got %d", narrowWidth, m.viewport.Width())
	}
	if m.contentLines != narrowWidthLines {
		t.Fatalf("expected contentLines %d after narrow re-wrap, got %d", narrowWidthLines, m.contentLines)
	}
}

func findWidthSensitiveViewportScenario(t *testing.T) (string, int, int, int, int) {
	t.Helper()

	patterns := []string{
		"alpha ",
		"alpha beta ",
		"alpha beta gamma ",
		"one two three four five ",
		"short mediumlength extralongword ",
		"supercalifragilisticexpialidocious",
		"0123456789012345678901234567890123456789",
	}

	for fullWidth := 21; fullWidth <= 60; fullWidth++ {
		narrowWidth := fullWidth - 1
		for _, pattern := range patterns {
			for n := 4; n < 160; n++ {
				content := strings.TrimSpace(strings.Repeat(pattern, n))

				fullModel := &chatModel{messages: []displayMsg{{role: "system", content: content}}}
				fullLines := renderedLineCount(fullModel.assembleViewportContent(fullWidth))

				narrowModel := &chatModel{messages: []displayMsg{{role: "system", content: content}}}
				narrowLines := renderedLineCount(narrowModel.assembleViewportContent(narrowWidth))

				if narrowLines > fullLines {
					return content, fullWidth, narrowWidth, fullLines, narrowLines
				}
			}
		}
	}

	t.Fatal("failed to find width-sensitive content for viewport render test")
	return "", 0, 0, 0, 0
}

func TestRenderStreamTail_IncrementalMatchesFullRender(t *testing.T) {
	full := "# Title\n\nFirst paragraph with **bold** text.\n\n" +
		"- item one\n- item two\n\n" +
		"```go\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```\n\n" +
		"> a quote line\n\nSecond paragraph after a fence.\n\n\nTrailing text"
	chunks := []int{3, 9, 20, 41, 55, 70, 90, 120, 150, len(full)}

	m := &chatModel{width: 100, partial: &strings.Builder{}}
	for _, end := range chunks {
		if end > len(full) {
			end = len(full)
		}
		m.partial.Reset()
		m.partial.WriteString(full[:end])

		got := m.renderStreamTail(100)
		raw := strings.TrimLeft(full[:end], "\n\r")
		// Compare against a single-pass render of the same partial: force
		// "prefix == whole raw" so the fresh model renders without splitting.
		fresh := &chatModel{width: 100, partial: &strings.Builder{}}
		fresh.partial.WriteString(full[:end])
		fresh.streamMDPrefixRaw = raw
		fresh.streamMDPrefixOut = renderMarkdown(sanitizeIdentity(raw), 97)
		fresh.streamMDWidth = 100
		want := fresh.renderStreamTail(100)
		if got != want {
			t.Fatalf("incremental render diverged at %d bytes:\n got: %q\nwant: %q", end, got, want)
		}
	}
}

func TestStreamStableBoundary_NeverInsideFence(t *testing.T) {
	raw := "before\n\n```txt\ntext with\n\nblank line inside fence\n\nmore\n```\nafter"
	b := streamStableBoundary(raw, 0)
	if want := len("before\n\n"); b != want {
		t.Fatalf("boundary = %d, want %d (must not split inside code fence)", b, want)
	}
	if b > 0 && strings.Count(raw[:b], "```")%2 != 0 {
		t.Fatal("boundary leaves an unbalanced fence in the prefix")
	}
}

func TestTrailingNewlines(t *testing.T) {
	tests := []struct{ in, want string }{
		{"abc", ""},
		{"abc\n\n", "\n\n"},
		{"abc\n \n\n", "\n\n\n"},
		{"abc\n\t\n", "\n\n"},
		{"\n\n", "\n\n"},
	}
	for _, tt := range tests {
		if got := trailingNewlines(tt.in); got != tt.want {
			t.Errorf("trailingNewlines(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// longStreamDoc builds a realistic multi-block markdown response for
// benchmarking the streaming render path.
func longStreamDoc() string {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("## Section heading number ")
		b.WriteString(strings.Repeat("x", 3))
		b.WriteString("\n\nThis is a paragraph of prose with some **bold** and `code` spans ")
		b.WriteString("that wraps across the viewport width to exercise the wrapper.\n\n")
		b.WriteString("- first bullet item in the list\n- second bullet item\n- third\n\n")
		b.WriteString("```go\nfunc example() {\n\tfmt.Println(\"block\")\n}\n```\n\n")
	}
	return b.String()
}

// BenchmarkRenderStreamTail_Cached measures the incremental (cached) streaming
// render across the full lifetime of a long response, one render per tick.
func BenchmarkRenderStreamTail_Cached(b *testing.B) {
	doc := longStreamDoc()
	steps := 50
	for n := 0; n < b.N; n++ {
		m := &chatModel{width: 100, partial: &strings.Builder{}}
		for s := 1; s <= steps; s++ {
			end := len(doc) * s / steps
			m.partial.Reset()
			m.partial.WriteString(doc[:end])
			_ = m.renderStreamTail(100)
		}
	}
}

// BenchmarkRenderStreamTail_Naive reproduces the pre-cache behavior:
// re-render the entire accumulated partial every tick. Kept as a baseline
// to quantify the incremental cache's win.
func BenchmarkRenderStreamTail_Naive(b *testing.B) {
	doc := longStreamDoc()
	steps := 50
	for n := 0; n < b.N; n++ {
		for s := 1; s <= steps; s++ {
			end := len(doc) * s / steps
			raw := strings.TrimLeft(doc[:end], "\n\r")
			_ = renderMarkdown(sanitizeIdentity(raw), 97)
		}
	}
}
