package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/GrayCodeAI/rho/internal/spec"
)

var (
	reSlugInvalid = regexp.MustCompile(`[^a-z0-9]+`)
	reSpecSlug    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = reSlugInvalid.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 40 {
		s = s[:40]
	}
	if s == "" {
		s = "spec"
	}
	return s
}

// specSlug reads the active spec slug via the per-session ToolContext
// closures. Session-scoped (not a package-level variable) so concurrent
// sessions/sub-agents in the same process never share or clobber each
// other's spec directory.
func specSlug(ctx context.Context) (string, error) {
	tc := GetToolContext(ctx)
	if tc == nil || tc.SpecSlugGet == nil {
		return "", fmt.Errorf("no active session context for spec workflow")
	}
	return tc.SpecSlugGet(), nil
}

func setSpecSlug(ctx context.Context, slug string) error {
	tc := GetToolContext(ctx)
	if tc == nil || tc.SpecSlugSet == nil {
		return fmt.Errorf("no active session context for spec workflow")
	}
	tc.SpecSlugSet(slug)
	return nil
}

func specDir(ctx context.Context) (string, error) {
	slug, err := specSlug(ctx)
	if err != nil {
		return "", err
	}
	if slug == "" {
		return "", fmt.Errorf("no active spec — call Specify first")
	}
	if !reSpecSlug.MatchString(slug) {
		return "", fmt.Errorf("invalid active spec slug")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, ".rho", "specs", slug), nil
}

func writeSpecArtifact(ctx context.Context, filename, content string) (string, error) {
	dir, err := specDir(ctx)
	if err != nil {
		return "", err
	}
	return writeSpecArtifactInDir(dir, filename, content)
}

func writeSpecArtifactForSlug(ctx context.Context, slug, filename, content string) (string, error) {
	if !reSpecSlug.MatchString(slug) {
		return "", fmt.Errorf("spec slug is required")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return writeSpecArtifactInDir(filepath.Join(cwd, ".rho", "specs", slug), filename, content)
}

func writeSpecArtifactInDir(dir, filename, content string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", filename, err)
	}
	return path, nil
}

// SpecifyTool writes spec.md — WHAT the system should do. Call after
// Proposal; can run in parallel with Design.
type SpecifyTool struct{}

func (SpecifyTool) Name() string { return "Specify" }

// SpecifyInput is the typed input for SpecifyTool.
type SpecifyInput struct {
	Title string `json:"title"`
	Spec  string `json:"spec"`
}

func (SpecifyTool) Aliases() []string { return []string{"specify"} }
func (SpecifyTool) Description() string {
	return "Write spec.md describing requirements and constraints. Call after Proposal, can run in parallel with Design. Write/Edit/Bash stay blocked until ApproveImplementation."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecifyTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"spec": {Type: "string", Description: "The spec content: problem statement, requirements, constraints"},
		},

		Required: []string{"spec"},
	}
}

func (SpecifyTool) Parameters() map[string]interface{} {
	return specifySchema.ToJSONSchema()
}

// specifySchema is the single source of truth for Specify's input schema.
var specifySchema = SpecifyTool{}.Schema()

func (SpecifyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[SpecifyInput]("Specify", input)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(p.Spec) == "" {
		return "", fmt.Errorf("spec is required")
	}
	slug, _ := specSlug(ctx)
	if slug == "" {
		slug = slugify(p.Title)
		if slug == "spec" {
			slug = slugify(firstLine(p.Spec))
		}
		slug = fmt.Sprintf("%s-%d", slug, time.Now().Unix())
		if err := setSpecSlug(ctx, slug); err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Join(".rho", "specs", slug), 0o700); err != nil {
			return "", fmt.Errorf("mkdir: %w", err)
		}
	}
	path, err := writeSpecArtifact(ctx, "spec.md", p.Spec)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Wrote %s. Next, call Plan (after both Specify and Design are complete).", path), nil
}

// PlanTool writes plan.md — the implementation plan. Requires both
// Specify and Design to be complete.
type PlanTool struct{}

func (PlanTool) Name() string { return "Plan" }

