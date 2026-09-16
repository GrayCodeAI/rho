package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	agentcontracts "github.com/GrayCodeAI/rho/internal/contracts/agent"
	"github.com/GrayCodeAI/rho/internal/spec"
)

type SpecParallelTool struct{}

func (SpecParallelTool) Name() string { return "SpecParallel" }

// SpecParallelInput is the typed input for SpecParallelTool.
type SpecParallelInput struct {
	DryRun      bool `json:"dry_run"`
	MaxParallel int  `json:"max_parallel"`
}

func (SpecParallelTool) Aliases() []string {
	return []string{"spec_parallel", "spec:parallel"}
}

func (SpecParallelTool) Description() string {
	return "Analyze tasks.md for parallel execution groups and execute independent tasks concurrently. Parses task dependencies, identifies conflict-free groups, and runs them in parallel sub-agents."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecParallelTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"dry_run":      {Type: "boolean", Description: "If true, only analyze and report groups without executing"},
			"max_parallel": {Type: "integer", Description: "Maximum number of parallel tasks (default 8)"},
		},
	}
}

func (SpecParallelTool) Parameters() map[string]interface{} {
	return specParallelSchema.ToJSONSchema()
}

// specParallelSchema is the single source of truth for SpecParallel's input schema.
var specParallelSchema = SpecParallelTool{}.Schema()

func (SpecParallelTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var p SpecParallelInput
	if len(input) > 0 && string(input) != "null" {
		decoded, err := DecodeInput[SpecParallelInput]("SpecParallel", input)
		if err != nil {
			return "", err
		}
		p = decoded
	}
	if p.MaxParallel <= 0 {
		p.MaxParallel = 8
	}

	dir, err := specDir(ctx)
	if err != nil {
		return "", err
	}

	tasksContent := readFileStr(filepath.Join(dir, "tasks.md"))
	if tasksContent == "" {
		return "No tasks.md found. Use Tasks tool first.", nil
	}

	tasks := spec.ParseTasks(tasksContent)
	if len(tasks) == 0 {
		return "No unchecked tasks found in tasks.md.", nil
	}

	groups := spec.AnalyzeTaskGroups(tasks)
	if len(groups) == 0 {
		return "No task groups identified.", nil
	}

	var b strings.Builder
	b.WriteString(spec.FormatTaskGroups(groups))
	b.WriteString("\n\n")

	if p.DryRun {
		b.WriteString("Dry run — no tasks executed.\n")
		return strings.TrimSpace(b.String()), nil
	}

	tc := GetToolContext(ctx)
	if tc == nil || tc.AgentSpawnFn == nil {
		b.WriteString("Sub-agent spawning not configured — dry run only.\n")
		return strings.TrimSpace(b.String()), nil
	}

	type taskResult struct {
		Task   spec.Task
		Output string
		Err    error
	}

	var results []taskResult
	var mu sync.Mutex
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, p.MaxParallel)

	for _, group := range groups {
		if !group.Parallel {
			for _, task := range group.Tasks {
				results = append(results, taskResult{
					Task:   task,
					Output: "Sequential task — skipped in parallel mode",
				})
			}
			continue
		}

		for _, task := range group.Tasks {
			wg.Add(1)
			semaphore <- struct{}{}
			go func(t spec.Task) {
				defer wg.Done()
				defer func() { <-semaphore }()

				start := time.Now()
				output, err := executeTask(ctx, tc, t)
				elapsed := time.Since(start)

				mu.Lock()
				results = append(results, taskResult{
					Task:   t,
					Output: fmt.Sprintf("%s (elapsed: %s)", output, elapsed.Round(time.Millisecond)),
					Err:    err,
				})
				mu.Unlock()
			}(task)
		}
	}

	wg.Wait()

	b.WriteString("## Execution Results\n\n")
	for _, r := range results {
		status := "OK"
		if r.Err != nil {
			status = "FAIL"
		}
		fmt.Fprintf(&b, "%s %s\n", status, r.Task.Description)
		fmt.Fprintf(&b, "  %s\n", r.Output)
		if r.Err != nil {
			fmt.Fprintf(&b, "  Error: %v\n", r.Err)
		}
	}

	return strings.TrimSpace(b.String()), nil
}

func executeTask(ctx context.Context, tc *ToolContext, task spec.Task) (string, error) {
	prompt := fmt.Sprintf("Implement the following task:\n\n%s\n\n", task.Description)
	if len(task.Files) > 0 {
		prompt += fmt.Sprintf("Files to modify: %s\n", strings.Join(task.Files, ", "))
	}
	if len(task.ReqIDs) > 0 {
		prompt += fmt.Sprintf("Related requirements: %s\n", strings.Join(task.ReqIDs, ", "))
	}
	prompt += "\nComplete the task and report what you did."

	req := agentcontracts.SpawnRequest{
		Prompt:       prompt,
		Description:  task.Description,
		SubagentType: "general-purpose",
		Isolation:    "none",
	}

	res, err := tc.AgentSpawnFn(ctx, req)
	if err != nil {
		return "", err
	}
	if res.Output != "" {
		return res.Output, nil
	}
	return res.Summary, nil
}

func init() {
	_ = SpecParallelTool{}
}
