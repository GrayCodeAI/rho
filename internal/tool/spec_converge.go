package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GrayCodeAI/rho/internal/spec"
)

// ConvergeTool assesses the gap between the active spec and the current
// codebase state. It checks for incomplete tasks, missing implementations,
// and unresolved requirements, then optionally appends convergence tasks.
type ConvergeTool struct{}

func (ConvergeTool) Name() string { return "Converge" }

// ConvergeInput is the typed input for ConvergeTool.
type ConvergeInput struct {
	AppendTasks bool `json:"append_tasks"`
}

func (ConvergeTool) Aliases() []string { return []string{"converge", "spec_converge", "spec:converge"} }

func (ConvergeTool) Description() string {
	return "Assess the gap between the active spec and the codebase. Checks incomplete tasks, missing REQ coverage, orphan citations, and constitution compliance. Optionally appends convergence tasks to tasks.md."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (ConvergeTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"append_tasks": {Type: "boolean", Description: "If true, append convergence tasks to tasks.md for remaining work"},
		},
	}
}

func (ConvergeTool) Parameters() map[string]interface{} {
	return convergeSchema.ToJSONSchema()
}

// convergeSchema is the single source of truth for Converge's input schema.
var convergeSchema = ConvergeTool{}.Schema()

func (ConvergeTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var p ConvergeInput
	if len(input) > 0 && string(input) != "null" {
		decoded, err := DecodeInput[ConvergeInput]("Converge", input)
		if err != nil {
			return "", err
		}
		p = decoded
	}

	slug, err := specSlug(ctx)
	if err != nil || slug == "" {
		return "", fmt.Errorf("no active spec — call Specify first")
	}

	// Use the existing convergence assessment from spec engine
	report := assessConvergence(slug)

	var b strings.Builder
	if report.Converged {
		b.WriteString("+ Implementation is converged with the spec.\n\n")
		b.WriteString(report.Summary)
	} else {
		fmt.Fprintf(&b, "Found %d gap(s) between spec and implementation:\n\n", len(report.Gaps))
		for i, gap := range report.Gaps {
			icon := "i"
			switch gap.Severity {
			case "critical":
				icon = "x"
			case "high":
				icon = "!"
			case "medium":
				icon = "○"
			}
			fmt.Fprintf(&b, "%d. %s [%s] %s\n", i+1, icon, gap.Severity, gap.Description)
			if gap.Source != "" {
				fmt.Fprintf(&b, "   Source: %s\n", gap.Source)
			}
		}
		b.WriteString("\n")
		b.WriteString(report.Summary)

		if p.AppendTasks {
			tasksPath, err := appendConvergenceTasks(slug, report)
			if err != nil {
				b.WriteString(fmt.Sprintf("\n\nFailed to append tasks: %v", err))
			} else {
				b.WriteString(fmt.Sprintf("\n\nConvergence tasks appended to %s", tasksPath))
			}
		}
	}

	return strings.TrimSpace(b.String()), nil
}

type convergenceGap struct {
	Description string
	Category    string
	Severity    string
	Source      string
}

type convergenceResult struct {
	Converged bool
	Gaps      []convergenceGap
	Summary   string
}

