package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ChecklistTool generates QA checklists from the active spec's requirements
// and scenarios. Checklists are derived from spec content and can be used
// to track verification of each requirement.
type ChecklistTool struct{}

func (ChecklistTool) Name() string { return "Checklist" }

// ChecklistInput is the typed input for ChecklistTool.
type ChecklistInput struct {
	IncludeReferences bool   `json:"include_references"`
	Artifact          string `json:"artifact"`
}

func (ChecklistTool) Aliases() []string {
	return []string{"checklist", "spec_checklist", "spec:checklist"}
}

func (ChecklistTool) Description() string {
	return "Generate a QA checklist from the active spec's requirements and scenarios. Each requirement becomes a checkable item. Optionally include reference checklists for accessibility, security, performance, observability, and testing."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (ChecklistTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"include_references": {Type: "boolean", Description: "If true, append reference checklists (accessibility, security, performance, observability, testing) alongside spec-derived checks"},
			"artifact":           {Type: "string", Enum: []interface{}{"spec.md", "tasks.md"}, Description: "Which artifact to generate checklist from: spec.md (default) or tasks.md"},
		},
	}
}

func (ChecklistTool) Parameters() map[string]interface{} {
	return checklistSchema.ToJSONSchema()
}

// checklistSchema is the single source of truth for Checklist's input schema.
var checklistSchema = ChecklistTool{}.Schema()

func (ChecklistTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var p ChecklistInput
	if len(input) > 0 && string(input) != "null" {
		decoded, err := DecodeInput[ChecklistInput]("Checklist", input)
		if err != nil {
			return "", err
		}
		p = decoded
	}
	if p.Artifact == "" {
		p.Artifact = "spec.md"
	}

	dir, err := specDir(ctx)
	if err != nil {
		return "", err
	}

	path := filepath.Join(dir, p.Artifact)
	data, err := os.ReadFile(path) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", p.Artifact, err)
	}
	content := string(data)
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("%s is empty", p.Artifact)
	}

	var b strings.Builder

	if p.Artifact == "spec.md" {
		checklist := generateSpecChecklist(content)
		b.WriteString("# Spec QA Checklist\n\n")
		b.WriteString(fmt.Sprintf("Generated from %s\n\n", p.Artifact))
		b.WriteString("## Requirements Verification\n\n")
		for _, item := range checklist {
			b.WriteString(fmt.Sprintf("- [ ] %s\n", item))
		}

		// Check for scenarios
		scenarios := extractScenarios(content)
		if len(scenarios) > 0 {
			b.WriteString("\n## Scenario Testing\n\n")
			for _, sc := range scenarios {
				b.WriteString(fmt.Sprintf("- [ ] Scenario: %s\n", sc))
				b.WriteString("  - WHEN: verified\n")
				b.WriteString("  - THEN: verified\n")
			}
		}
	} else {
		// tasks.md checklist
		checklist := generateTasksChecklist(content)
		b.WriteString("# Tasks QA Checklist\n\n")
		for _, item := range checklist {
			b.WriteString(fmt.Sprintf("- [ ] %s\n", item))
		}
	}

	if p.IncludeReferences {
		b.WriteString("\n---\n\n")
		b.WriteString("## Reference Checklists\n\n")
		b.WriteString(generateReferenceChecklists())
	}

	// Save checklist to spec dir
	checklistPath := filepath.Join(dir, "checklist.md")
	if err := os.WriteFile(checklistPath, []byte(b.String()), 0o600); err != nil {
		return "", fmt.Errorf("write checklist: %w", err)
	}

	return fmt.Sprintf("Generated checklist at %s\n\n%s", checklistPath, strings.TrimSpace(b.String())), nil
}

// Package-level compiled patterns (M14): checklist generation runs per spec
// file; regexp.MustCompile per call wasted CPU and allocation.
var (
	checklistRequirementRe = regexp.MustCompile(`(?m)^###?\s+Requirement:\s*(.+)$`)
	checklistScenarioRe    = regexp.MustCompile(`(?m)^#{2,4}\s+Scenario:\s*(.+)$`)
	checklistTaskRe        = regexp.MustCompile(`(?m)^- \[ \]\s+(.+)$`)
)

