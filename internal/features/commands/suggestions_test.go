package commands

import "testing"

func TestSuggestionsIncludesDescriptionsAndAliases(t *testing.T) {
	got := Suggestions("/he", []string{"/help", "/history"}, map[string]string{"/help": "show help"}, map[string]string{"/h": "/help"})
	if len(got) != 1 || got[0] != "/help  show help" {
		t.Fatalf("Suggestions = %#v", got)
	}
	got = Suggestions("/h", []string{"/help"}, map[string]string{}, map[string]string{"/h": "/help"})
	if len(got) != 1 || got[0] != "/help" {
		t.Fatalf("alias Suggestions = %#v", got)
	}
}

func TestApplySuggestionResolvesAlias(t *testing.T) {
	if got := ApplySuggestion("/h → /help", map[string]string{"/h": "/help"}); got != "/help " {
		t.Fatalf("ApplySuggestion = %q", got)
	}
}

func TestSuggestTypo(t *testing.T) {
	if got := SuggestTypo("/hlp", []string{"/help", "/history"}); got != "/help" {
		t.Fatalf("SuggestTypo = %q", got)
	}
	if got := SuggestTypo("/something-else", []string{"/help"}); got != "" {
		t.Fatalf("SuggestTypo unrelated = %q", got)
	}
}
