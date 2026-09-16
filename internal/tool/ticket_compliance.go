package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

// Ticket represents a linked issue/ticket from any tracking system.
type Ticket struct {
	ID                 string
	Title              string
	Description        string
	AcceptanceCriteria []string
	Labels             []string
	Source             string // "github", "jira", "linear"
}

// ComplianceResult holds the outcome of checking a PR against a ticket.
type ComplianceResult struct {
	Ticket      *Ticket
	Satisfied   []string
	Unsatisfied []string
	Score       float64
	Suggestions []string
}

// TicketCompliance verifies that PR changes satisfy linked issue requirements.
type TicketCompliance struct {
	mu sync.Mutex
}

// Package-level compiled patterns (M14): ExtractTicketRef and the criteria
// parsers run per PR review; regexp.MustCompile per call wasted CPU and
// allocation.
var (
	jiraRefRe            = regexp.MustCompile(`\b([A-Z][A-Z0-9]+-\d+)\b`)
	githubRefRe          = regexp.MustCompile(`#(\d+)`)
	keywordRefRe         = regexp.MustCompile(`(?i)(?:fix(?:es)?|close[sd]?|resolve[sd]?)\s+#(\d+)`)
	keywordJiraRefRe     = regexp.MustCompile(`(?i)(?:fix(?:es)?|close[sd]?|resolve[sd]?)\s+([A-Z][A-Z0-9]+-\d+)`)
	branchJiraRefRe      = regexp.MustCompile(`(?:^|/)([A-Z][A-Z0-9]+-\d+)`)
	acceptanceCheckboxRe = regexp.MustCompile(`^\s*-\s*\[[ x]?\]\s*(.+)`)
	acceptanceNumberedRe = regexp.MustCompile(`^\s*\d+\.\s+(.+)`)
	acceptanceShouldRe   = regexp.MustCompile(`(?i)^.*\bshould\b\s+(.+)`)
	acceptanceHeaderRe   = regexp.MustCompile(`(?i)^\s*#{0,6}\s*(?:acceptance\s+criteria|requirements|definition\s+of\s+done|criteria)\s*:?\s*$`)
	keywordSplitterRe    = regexp.MustCompile(`[^a-zA-Z0-9]+`)
)

// NewTicketCompliance creates a new TicketCompliance checker.
func NewTicketCompliance() *TicketCompliance {
	return &TicketCompliance{}
}

// ExtractTicketRef parses ticket references from branch names and PR descriptions.
// It recognizes patterns like: #123, PROJ-456, fixes #789, closes #101,
// feature/PROJ-123-description, "Fixes #42", "Resolves RHO-99".
func (tc *TicketCompliance) ExtractTicketRef(branchName, prDescription string) []string {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	seen := make(map[string]bool)
	var refs []string

	addRef := func(ref string) {
		ref = strings.TrimSpace(ref)
		if ref != "" && !seen[ref] {
			seen[ref] = true
			refs = append(refs, ref)
		}
	}

	// Extract from branch name.
	// Pattern: feature/PROJ-123-description or bugfix/PROJ-123-foo
	if branchName != "" {
		matches := branchJiraRefRe.FindAllStringSubmatch(branchName, -1)
		for _, m := range matches {
			addRef(m[1])
		}
	}

	// Extract from PR description.
	if prDescription != "" {
		// Keyword-linked GitHub references (fixes #42, closes #101).
		matches := keywordRefRe.FindAllStringSubmatch(prDescription, -1)
		for _, m := range matches {
			addRef("#" + m[1])
		}

		// Keyword-linked JIRA references (Resolves RHO-99).
		matches = keywordJiraRefRe.FindAllStringSubmatch(prDescription, -1)
		for _, m := range matches {
			addRef(m[1])
		}

		// Standalone JIRA-style references.
		matches = jiraRefRe.FindAllStringSubmatch(prDescription, -1)
		for _, m := range matches {
			addRef(m[1])
		}

		// Standalone GitHub-style references.
		matches = githubRefRe.FindAllStringSubmatch(prDescription, -1)
		for _, m := range matches {
			addRef("#" + m[1])
		}
	}

	return refs
}