// PlanInput is the typed input for PlanTool.
type PlanInput struct {
	Plan string `json:"plan"`
}

func (PlanTool) Aliases() []string { return []string{"plan"} }
func (PlanTool) Description() string {
	return "Write plan.md describing the implementation approach. Call after both Specify and Design are complete."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (PlanTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"plan": {Type: "string", Description: "The technical approach: architecture, files to change, key decisions"},
		},

		Required: []string{"plan"},
	}
}

func (PlanTool) Parameters() map[string]interface{} {
	return planSchema.ToJSONSchema()
}

// planSchema is the single source of truth for Plan's input schema.
var planSchema = PlanTool{}.Schema()

func (PlanTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[PlanInput]("Plan", input)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(p.Plan) == "" {
		return "", fmt.Errorf("plan is required")
	}
	path, err := writeSpecArtifact(ctx, "plan.md", p.Plan)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Wrote %s. Next, call Tasks with a breakdown.", path), nil
}

// TasksTool writes tasks.md — the implementation breakdown for an active spec.
type TasksTool struct{}

func (TasksTool) Name() string { return "Tasks" }

// TasksInput is the typed input for TasksTool.
type TasksInput struct {
	Tasks string `json:"tasks"`
}

func (TasksTool) Aliases() []string { return []string{"tasks"} }
func (TasksTool) Description() string {
	return "Write tasks.md breaking the plan into concrete implementation steps. Each task should reference REQ-XXX.Y.Z IDs from the spec. Call after Plan."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (TasksTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"tasks": {Type: "string", Description: "The task breakdown, as a list"},
		},

		Required: []string{"tasks"},
	}
}

func (TasksTool) Parameters() map[string]interface{} {
	return tasksSchema.ToJSONSchema()
}

// tasksSchema is the single source of truth for Tasks's input schema.
var tasksSchema = TasksTool{}.Schema()

func (TasksTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[TasksInput]("Tasks", input)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(p.Tasks) == "" {
		return "", fmt.Errorf("tasks is required")
	}
	path, err := writeSpecArtifact(ctx, "tasks.md", p.Tasks)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Wrote %s. Call ApproveImplementation to ask the user to approve moving to implementation.", path), nil
}

// ApproveImplementationTool requests the user's approval to lift the spec
// gate and unlock Write/Edit/Bash. Unlike Specify/Plan/Tasks, this call
// always goes through a real permission prompt (see
// PermissionEngine.CheckTool) regardless of autonomy tier.
type ApproveImplementationTool struct{}

func (ApproveImplementationTool) Name() string      { return "ApproveImplementation" }
func (ApproveImplementationTool) Aliases() []string { return []string{"approve_implementation"} }
func (ApproveImplementationTool) Description() string {
	return "Ask the user to approve moving from spec to implementation. Only after approval will Write/Edit/Bash be permitted. Call this once spec.md, plan.md, and tasks.md are all written."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (ApproveImplementationTool) Schema() ToolSchema {
	return ToolSchema{
		Type:       "object",
		Properties: map[string]SchemaProperty{},
	}
}

func (ApproveImplementationTool) Parameters() map[string]interface{} {
	return approveImplementationSchema.ToJSONSchema()
}

// approveImplementationSchema is the single source of truth for ApproveImplementation's input schema.
var approveImplementationSchema = ApproveImplementationTool{}.Schema()

func (ApproveImplementationTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	return "Approved. You may now implement the plan and make changes.", nil
}

// SpecStatusTool reports the current spec stage and the status of all spec artifacts.
type SpecStatusTool struct{}

func (SpecStatusTool) Name() string { return "SpecStatus" }

// SpecStatusInput is the typed input for SpecStatusTool.
type SpecStatusInput struct {
	Slug string `json:"slug"`
}

func (SpecStatusTool) Aliases() []string { return []string{"spec_status"} }
func (SpecStatusTool) Description() string {
	return "Show the current spec stage and validation status of spec artifacts (spec.md, plan.md, tasks.md). Use this to check progress in the spec workflow."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecStatusTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"slug": {Type: "string", Description: "Optional: check a specific spec slug instead of the active one"},
		},
	}
}

