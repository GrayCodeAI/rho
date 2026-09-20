package shellmode

import "strings"

// nlMarkers are common English words unusual as shell arguments.
// If a failed command contains these, it's likely natural language.
var nlMarkers = map[string]bool{
	"the": true, "a": true, "an": true, "this": true, "that": true,
	"is": true, "are": true, "was": true, "were": true, "be": true,
	"to": true, "of": true, "in": true, "for": true, "with": true,
	"from": true, "about": true, "into": true, "through": true,
	"and": true, "but": true, "or": true, "so": true, "because": true,
	"i": true, "we": true, "you": true, "it": true, "they": true,
	"my": true, "our": true, "your": true, "their": true,
	"please": true, "can": true, "could": true, "would": true, "should": true,
	"how": true, "what": true, "why": true, "where": true, "when": true,
	"sure": true, "just": true, "all": true, "some": true, "any": true,
}

// shellErrorPatterns indicate the shell tried to interpret natural language.
var shellErrorPatterns = []string{
	"command not found",
	"no such file or directory",
	"syntax error",
	"unexpected token",
	"unknown command",
	"no rule to make target",
	"is not a git command",
	"invalid option",
	"unknown option",
}

// RerouteCandidate checks if a failed shell command should be rerouted to the AI.
// Returns true if the command has NL markers AND the error matches known patterns.
func RerouteCandidate(cmdStr string, stderr string, exitCode int) bool {
	if exitCode == 0 || exitCode >= 128 {
		return false // success or signal — don't reroute
	}
	return hasNLMarkers(cmdStr) && matchesErrorPattern(stderr)
}

// hasNLMarkers checks if the command contains natural language markers
// (at least 2 NL marker words after the first word).
func hasNLMarkers(input string) bool {
	words := strings.Fields(input)
	if len(words) < 3 {
		return false
	}
	count := 0
	for _, w := range words[1:] {
		lower := strings.ToLower(w)
		if strings.HasPrefix(lower, "-") || strings.HasPrefix(lower, "/") ||
			strings.HasPrefix(lower, ".") || strings.HasPrefix(lower, "$") {
			continue
		}
		if nlMarkers[lower] {
			count++
		}
	}
	return count >= 2
}

// matchesErrorPattern checks if stderr contains a known shell error.
func matchesErrorPattern(stderr string) bool {
	lower := strings.ToLower(stderr)
	for _, pat := range shellErrorPatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}
