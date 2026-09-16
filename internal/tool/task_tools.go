package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/GrayCodeAI/rho/internal/env"
	"github.com/GrayCodeAI/rho/internal/taskruntime"
)

const (
	maxTaskOutputBytes = 200_000
	maxBackgroundTasks = 50
	completedRetention = 8 * time.Hour
)

type backgroundTask struct {
	id      string
	command string
	cmd     *exec.Cmd
	started time.Time
	done    chan struct{}

	mu       sync.RWMutex
	output   bytes.Buffer
	exitText string
	stopped  bool
}

var backgroundTasks = struct {
	sync.RWMutex
	next  int
	tasks map[string]*backgroundTask
}{tasks: make(map[string]*backgroundTask)}

func startBackgroundBash(ctx context.Context, command string, execName string, execArgs []string) (string, error) {
	// Auto-cleanup completed tasks older than retention period.
	backgroundTasks.Lock()
	for id, t := range backgroundTasks.tasks {
		select {
		case <-t.done:
			if time.Since(t.started) > completedRetention {
				delete(backgroundTasks.tasks, id)
			}
		default:
		}
	}
	// Enforce max concurrent limit.
	active := 0
	for _, t := range backgroundTasks.tasks {
		select {
		case <-t.done:
		default:
			active++
		}
	}
	if active >= maxBackgroundTasks {
		backgroundTasks.Unlock()
		return "", fmt.Errorf("max background tasks (%d) reached — stop a task first", maxBackgroundTasks)
	}
	backgroundTasks.next++
	id := fmt.Sprintf("task_%d", backgroundTasks.next)
	backgroundTasks.Unlock()

	// Background tasks must outlive the request, so use an independent
	// context. The request ctx would kill the task when the HTTP/Tool-call
	// request times out.
	bgCtx := context.Background()

	// The Bash tool performs command policy/approval checks before starting a
	// background task; this is the intentional shell execution boundary.
	// execName/execArgs are already sandbox-wrapped by the Bash tool (or
	// default to "bash" "-c" when sandbox is off).
	cmd := exec.CommandContext(bgCtx, execName, execArgs...) // #nosec G204 -- intentional Bash tool execution after policy checks and sandbox wrapping
	// Background tasks are long-lived and observable by the agent; scrub
	// provider API keys so the child environment cannot leak credentials.
	cmd.Env = env.SubprocessEnv()
	// Put the child in its own process group so we can kill the whole tree
	// (including grandchildren spawned by the shell) via kill(-pgid). Without
	// this, e.g. `bash -c 'sleep 60 &'` leaves an orphan when the parent is
	// killed.
	setCmdProcessGroup(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}

	task := &backgroundTask{id: id, command: command, cmd: cmd, started: time.Now(), done: make(chan struct{})}
	backgroundTasks.Lock()
	backgroundTasks.tasks[id] = task
	backgroundTasks.Unlock()

	// Bridge into unified taskruntime (PACK-06).
	shellCtx, shellCancel := context.WithCancel(bgCtx)
	taskruntime.Default.RegisterExternal(id, taskruntime.KindShell, command, shellCancel)

	if err := cmd.Start(); err != nil {
		shellCancel()
		removeBackgroundTask(id)
		taskruntime.Default.FinishExternal(id, taskruntime.StatusFailed, "", err.Error())
		return "", err
	}

	var captureWg sync.WaitGroup
	captureWg.Add(2)
	go func() { task.capture(stdout); captureWg.Done() }()
	go func() { task.capture(stderr); captureWg.Done() }()

	// Single outer goroutine: flatten the nested-goroutine pattern. Honor
	// registry kill via shellCancel → kill process group, then wait.
	go func() {
		// Watch shellCancel (registry kill) in parallel with cmd.Wait. The Wait
		// goroutine publishes its error on waitCh so the outer goroutine never
		// reads cmd.ProcessState while Wait is still writing it (data race).
		waitCh := make(chan error, 1)
		go func() {
			waitCh <- cmd.Wait()
		}()
		var waitErr error
		select {
		case <-shellCtx.Done():
			_ = task.stop()
			// Join the Wait goroutine before inspecting the result.
			waitErr = <-waitCh
		case waitErr = <-waitCh:
		}
		captureWg.Wait()
		task.mu.Lock()
		status := taskruntime.StatusCompleted
		errMsg := ""
		if task.stopped {
			status = taskruntime.StatusKilled
			errMsg = "killed"
		} else if waitErr != nil {
			status = taskruntime.StatusFailed
			errMsg = waitErr.Error()
		}
		out := task.output.String()
		task.mu.Unlock()
		close(task.done)
		taskruntime.Default.FinishExternal(id, status, out, errMsg)
		shellCancel()
	}()

	return id, nil
}

