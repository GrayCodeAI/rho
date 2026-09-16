package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/GrayCodeAI/rho/internal/spec"
)

type SpecProgressTool struct{}

func (SpecProgressTool) Name() string { return "SpecProgress" }

// SpecProgressInput is the typed input for SpecProgressTool.
type SpecProgressInput struct {
	AutoUpdate bool   `json:"auto_update"`
	ScanDir    string `json:"scan_dir"`
}

func (SpecProgressTool) Aliases() []string {
	return []string{"spec_progress", "spec:progress"}
}

func (SpecProgressTool) Description() string {
	return "Analyze implementation progress by scanning code for REQ citations. Marks tasks complete when their requirements are implemented. Reports completion percentage and remaining work."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecProgressTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"auto_update": {Type: "boolean", Description: "If true, automatically mark implemented tasks as complete in tasks.md"},
			"scan_dir":    {Type: "string", Description: "Directory to scan for REQ citations (default: current directory)"},
		},
	}
}

func (SpecProgressTool) Parameters() map[string]interface{} {
	return specProgressSchema.ToJSONSchema()
}

// specProgressSchema is the single source of truth for SpecProgress's input schema.
var specProgressSchema = SpecProgressTool{}.Schema()

func (SpecProgressTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var p SpecProgressInput
	if len(input) > 0 && string(input) != "null" {
		decoded, err := DecodeInput[SpecProgressInput]("SpecProgress", input)
		if err != nil {
			return "", err
		}
		p = decoded
	}
	if p.ScanDir == "" {
		p.ScanDir, _ = os.Getwd()
	}

	dir, err := specDir(ctx)
	if err != nil {
		return "", err
	}

	tasksContent := readFileStr(filepath.Join(dir, "tasks.md"))
	if tasksContent == "" {
		return "No tasks.md found. Use Tasks tool first.", nil
	}

	specContent := readFileStr(filepath.Join(dir, "spec.md"))

	codeFiles := spec.ScanCodeForReqIDs(p.ScanDir)
	citedReqs := make(map[string]bool)
	for _, ids := range codeFiles {
		for _, id := range ids {
			citedReqs[id] = true
		}
	}

	specReqs := make(map[string]bool)
	for _, id := range spec.ExtractReqIDs(specContent) {
		specReqs[id.Raw] = true
	}

	lines := strings.Split(tasksContent, "\n")
	updated := make([]string, len(lines))
	copy(updated, lines)

	reTaskLine := regexp.MustCompile(`^(\s*-\s+\[)(\s| x| X)(\]\s+.*)`)
	type taskProgress struct {
		line     string
		reqIDs   []string
		complete bool
		lineNum  int
	}

	var tasks []taskProgress
	totalTasks := 0
	completeTasks := 0

	for i, line := range updated {
		if !reTaskLine.MatchString(line) {
			continue
		}
		totalTasks++
		matches := reTaskLine.FindStringSubmatch(line)
		checkbox := strings.TrimSpace(matches[2])
		isComplete := checkbox == "x" || checkbox == "X"
		if isComplete {
			completeTasks++
		}
		reqs := spec.ExtractReqIDs(line)
		tasks = append(tasks, taskProgress{
			line:     line,
			reqIDs:   reqIDsToStrings(reqs),
			complete: isComplete,
			lineNum:  i,
		})
	}

	newlyComplete := 0
	if p.AutoUpdate {
		for _, task := range tasks {
			if task.complete {
				continue
			}
			if allReqsCited(task.reqIDs, citedReqs, specReqs) {
				line := updated[task.lineNum]
				updated[task.lineNum] = reTaskLine.ReplaceAllString(line, "${1}x${3}")
				newlyComplete++
				completeTasks++
			}
		}
	}

	if p.AutoUpdate && newlyComplete > 0 {
		newContent := strings.Join(updated, "\n")
		tasksPath := filepath.Join(dir, "tasks.md")
		if err := os.WriteFile(tasksPath, []byte(newContent), 0o600); err != nil {
			return "", fmt.Errorf("write tasks.md: %w", err)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## Spec Progress\n\n")
	if totalTasks > 0 {
		pct := float64(completeTasks) / float64(totalTasks) * 100
		fmt.Fprintf(&b, "**%.0f%% complete** (%d/%d tasks)\n\n", pct, completeTasks, totalTasks)
	}

	if len(specReqs) > 0 {
		implemented := 0
		for req := range specReqs {
			if citedReqs[req] {
				implemented++
			}
		}
		fmt.Fprintf(&b, "**REQ Coverage**: %d/%d requirements cited in code\n\n", implemented, len(specReqs))
	}

	if newlyComplete > 0 {
		fmt.Fprintf(&b, "**Auto-updated**: %d task(s) marked complete\n\n", newlyComplete)
	}

	incomplete := totalTasks - completeTasks
	if incomplete > 0 {
		fmt.Fprintf(&b, "### Remaining Tasks\n\n")
		for _, task := range tasks {
			if !task.complete && !(p.AutoUpdate && allReqsCited(task.reqIDs, citedReqs, specReqs)) {
				fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(task.line))
			}
		}
	}

	return strings.TrimSpace(b.String()), nil
}

func reqIDsToStrings(ids []spec.ReqID) []string {
	var result []string
	for _, id := range ids {
		result = append(result, id.Raw)
	}
	return result
}

func allReqsCited(taskReqs []string, citedReqs, specReqs map[string]bool) bool {
	if len(taskReqs) == 0 {
		return false
	}
	for _, req := range taskReqs {
		if !citedReqs[req] && specReqs[req] {
			return false
		}
	}
	return true
}

func init() {
	_ = SpecProgressTool{}
}
