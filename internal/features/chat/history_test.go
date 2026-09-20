package chat

import "testing"

func TestHistoryBoundsAndNavigation(t *testing.T) {
	h := NewHistory([]string{"one", "two"}, 2)
	if got, ok := h.Up("draft"); !ok || got != "two" {
		t.Fatalf("Up() = %q, %v", got, ok)
	}
	if got, ok := h.Up("ignored"); !ok || got != "one" {
		t.Fatalf("second Up() = %q, %v", got, ok)
	}
	if got, ok := h.Down(); !ok || got != "two" {
		t.Fatalf("Down() = %q, %v", got, ok)
	}
	if got, ok := h.Down(); !ok || got != "draft" {
		t.Fatalf("draft Down() = %q, %v", got, ok)
	}
	h.Add("three")
	if got := h.Entries(); len(got) != 2 || got[0] != "two" || got[1] != "three" {
		t.Fatalf("bounded entries = %#v", got)
	}
}

func TestHistoryEntriesAreCopied(t *testing.T) {
	h := NewHistory([]string{"one"}, 2)
	entries := h.Entries()
	entries[0] = "changed"
	if got, _ := h.Last(); got != "one" {
		t.Fatalf("history was mutated through Entries(): %q", got)
	}
}