func getBackgroundTask(id string) (*backgroundTask, bool) {
	backgroundTasks.RLock()
	defer backgroundTasks.RUnlock()
	task, ok := backgroundTasks.tasks[id]
	return task, ok
}

func removeBackgroundTask(id string) {
	backgroundTasks.Lock()
	defer backgroundTasks.Unlock()
	delete(backgroundTasks.tasks, id)
}

func (t *backgroundTask) capture(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			t.appendOutput(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (t *backgroundTask) appendOutput(data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.output.Write(data)
	if t.output.Len() <= maxTaskOutputBytes {
		return
	}
	trimmed := t.output.Bytes()[t.output.Len()-maxTaskOutputBytes:]
	t.output.Reset()
	t.output.WriteString("... (output truncated)\n")
	t.output.Write(trimmed)
}

func (t *backgroundTask) snapshot() (status, output string) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	status = "running"
	select {
	case <-t.done:
		status = "completed"
		if t.stopped {
			status = "stopped"
		}
	default:
	}
	output = strings.TrimRight(t.output.String(), "\n")
	if t.exitText != "" {
		if output != "" {
			output += "\n\n"
		}
		output += t.exitText
	}
	return status, output
}

func (t *backgroundTask) stop() error {
	t.mu.Lock()
	t.stopped = true
	t.mu.Unlock()
	if t.cmd.Process == nil {
		return nil
	}
	// Kill the whole process group (grandchildren spawned by the shell too),
	// since the child was started with Setpgid: true.
	return killProcessGroup(t.cmd.Process)
}

type TaskOutputTool struct{}

// TaskOutputInput is the typed input for TaskOutputTool.
type TaskOutputInput struct {
	TaskID string `json:"task_id"`
}

func (TaskOutputTool) Name() string      { return "TaskOutput" }
func (TaskOutputTool) Aliases() []string { return []string{"task_output"} }
func (TaskOutputTool) Description() string {
	return "Read output from a background task (shell, agent, or monitor) by task_id."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (TaskOutputTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"task_id": {Type: "string", Description: "Background task ID"},
		},
		Required: []string{"task_id"},
	}
}

func (TaskOutputTool) Parameters() map[string]interface{} {
	return taskOutputSchema.ToJSONSchema()
}

// taskOutputSchema is the single source of truth for TaskOutput's input schema.
var taskOutputSchema = TaskOutputTool{}.Schema()

func (TaskOutputTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[TaskOutputInput]("TaskOutput", input)
	if err != nil {
		return "", err
	}
	// Accept legacy taskId field
	if p.TaskID == "" {
		var legacy struct {
			TaskID string `json:"taskId"`
		}
		_ = json.Unmarshal(input, &legacy)
		p.TaskID = legacy.TaskID
	}
	st, kind, label, out, err := resolveTaskOutput(ctx, p.TaskID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Task: %s\nKind: %s\nLabel: %s\nStatus: %s\n\n%s", p.TaskID, kind, label, st, out), nil
}

type TaskStopTool struct{}

// TaskStopInput is the typed input for TaskStopTool.
type TaskStopInput struct {
	TaskID string `json:"task_id"`
}

func (TaskStopTool) Name() string      { return "TaskStop" }
func (TaskStopTool) Aliases() []string { return []string{"task_stop"} }
func (TaskStopTool) Description() string {
	return "Stop a background shell, agent, or monitor task."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (TaskStopTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"task_id": {Type: "string", Description: "Background task ID"},
		},
		Required: []string{"task_id"},
	}
}

func (TaskStopTool) Parameters() map[string]interface{} {
	return taskStopSchema.ToJSONSchema()
}

// taskStopSchema is the single source of truth for TaskStop's input schema.
var taskStopSchema = TaskStopTool{}.Schema()

func (TaskStopTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[TaskStopInput]("TaskStop", input)
	if err != nil {
		return "", err
	}
	if p.TaskID == "" {
		var legacy struct {
			TaskID string `json:"taskId"`
		}
		_ = json.Unmarshal(input, &legacy)
		p.TaskID = legacy.TaskID
	}
	if err := killTask(ctx, p.TaskID); err != nil {
		return "", err
	}
	return fmt.Sprintf("Stopped background task %s", p.TaskID), nil
}