func (SpecStatusTool) Parameters() map[string]interface{} {
	return specStatusSchema.ToJSONSchema()
}

// specStatusSchema is the single source of truth for SpecStatus's input schema.
var specStatusSchema = SpecStatusTool{}.Schema()

func (SpecStatusTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var p SpecStatusInput
	if len(input) > 0 && string(input) != "null" {
		decoded, err := DecodeInput[SpecStatusInput]("SpecStatus", input)
		if err != nil {
			return "", err
		}
		p = decoded
	}

	var slug string
	if p.Slug != "" {
		slug = p.Slug
	} else {
		var err error
		slug, err = specSlug(ctx)
		if err != nil || slug == "" {
			return "No active spec workflow. Use Specify to start, or check `SpecList` to see existing specs.", nil
		}
	}

	dir, err := specsDir()
	if err != nil {
		return "", err
	}
	specDir := filepath.Join(dir, slug)

	meta := spec.LoadStageMeta(slug)

	var b strings.Builder
	if meta != nil {
		fmt.Fprintf(&b, "Spec: %s\n", meta.Title)
		fmt.Fprintf(&b, "Stage: %s\n", spec.StageEnumDisplayName(meta.Stage))
		if !meta.CreatedAt.IsZero() {
			fmt.Fprintf(&b, "Created: %s\n", meta.CreatedAt.Format(time.RFC3339))
		}
		if !meta.UpdatedAt.IsZero() {
			fmt.Fprintf(&b, "Updated: %s\n", meta.UpdatedAt.Format(time.RFC3339))
		}
		b.WriteString("\n")
	}

	b.WriteString("Artifacts:\n")
	for _, f := range []string{"proposal.md", "spec.md", "design.md", "plan.md", "tasks.md"} {
		path := filepath.Join(specDir, f)
		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(&b, "  %s — missing\n", f)
			continue
		}
		fmt.Fprintf(&b, "  %s — %d bytes, modified %s\n", f, info.Size(), info.ModTime().Format(time.RFC3339))
	}

	b.WriteString("\n")

	// Run validation on existing artifacts
	hasErrors := false
	for _, f := range []string{"spec.md", "plan.md", "tasks.md"} {
		path := filepath.Join(specDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		data, err := os.ReadFile(path) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
		if err != nil {
			continue
		}
		var vr spec.ValidationResult
		switch f {
		case "spec.md":
			vr = spec.ValidateSpec(string(data))
		case "plan.md":
			vr = spec.ValidatePlan(string(data))
		case "tasks.md":
			vr = spec.ValidateTasks(string(data))
		}
		if len(vr.Issues) > 0 {
			hasErrors = true
			fmt.Fprintf(&b, "  %s validation:\n", f)
			for _, iss := range vr.Issues {
				icon := "i"
				switch iss.Level {
				case spec.ValidationError:
					icon = "x"
				case spec.ValidationWarning:
					icon = "!"
				}
				fmt.Fprintf(&b, "    %s [%s] %s\n", icon, iss.Code, iss.Message)
			}
		}
	}
	if !hasErrors {
		b.WriteString("  All artifacts validated clean.\n")
	}

	// Definition of Done check
	b.WriteString("\nDefinition of Done:\n")
	dod := checkDefinitionOfDone(specDir)
	for _, item := range dod {
		icon := "+"
		if !item.Done {
			icon = "x"
		}
		fmt.Fprintf(&b, "  %s %s\n", icon, item.Description)
	}

	return strings.TrimSpace(b.String()), nil
}

type dodItem struct {
	Description string
	Done        bool
}

