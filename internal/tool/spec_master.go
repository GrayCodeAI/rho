package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GrayCodeAI/rho/internal/spec"
)

type SpecMasterTool struct{}

func (SpecMasterTool) Name() string { return "SpecMaster" }

// SpecMasterInput is the typed input for SpecMasterTool.
type SpecMasterInput struct {
	Action string `json:"action"`
}

func (SpecMasterTool) Aliases() []string {
	return []string{"spec_master", "spec:master"}
}

func (SpecMasterTool) Description() string {
	return "Generate or update MASTER.md progress index for cross-session continuity. Captures current spec state, completed tasks, pending work, and key decisions so work can resume across sessions."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecMasterTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action": {Type: "string", Enum: []interface{}{"read", "update", "resume"}, Description: "Action: read (show current), update (regenerate), resume (load state for continuation)"},
		},

		Required: []string{"action"},
	}
}

func (SpecMasterTool) Parameters() map[string]interface{} {
	return specMasterSchema.ToJSONSchema()
}

// specMasterSchema is the single source of truth for SpecMaster's input schema.
var specMasterSchema = SpecMasterTool{}.Schema()

func (SpecMasterTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[SpecMasterInput]("SpecMaster", input)
	if err != nil {
		return "", err
	}

	dir, err := specDir(ctx)
	if err != nil {
		return "", err
	}

	masterPath := filepath.Join(dir, "MASTER.md")

	switch p.Action {
	case "read":
		return readMaster(masterPath)
	case "update":
		return updateMaster(dir, masterPath)
	case "resume":
		return resumeFromMaster(dir, masterPath)
	default:
		return "", fmt.Errorf("unknown action %q", p.Action)
	}
}

func readMaster(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "No MASTER.md found. Use action='update' to create one.", nil
	}
	return string(data), nil
}

func updateMaster(dir, masterPath string) (string, error) {
	var b strings.Builder

	fmt.Fprintf(&b, "# MASTER: Spec Progress Index\n\n")
	fmt.Fprintf(&b, "> Generated: %s\n\n", time.Now().Format(time.RFC3339))

	fmt.Fprintf(&b, "## Artifacts\n\n")
	artifacts := []string{"constitution.md", "proposal.md", "spec.md", "design.md", "plan.md", "tasks.md"}
	for _, a := range artifacts {
		path := filepath.Join(dir, a)
		if _, err := os.Stat(path); err == nil {
			fmt.Fprintf(&b, "- [x] %s\n", a)
		} else {
			fmt.Fprintf(&b, "- [ ] %s\n", a)
		}
	}
	b.WriteString("\n")

	tasksContent := readFileStr(filepath.Join(dir, "tasks.md"))
	if tasksContent != "" {
		tasks := spec.ParseTasks(tasksContent)
		total := len(tasks)
		complete := 0
		for _, t := range tasks {
			if strings.HasPrefix(strings.TrimSpace(t.Description), "- [x]") {
				complete++
			}
		}
		fmt.Fprintf(&b, "## Task Progress\n\n")
		fmt.Fprintf(&b, "**%d/%d tasks complete**\n\n", complete, total)

		if total > 0 {
			fmt.Fprintf(&b, "### Completed\n\n")
			for _, t := range tasks {
				trimmed := strings.TrimSpace(t.Description)
				if strings.HasPrefix(trimmed, "- [x]") {
					fmt.Fprintf(&b, "- %s\n", trimmed)
				}
			}
			b.WriteString("\n")

			fmt.Fprintf(&b, "### Pending\n\n")
			for _, t := range tasks {
				trimmed := strings.TrimSpace(t.Description)
				if strings.HasPrefix(trimmed, "- [ ]") {
					fmt.Fprintf(&b, "- %s\n", trimmed)
				}
			}
			b.WriteString("\n")
		}
	}

	specContent := readFileStr(filepath.Join(dir, "spec.md"))
	if specContent != "" {
		reqs := spec.ExtractReqIDs(specContent)
		if len(reqs) > 0 {
			fmt.Fprintf(&b, "## Requirements\n\n")
			for _, req := range reqs {
				fmt.Fprintf(&b, "- `%s`\n", req.Raw)
			}
			b.WriteString("\n")
		}
	}

	telemetryPath := filepath.Join(dir, ".telemetry.json")
	if _, err := os.Stat(telemetryPath); err == nil {
		fmt.Fprintf(&b, "## Adaptive Control\n\n")
		b.WriteString("See `.telemetry.json` for drift data.\n\n")
	}

	fmt.Fprintf(&b, "## Key Decisions\n\n")
	b.WriteString("_Document important architectural decisions here._\n\n")

	fmt.Fprintf(&b, "## Next Actions\n\n")
	b.WriteString("_What to do when resuming this spec._\n\n")

	content := b.String()
	if err := os.WriteFile(masterPath, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("write MASTER.md: %w", err)
	}

	return fmt.Sprintf("Updated MASTER.md\n\n%s", content), nil
}

func resumeFromMaster(dir, masterPath string) (string, error) {
	data, err := os.ReadFile(masterPath)
	if err != nil {
		return "No MASTER.md found. Start with Proposal to begin a new spec.", nil
	}

	var b strings.Builder
	b.WriteString("## Resuming Spec Session\n\n")
	b.WriteString(string(data))
	b.WriteString("\n\n### Quick Actions\n\n")
	b.WriteString("- Use `SpecStatus` to check current stage\n")
	b.WriteString("- Use `SpecProgress` to see task completion\n")
	b.WriteString("- Use `SpecGround` to refresh context\n")
	b.WriteString("- Use `SpecAdaptive` to report task completion\n")

	return strings.TrimSpace(b.String()), nil
}

func init() {
	_ = SpecMasterTool{}
}
