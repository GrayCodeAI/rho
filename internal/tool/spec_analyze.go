package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/GrayCodeAI/rho/internal/spec"
)

// AnalyzeTool performs cross-artifact consistency and quality analysis on the
// active spec's artifacts. It checks for gaps between spec, plan, and tasks,
// identifies orphaned requirements, and reports structural quality issues.
type AnalyzeTool struct{}

func (AnalyzeTool) Name() string      { return "Analyze" }
func (AnalyzeTool) Aliases() []string { return []string{"analyze", "spec_analyze", "spec:analyze"} }
func (AnalyzeTool) Description() string {
	return "Analyze the active spec for cross-artifact consistency: check that spec requirements are covered by plan and tasks, identify orphaned work, and report quality issues. Read-only analysis — does not modify any files."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (AnalyzeTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
	}
}

func (AnalyzeTool) Parameters() map[string]interface{} {
	return analyzeSchema.ToJSONSchema()
}

// analyzeSchema is the single source of truth for Analyze's input schema.
var analyzeSchema = AnalyzeTool{}.Schema()

func (AnalyzeTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	dir, err := specDir(ctx)
	if err != nil {
		return "", err
	}

	specContent := readFileStr(filepath.Join(dir, "spec.md"))
	planContent := readFileStr(filepath.Join(dir, "plan.md"))
	tasksContent := readFileStr(filepath.Join(dir, "tasks.md"))

	if specContent == "" && planContent == "" && tasksContent == "" {
		return "No spec artifacts found. Use Specify, Plan, and Tasks first.", nil
	}

	report := analyzeCrossArtifact(specContent, planContent, tasksContent)
	return formatAnalysisReport(report), nil
}

type analysisReport struct {
	QualityScore int              `json:"quality_score"` // 0-100
	Issues       []analysisIssue  `json:"issues"`
	Coverage     analysisCoverage `json:"coverage"`
	Summary      string           `json:"summary"`
}

type analysisIssue struct {
	Severity string `json:"severity"` // critical, warning, info
	Category string `json:"category"`
	Message  string `json:"message"`
}

type analysisCoverage struct {
	SpecRequirements   int `json:"spec_requirements"`
	PlanDecisions      int `json:"plan_decisions"`
	TasksTotal         int `json:"tasks_total"`
	TasksComplete      int `json:"tasks_complete"`
	OrphanedReqs       int `json:"orphaned_requirements"` // reqs with no plan decision
	UntrackedDecisions int `json:"untracked_decisions"`   // plan decisions with no task
}

func analyzeCrossArtifact(spec, plan, tasks string) analysisReport {
	report := analysisReport{QualityScore: 100}

	// Extract requirements from spec
	specReqs := extractRequirements(spec)
	planDecisions := extractDecisions(plan)
	tasksTotal, tasksComplete := countTaskStats(tasks)

	report.Coverage = analysisCoverage{
		SpecRequirements: len(specReqs),
		PlanDecisions:    len(planDecisions),
		TasksTotal:       tasksTotal,
		TasksComplete:    tasksComplete,
	}

	// Check spec quality
	if spec != "" {
		validateSpecQuality(spec, &report)
	}

	// Check plan quality
	if plan != "" {
		validatePlanQuality(plan, &report)
	}

	// Check tasks quality
	if tasks != "" {
		validateTasksQuality(tasks, &report)
	}

	// Cross-artifact checks
	if spec != "" && plan != "" {
		checkSpecPlanConsistency(specReqs, planDecisions, &report)
	}
	if plan != "" && tasks != "" {
		checkPlanTasksConsistency(planDecisions, tasks, &report)
	}
	if spec != "" && tasks != "" {
		checkSpecTasksConsistency(specReqs, tasks, &report)
	}

	// Orphan REQ detection (code cites REQ not in spec = hallucination)
	if spec != "" {
		checkOrphanReqs(spec, &report)
	}

	// Missing artifacts
	if spec == "" {
		report.Issues = append(report.Issues, analysisIssue{
			Severity: "critical", Category: "Missing Artifact",
			Message: "No spec.md found — requirements are not documented",
		})
		report.QualityScore -= 30
	}
	if plan == "" && spec != "" {
		report.Issues = append(report.Issues, analysisIssue{
			Severity: "warning", Category: "Missing Artifact",
			Message: "No plan.md found — technical approach is not documented",
		})
		report.QualityScore -= 15
	}
	if tasks == "" && spec != "" {
		report.Issues = append(report.Issues, analysisIssue{
			Severity: "warning", Category: "Missing Artifact",
			Message: "No tasks.md found — implementation breakdown is not documented",
		})
		report.QualityScore -= 15
	}

	if report.QualityScore < 0 {
		report.QualityScore = 0
	}

	// Generate summary
	criticalCount, warningCount, infoCount := 0, 0, 0
	for _, iss := range report.Issues {
		switch iss.Severity {
		case "critical":
			criticalCount++
		case "warning":
			warningCount++
		case "info":
			infoCount++
		}
	}
	report.Summary = fmt.Sprintf("Quality score: %d/100 | %d critical, %d warning, %d info | %d requirements, %d plan decisions, %d tasks (%d done)",
		report.QualityScore, criticalCount, warningCount, infoCount,
		report.Coverage.SpecRequirements, report.Coverage.PlanDecisions,
		report.Coverage.TasksTotal, report.Coverage.TasksComplete)

	return report
}

