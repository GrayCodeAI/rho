package commands

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/GrayCodeAI/rho/internal/storage"
)

// MaxHistoryEntries bounds persisted interactive input history.
const MaxHistoryEntries = 1000

// HistoryPath returns the persistent input-history path.
func HistoryPath() string {
	return filepath.Join(storage.StateDir(), "history")
}

// LoadHistory loads history, returning an empty slice when no file exists.
func LoadHistory() []string {
	data, err := os.ReadFile(HistoryPath())
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	entries := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			entries = append(entries, line)
		}
	}
	return entries
}

// SaveHistory deduplicates entries, keeps the last occurrence, and persists
// at most MaxHistoryEntries entries in chronological order.
func SaveHistory(history []string) {
	seen := make(map[string]bool)
	deduped := make([]string, 0, len(history))
	for i := len(history) - 1; i >= 0; i-- {
		entry := strings.TrimSpace(history[i])
		if entry == "" || seen[entry] {
			continue
		}
		seen[entry] = true
		deduped = append(deduped, entry)
	}
	for i, j := 0, len(deduped)-1; i < j; i, j = i+1, j-1 {
		deduped[i], deduped[j] = deduped[j], deduped[i]
	}
	if len(deduped) > MaxHistoryEntries {
		deduped = deduped[len(deduped)-MaxHistoryEntries:]
	}

	path := HistoryPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o750)
	_ = os.WriteFile(path, []byte(strings.Join(deduped, "\n")+"\n"), 0o600)
}

// AppendHistory adds one non-empty entry to persistent history.
func AppendHistory(entry string) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return
	}
	history := LoadHistory()
	SaveHistory(append(history, entry))
}
