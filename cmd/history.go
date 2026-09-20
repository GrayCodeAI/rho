package cmd

import commandfeature "github.com/GrayCodeAI/rho/internal/features/commands"

// Compatibility wrappers keep the TUI and existing command tests independent
// of the feature package's storage implementation during migration.
const maxHistoryEntries = commandfeature.MaxHistoryEntries

func historyFilePath() string           { return commandfeature.HistoryPath() }
func loadInputHistory() []string        { return commandfeature.LoadHistory() }
func saveInputHistory(history []string) { commandfeature.SaveHistory(history) }
func appendToHistory(entry string)      { commandfeature.AppendHistory(entry) }