func checkDefinitionOfDone(specDir string) []dodItem {
	var items []dodItem

	// Check spec.md exists and is non-empty
	specContent := readFileStr(filepath.Join(specDir, "spec.md"))
	items = append(items, dodItem{
		Description: "spec.md written with requirements",
		Done:        specContent != "" && strings.TrimSpace(specContent) != "",
	})

	// Check plan.md exists and is non-empty
	planContent := readFileStr(filepath.Join(specDir, "plan.md"))
	items = append(items, dodItem{
		Description: "plan.md written with technical approach",
		Done:        planContent != "" && strings.TrimSpace(planContent) != "",
	})

	// Check tasks.md exists and is non-empty
	tasksContent := readFileStr(filepath.Join(specDir, "tasks.md"))
	items = append(items, dodItem{
		Description: "tasks.md written with implementation steps",
		Done:        tasksContent != "" && strings.TrimSpace(tasksContent) != "",
	})

	// Check all tasks are complete
	if tasksContent != "" {
		incomplete := countUncheckedTasks(tasksContent)
		items = append(items, dodItem{
			Description: "All tasks marked complete",
			Done:        incomplete == 0,
		})
	}

	// Check spec has SHALL/MUST requirements
	if specContent != "" {
		items = append(items, dodItem{
			Description: "Spec uses normative language (SHALL/MUST)",
			Done:        strings.Contains(specContent, "SHALL") || strings.Contains(specContent, "MUST"),
		})
	}

	// Check spec has test scenarios
	if specContent != "" {
		hasScenarios := strings.Contains(specContent, "#### Scenario:") || strings.Contains(specContent, "### Scenario:")
		items = append(items, dodItem{
			Description: "Spec includes test scenarios",
			Done:        hasScenarios,
		})
	}

	// Check constitution exists
	constPath := filepath.Join(specDir, "constitution.md")
	_, err := os.Stat(constPath)
	items = append(items, dodItem{
		Description: "Project constitution defined",
		Done:        err == nil,
	})

	return items
}

func countUncheckedTasks(content string) int {
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

// SpecEditTool modifies the active spec by applying a delta spec or replacing artifact content.
type SpecEditTool struct{}

func (SpecEditTool) Name() string { return "SpecEdit" }

// SpecEditInput is the typed input for SpecEditTool.
type SpecEditInput struct {
	Artifact string `json:"artifact"`
	Delta    string `json:"delta"`
	Content  string `json:"content"`
}

func (SpecEditTool) Aliases() []string { return []string{"spec_edit"} }
func (SpecEditTool) Description() string {
	return "Edit the active spec: apply a delta (ADDED/MODIFIED/REMOVED/RENAMED requirements) to spec.md, or replace an artifact entirely with new content. Call this to refine requirements without starting over."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecEditTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"artifact": {Type: "string", Enum: []interface{}{"spec.md", "plan.md", "tasks.md", "specs.md"}, Description: "Which file to edit: spec.md, plan.md, tasks.md, or specs.md"},
			"delta":    {Type: "string", Description: "Delta spec content with ## ADDED/MODIFIED/REMOVED/RENAMED Requirements sections. Applies structured changes to the artifact."},
			"content":  {Type: "string", Description: "Full replacement content for the artifact (replaces the entire file). Use this instead of delta for wholesale changes."},
		},

		Required: []string{"artifact"},
	}
}

func (SpecEditTool) Parameters() map[string]interface{} {
	return specEditSchema.ToJSONSchema()
}

// specEditSchema is the single source of truth for SpecEdit's input schema.
var specEditSchema = SpecEditTool{}.Schema()