func extractRequirements(spec string) []string {
	if spec == "" {
		return nil
	}
	re := regexp.MustCompile(`(?m)^###?\s+Requirement:\s*(.+)$`)
	matches := re.FindAllStringSubmatch(spec, -1)
	var reqs []string
	for _, m := range matches {
		reqs = append(reqs, strings.TrimSpace(m[1]))
	}
	return reqs
}

func extractDecisions(plan string) []string {
	if plan == "" {
		return nil
	}
	re := regexp.MustCompile(`(?m)^###?\s+(Decision|Approach|Architecture|Design)[:\s]*(.+)$`)
	matches := re.FindAllStringSubmatch(plan, -1)
	var decisions []string
	for _, m := range matches {
		decisions = append(decisions, strings.TrimSpace(m[2]))
	}
	return decisions
}

func countTaskStats(tasks string) (total, complete int) {
	if tasks == "" {
		return 0, 0
	}
	total = len(regexp.MustCompile(`(?m)^- \[[ x]\]`).FindAllString(tasks, -1))
	complete = len(regexp.MustCompile(`(?m)^- \[x\]`).FindAllString(tasks, -1))
	return
}

func validateSpecQuality(spec string, report *analysisReport) {
	if !strings.Contains(spec, "SHALL") && !strings.Contains(spec, "MUST") {
		report.Issues = append(report.Issues, analysisIssue{
			Severity: "warning", Category: "Spec Quality",
			Message: "No SHALL/MUST keywords — requirements may not be normative enough",
		})
		report.QualityScore -= 5
	}

	reScenario := regexp.MustCompile(`(?m)^#{2,4}\s+Scenario:`)
	scenarios := reScenario.FindAllString(spec, -1)
	if len(scenarios) == 0 {
		report.Issues = append(report.Issues, analysisIssue{
			Severity: "warning", Category: "Spec Quality",
			Message: "No test scenarios found — add WHEN/THEN scenarios for testability",
		})
		report.QualityScore -= 10
	}

	reqs := extractRequirements(spec)
	if len(reqs) > 0 && len(scenarios) < len(reqs) {
		report.Issues = append(report.Issues, analysisIssue{
			Severity: "info", Category: "Spec Quality",
			Message: fmt.Sprintf("Only %d scenario(s) for %d requirement(s) — consider adding more scenarios", len(scenarios), len(reqs)),
		})
		report.QualityScore -= 3
	}
}

func validatePlanQuality(plan string, report *analysisReport) {
	lower := strings.ToLower(plan)
	if !strings.Contains(lower, "decision") && !strings.Contains(lower, "approach") {
		report.Issues = append(report.Issues, analysisIssue{
			Severity: "info", Category: "Plan Quality",
			Message: "No explicit decisions section — consider documenting key technical choices",
		})
		report.QualityScore -= 3
	}

	if !strings.Contains(lower, "risk") && !strings.Contains(lower, "trade-off") {
		report.Issues = append(report.Issues, analysisIssue{
			Severity: "info", Category: "Plan Quality",
			Message: "No risks or trade-offs documented",
		})
		report.QualityScore -= 2
	}
}

