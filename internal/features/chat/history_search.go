package chat

import "strings"

// SearchHistory ranks prompt-history entries for an interactive search.
// Exact, prefix, substring, and subsequence matches are ordered by quality;
// newer entries win ties within the same quality tier.
func SearchHistory(entries []string, query string) []string {
	query = strings.ToLower(query)
	if query == "" {
		out := make([]string, 0, len(entries))
		for i := len(entries) - 1; i >= 0; i-- {
			out = append(out, entries[i])
		}
		return out
	}

	type scored struct {
		entry string
		score int
		index int
	}
	results := make([]scored, 0, len(entries))
	for i, entry := range entries {
		lower := strings.ToLower(entry)
		score := 0
		switch {
		case lower == query:
			score = 1000
		case strings.HasPrefix(lower, query):
			score = 800
		case strings.Contains(lower, query):
			score = 600
		case subsequenceMatch(lower, query):
			score = 400
		}
		if score == 0 {
			continue
		}
		results = append(results, scored{
			entry: entry,
			score: score + i*10/max(len(entries), 1),
			index: i,
		})
	}

	// The history is small and this keeps the ordering rule explicit.
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].score > results[i].score ||
				(results[j].score == results[i].score && results[j].index > results[i].index) {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	out := make([]string, 0, len(results))
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
	q := 0
	for _, r := range target {
		if queryRunes[q] == r {
			q++
			if q == len(queryRunes) {
				return true
			}
		}
	}
	return false
}