func generateSpecChecklist(content string) []string {
	var items []string

	matches := checklistRequirementRe.FindAllStringSubmatch(content, -1)

	for _, m := range matches {
		reqName := strings.TrimSpace(m[1])
		items = append(items, fmt.Sprintf("Requirement implemented: %s", reqName))
		items = append(items, fmt.Sprintf("Requirement tested: %s", reqName))
		items = append(items, fmt.Sprintf("Requirement documented: %s", reqName))
	}

	if len(items) == 0 {
		items = append(items, "No requirements found — consider adding ### Requirement: headers")
	}

	return items
}

func extractScenarios(content string) []string {
	matches := checklistScenarioRe.FindAllStringSubmatch(content, -1)
	var scenarios []string
	for _, m := range matches {
		scenarios = append(scenarios, strings.TrimSpace(m[1]))
	}
	return scenarios
}

func generateTasksChecklist(content string) []string {
	var items []string

	matches := checklistTaskRe.FindAllStringSubmatch(content, -1)

	for _, m := range matches {
		items = append(items, strings.TrimSpace(m[1]))
	}

	if len(items) == 0 {
		items = append(items, "No unchecked tasks found")
	}

	return items
}

func generateReferenceChecklists() string {
	var b strings.Builder

	b.WriteString("### Accessibility Checklist\n")
	b.WriteString("- [ ] Keyboard navigation works for all interactive elements\n")
	b.WriteString("- [ ] Screen reader compatible (ARIA labels, alt text)\n")
	b.WriteString("- [ ] Color contrast meets WCAG AA (4.5:1 ratio)\n")
	b.WriteString("- [ ] Focus indicators visible\n")
	b.WriteString("- [ ] Form inputs have associated labels\n")
	b.WriteString("- [ ] Error messages are accessible\n\n")

	b.WriteString("### Security Checklist\n")
	b.WriteString("- [ ] Input validation on all user inputs\n")
	b.WriteString("- [ ] SQL injection prevention (parameterized queries)\n")
	b.WriteString("- [ ] XSS prevention (output encoding)\n")
	b.WriteString("- [ ] CSRF protection on state-changing operations\n")
	b.WriteString("- [ ] Authentication on protected endpoints\n")
	b.WriteString("- [ ] Authorization checks (least privilege)\n")
	b.WriteString("- [ ] Secrets not in code or logs\n")
	b.WriteString("- [ ] Rate limiting on API endpoints\n\n")

	b.WriteString("### Performance Checklist\n")
	b.WriteString("- [ ] Database queries optimized (indexes, N+1 prevention)\n")
	b.WriteString("- [ ] Caching strategy implemented\n")
	b.WriteString("- [ ] Response size reasonable\n")
	b.WriteString("- [ ] Lazy loading for heavy resources\n")
	b.WriteString("- [ ] No blocking operations in hot paths\n")
	b.WriteString("- [ ] Memory usage within bounds\n\n")

	b.WriteString("### Observability Checklist\n")
	b.WriteString("- [ ] Structured logging with context\n")
	b.WriteString("- [ ] Error tracking configured\n")
	b.WriteString("- [ ] Metrics for key operations\n")
	b.WriteString("- [ ] Distributed tracing (if applicable)\n")
	b.WriteString("- [ ] Health check endpoint\n")
	b.WriteString("- [ ] Alerting on critical failures\n\n")

	b.WriteString("### Testing Checklist\n")
	b.WriteString("- [ ] Unit tests for core logic\n")
	b.WriteString("- [ ] Integration tests for API endpoints\n")
	b.WriteString("- [ ] Edge cases covered\n")
	b.WriteString("- [ ] Error paths tested\n")
	b.WriteString("- [ ] Race conditions tested (if concurrent)\n")
	b.WriteString("- [ ] Test coverage meets threshold\n")

	return b.String()
}
