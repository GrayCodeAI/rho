package session

import (
	"sort"
	"strings"

	store "github.com/GrayCodeAI/rho/internal/session"
)

// FilterEntries ranks saved sessions by ID, preview, and working-directory
// relevance while preserving recency as the tie-breaker.
func FilterEntries(entries []store.Entry, query string) []store.Entry {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return append([]store.Entry(nil), entries...)
	}
	type scored struct {
		entry store.Entry
		score int
		idx   int
	}
	results := make([]scored, 0, len(entries))
	for i, entry := range entries {
		id := strings.ToLower(entry.ID)
		preview := strings.ToLower(entry.Preview)
		cwd := strings.ToLower(entry.CWD)
		score := 0
		switch {
		case strings.HasPrefix(id, query):
			score = 1000
		case strings.Contains(id, query):
			score = 800
		case strings.HasPrefix(preview, query):
			score = 600
		case strings.Contains(preview, query):
			score = 500
		case strings.Contains(cwd, query):
			score = 400
		case subsequenceMatch(preview, query):
			score = 300
		case subsequenceMatch(id, query):
			score = 200
		}
		if score > 0 {
			score += i * 10 / max(len(entries), 1)
			results = append(results, scored{entry: entry, score: score, idx: i})
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].idx > results[j].idx
	})
	out := make([]store.Entry, 0, len(results))
	for _, result := range results {
		out = append(out, result.entry)
	}
	return out
}

func subsequenceMatch(target, query string) bool {
	if query == "" {
		return true
	}
	queryRunes := []rune(query)
	index := 0
	for _, r := range target {
		if index < len(queryRunes) && r == queryRunes[index] {
			index++
		}
	}
	return index == len(queryRunes)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
