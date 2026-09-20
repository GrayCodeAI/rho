package session

import (
	"testing"

	store "github.com/GrayCodeAI/rho/internal/session"
)

func TestFilterEntriesRanksIDPrefixFirst(t *testing.T) {
	entries := []store.Entry{
		{ID: "older", Preview: "target"},
		{ID: "target-session", Preview: "other"},
		{ID: "newest", Preview: "target"},
	}
	got := FilterEntries(entries, "target")
	if len(got) != 3 || got[0].ID != "target-session" {
		t.Fatalf("FilterEntries = %#v", got)
	}
}

func TestFilterEntriesEmptyQueryPreservesOrderAndCopies(t *testing.T) {
	entries := []store.Entry{{ID: "one"}, {ID: "two"}}
	got := FilterEntries(entries, "")
	if len(got) != 2 || got[0].ID != "one" || &got[0] == &entries[0] {
		t.Fatalf("FilterEntries empty = %#v", got)
	}
}
