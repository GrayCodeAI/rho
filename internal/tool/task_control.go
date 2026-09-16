package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/GrayCodeAI/rho/internal/taskruntime"
)

// resolveTask looks up a task_id in shell background map, session BackgroundManager,
// or the process-wide taskruntime.Default registry.
func resolveTaskOutput(ctx context.Context, id string) (status, kind, label, output string, err error) {
	// Shell background tasks
	if t, ok := getBackgroundTask(id); ok {
		st, out := t.snapshot()
		return st, "shell", t.command, out, nil
	}
	// Session agent manager
	if tc := GetToolContext(ctx); tc != nil && tc.BackgroundManager != nil {
		if tc.BackgroundManager.IsRunning(id) {
			elapsed := tc.BackgroundManager.Elapsed(id)
			return "running", "agent", id, fmt.Sprintf("running for %s", elapsed), nil
		}
		if res, ok := tc.BackgroundManager.GetResult(id); ok {
			st := "completed"
			out := res.Output
			if res.Err != nil {
				st = "failed"
				out = res.Err.Error() + "\n" + out
			}
			return st, "agent", res.Prompt, out, nil
		}
	}
	// Global taskruntime (shell bridge + monitors + agents)
	if t, ok := taskruntime.Default.Get(id); ok {
		return string(t.Status), string(t.Kind), t.Prompt, t.Output, nil
	}
	if tc := GetToolContext(ctx); tc != nil && tc.BackgroundManager != nil && tc.BackgroundManager.Registry() != nil {
		if t, ok := tc.BackgroundManager.Registry().Get(id); ok {
			return string(t.Status), string(t.Kind), t.Prompt, t.Output, nil
		}
	}
	return "", "", "", "", fmt.Errorf("task %q not found", id)
}

func killTask(ctx context.Context, id string) error {
	if t, ok := getBackgroundTask(id); ok {
		if err := t.stop(); err != nil {
			return err
		}
		taskruntime.Default.FinishExternal(id, taskruntime.StatusKilled, "", "killed")
		return nil
	}
	if tc := GetToolContext(ctx); tc != nil && tc.BackgroundManager != nil {
		if err := tc.BackgroundManager.Kill(id); err == nil {
			return nil
		}
	}
	if err := taskruntime.Default.Kill(id); err == nil {
		return nil
	}
	return fmt.Errorf("task %q not running or not found", id)
}

// WaitTasksTool waits for one or more background tasks.
type WaitTasksTool struct{}

// WaitTasksInput is the typed input for WaitTasksTool.
type WaitTasksInput struct {
	TaskID     string   `json:"task_id"`
	TaskIDs    []string `json:"task_ids"`
	TimeoutSec int      `json:"timeout_sec"`
}

func (WaitTasksTool) Name() string      { return "WaitTasks" }
func (WaitTasksTool) Aliases() []string { return []string{"wait_tasks", "GetTaskOutput"} }
func (WaitTasksTool) RiskLevel() string { return "low" }
func (WaitTasksTool) Description() string {
	return "Wait for background task(s) to complete and return their output. " +
		"Works for shell (Bash run_in_background), agent spawns, and Monitor tasks."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (WaitTasksTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"task_id":     {Type: "string", Description: "Single task ID to wait for"},
			"task_ids":    {Type: "array", Items: &SchemaProperty{Type: "string"}, Description: "Multiple task IDs to wait for"},
			"timeout_sec": {Type: "integer", Description: "Max seconds to wait (default 120, max 600)"},
		},
	}
}

func (WaitTasksTool) Parameters() map[string]interface{} {
	return waitTasksSchema.ToJSONSchema()
}

// waitTasksSchema is the single source of truth for WaitTasks' input schema.
var waitTasksSchema = WaitTasksTool{}.Schema()

func (WaitTasksTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[WaitTasksInput]("WaitTasks", input)
	if err != nil {
		return "", err
	}
	ids := append([]string{}, p.TaskIDs...)
	if p.TaskID != "" {
		ids = append(ids, p.TaskID)
	}
	if len(ids) == 0 {
		return "", fmt.Errorf("task_id or task_ids is required")
	}
	timeout := time.Duration(p.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	if timeout > 10*time.Minute {
		timeout = 10 * time.Minute
	}

	// Prefer session registry for agent tasks; always include Default for shell/monitor.
	deadline := time.Now().Add(timeout)
	var b strings.Builder
	for {
		allDone := true
		for _, id := range ids {
			st, _, _, _, err := resolveTaskOutput(ctx, id)
			if err != nil {
				return "", err
			}
			if st == "running" || st == string(taskruntime.StatusRunning) {
				allDone = false
			}
		}
		if allDone || time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}

	for i, id := range ids {
		st, kind, label, out, err := resolveTaskOutput(ctx, id)
		if err != nil {
			fmt.Fprintf(&b, "=== %s ===\nerror: %v\n", id, err)
			continue
		}
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "=== %s (%s) status=%s ===\nlabel: %s\n%s\n", id, kind, st, truncateLabel(label, 80), out)
	}
	return strings.TrimSpace(b.String()), nil
}

// KillTaskTool stops a running background task.
type KillTaskTool struct{}

// KillTaskInput is the typed input for KillTaskTool.
type KillTaskInput struct {
	TaskID string `json:"task_id"`
}

func (KillTaskTool) Name() string      { return "KillTask" }
func (KillTaskTool) Aliases() []string { return []string{"kill_task"} }
func (KillTaskTool) RiskLevel() string { return "medium" }
func (KillTaskTool) Description() string {
	return "Kill a running background shell, agent, or monitor task by task_id."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (KillTaskTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"task_id": {Type: "string", Description: "Background task ID"},
		},
		Required: []string{"task_id"},
	}
}

