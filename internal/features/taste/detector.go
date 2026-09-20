package taste

import (
	"regexp"
	"strings"
)

// Naming style constants.
const (
	NamingCamelCase  = "camelCase"
	NamingSnakeCase  = "snake_case"
	NamingPascalCase = "PascalCase"
	NamingKebabCase  = "kebab-case"
)

// Error style constants.
const (
	ErrorSentinel = "sentinel"
	ErrorWrapped  = "wrapped"
	ErrorPanic    = "panic"
	ErrorCustom   = "custom_type"
)

// Abstraction level constants.
const (
	AbstractionInlined   = "inlined"
	AbstractionExtracted = "extracted"
)

// Test style constants.
const (
	TestTableDriven = "table_driven"
	TestSubtests    = "subtests"
	TestAssertLib   = "assert_lib"
	TestPlain       = "plain"
)

var (
	snakeCaseRe   = regexp.MustCompile(`\b[a-z][a-z0-9]*(_[a-z][a-z0-9]*)+\b`)
	camelCaseRe   = regexp.MustCompile(`\b[a-z]+[A-Z][a-zA-Z0-9]*\b`)
	pascalCaseRe  = regexp.MustCompile(`\b[A-Z][a-z]+[A-Z][a-zA-Z0-9]*\b`)
	kebabCaseRe   = regexp.MustCompile(`\b[a-z][a-z0-9]*(-[a-z][a-z0-9]*)+\b`)
	errWrapRe     = regexp.MustCompile(`fmt\.Errorf\(.+%w`)
	errSentinelRe = regexp.MustCompile(`(var|=)\s+Err[A-Z]\w*\s*=\s*(errors\.New|fmt\.Errorf)`)
	errPanicRe    = regexp.MustCompile(`panic\(`)
	errCustomRe   = regexp.MustCompile(`type\s+\w+Error\s+struct`)
	funcDefRe     = regexp.MustCompile(`func\s+(\w+)?\s*\(`)
	tableDrivenRe = regexp.MustCompile(`(tests|testCases|cases|tt)\s*:?=\s*\[\]`)
	subtestRe     = regexp.MustCompile(`t\.Run\(`)
	assertLibRe   = regexp.MustCompile(`(assert\.|require\.|expect\()`)
	commentLineRe = regexp.MustCompile(`^\s*(//|#|/\*|\*)`)
)

// DetectNamingStyle analyzes code to determine the predominant naming convention.
func DetectNamingStyle(code string) string {
	if code == "" {
		return "unknown"
	}

	scores := map[string]int{
		NamingSnakeCase:  len(snakeCaseRe.FindAllString(code, -1)),
		NamingCamelCase:  len(camelCaseRe.FindAllString(code, -1)),
		NamingPascalCase: len(pascalCaseRe.FindAllString(code, -1)),
		NamingKebabCase:  len(kebabCaseRe.FindAllString(code, -1)),
	}

	best := "unknown"
	bestCount := 0
	for style, count := range scores {
		if count > bestCount {
			best = style
			bestCount = count
		}
	}

	if bestCount < 2 {
		return "unknown"
	}
	return best
}

// DetectCommentDensity returns the ratio of comment lines to total non-empty lines.
func DetectCommentDensity(code string) float64 {
	if code == "" {
		return 0
	}

	lines := strings.Split(code, "\n")
	totalNonEmpty := 0
	commentLines := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		totalNonEmpty++
		if commentLineRe.MatchString(line) {
			commentLines++
		}
	}

	if totalNonEmpty == 0 {
		return 0
	}
	return float64(commentLines) / float64(totalNonEmpty)
}

// DetectErrorPattern analyzes Go/Python/JS code to categorize error handling style.
func DetectErrorPattern(code string) string {
	if code == "" {
		return "unknown"
	}

	scores := map[string]int{
		ErrorWrapped:  len(errWrapRe.FindAllString(code, -1)),
		ErrorSentinel: len(errSentinelRe.FindAllString(code, -1)),
		ErrorPanic:    len(errPanicRe.FindAllString(code, -1)),
		ErrorCustom:   len(errCustomRe.FindAllString(code, -1)),
	}

	best := "unknown"
	bestCount := 0
	for style, count := range scores {
		if count > bestCount {
			best = style
			bestCount = count
		}
	}

	if bestCount == 0 {
		return "unknown"
	}
	return best
}

// DetectAbstractionLevel estimates if code prefers inlining or extraction.
func DetectAbstractionLevel(code string) string {
	if code == "" {
		return "unknown"
	}

	funcDefs := len(funcDefRe.FindAllString(code, -1))
	lines := strings.Split(code, "\n")
	nonEmpty := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty++
		}
	}

	if nonEmpty == 0 {
		return "unknown"
	}

	// Ratio of function definitions to lines — more functions per line = more extracted.
	ratio := float64(funcDefs) / float64(nonEmpty)

	switch {
	case ratio > 0.05:
		return AbstractionExtracted
	case funcDefs >= 1 && nonEmpty > 30:
		return AbstractionInlined
	default:
		return "unknown"
	}
}

// DetectTestStyle analyzes test code to determine testing patterns.
func DetectTestStyle(code string) string {
	if code == "" {
		return "unknown"
	}

	if tableDrivenRe.MatchString(code) {
		return TestTableDriven
	}
	if subtestRe.MatchString(code) {
		return TestSubtests
	}
	if assertLibRe.MatchString(code) {
		return TestAssertLib
	}

	// Check if it looks like test code at all.
	if strings.Contains(code, "func Test") || strings.Contains(code, "def test_") {
		return TestPlain
	}

	return "unknown"
}

// DetectLanguage attempts to identify the programming language from code content.
func DetectLanguage(code string) string {
	switch {
	case strings.Contains(code, "package ") && strings.Contains(code, "func "):
		return "go"
	case strings.Contains(code, "def ") && strings.Contains(code, "import "):
		return "python"
	case strings.Contains(code, "interface ") && strings.Contains(code, ": "):
		return "typescript"
	case strings.Contains(code, "function ") || strings.Contains(code, "const ") || strings.Contains(code, "let "):
		return "javascript"
	case strings.Contains(code, "fn ") && strings.Contains(code, "let mut"):
		return "rust"
	default:
		return "unknown"
	}
}

// AnalyzeCode runs all detectors on a code sample and returns detected signals.
func AnalyzeCode(code string) map[string]string {
	results := make(map[string]string)

	if naming := DetectNamingStyle(code); naming != "unknown" {
		results[CategoryNaming] = naming
	}

	density := DetectCommentDensity(code)
	results[CategoryComments] = categorizeCommentDensity(density)

	if errStyle := DetectErrorPattern(code); errStyle != "unknown" {
		results[CategoryErrorHandling] = errStyle
	}

	if abstraction := DetectAbstractionLevel(code); abstraction != "unknown" {
		results[CategoryAbstraction] = abstraction
	}

	if testStyle := DetectTestStyle(code); testStyle != "unknown" {
		results[CategoryTesting] = testStyle
	}

	return results
}