func (SpecEditTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[SpecEditInput]("SpecEdit", input)
	if err != nil {
		return "", err
	}

	if p.Delta == "" && p.Content == "" {
		return "", fmt.Errorf("spec edit requires either 'delta' or 'content'")
	}

	slug, err := specSlug(ctx)
	if err != nil || slug == "" {
		return "", fmt.Errorf("no active spec — call Specify first")
	}

	dir, err := specDir(ctx)
	if err != nil {
		return "", err
	}
	if p.Artifact != "spec.md" && p.Artifact != "plan.md" && p.Artifact != "tasks.md" && p.Artifact != "specs.md" {
		return "", fmt.Errorf("invalid spec artifact %q", p.Artifact)
	}
	path := filepath.Join(dir, p.Artifact)

	// Ensure directory exists
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}

	if p.Content != "" {
		// Full replacement
		if err := writeGuardedFile(ctx, path, []byte(p.Content), 0o600); err != nil {
			return "", fmt.Errorf("write %s: %w", p.Artifact, err)
		}
		// Update stage meta
		_ = spec.WriteStageMeta(slug, "", "", "")
		return fmt.Sprintf("Replaced %s (%d bytes)", path, len(p.Content)), nil
	}

	if p.Delta != "" {
		// Parse delta
		delta, err := spec.ParseDeltaSpec(p.Delta)
		if err != nil {
			return "", fmt.Errorf("invalid delta spec: %w", err)
		}

		// Validate delta
		vr := spec.ValidateDeltaSpec(delta)
		if !vr.Valid {
			return "", fmt.Errorf("delta validation failed:\n%s", vr.Format())
		}

		// Read existing content
		existing, err := readGuardedFile(ctx, path)
		if err != nil {
			// File doesn't exist yet — just write the delta as-is
			if writeErr := writeGuardedFile(ctx, path, []byte(p.Delta), 0o600); writeErr != nil {
				return "", fmt.Errorf("write %s: %w", p.Artifact, writeErr)
			}
			return fmt.Sprintf("Created %s with delta content (%d requirements)", path, len(delta.Requirements)), nil
		}

		// Apply delta merge
		merged, err := spec.ApplyDelta(string(existing), delta)
		if err != nil {
			return "", fmt.Errorf("apply delta: %w", err)
		}

		if err := writeGuardedFile(ctx, path, []byte(merged), 0o600); err != nil {
			return "", fmt.Errorf("write merged %s: %w", p.Artifact, err)
		}

		_ = spec.WriteStageMeta(slug, "", "", "")
		return fmt.Sprintf("Applied delta to %s (%d requirements processed)", path, len(delta.Requirements)), nil
	}

	return "", fmt.Errorf("specify either delta or content")
}

// SpecListTool lists all spec workflows with their current stage.
type SpecListTool struct{}

func (SpecListTool) Name() string { return "SpecList" }

// SpecResetInput is the typed input for SpecResetTool.
type SpecResetInput struct {
	Slug   string `json:"slug"`
	Delete bool   `json:"delete"`
}

func (SpecListTool) Aliases() []string { return []string{"spec_list"} }
func (SpecListTool) Description() string {
	return "List all spec workflows in .rho/specs/ with their stage and title. Useful for finding existing specs to resume."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecListTool) Schema() ToolSchema {
	return ToolSchema{
		Type:       "object",
		Properties: map[string]SchemaProperty{},
	}
}

func (SpecListTool) Parameters() map[string]interface{} {
	return specListSchema.ToJSONSchema()
}

// specListSchema is the single source of truth for SpecList's input schema.
var specListSchema = SpecListTool{}.Schema()

func (SpecListTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	metas, err := spec.ListSpecs()
	if err != nil {
		return "", fmt.Errorf("list specs: %w", err)
	}

	if len(metas) == 0 {
		return "No spec workflows found. Use Specify to start a new one.", nil
	}

	var b strings.Builder
	b.WriteString("Found spec workflows:\n\n")
	for _, m := range metas {
		title := m.Title
		if title == "" {
			title = m.Slug
		}
		stage := spec.StageEnumDisplayName(m.Stage)
		created := ""
		if !m.CreatedAt.IsZero() {
			created = m.CreatedAt.Format("2006-01-02 15:04")
		}
		fmt.Fprintf(&b, "  %s\n", title)
		fmt.Fprintf(&b, "    Slug:   %s\n", m.Slug)
		fmt.Fprintf(&b, "    Stage:  %s\n", stage)
		if created != "" {
			fmt.Fprintf(&b, "    Created: %s\n", created)
		}
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String()), nil
}

// SpecResetTool resets the active spec workflow, clearing its stage.
type SpecResetTool struct{}

func (SpecResetTool) Name() string      { return "SpecReset" }
func (SpecResetTool) Aliases() []string { return []string{"spec_reset"} }
func (SpecResetTool) Description() string {
	return "Reset/delete the active spec workflow. Clears the spec stage so Write/Edit/Bash follow the trust tier again. Optionally delete the spec artifacts entirely."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecResetTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"slug":   {Type: "string", Description: "Optional: target a specific slug instead of the active one"},
			"delete": {Type: "boolean", Description: "If true, delete the spec artifacts from disk"},
		},
	}
}