// ParseTicket extracts ticket information from raw content.
// It parses title, description, and acceptance criteria from issue body text.
// Acceptance criteria are extracted from:
//   - Checkboxes: "- [ ] criterion"
//   - Numbered lists: "1. criterion"
//   - "should" statements: "The system should ..."
func (tc *TicketCompliance) ParseTicket(content string) *Ticket {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	ticket := &Ticket{}
	lines := strings.Split(content, "\n")

	if len(lines) == 0 {
		return ticket
	}

	// First non-empty line is the title.
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			// Strip leading markdown heading markers.
			ticket.Title = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			lines = lines[i+1:]
			break
		}
	}

	// Collect description and acceptance criteria.
	var descLines []string
	var criteria []string
	var shouldStatements []string
	inCriteria := false
	hasExplicitCriteria := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check if we hit an acceptance criteria section header.
		if acceptanceHeaderRe.MatchString(trimmed) {
			inCriteria = true
			hasExplicitCriteria = true
			continue
		}

		// Extract checkboxes anywhere in the content.
		if m := acceptanceCheckboxRe.FindStringSubmatch(line); m != nil {
			criteria = append(criteria, strings.TrimSpace(m[1]))
			inCriteria = true
			hasExplicitCriteria = true
			continue
		}

		// If we're in a criteria section, extract numbered lists.
		if inCriteria {
			if m := acceptanceNumberedRe.FindStringSubmatch(line); m != nil {
				criteria = append(criteria, strings.TrimSpace(m[1]))
				continue
			}
			// End of criteria section if we hit a blank line after some criteria.
			if trimmed == "" && len(criteria) > 0 {
				inCriteria = false
			}
			continue
		}

		// Collect "should" statements from description as fallback criteria.
		if acceptanceShouldRe.MatchString(line) {
			shouldStatements = append(shouldStatements, strings.TrimSpace(trimmed))
		}

		descLines = append(descLines, line)
	}

	// Only use "should" statements as criteria if there are no explicit criteria.
	if !hasExplicitCriteria && len(criteria) == 0 {
		criteria = shouldStatements
	}

	ticket.Description = strings.TrimSpace(strings.Join(descLines, "\n"))
	ticket.AcceptanceCriteria = criteria

	return ticket
}

// CheckCompliance verifies whether the diff and commit messages satisfy ticket criteria.
// For each acceptance criterion, it checks if the diff or commits address it using
// keyword matching.
func (tc *TicketCompliance) CheckCompliance(ticket *Ticket, diff, commitMessages string) *ComplianceResult {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	result := &ComplianceResult{
		Ticket: ticket,
	}

	if len(ticket.AcceptanceCriteria) == 0 {
		result.Score = 1.0
		result.Suggestions = append(result.Suggestions, "No acceptance criteria defined in ticket")
		return result
	}

	// Combine diff and commits as searchable corpus in lowercase.
	corpus := strings.ToLower(diff + "\n" + commitMessages)

	for _, criterion := range ticket.AcceptanceCriteria {
		if criterionSatisfied(criterion, corpus) {
			result.Satisfied = append(result.Satisfied, criterion)
		} else {
			result.Unsatisfied = append(result.Unsatisfied, criterion)
		}
	}

	total := len(ticket.AcceptanceCriteria)
	if total > 0 {
		result.Score = float64(len(result.Satisfied)) / float64(total)
	}

	// Generate suggestions for unsatisfied criteria.
	for _, criterion := range result.Unsatisfied {
		suggestion := generateSuggestion(criterion)
		if suggestion != "" {
			result.Suggestions = append(result.Suggestions, suggestion)
		}
	}

	return result
}

// criterionSatisfied checks if a criterion is addressed by the corpus using keyword matching.
func criterionSatisfied(criterion, corpus string) bool {
	// Extract meaningful keywords from the criterion (skip short/common words).
	words := extractKeywords(criterion)

	if len(words) == 0 {
		return false
	}

	// A criterion is considered satisfied if a significant portion of its
	// keywords appear in the corpus.
	matchCount := 0
	for _, word := range words {
		if strings.Contains(corpus, word) {
			matchCount++
		}
	}

	// Require at least 50% of keywords to match.
	threshold := float64(len(words)) * 0.5
	if threshold < 1.0 {
		threshold = 1.0
	}

	return float64(matchCount) >= threshold
}

// extractKeywords splits a criterion into meaningful lowercase keywords,
// filtering out common stop words and short words.
func extractKeywords(text string) []string {
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "shall": true, "can": true, "need": true,
		"must": true, "to": true, "of": true, "in": true, "for": true,
		"on": true, "with": true, "at": true, "by": true, "from": true,
		"that": true, "this": true, "it": true, "and": true, "or": true,
		"but": true, "not": true, "no": true, "all": true, "each": true,
		"every": true, "any": true, "both": true, "few": true, "more": true,
		"most": true, "other": true, "some": true, "such": true, "than": true,
		"too": true, "very": true, "just": true, "also": true, "when": true,
		"where": true, "how": true, "what": true, "which": true, "who": true,
		"whom": true, "there": true, "here": true, "then": true, "so": true,
		"if": true, "because": true, "as": true, "until": true, "while": true,
		"about": true, "between": true, "through": true, "during": true,
		"before": true, "after": true, "above": true, "below": true, "up": true,
		"down": true, "out": true, "off": true, "over": true, "under": true,
		"again": true, "further": true, "once": true, "into": true, "add": true,
		"added": true, "ensure": true, "implement": true, "create": true,
	}

	// Split on non-alphanumeric characters.
	parts := keywordSplitterRe.Split(strings.ToLower(text), -1)

	var keywords []string
	for _, p := range parts {
		if len(p) < 3 {
			continue
		}
		if stopWords[p] {
			continue
		}
		keywords = append(keywords, p)
	}

	return keywords
}

