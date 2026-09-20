package cmd

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-runewidth"
)

// Suggestion represents a single autocompletion suggestion.
type Suggestion struct {
	Text        string
	Description string
	Category    string // "command", "file", "tool", "history", "model"
	Score       float64
}

// Autocompleter provides context-aware autocompletion for the REPL.
type Autocompleter struct {
	History       []string
	Tools         []string
	SlashCommands []string
	Files         []string
	ProjectDir    string
	mu            sync.RWMutex

	// internal state
	usageCount map[string]int
	fileMTimes map[string]time.Time
}

// NewAutocompleter creates an Autocompleter initialized for the given project directory.
// globalAutocompleter is a singleton autocompleter instance
var globalAutocompleter *Autocompleter

// globalProjectDir caches the project directory
var globalProjectDir = ""

// GetAutocompleter returns the singleton autocompleter
func GetAutocompleter(projectDir string) *Autocompleter {
	if globalAutocompleter == nil || globalProjectDir != projectDir {
		globalAutocompleter = &Autocompleter{
			ProjectDir:    projectDir,
			SlashCommands: slashCommands(),
			usageCount:    make(map[string]int),
			fileMTimes:    make(map[string]time.Time),
		}
		globalProjectDir = projectDir
		globalAutocompleter.RefreshFiles()
	}
	return globalAutocompleter
}

// Complete returns context-aware suggestions for the given input and cursor position.
func (ac *Autocompleter) Complete(input string, cursorPos int) []Suggestion {
	if cursorPos > len(input) {
		cursorPos = len(input)
	}
	if cursorPos < 0 {
		cursorPos = 0
	}

	// Work with the text up to the cursor
	text := input[:cursorPos]
	if text == "" {
		return nil
	}

	// Find the current token (word being typed)
	token := extractCurrentToken(text)

	ac.mu.RLock()
	defer ac.mu.RUnlock()

	var suggestions []Suggestion

	switch {
	case strings.HasPrefix(token, "/"):
		// Slash command completion
		suggestions = ac.CompleteSlashCommand(token)
	case strings.HasPrefix(token, "@"):
		// File path completion (@ prefix)
		prefix := strings.TrimPrefix(token, "@")
		suggestions = ac.CompleteFilePath(prefix)
	case strings.HasPrefix(token, "--"):
		// Flag completion
		suggestions = ac.completeFlags(token)
	case isToolContext(text, token):
		// Tool argument completion
		suggestions = ac.completeToolArgs(token)
	default:
		// General: history + files + suggestions
		suggestions = ac.completeGeneral(token)
	}

	return ac.RankSuggestions(suggestions)
}

// CompleteSlashCommand returns suggestions for slash commands matching the prefix.
func (ac *Autocompleter) CompleteSlashCommand(prefix string) []Suggestion {
	var suggestions []Suggestion

	for _, cmd := range ac.SlashCommands {
		matched, score := FuzzyMatch(prefix, cmd)
		if !matched {
			continue
		}

		desc := ""
		if d, ok := slashDescriptions[cmd]; ok {
			desc = d
		}

		// Boost score by usage frequency
		usageBoost := float64(ac.usageCount[cmd]) * 0.05
		if usageBoost > 0.3 {
			usageBoost = 0.3
		}

		suggestions = append(suggestions, Suggestion{
			Text:        cmd,
			Description: desc,
			Category:    "command",
			Score:       score + usageBoost,
		})
	}

	return suggestions
}

// CompleteFilePath returns suggestions for file paths matching the prefix.
func (ac *Autocompleter) CompleteFilePath(prefix string) []Suggestion {
	var suggestions []Suggestion

	for _, f := range ac.Files {
		matched, score := FuzzyMatch(prefix, f)
		if !matched && prefix != "" {
			continue
		}
		if prefix == "" {
			score = 0.5
		}

		// Boost recently modified files
		if mtime, ok := ac.fileMTimes[f]; ok {
			age := time.Since(mtime)
			if age < 1*time.Hour {
				score += 0.3
			} else if age < 24*time.Hour {
				score += 0.15
			} else if age < 7*24*time.Hour {
				score += 0.05
			}
		}

		suggestions = append(suggestions, Suggestion{
			Text:        f,
			Description: "",
			Category:    "file",
			Score:       score,
		})
	}

	return suggestions
}

