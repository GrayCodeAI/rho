package session

import (
	"fmt"
	"strings"
	"time"

	store "github.com/GrayCodeAI/rho/internal/session"
)

// HistoryReport formats the saved-session listing for a terminal client.
// found is false when there are no saved sessions.
func HistoryReport() (report string, found bool, err error) {
	entries, err := store.List()
	if err != nil {
		return "", false, err
	}
	if len(entries) == 0 {
		return "", false, nil
	}
	return formatHistory(entries), true, nil
}

func formatHistory(entries []store.Entry) string {
	var b strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&b, "  %s  %s  %s\n", entry.ID, entry.UpdatedAt.Format("Jan 02 15:04"), entry.Preview)
	}
	return b.String()
}

// SearchReport formats matching saved-session messages for a terminal client.
// found is false when the query has no results.
func SearchReport(query string, maxResults int) (report string, found bool, err error) {
	results, err := store.SearchSessions(query, maxResults)
	if err != nil {
		return "", false, err
	}
	if len(results) == 0 {
		return "", false, nil
	}
	return formatSearch(query, results), true, nil
}

func formatSearch(query string, results []store.SearchResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Search results for %q:\n", query)
	for _, result := range results {
		fmt.Fprintf(&b, "  [%s] msg %d (%s): %s\n", result.SessionID, result.MsgIndex, result.Role, result.Preview)
	}
	return b.String()
}

// CleanOld removes sessions older than age and returns the number removed.
func CleanOld(age time.Duration) (int, error) {
	return store.CleanOldSessions(age)
}

// CompressOld compresses sessions older than age and returns the number
// compressed.
func CompressOld(age time.Duration) (int, error) {
	return store.CompressOldSessions(age)
}

// IntegrityReport loads and formats the structural integrity report for a
// saved session.
func IntegrityReport(id string) (string, error) {
	saved, err := store.Load(id)
	if err != nil {
		return "", err
	}
	check := store.ValidateIntegrity(saved)
	var b strings.Builder
	if check.Valid {
		b.WriteString("Session integrity: VALID\n")
	} else {
		b.WriteString("Session integrity: INVALID\n")
	}
	fmt.Fprintf(&b, "Messages: %d (user: %d, assistant: %d)\n", check.Stats.MessageCount, check.Stats.UserMessages, check.Stats.AssistantMessages)
	fmt.Fprintf(&b, "Tool uses: %d, Tool results: %d\n", check.Stats.ToolUses, check.Stats.ToolResults)
	if check.Stats.OrphanedResults > 0 {
		fmt.Fprintf(&b, "Orphaned results: %d\n", check.Stats.OrphanedResults)
	}
	for _, warning := range check.Warnings {
		b.WriteString("  warning: " + warning + "\n")
	}
	for _, problem := range check.Errors {
		b.WriteString("  error: " + problem + "\n")
	}
	return b.String(), nil
}