func (SpecResetTool) Parameters() map[string]interface{} {
	return specResetSchema.ToJSONSchema()
}

// specResetSchema is the single source of truth for SpecReset's input schema.
var specResetSchema = SpecResetTool{}.Schema()

func (SpecResetTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var p SpecResetInput
	if len(input) > 0 && string(input) != "null" {
		decoded, err := DecodeInput[SpecResetInput]("SpecReset", input)
		if err != nil {
			return "", err
		}
		p = decoded
	}

	var slug string
	if p.Slug != "" {
		slug = p.Slug
	} else {
		var err error
		slug, err = specSlug(ctx)
		if err != nil || slug == "" {
			return "No active spec to reset.", nil
		}
	}

	if p.Delete {
		if err := spec.DeleteSpec(slug); err != nil {
			return "", fmt.Errorf("delete spec %s: %w", slug, err)
		}
	} else {
		// Just reset the stage — keep artifacts
		_ = spec.WriteStageMeta(slug, "none", "", "")
	}

	// If this is the active spec, reset the session slug too
	activeSlug, err := specSlug(ctx)
	if err == nil && activeSlug == slug {
		_ = setSpecSlug(ctx, "")
	}

	if p.Delete {
		return fmt.Sprintf("Deleted spec %s entirely.", slug), nil
	}
	return fmt.Sprintf("Reset spec %s — stage cleared, artifacts preserved.", slug), nil
}

// specsDir returns the .rho/specs directory path.
func specsDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, ".rho", "specs"), nil
}

// SpecConfigTool allows the agent to read and update spec configuration.
type SpecConfigTool struct{}

func (SpecConfigTool) Name() string { return "SpecConfig" }

// SpecConfigInput is the typed input for SpecConfigTool.
type SpecConfigInput struct {
	Action string `json:"action"`
	Field  string `json:"field"`
	Value  string `json:"value"`
}

func (SpecConfigTool) Aliases() []string { return []string{"spec_config"} }
func (SpecConfigTool) Description() string {
	return "Read or update spec configuration (language, framework, methodology, architecture). Call this to check preferences before writing specs, or to update config as needed."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecConfigTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action": {Type: "string", Enum: []interface{}{"get", "set", "list"}, Description: "'get' to read config, 'set' to update, 'list' to show available fields"},
			"field":  {Type: "string", Description: "The config field to update (required for 'set'). One of: language, framework, methodology, architecture, repo_structure, custom_prompt"},
			"value":  {Type: "string", Description: "The new value for the field. Use 'ai' to let the AI decide."},
		},

		Required: []string{"action"},
	}
}

func (SpecConfigTool) Parameters() map[string]interface{} {
	return specConfigSchema.ToJSONSchema()
}

// specConfigSchema is the single source of truth for SpecConfig's input schema.
var specConfigSchema = SpecConfigTool{}.Schema()

