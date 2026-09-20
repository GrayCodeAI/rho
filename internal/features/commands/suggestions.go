// Package commands owns command-catalog algorithms that are independent of
// Bubble Tea, plugin execution, and the CLI model.
package commands

import (
	"sort"
	"strings"
)

// Suggestions returns matching command names with descriptions and aliases.
func Suggestions(input string, commandNames []string, descriptions, aliases map[string]string) []string {
	v := strings.TrimSpace(input)
	if !strings.HasPrefix(v, "/") || strings.Contains(v, " ") {
		return nil
	}
	v = strings.ToLower(v)
	var out []string
	seen := map[string]bool{}
	for _, command := range commandNames {
		command = strings.ToLower(command)
		if strings.HasPrefix(command, v) {
			seen[command] = true
			if desc := descriptions[command]; desc != "" {
				out = append(out, command+"  "+desc)
			} else {
				out = append(out, command)
			}
		}
	}
	aliasNames := make([]string, 0, len(aliases))
	for alias := range aliases {
		aliasNames = append(aliasNames, alias)
	}
	sort.Strings(aliasNames)
	for _, alias := range aliasNames {
		target := aliases[alias]
		alias = strings.ToLower(alias)
		if strings.HasPrefix(alias, v) && !seen[target] {
			seen[alias] = true
			out = append(out, alias+" → "+target)
		}
	}
	if len(out) == 1 && strings.HasPrefix(out[0], v+" ") && strings.Fields(out[0])[0] == v {
		return nil
	}
	return out
}

// ApplySuggestion turns a selected display value into a command input.
func ApplySuggestion(input string, aliases map[string]string) string {
	choice := strings.TrimSpace(input)
	if before, _, ok := strings.Cut(choice, " → "); ok {
		choice = before
	}
	parts := strings.Fields(choice)
	if len(parts) > 0 {
		choice = parts[0]
	}
	if target, ok := aliases[choice]; ok {
		choice = target
	}
	return choice + " "
}

// SuggestTypo returns the closest known command when the edit distance is a
// plausible typo rather than a different word.
func SuggestTypo(typo string, commandNames []string) string {
	if len(typo) < 2 {
		return ""
	}
	clean := strings.ToLower(strings.TrimPrefix(typo, "/"))
	if clean == "" {
		return ""
	}
	best := ""
	bestDist := 999
	for _, command := range commandNames {
		target := strings.ToLower(strings.TrimPrefix(command, "/"))
		distance := levenshtein(clean, target)
		if distance < bestDist {
			bestDist = distance
			best = command
		}
	}
	if best == "" {
		return ""
	}
	target := strings.ToLower(strings.TrimPrefix(best, "/"))
	maxDist := 1
	if len(target) > 5 {
		maxDist = 2
	}
	if len(clean) > len(target)+maxDist || bestDist > maxDist || bestDist == 0 {
		return ""
	}
	return best
}

func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	if len(b) > len(a) {
		a, b = b, a
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, min(curr[j-1]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}
