package eval

import (
	"regexp"
	"strings"
)

// Package-level compiled patterns (M14): these filters run per evaluation
// candidate, and regexp.MustCompile per call wasted CPU and allocation.
var (
	genericCodeBlockRe = regexp.MustCompile("(?s)```\\s*\n(.*?)```")
	markdownBlockRe    = regexp.MustCompile("(?s)```[a-z]*\\s*\n(.*?)```")
)

// Filter transforms LLM output before validation.
type Filter func(string) string

// ExtractCodeBlock extracts the first fenced code block matching the given language.
// If no match, returns the original string.
func ExtractCodeBlock(lang string) Filter {
	pattern := regexp.MustCompile("(?s)```" + regexp.QuoteMeta(lang) + `\s*\n(.*?)` + "```")
	return func(s string) string {
		matches := pattern.FindStringSubmatch(s)
		if len(matches) > 1 {
			return strings.TrimSpace(matches[1])
		}
		// Try generic code block
		if m := genericCodeBlockRe.FindStringSubmatch(s); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
		return s
	}
}

// StripMarkdown removes all markdown formatting, keeping only code content.
func StripMarkdown(s string) string {
	// Extract all code blocks
	matches := markdownBlockRe.FindAllStringSubmatch(s, -1)
	if len(matches) > 0 {
		var parts []string
		for _, m := range matches {
			parts = append(parts, strings.TrimSpace(m[1]))
		}
		return strings.Join(parts, "\n\n")
	}
	return s
}

// TrimExplanation removes common LLM explanation prefixes/suffixes.
func TrimExplanation(s string) string {
	lines := strings.Split(s, "\n")
	var code []string
	inCode := false
	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			code = append(code, line)
		}
	}
	if len(code) > 0 {
		return strings.Join(code, "\n")
	}
	return s
}

// ApplyFilters runs a chain of filters on the input.
func ApplyFilters(input string, filters ...Filter) string {
	for _, f := range filters {
		input = f(input)
	}
	return input
}