func (SpecConfigTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[SpecConfigInput]("SpecConfig", input)
	if err != nil {
		return "", err
	}

	switch p.Action {
	case "get":
		cfg := spec.LoadSpecConfig()
		return "Current spec configuration:\n" + cfg.Format(), nil

	case "list":
		var b strings.Builder
		b.WriteString("Available config fields:\n\n")
		for _, f := range spec.SpecConfigFields() {
			fmt.Fprintf(&b, "  %s (%s)\n", f.Label, f.Key)
			fmt.Fprintf(&b, "    %s\n", f.Help)
			if len(f.Examples) > 0 {
				fmt.Fprintf(&b, "    Examples: %s\n", strings.Join(f.Examples, ", "))
			}
			b.WriteString("\n")
		}
		b.WriteString("Use `SpecConfig` with action='set', field='key', value='your value'.\n")
		b.WriteString("Use value 'ai' to let the AI decide any field.")
		return strings.TrimSpace(b.String()), nil

	case "set":
		if p.Field == "" {
			return "", fmt.Errorf("field is required for 'set' action")
		}
		valid := false
		for _, f := range spec.SpecConfigFields() {
			if f.Key == p.Field {
				valid = true
				break
			}
		}
		if !valid {
			return "", fmt.Errorf("unknown field %q. Use action='list' to see available fields", p.Field)
		}

		cfg := spec.LoadSpecConfig()
		switch p.Field {
		case "language":
			cfg.Language = p.Value
		case "framework":
			cfg.Framework = p.Value
		case "methodology":
			cfg.Methodology = p.Value
		case "architecture":
			cfg.Architecture = p.Value
		case "repo_structure":
			cfg.RepoStructure = p.Value
		case "custom_prompt":
			cfg.CustomPrompt = p.Value
		}

		if err := spec.SaveSpecConfig(cfg); err != nil {
			return "", fmt.Errorf("save config: %w", err)
		}
		return fmt.Sprintf("Updated spec config field %q to %q.\n\n%s", p.Field, p.Value, cfg.Format()), nil

	default:
		return "", fmt.Errorf("unknown action %q. Use 'get', 'set', or 'list'", p.Action)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// ProposalTool starts a spec workflow by writing proposal.md — the "why"
// document that establishes the problem and goals before any technical work.
type ProposalTool struct{}

func (ProposalTool) Name() string { return "Proposal" }

// ProposalInput is the typed input for ProposalTool.
type ProposalInput struct {
	Title    string `json:"title"`
	Proposal string `json:"proposal"`
}

func (ProposalTool) Aliases() []string { return []string{"proposal"} }
func (ProposalTool) Description() string {
	return "Write proposal.md outlining WHY this change is needed. Call this first to start a spec-driven workflow."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (ProposalTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"title":    {Type: "string", Description: "Short title for this spec, used to name its directory"},
			"proposal": {Type: "string", Description: "The proposal content: problem statement, goals, out of scope, success criteria"},
		},

		Required: []string{"proposal"},
	}
}

func (ProposalTool) Parameters() map[string]interface{} {
	return proposalSchema.ToJSONSchema()
}

// proposalSchema is the single source of truth for Proposal's input schema.
var proposalSchema = ProposalTool{}.Schema()

func (ProposalTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[ProposalInput]("Proposal", input)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(p.Proposal) == "" {
		return "", fmt.Errorf("proposal is required")
	}
	slug := slugify(p.Title)
	if slug == "spec" {
		slug = slugify(firstLine(p.Proposal))
	}
	slug = fmt.Sprintf("%s-%d", slug, time.Now().Unix())
	if err := setSpecSlug(ctx, ""); err != nil {
		return "", err
	}
	path, err := writeSpecArtifactForSlug(ctx, slug, "proposal.md", p.Proposal)
	if err != nil {
		return "", err
	}
	if err := setSpecSlug(ctx, slug); err != nil {
		return "", err
	}
	return fmt.Sprintf("Wrote %s. Next, call Specify (requirements) and/or Design (technical approach) in parallel.", path), nil
}

// DesignTool writes design.md — the technical approach for an active spec.
// Can run in parallel with Specify after Proposal completes.
type DesignTool struct{}

func (DesignTool) Name() string { return "Design" }

// DesignInput is the typed input for DesignTool.
type DesignInput struct {
	Design string `json:"design"`
}

func (DesignTool) Aliases() []string { return []string{"design"} }
func (DesignTool) Description() string {
	return "Write design.md describing the technical approach. Call after Proposal, can run in parallel with Specify."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (DesignTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"design": {Type: "string", Description: "The technical design: architecture, data flow, key decisions, components, interfaces"},
		},

		Required: []string{"design"},
	}
}

func (DesignTool) Parameters() map[string]interface{} {
	return designSchema.ToJSONSchema()
}

// designSchema is the single source of truth for Design's input schema.
var designSchema = DesignTool{}.Schema()

func (DesignTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[DesignInput]("Design", input)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(p.Design) == "" {
		return "", fmt.Errorf("design is required")
	}
	path, err := writeSpecArtifact(ctx, "design.md", p.Design)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Wrote %s. Next, call Plan (after both Specify and Design are complete).", path), nil
}

func init() {
	_ = ProposalTool{}
	_ = DesignTool{}
}
