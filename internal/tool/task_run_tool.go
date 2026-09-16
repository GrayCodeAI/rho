package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// TaskRunTool drives ready tasks through the task executor. It lets the agent
// (or a host) turn the validated task graph into execution: tasks run in
// dependency order, are retried up to their retry budget with backoff, and are
// parked failed when the budget is exhausted. It is the tool-level front door
// for TaskRunner.
type TaskRunTool struct{}

// TaskRunInput is the typed input for TaskRunTool.
type TaskRunInput struct {
	TimeoutSec    int `json:"timeout_sec"`
	MaxTotalTasks int `json:"max_total_tasks"`
}

func (TaskRunTool) Name() string      { return "TaskRun" }
func (TaskRunTool) Aliases() []string { return []string{"task_run"} }
func (TaskRunTool) Description() string {
	return "Execute all ready tasks (pending with no blockers) through the task executor. " +
		"Tasks run in dependency order, are retried up to their retry budget, and are parked failed " +
		"when the budget is exhausted. Returns a run summary."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (TaskRunTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"timeout_sec":     {Type: "integer", Description: "Per-task execution timeout in seconds (default 300)"},
			"max_total_tasks": {Type: "integer", Description: "Watchdog cap on distinct tasks that may reach a terminal state (default 50)"},
		},
	}
}

func (TaskRunTool) Parameters() map[string]interface{} {
	return taskRunSchema.ToJSONSchema()
}

// taskRunSchema is the single source of truth for TaskRun's input schema.
var taskRunSchema = TaskRunTool{}.Schema()

func (TaskRunTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	tc := GetToolContext(ctx)
	if tc == nil || tc.TaskExecutor == nil {
		return "", fmt.Errorf("TaskRun requires a task executor; none is configured for this session")
	}

	var p TaskRunInput
	if len(input) > 0 && string(input) != "null" {
		decoded, err := DecodeInput[TaskRunInput]("TaskRun", input)
		if err != nil {
			return "", err
		}
		p = decoded
	}
	timeout := time.Duration(p.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}

	runner := NewTaskRunner(TaskRunnerOptions{
		Store:          GetTaskStore(),
		Execute:        tc.TaskExecutor,
		DefaultTimeout: timeout,
		MaxTotalTasks:  p.MaxTotalTasks,
	})
	if err := runner.Run(ctx); err != nil {
		return "", err
	}
	s := runner.Status()
	parts := []string{
		fmt.Sprintf("%d executed", s.Executed),
		fmt.Sprintf("%d completed", s.Completed),
		fmt.Sprintf("%d failed", s.Failed),
		fmt.Sprintf("%d replanned", s.Replanned),
	}
	if s.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", s.Skipped))
	}
	if s.Cancelled > 0 {
		parts = append(parts, fmt.Sprintf("%d cancelled", s.Cancelled))
	}
	return "Task run finished: " + strings.Join(parts, ", "), nil
}