// CompleteFromHistory returns suggestions from command history matching the prefix.
func (ac *Autocompleter) CompleteFromHistory(prefix string) []Suggestion {
	var suggestions []Suggestion
	seen := make(map[string]bool)

	// Iterate in reverse for most-recent-first
	for i := len(ac.History) - 1; i >= 0; i-- {
		entry := ac.History[i]
		if seen[entry] {
			continue
		}

		if prefix == "" || strings.HasPrefix(strings.ToLower(entry), strings.ToLower(prefix)) {
			seen[entry] = true
			// Score decreases with distance from end (more recent = higher)
			recencyScore := 1.0 - float64(len(ac.History)-1-i)*0.02
			if recencyScore < 0.1 {
				recencyScore = 0.1
			}

			suggestions = append(suggestions, Suggestion{
				Text:        entry,
				Description: "history",
				Category:    "history",
				Score:       recencyScore,
			})
		}
	}

	return suggestions
}

// FuzzyMatch performs subsequence matching between input and candidate.
// Returns whether there is a match and a quality score.
// Scores are based on: consecutive matches, start-of-word matches, exact prefix bonus.
func FuzzyMatch(input, candidate string) (bool, float64) {
	if input == "" {
		return true, 0.0
	}
	if candidate == "" {
		return false, 0.0
	}

	inputLower := strings.ToLower(input)
	candidateLower := strings.ToLower(candidate)

	inputRunes := []rune(inputLower)
	candidateRunes := []rune(candidateLower)

	// Check if all input characters exist in order in candidate
	qi := 0
	for ci := 0; ci < len(candidateRunes) && qi < len(inputRunes); ci++ {
		if candidateRunes[ci] == inputRunes[qi] {
			qi++
		}
	}
	if qi < len(inputRunes) {
		return false, 0.0
	}

	// Calculate score
	score := 0.0

	// Exact prefix bonus
	if strings.HasPrefix(candidateLower, inputLower) {
		score += 2.0
	}

	// Score the match quality
	qi = 0
	lastMatchIdx := -1
	consecutiveCount := 0

	for ci := 0; ci < len(candidateRunes) && qi < len(inputRunes); ci++ {
		if candidateRunes[ci] == inputRunes[qi] {
			points := 1.0

			// Consecutive match bonus
			if lastMatchIdx == ci-1 {
				consecutiveCount++
				points += float64(consecutiveCount) * 0.5
			} else {
				consecutiveCount = 0
			}

			// Start of word bonus
			if ci == 0 {
				points += 1.5
			} else if ci > 0 {
				prev := candidateRunes[ci-1]
				if prev == '/' || prev == '-' || prev == '_' || prev == ' ' || prev == '.' {
					points += 1.0
				}
			}

			score += points
			lastMatchIdx = ci
			qi++
		}
	}

	// Normalize score
	maxPossible := float64(len(inputRunes)) * 4.0
	if maxPossible == 0 {
		return true, 0.0
	}
	normalized := score / maxPossible
	if normalized > 1.0 {
		normalized = 1.0
	}

	// Length penalty: prefer shorter candidates
	lengthRatio := float64(len(inputRunes)) / float64(len(candidateRunes))
	if lengthRatio > 1.0 {
		lengthRatio = 1.0
	}

	finalScore := normalized*0.75 + lengthRatio*0.25
	return true, finalScore
}

// RecordInput records user input for future history-based completions.
func (ac *Autocompleter) RecordInput(input string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	input = strings.TrimSpace(input)
	if input == "" {
		return
	}

	ac.History = append(ac.History, input)

	// Track slash command usage
	if strings.HasPrefix(input, "/") {
		parts := strings.Fields(input)
		if len(parts) > 0 {
			ac.usageCount[parts[0]]++
		}
	}
}

// RefreshFiles rescans the project directory for file completions.
func (ac *Autocompleter) RefreshFiles() {
	if ac.ProjectDir == "" {
		return
	}

	var files []string
	fileMTimes := make(map[string]time.Time)

	_ = filepath.WalkDir(ac.ProjectDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip hidden directories and common non-essential dirs
		name := d.Name()
		if d.IsDir() {
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip hidden files
		if strings.HasPrefix(name, ".") {
			return nil
		}

		rel, err := filepath.Rel(ac.ProjectDir, path)
		if err != nil {
			return nil
		}

		files = append(files, rel)
		fi, fiErr := d.Info()
		if fiErr != nil {
			return fiErr
		}
		fileMTimes[rel] = fi.ModTime()

		// Cap file count
		if len(files) > 5000 {
			return fmt.Errorf("too many files")
		}

		return nil
	})

	ac.mu.Lock()
	ac.Files = files
	ac.fileMTimes = fileMTimes
	ac.mu.Unlock()
}

