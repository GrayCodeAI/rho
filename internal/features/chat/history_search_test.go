package chat

import (
	"reflect"
	"testing"
)

func TestSearchHistoryRanksQualityThenRecency(t *testing.T) {
	entries := []string{"git status", "fix parser", "git commit"}
	want := []string{"git commit", "git status"}
	if got := SearchHistory(entries, "git"); !reflect.DeepEqual(got, want) {
		t.Fatalf("SearchHistory() = %#v, want %#v", got, want)
	}
}

func TestSearchHistorySupportsSubsequenceAndReverseEmptyQuery(t *testing.T) {
	entries := []string{"alpha", "build api", "beta"}
	if got := SearchHistory(entries, "bai"); !reflect.DeepEqual(got, []string{"build api"}) {
		t.Fatalf("subsequence search = %#v", got)
	}
	if got := SearchHistory(entries, ""); !reflect.DeepEqual(got, []string{"beta", "build api", "alpha"}) {
		t.Fatalf("empty search = %#v", got)
	}
}
