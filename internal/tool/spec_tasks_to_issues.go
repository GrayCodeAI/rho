package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// TasksToIssuesTool converts tasks from tasks.md into GitHub issues using
// the `gh` CLI. Each unchecked task becomes an issue, with labels derived
// from the task group heading.
type TasksToIssuesTool struct{}

func (TasksToIssuesTool) Name() string { return "TasksToIssues" }

// TasksToIssuesInput is the typed input for TasksToIssuesTool.
type TasksToIssuesInput struct {
	DryRun bool   `json:"dry_run"`
	Labels string `json:"labels"`
}

func (TasksToIssuesTool) Aliases() []string {
	return []string{"tasks_to_issues", "spec:tasks-to-issues"}
}

func (TasksToIssuesTool) Description() string {
	return "Convert unchecked tasks from tasks.md into GitHub issues. Requires `gh` CLI authenticated. Each task becomes an issue with labels derived from its phase heading."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (TasksToIssuesTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"dry_run": {Type: "boolean", Description: "If true, show what issues would be created without actually creating them"},
			"labels":  {Type: "string", Description: "Comma-separated additional labels to add to all issues (e.g., 'spec,needs-review')"},
		},
	}
}

func (TasksToIssuesTool) Parameters() map[string]interface{} {
	return tasksToIssuesSchema.ToJSONSchema()
}

// tasksToIssuesSchema is the single source of truth for TasksToIssues's input schema.
var tasksToIssuesSchema = TasksToIssuesTool{}.Schema()

func (TasksToIssuesTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var p TasksToIssuesInput
	if len(input) > 0 && string(input) != "null" {
		decoded, err := DecodeInput[TasksToIssuesInput]("TasksToIssues", input)
		if err != nil {
			return "", err
		}
		p = decoded
	}

	// Check gh CLI availability
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("gh CLI not found — install it from https://cli.github.com")
	}

	// Check if we're in a git repo
	if err := exec.Command("gh", "auth", "status").Run(); err != nil {
		return "", fmt.Errorf("gh CLI not authenticated — run `gh auth login` first")
	}

	dir, err := specDir(ctx)
	if err != nil {
		return "", err
	}

	tasksPath := filepath.Join(dir, "tasks.md")
	data, err := os.ReadFile(tasksPath) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return "", fmt.Errorf("cannot read tasks.md: %w — write tasks first with Tasks tool", err)
	}

	tasks := parseTasks(string(data))
	if len(tasks) == 0 {
		return "No unchecked tasks found in tasks.md", nil
	}

	var additionalLabels []string
	if p.Labels != "" {
		for _, l := range strings.Split(p.Labels, ",") {
			l = strings.TrimSpace(l)
			if l != "" {
				additionalLabels = append(additionalLabels, l)
			}
		}
	}

	var b strings.Builder
	if p.DryRun {
		fmt.Fprintf(&b, "Dry run — would create %d issue(s):\n\n", len(tasks))
	} else {
		fmt.Fprintf(&b, "Creating %d issue(s):\n\n", len(tasks))
	}

	created := 0
	for _, task := range tasks {
		labels := append([]string{"spec", task.Phase}, additionalLabels...)
		title := fmt.Sprintf("[Spec] %s", task.Description)
		body := fmt.Sprintf("## Task\n\n%s\n\n## Phase\n\n%s\n\n## Source\n\nCreated from spec tasks.md", task.Description, task.Phase)

		if p.DryRun {
			fmt.Fprintf(&b, "  Would create: %s (labels: %s)\n", title, strings.Join(labels, ", "))
			continue
		}

		args := []string{"issue", "create", "--title", title, "--body", body}
		for _, label := range labels {
			args = append(args, "--label", label)
		}

		cmd := exec.Command("gh", args...)       // #nosec G204 -- gh CLI invocation with internally-derived args
		cmd.Dir = filepath.Clean(dir + "/../..") // go to project root
		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Fprintf(&b, "  x Failed to create issue for %q: %v\n%s\n", task.Description, err, string(output))
			continue
		}

		url := strings.TrimSpace(string(output))
		fmt.Fprintf(&b, "  + Created: %s → %s\n", task.Description, url)
		created++
	}

	b.WriteString(fmt.Sprintf("\n%d/%d issues created.", created, len(tasks)))
	return strings.TrimSpace(b.String()), nil
}

type parsedTask struct {
	Phase       string
	Description string
	Number      string
}

var (
	reTaskLine  = regexp.MustCompile(`^- \[ \]\s+(\S+)\s+(.+)$`)
	rePhaseLine = regexp.MustCompile(`^##\s+\d+\.\s*(.+)$`)
)

func parseTasks(content string) []parsedTask {
	var tasks []parsedTask
	lines := strings.Split(content, "\n")
	currentPhase := "General"

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for phase header
		if m := rePhaseLine.FindStringSubmatch(trimmed); m != nil {
			currentPhase = strings.TrimSpace(m[1])
			continue
		}

		// Check for unchecked task
		if m := reTaskLine.FindStringSubmatch(trimmed); m != nil {
			tasks = append(tasks, parsedTask{
				Number:      m[1],
				Description: m[2],
				Phase:       currentPhase,
			})
		}
	}

	return tasks
}