// FormatSuggestions formats suggestions for terminal display.
func FormatSuggestions(suggestions []Suggestion, maxDisplay int) string {
	if len(suggestions) == 0 {
		return ""
	}

	if maxDisplay <= 0 {
		maxDisplay = 10
	}
	if len(suggestions) > maxDisplay {
		suggestions = suggestions[:maxDisplay]
	}

	// Find max text width for alignment (display width, so multi-byte text aligns)
	maxWidth := 0
	for _, s := range suggestions {
		if w := runewidth.StringWidth(s.Text); w > maxWidth {
			maxWidth = w
		}
	}

	var b strings.Builder
	for _, s := range suggestions {
		if s.Description != "" {
			padding := strings.Repeat(" ", maxWidth-runewidth.StringWidth(s.Text)+4)
			b.WriteString(fmt.Sprintf("%s%s%s\n", s.Text, padding, s.Description))
		} else {
			b.WriteString(s.Text + "\n")
		}
	}

	return b.String()
}

// RankSuggestions sorts suggestions by: exact prefix first, then by score, then alphabetical.
func (ac *Autocompleter) RankSuggestions(suggestions []Suggestion) []Suggestion {
	if len(suggestions) == 0 {
		return suggestions
	}

	sort.SliceStable(suggestions, func(i, j int) bool {
		// Higher score first
		if suggestions[i].Score != suggestions[j].Score {
			return suggestions[i].Score > suggestions[j].Score
		}
		// Alphabetical tiebreaker
		return suggestions[i].Text < suggestions[j].Text
	})

	return suggestions
}

// completeFlags returns flag suggestions matching the prefix.
func (ac *Autocompleter) completeFlags(prefix string) []Suggestion {
	flags := []struct {
		name string
		desc string
	}{
		{"--model", "Model to use"},
		{"--provider", "LLM provider"},
		{"--print", "Print response and exit"},
		{"--resume", "Resume a saved session"},
		{"--continue", "Continue most recent conversation"},
		{"--max-turns", "Maximum agentic turns"},
		{"--max-budget-usd", "Maximum API spend"},
		{"--system-prompt", "System prompt to use"},
		{"--output-format", "Output format"},
		{"--auto-commit", "Auto-commit changes"},
		{"--watch", "Watch for file changes"},
		{"--power", "Power level 1-10"},
		{"--timeout", "Time budget"},
		{"--session-id", "Session ID"},
	}

	var suggestions []Suggestion
	for _, f := range flags {
		matched, score := FuzzyMatch(prefix, f.name)
		if !matched {
			continue
		}
		suggestions = append(suggestions, Suggestion{
			Text:        f.name,
			Description: f.desc,
			Category:    "command",
			Score:       score,
		})
	}
	return suggestions
}

// completeToolArgs returns tool argument suggestions.
func (ac *Autocompleter) completeToolArgs(token string) []Suggestion {
	var suggestions []Suggestion
	for _, t := range ac.Tools {
		matched, score := FuzzyMatch(token, t)
		if !matched {
			continue
		}
		suggestions = append(suggestions, Suggestion{
			Text:        t,
			Description: "tool",
			Category:    "tool",
			Score:       score,
		})
	}
	return suggestions
}

// completeGeneral returns mixed suggestions from history, files, and tools.
func (ac *Autocompleter) completeGeneral(token string) []Suggestion {
	var suggestions []Suggestion

	// History completions
	historySugg := ac.CompleteFromHistory(token)
	suggestions = append(suggestions, historySugg...)

	// File completions (limit to top matches)
	fileSugg := ac.CompleteFilePath(token)
	if len(fileSugg) > 10 {
		fileSugg = fileSugg[:10]
	}
	suggestions = append(suggestions, fileSugg...)

	// Tool completions
	toolSugg := ac.completeToolArgs(token)
	suggestions = append(suggestions, toolSugg...)

	return suggestions
}

// extractCurrentToken extracts the current word being typed from the input.
func extractCurrentToken(text string) string {
	if text == "" {
		return ""
	}

	// Find last space or start of string
	lastSpace := strings.LastIndexByte(text, ' ')
	if lastSpace == -1 {
		return text
	}
	return text[lastSpace+1:]
}

// isToolContext determines if the current input is in a tool argument context.
func isToolContext(fullText, token string) bool {
	// Check if we're after a tool-related slash command
	trimmed := strings.TrimSpace(fullText)
	toolCommands := []string{"/run ", "/tools ", "/mcp "}
	for _, tc := range toolCommands {
		if strings.HasPrefix(trimmed, tc) {
			return true
		}
	}
	return false
}