func assessConvergence(slug string) convergenceResult {
	dir, err := specsDir()
	if err != nil {
		return convergenceResult{
			Summary: fmt.Sprintf("cannot access specs: %v", err),
		}
	}
	specDir := filepath.Join(dir, slug)

	result := convergenceResult{Converged: true}

	// Read spec
	specContent := readArtifact(filepath.Join(specDir, "spec.md"))
	if specContent == "" {
		result.Gaps = append(result.Gaps, convergenceGap{
			Description: "No spec.md found — cannot assess convergence",
			Category:    "missing",
			Severity:    "critical",
			Source:      "spec.md",
		})
		result.Converged = false
	}

	// Read tasks
	tasksContent := readArtifact(filepath.Join(specDir, "tasks.md"))
	if tasksContent != "" {
		incomplete := countUnchecked(tasksContent)
		if incomplete > 0 {
			result.Gaps = append(result.Gaps, convergenceGap{
				Description: fmt.Sprintf("%d task(s) still incomplete in tasks.md", incomplete),
				Category:    "partial",
				Severity:    "high",
				Source:      "tasks.md",
			})
			result.Converged = false
		}
	}

	// Read plan
	planContent := readArtifact(filepath.Join(specDir, "plan.md"))
	if planContent == "" && specContent != "" {
		result.Gaps = append(result.Gaps, convergenceGap{
			Description: "No plan.md found — technical approach not documented",
			Category:    "missing",
			Severity:    "medium",
			Source:      "plan.md",
		})
	}

	// Check for unresolved clarification markers
	if strings.Contains(specContent, "[NEEDS CLARIFICATION") {
		result.Gaps = append(result.Gaps, convergenceGap{
			Description: "Spec still contains [NEEDS CLARIFICATION] markers",
			Category:    "partial",
			Severity:    "high",
			Source:      "spec.md",
		})
		result.Converged = false
	}

	// Check for SHALL/MUST in spec
	if specContent != "" && !strings.Contains(specContent, "SHALL") && !strings.Contains(specContent, "MUST") {
		result.Gaps = append(result.Gaps, convergenceGap{
			Description: "No SHALL/MUST requirements found — requirements may not be explicit",
			Category:    "partial",
			Severity:    "medium",
			Source:      "spec.md",
		})
	}

	// Check REQ coverage: orphan REQs in code, missing REQ citations
	if specContent != "" {
		cwd, _ := os.Getwd()
		codeFiles := spec.ScanCodeForReqIDs(cwd)
		var allCodeIDs []string
		for _, ids := range codeFiles {
			allCodeIDs = append(allCodeIDs, ids...)
		}
		orphans := spec.FindOrphanReqIDs(allCodeIDs, specContent)
		if len(orphans) > 0 {
			result.Gaps = append(result.Gaps, convergenceGap{
				Description: fmt.Sprintf("%d orphan REQ ID(s) in code not in spec: %s", len(orphans), strings.Join(orphans, ", ")),
				Category:    "hallucination",
				Severity:    "critical",
				Source:      "code",
			})
			result.Converged = false
		}
		missing := spec.FindMissingReqIDs(specContent, codeFiles)
		if len(missing) > 0 {
			result.Gaps = append(result.Gaps, convergenceGap{
				Description: fmt.Sprintf("%d REQ ID(s) in spec not cited in code: %s", len(missing), strings.Join(missing, ", ")),
				Category:    "missing",
				Severity:    "high",
				Source:      "spec.md",
			})
			result.Converged = false
		}
	}

	if result.Converged && len(result.Gaps) == 0 {
		result.Summary = "All requirements addressed, all tasks complete."
	} else {
		result.Summary = fmt.Sprintf("Found %d gap(s) — implementation does not fully satisfy the spec.", len(result.Gaps))
	}

	return result
}

func appendConvergenceTasks(slug string, report convergenceResult) (string, error) {
	if report.Converged {
		return "", nil
	}

	dir, err := specsDir()
	if err != nil {
		return "", err
	}
	tasksPath := filepath.Join(dir, slug, "tasks.md")

	existing := readArtifact(tasksPath)

	var b strings.Builder
	b.WriteString(existing)
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		b.WriteString("\n")
	}

	phaseNum := countPhasesInContent(existing) + 1
	b.WriteString(fmt.Sprintf("\n## %d. Convergence Tasks\n\n", phaseNum))
	b.WriteString("**Purpose**: Address gaps identified in convergence assessment.\n\n")

	taskNum := countTasksInContent(existing) + 1
	for _, gap := range report.Gaps {
		b.WriteString(fmt.Sprintf("- [ ] T%03d %s — %s\n", taskNum, gap.Description, gap.Severity))
		taskNum++
	}

	content := b.String()
	if err := os.WriteFile(tasksPath, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("write convergence tasks: %w", err)
	}

	return tasksPath, nil
}

func readArtifact(path string) string {
	data, err := os.ReadFile(path) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return ""
	}
	return string(data)
}

func countUnchecked(content string) int {
	count := 0
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- [ ]") {
			count++
		}
	}
	return count
}

func countPhasesInContent(content string) int {
	count := 0
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 3 && trimmed[:2] == "##" && trimmed[3] >= '0' && trimmed[3] <= '9' {
			count++
		}
	}
	return count
}

func countTasksInContent(content string) int {
	return countUnchecked(content) + countChecked(content)
}

func countChecked(content string) int {
	count := 0
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- [x]") || strings.HasPrefix(trimmed, "- [X]") {
			count++
		}
	}
	return count
}