func validateTasksQuality(tasks string, report *analysisReport) {
	if !strings.Contains(tasks, "- [ ]") && !strings.Contains(tasks, "- [x]") {
		report.Issues = append(report.Issues, analysisIssue{
			Severity: "warning", Category: "Tasks Quality",
			Message: "Tasks should use checkbox format (- [ ] / - [x]) for trackable progress",
		})
		report.QualityScore -= 5
	}

	rePhase := regexp.MustCompile(`(?m)^##\s+\d+\.`)
	phases := rePhase.FindAllString(tasks, -1)
	if len(phases) == 0 {
		report.Issues = append(report.Issues, analysisIssue{
			Severity: "info", Category: "Tasks Quality",
			Message: "Consider organizing tasks under numbered phase headings",
		})
		report.QualityScore -= 2
	}
}

func checkSpecPlanConsistency(reqs, decisions []string, report *analysisReport) {
	if len(reqs) == 0 || len(decisions) == 0 {
		return
	}
	// Simple heuristic: if we have many requirements but few decisions, flag it
	ratio := float64(len(decisions)) / float64(len(reqs))
	if ratio < 0.3 && len(reqs) > 3 {
		report.Issues = append(report.Issues, analysisIssue{
			Severity: "warning", Category: "Coverage",
			Message: fmt.Sprintf("Only %d plan decision(s) for %d spec requirement(s) — plan may be incomplete", len(decisions), len(reqs)),
		})
		report.QualityScore -= 5
	}
}

func checkPlanTasksConsistency(decisions []string, tasks string, report *analysisReport) {
	if len(decisions) == 0 {
		return
	}
	total, _ := countTaskStats(tasks)
	if total == 0 && len(decisions) > 0 {
		report.Issues = append(report.Issues, analysisIssue{
			Severity: "warning", Category: "Coverage",
			Message: "Plan has decisions but tasks.md has no implementation tasks",
		})
		report.QualityScore -= 5
	}
}

func checkSpecTasksConsistency(reqs []string, tasks string, report *analysisReport) {
	if len(reqs) == 0 {
		return
	}

	// Check if requirement names appear in tasks
	untracked := 0
	for _, req := range reqs {
		if !strings.Contains(tasks, req) && !strings.Contains(strings.ToLower(tasks), strings.ToLower(req)) {
			untracked++
		}
	}

	if untracked > 0 {
		report.Coverage.OrphanedReqs = untracked
		report.Issues = append(report.Issues, analysisIssue{
			Severity: "warning", Category: "Traceability",
			Message: fmt.Sprintf("%d requirement(s) not referenced in tasks.md — may be unimplemented", untracked),
		})
		report.QualityScore -= 5
	}
}

func formatAnalysisReport(report analysisReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", report.Summary)

	if len(report.Issues) == 0 {
		b.WriteString("No issues found.\n")
		return strings.TrimSpace(b.String())
	}

	// Sort by severity
	sort.Slice(report.Issues, func(i, j int) bool {
		order := map[string]int{"critical": 0, "warning": 1, "info": 2}
		return order[report.Issues[i].Severity] < order[report.Issues[j].Severity]
	})

	b.WriteString("Issues:\n")
	for _, iss := range report.Issues {
		icon := "i"
		switch iss.Severity {
		case "critical":
			icon = "x"
		case "warning":
			icon = "!"
		}
		fmt.Fprintf(&b, "  %s [%s] %s: %s\n", icon, iss.Severity, iss.Category, iss.Message)
	}

	return strings.TrimSpace(b.String())
}

func readFileStr(path string) string {
	data, err := os.ReadFile(path) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return ""
	}
	return string(data)
}

func checkOrphanReqs(specContent string, report *analysisReport) {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	codeFiles := spec.ScanCodeForReqIDs(cwd)
	if len(codeFiles) == 0 {
		return
	}
	var allCodeIDs []string
	for _, ids := range codeFiles {
		allCodeIDs = append(allCodeIDs, ids...)
	}
	orphans := spec.FindOrphanReqIDs(allCodeIDs, specContent)
	if len(orphans) > 0 {
		report.Issues = append(report.Issues, analysisIssue{
			Severity: "critical", Category: "Hallucination Detection",
			Message: fmt.Sprintf("%d orphan REQ ID(s) in code not found in spec: %s — possible hallucination", len(orphans), strings.Join(orphans, ", ")),
		})
		report.QualityScore -= 15
	}
	missing := spec.FindMissingReqIDs(specContent, codeFiles)
	if len(missing) > 0 {
		report.Issues = append(report.Issues, analysisIssue{
			Severity: "warning", Category: "Traceability",
			Message: fmt.Sprintf("%d REQ ID(s) in spec not cited in code: %s — unimplemented or missing citation", len(missing), strings.Join(missing, ", ")),
		})
		report.QualityScore -= 5
	}
}
