package cmd

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

func runMouseScrollSplitPanePass(t *testing.T, pass int) {
	t.Helper()

	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(14))
	vp.SetContent(strings.Repeat("line\n", 40))
	vp.SetYOffset(5)

	ta := textarea.New()
	ta.SetHeight(1)
	m := chatModel{
		viewport: vp,
		input:    ta,
		height:   24,
		width:    80,
		uiFocus:  focusPrompt,
	}
	m = m.syncViewportMouseWheel().withSyncedLayout()
	before := m.viewport.YOffset()

	wheelChat := tea.MouseWheelMsg{
		X:      40,
		Y:      m.chatPaneTopY(),
		Button: tea.MouseWheelDown,
	}
	next, _ := m.Update(wheelChat)
	m = next.(chatModel)
	if m.viewport.YOffset() <= before {
		t.Fatalf("pass %d: wheel over chat should scroll viewport (before=%d after=%d)", pass, before, m.viewport.YOffset())
	}

	m.viewport.SetYOffset(before)
	wheelInput := tea.MouseWheelMsg{
		X:      40,
		Y:      m.bottomBarTopY(),
		Button: tea.MouseWheelDown,
	}
	next, _ = m.Update(wheelInput)
	m = next.(chatModel)
	if m.viewport.YOffset() != before {
		t.Fatalf("pass %d: wheel over input must not scroll chat (before=%d after=%d)", pass, before, m.viewport.YOffset())
	}
	if !m.input.Focused() {
		t.Fatalf("pass %d: input must stay focused after mouse wheel so typing still works", pass)
	}

	up := tea.KeyPressMsg{Code: tea.KeyUp}
	m.history.SetEntries([]string{"first", "second"})
	m.input.SetValue("")
	if m.routeKeyToViewport(up) {
		t.Fatalf("pass %d: up in prompt focus should not route to viewport", pass)
	}
	next, cmd := m.Update(up)
	if cmd != nil {
		next, _ = next.(chatModel).Update(cmd())
	}
	m = next.(chatModel)
	if m.input.Value() != "second" {
		t.Fatalf("pass %d: up should navigate input history, got %q", pass, m.input.Value())
	}
	if m.viewport.YOffset() != before {
		t.Fatalf("pass %d: up in prompt focus must not scroll chat", pass)
	}
}

func TestUpdate_MouseMotionDoesNotReflowLayout(t *testing.T) {
	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(14))
	vp.SetContent(strings.Repeat("line\n", 40))
	m := chatModel{
		viewport:             vp,
		input:                textarea.New(),
		height:               24,
		width:                80,
		uiFocus:              focusPrompt,
		cachedBottomBarLines: 10,
		layoutKey:            65536,
	}
	before := m.viewport.Height()

	motion := tea.MouseMotionMsg{Y: 8, X: 10}
	next, _ := m.Update(motion)
	m = next.(chatModel)
	if m.viewport.Height() != before {
		t.Fatal("mouse motion should not trigger layout reflow")
	}
	if m.lastMouseY != 8 {
		t.Fatalf("motion should track pointer row, got %d", m.lastMouseY)
	}
}

func TestUpdate_MouseWheelSplitPane(t *testing.T) {
	runMouseScrollSplitPanePass(t, 1)
	runMouseScrollSplitPanePass(t, 2)
}

func TestUpdate_InputHistoryWhileWaiting(t *testing.T) {
	m := chatModel{
		viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(14)),
		input:    textarea.New(),
		height:   24,
		width:    80,
		uiFocus:  focusPrompt,
		waiting:  true,
	}
	m.history.SetEntries([]string{"first", "second"})
	m = m.withSyncedLayout()

	up := tea.KeyPressMsg{Code: tea.KeyUp}
	next, cmd := m.Update(up)
	if cmd != nil {
		next, _ = next.Update(cmd())
	}
	m = next.(chatModel)
	if m.input.Value() != "second" {
		t.Fatalf("up while waiting should navigate history, got %q", m.input.Value())
	}

	down := tea.KeyPressMsg{Code: tea.KeyDown}
	next, cmd = m.Update(down)
	if cmd != nil {
		next, _ = next.Update(cmd())
	}
	m = next.(chatModel)
	if m.input.Value() != "" {
		t.Fatalf("down while waiting should restore empty draft, got %q", m.input.Value())
	}
}
