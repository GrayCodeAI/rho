// Package review owns the multi-concern code-review pipeline.
package review

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ChatFunc sends one review prompt to an LLM and returns its response.
type ChatFunc func(ctx context.Context, prompt string) (string, error)

// Concern represents one aspect of code review.
type Concern struct {
	Name   string
	Prompt string
}

// Finding represents one issue found during review.
type Finding struct {
	Concern  string
	Severity string
	File     string
	Line     int
	Message  string
	Fix      string
}

// DefaultConcerns returns the standard review concern set.
func DefaultConcerns() []Concern {
	return []Concern{
		{Name: "security", Prompt: `Review the following code for security vulnerabilities:
- Injection attacks (SQL, command, path traversal)
- Authentication and authorization flaws
- Sensitive data exposure
- Insecure deserialization
- Missing input validation
Report each issue with: File, Line (approx), Severity (critical/high/medium/low), Description, Fix.`},
		{Name: "performance", Prompt: `Review the following code for performance issues:
- Unnecessary allocations and copies
- O(n^2) or worse algorithms where O(n) is possible
- Missing caching opportunities
- Unbounded growth (slices, maps, channels)
- Blocking operations that could be async
Report each issue with: File, Line (approx), Severity (critical/high/medium/low), Description, Fix.`},
		{Name: "bugs", Prompt: `Review the following code for bugs and logic errors:
- Off-by-one errors
- Nil/null pointer dereferences
- Race conditions
- Resource leaks (unclosed files, connections)
- Incorrect error handling
- Dead code and unreachable branches
Report each issue with: File, Line (approx), Severity (critical/high/medium/low), Description, Fix.`},
		{Name: "correctness", Prompt: `Review the following code for correctness:
- Does the code match its stated intent (function names, comments)?
- Are edge cases handled?
- Are return values and errors checked?
- Is concurrency handled correctly (mutexes, channels)?
- Are API contracts respected?
Report each issue with: File, Line (approx), Severity (critical/high/medium/low), Description, Fix.`},
		{Name: "style", Prompt: `Review the following code for style and maintainability:
- Naming conventions
- Function length and complexity
- Code duplication
- Missing or misleading comments
- Consistent formatting
Report each issue with: File, Line (approx), Severity (critical/high/medium/low), Description, Fix.`},
	}
}

// Run executes all concerns concurrently, then deduplicates and orders the
// findings before producing the human-readable report.
func Run(ctx context.Context, files []string, concerns []Concern, chat ChatFunc) ([]Finding, string) {
	if len(files) == 0 || len(concerns) == 0 {
		return nil, "No files or concerns specified."
	}
	if chat == nil {
		return nil, "No LLM chat function provided."
	}

	var mu sync.Mutex
	var all []Finding
	var wg sync.WaitGroup
	for _, concern := range concerns {
		wg.Add(1)
		go func(c Concern) {
			defer wg.Done()
			findings := runConcern(ctx, files, c, chat)
			mu.Lock()
			all = append(all, findings...)
			mu.Unlock()
		}(concern)
	}
	wg.Wait()

	all = deduplicate(all)
	sortFindings(all)
	return all, FormatReport(all)
}

func runConcern(ctx context.Context, files []string, concern Concern, chat ChatFunc) []Finding {
	response, err := chat(ctx, BuildPrompt(files, concern))
	if err != nil {
		return nil
	}
	return ParseFindings(response, concern.Name)
}

// RunConcern exposes one concern execution for composition and compatibility
// adapters that need to inspect a single pipeline leg.
func RunConcern(ctx context.Context, files []string, concern Concern, chat ChatFunc) []Finding {
	return runConcern(ctx, files, concern, chat)
}

type llmFinding struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Fix      string `json:"fix"`
}

// ParseFindings extracts JSON findings, falling back to a single textual
// finding so a malformed model response is still visible to the caller.
func ParseFindings(response, concernName string) []Finding {
	jsonStart := strings.Index(response, "[")
	jsonEnd := strings.LastIndex(response, "]")
	if jsonStart >= 0 && jsonEnd > jsonStart {
		var raw []llmFinding
		if err := json.Unmarshal([]byte(response[jsonStart:jsonEnd+1]), &raw); err == nil {
			findings := make([]Finding, 0, len(raw))
			for _, item := range raw {
				findings = append(findings, Finding{Concern: concernName, Severity: item.Severity, File: item.File, Line: item.Line, Message: item.Message, Fix: item.Fix})
			}
			return findings
		}
	}
	trimmed := strings.TrimSpace(response)
	if trimmed == "" || strings.EqualFold(trimmed, "no issues found") || strings.EqualFold(trimmed, "no issues found.") {
		return nil
	}
	return []Finding{{Concern: concernName, Severity: "medium", Message: trimmed}}
}

// BuildPrompt constructs the structured-output prompt for one concern.
func BuildPrompt(files []string, concern Concern) string {
	var b strings.Builder
	b.WriteString(concern.Prompt)
	b.WriteString("\n\n## Output format\n\n")
	b.WriteString("Return your findings as a JSON array. Each element must have:\n")
	b.WriteString("  \"file\" (string), \"line\" (int), \"severity\" (critical|high|medium|low),\n")
	b.WriteString("  \"message\" (string), \"fix\" (string, suggested fix).\n")
	b.WriteString("If no issues are found, return an empty array: []\n")
	b.WriteString("\n## Files to review\n\n")
	for _, file := range files {
		b.WriteString(fmt.Sprintf("- %s\n", file))
	}
	return b.String()
}

// FormatReport formats findings grouped by severity.
func FormatReport(findings []Finding) string {
	if len(findings) == 0 {
		return "No issues found."
	}
	var b strings.Builder
	b.WriteString("=== Review Report ===\n\n")
	groups := map[string][]Finding{}
	for _, finding := range findings {
		groups[finding.Severity] = append(groups[finding.Severity], finding)
	}
	for _, severity := range []string{"critical", "high", "medium", "low"} {
		items := groups[severity]
		if len(items) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("## %s (%d)\n\n", strings.ToUpper(severity), len(items)))
		for _, finding := range items {
			location := finding.File
			if finding.Line > 0 {
				location = fmt.Sprintf("%s:%d", finding.File, finding.Line)
			}
			b.WriteString(fmt.Sprintf("  [%s] %s\n", location, finding.Message))
			if finding.Fix != "" {
				b.WriteString(fmt.Sprintf("    Fix: %s\n", finding.Fix))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString(fmt.Sprintf("--- %d issue(s) total ---\n", len(findings)))
	return b.String()
}

func deduplicate(findings []Finding) []Finding {
	type key struct {
		file, message string
		line          int
	}
	seen := map[key]bool{}
	result := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		k := key{file: finding.File, line: finding.Line, message: finding.Message}
		if seen[k] {
			continue
		}
		seen[k] = true
		result = append(result, finding)
	}
	return result
}

// DeduplicateFindings returns findings with identical location and message
// collapsed to one entry.
func DeduplicateFindings(findings []Finding) []Finding {
	return deduplicate(findings)
}

func sortFindings(findings []Finding) {
	order := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3}
	sort.Slice(findings, func(i, j int) bool {
		if order[findings[i].Severity] != order[findings[j].Severity] {
			return order[findings[i].Severity] < order[findings[j].Severity]
		}
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
}

// SortBySeverity orders findings from critical to low severity.
func SortBySeverity(findings []Finding) {
	sortFindings(findings)
}