// generateSuggestion creates an actionable suggestion for an unsatisfied criterion.
func generateSuggestion(criterion string) string {
	lower := strings.ToLower(criterion)

	switch {
	case strings.Contains(lower, "test"):
		return "Add tests to cover: " + criterion
	case strings.Contains(lower, "document") || strings.Contains(lower, "documentation"):
		return "Add documentation for: " + criterion
	case strings.Contains(lower, "error") || strings.Contains(lower, "validation"):
		return "Implement error handling/validation for: " + criterion
	case strings.Contains(lower, "log") || strings.Contains(lower, "logging"):
		return "Add logging for: " + criterion
	default:
		return "Address missing requirement: " + criterion
	}
}

// FormatComplianceResult formats a ComplianceResult into a human-readable report.
func FormatComplianceResult(result *ComplianceResult) string {
	var sb strings.Builder

	ticketID := result.Ticket.ID
	if ticketID == "" {
		ticketID = "UNKNOWN"
	}
	title := result.Ticket.Title
	if title == "" {
		title = "Untitled"
	}

	header := fmt.Sprintf("Ticket Compliance: %s %q", ticketID, title)
	sb.WriteString(header)
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("═", len(header)))
	sb.WriteString("\n")

	total := len(result.Satisfied) + len(result.Unsatisfied)
	percentage := int(result.Score * 100)
	sb.WriteString(fmt.Sprintf("Score: %d%% (%d/%d criteria met)\n", percentage, len(result.Satisfied), total))
	sb.WriteString("\n")

	for _, s := range result.Satisfied {
		sb.WriteString(fmt.Sprintf(icons.CheckBold()+" %s\n", s))
	}
	for _, u := range result.Unsatisfied {
		sb.WriteString(fmt.Sprintf(icons.CloseThick()+" %s\n", u))
	}

	if len(result.Suggestions) > 0 {
		sb.WriteString("\nSuggestions:\n")
		for _, suggestion := range result.Suggestions {
			sb.WriteString(fmt.Sprintf("- %s\n", suggestion))
		}
	}

	return sb.String()
}

// TicketComplianceTool implements the Tool interface for ticket compliance checking.
type TicketComplianceTool struct{}

// Name returns the tool name.
func (t *TicketComplianceTool) Name() string {
	return "ticket_compliance"
}

// Description returns the tool description.
func (t *TicketComplianceTool) Description() string {
	return "Verify that PR changes satisfy linked issue/ticket requirements. Extracts ticket references, parses acceptance criteria, and checks compliance against diffs and commit messages."
}

// Parameters returns the JSON schema for the tool's input.
// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (t *TicketComplianceTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"branch_name":     {Type: "string", Description: "The current branch name to extract ticket references from"},
			"pr_description":  {Type: "string", Description: "The PR description/body text to extract ticket references from"},
			"ticket_content":  {Type: "string", Description: "The raw ticket/issue content including title, description, and acceptance criteria"},
			"ticket_id":       {Type: "string", Description: "The ticket/issue identifier (e.g., RHO-123, #42)"},
			"ticket_source":   {Type: "string", Enum: []interface{}{"github", "jira", "linear"}, Description: "The ticket source system: github, jira, or linear"},
			"diff":            {Type: "string", Description: "The PR diff to check against ticket requirements"},
			"commit_messages": {Type: "string", Description: "The commit messages in the PR"},
		},
		Required: []string{"ticket_content", "diff"},
	}
}

func (t *TicketComplianceTool) Parameters() map[string]interface{} {
	return ticketComplianceSchema.ToJSONSchema()
}

// ticketComplianceSchema is the single source of truth for TicketCompliance's input schema.
var ticketComplianceSchema = (&TicketComplianceTool{}).Schema()

// TicketComplianceInput is the typed input for TicketComplianceTool.
type TicketComplianceInput struct {
	BranchName    string `json:"branch_name"`
	PRDescription string `json:"pr_description"`
	TicketContent string `json:"ticket_content"`
	TicketID      string `json:"ticket_id"`
	TicketSource  string `json:"ticket_source"`
	Diff          string `json:"diff"`
	CommitMsgs    string `json:"commit_messages"`
}

// Execute runs the ticket compliance tool.
func (t *TicketComplianceTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	params, err := DecodeInput[TicketComplianceInput]("TicketCompliance", input)
	if err != nil {
		return "", err
	}

	if params.TicketContent == "" {
		return "", fmt.Errorf("ticket_content is required")
	}
	if params.Diff == "" {
		return "", fmt.Errorf("diff is required")
	}

	tc := NewTicketCompliance()

	// Extract ticket references if not explicitly provided.
	refs := tc.ExtractTicketRef(params.BranchName, params.PRDescription)

	// Parse ticket.
	ticket := tc.ParseTicket(params.TicketContent)
	if params.TicketID != "" {
		ticket.ID = params.TicketID
	} else if len(refs) > 0 {
		ticket.ID = refs[0]
	}
	if params.TicketSource != "" {
		ticket.Source = params.TicketSource
	}

	// Check compliance.
	result := tc.CheckCompliance(ticket, params.Diff, params.CommitMsgs)

	// Format the result.
	output := FormatComplianceResult(result)

	// Append extracted references if any.
	if len(refs) > 0 {
		output += fmt.Sprintf("\nLinked tickets: %s\n", strings.Join(refs, ", "))
	}

	return output, nil
}