func (KillTaskTool) Parameters() map[string]interface{} {
	return killTaskSchema.ToJSONSchema()
}

// killTaskSchema is the single source of truth for KillTask's input schema.
var killTaskSchema = KillTaskTool{}.Schema()

func (KillTaskTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[KillTaskInput]("KillTask", input)
	if err != nil {
		return "", err
	}
	if p.TaskID == "" {
		return "", fmt.Errorf("task_id is required")
	}
	if err := killTask(ctx, p.TaskID); err != nil {
		return "", err
	}
	return fmt.Sprintf("Killed task %s", p.TaskID), nil
}

// MonitorTool runs a command, streams lines into a background task, with rate limits.
type MonitorTool struct{}

// MonitorInput is the typed input for MonitorTool.
type MonitorInput struct {
	Command        string `json:"command"`
	MaxRuntimeSec  int    `json:"max_runtime_sec"`
	MaxLinesPerSec int    `json:"max_lines_per_sec"`
	Description    string `json:"description"`
}

func (MonitorTool) Name() string      { return "Monitor" }
func (MonitorTool) Aliases() []string { return []string{"monitor"} }
func (MonitorTool) RiskLevel() string { return "medium" }
func (MonitorTool) Description() string {
	return "Run a shell command as a background monitor that captures line-oriented output. " +
		"Use WaitTasks/TaskOutput to read output and KillTask to stop. " +
		"Rate-limited (max lines/sec) and auto-killed after max_runtime_sec."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (MonitorTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"command":           {Type: "string", Description: "Shell command to monitor (e.g. 'tail -f log' or 'npm run dev')"},
			"max_runtime_sec":   {Type: "integer", Description: "Auto-kill after this many seconds (default 300, max 3600)"},
			"max_lines_per_sec": {Type: "integer", Description: "Drop excess lines above this rate (default 50)"},
			"description":       {Type: "string", Description: "Short label for the monitor task"},
		},
		Required: []string{"command"},
	}
}

func (MonitorTool) Parameters() map[string]interface{} {
	return monitorSchema.ToJSONSchema()
}

// monitorSchema is the single source of truth for Monitor's input schema.
var monitorSchema = MonitorTool{}.Schema()

func (MonitorTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[MonitorInput]("Monitor", input)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(p.Command) == "" {
		return "", fmt.Errorf("command is required")
	}
	if tc := GetToolContext(ctx); tc != nil && tc.ReadOnlyBash {
		if err := ExploreBashAllowed(p.Command); err != nil {
			return "", fmt.Errorf("blocked (read-only bash): %w", err)
		}
	}
	maxRun := p.MaxRuntimeSec
	if maxRun <= 0 {
		maxRun = 300
	}
	if maxRun > 3600 {
		maxRun = 3600
	}
	rate := p.MaxLinesPerSec
	if rate <= 0 {
		rate = 50
	}
	label := p.Description
	if label == "" {
		label = p.Command
	}

	mctx, cancel := context.WithTimeout(context.Background(), time.Duration(maxRun)*time.Second)
	id := fmt.Sprintf("mon_%d", time.Now().UnixNano())
	taskruntime.Default.RegisterExternal(id, taskruntime.KindMonitor, label, cancel)

	cmd := exec.CommandContext(mctx, "bash", "-c", p.Command) // #nosec G204 -- agent-supplied monitor command, same trust model as Bash
	if tc := GetToolContext(ctx); tc != nil && tc.WorkingDir != "" {
		cmd.Dir = tc.WorkingDir
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return "", err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		cancel()
		taskruntime.Default.FinishExternal(id, taskruntime.StatusFailed, "", err.Error())
		return "", err
	}

	go func() {
		defer cancel()
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		var (
			windowStart = time.Now()
			linesInWin  int
			mu          sync.Mutex
		)
		for sc.Scan() {
			line := sc.Text()
			mu.Lock()
			if time.Since(windowStart) >= time.Second {
				windowStart = time.Now()
				linesInWin = 0
			}
			linesInWin++
			drop := linesInWin > rate
			mu.Unlock()
			if drop {
				continue
			}
			taskruntime.Default.AppendOutput(id, line+"\n")
			select {
			case <-mctx.Done():
				_ = cmd.Process.Kill()
				taskruntime.Default.FinishExternal(id, taskruntime.StatusKilled, "", "timeout or cancelled")
				return
			default:
			}
		}
		waitErr := cmd.Wait()
		status := taskruntime.StatusCompleted
		errMsg := ""
		if mctx.Err() != nil {
			status = taskruntime.StatusKilled
			errMsg = mctx.Err().Error()
		} else if waitErr != nil {
			status = taskruntime.StatusFailed
			errMsg = waitErr.Error()
		}
		// Pull accumulated output
		if t, ok := taskruntime.Default.Get(id); ok {
			taskruntime.Default.FinishExternal(id, status, t.Output, errMsg)
		} else {
			taskruntime.Default.FinishExternal(id, status, "", errMsg)
		}
	}()

	return fmt.Sprintf("Monitor started: task_id=%s max_runtime=%ds rate=%d lines/s\nUse WaitTasks or TaskOutput with task_id=%q; KillTask to stop.",
		id, maxRun, rate, id), nil
}

func truncateLabel(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	// Rune-safe truncation: never split a multibyte UTF-8 sequence.
	if runes := []rune(s); len(runes) > n {
		return string(runes[:n]) + "..."
	}
	return s
}
