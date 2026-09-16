package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// TaskStatus represents the state of a task.
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusReviewing  TaskStatus = "reviewing"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
	TaskStatusSkipped    TaskStatus = "skipped"
	TaskStatusCancelled  TaskStatus = "cancelled"
)

// DefaultMaxAttempts is the retry budget used when a task does not declare
// its own MaxAttempts. After the budget is exhausted the task is left in the
// failed state for replanning rather than being retried forever.
const DefaultMaxAttempts = 3

// TaskDependency represents a typed dependency between tasks.
type TaskDependency struct {
	TargetID string `json:"targetId"`
	Type     string `json:"type"` // "blocks", "related", "parent-child"
}

// Task represents a structured task in the task list.
type Task struct {
	ID           string           `json:"id"`
	ParentID     string           `json:"parentId,omitempty"`
	Subject      string           `json:"subject"`
	Description  string           `json:"description"`
	ActiveForm   string           `json:"activeForm,omitempty"`
	Status       TaskStatus       `json:"status"`
	Owner        string           `json:"owner,omitempty"`
	Dependencies []TaskDependency `json:"dependencies"`
	Metadata     map[string]any   `json:"metadata,omitempty"`
	CreatedAt    time.Time        `json:"createdAt"`
	UpdatedAt    time.Time        `json:"updatedAt"`
	// Attempts is the number of times execution of this task has been
	// attempted. Incremented by MarkFailed; a task is requeued (back to
	// pending) while Attempts < MaxAttempts and parked in failed afterwards.
	Attempts int `json:"attempts,omitempty"`
	// MaxAttempts is the retry budget for this task. 0 means the store-wide
	// DefaultMaxAttempts.
	MaxAttempts int `json:"maxAttempts,omitempty"`
	// LastError records the most recent failure message for diagnostics and
	// replanning input.
	LastError string `json:"lastError,omitempty"`
	// Checkpoint holds arbitrary resumable progress (e.g. last completed
	// phase, partial outputs) so a replan or resume does not start from zero.
	Checkpoint map[string]any `json:"checkpoint,omitempty"`
}

// EffectiveMaxAttempts resolves the retry budget for a task.
func (t *Task) EffectiveMaxAttempts() int {
	if t.MaxAttempts > 0 {
		return t.MaxAttempts
	}
	return DefaultMaxAttempts
}

// TaskStore is a thread-safe in-memory store for tasks.
type TaskStore struct {
	mu      sync.RWMutex
	tasks   map[string]*Task
	next    int
	persist *persistState // non-nil when disk persistence is enabled
}

// Global task store.
var globalTaskStore = &TaskStore{tasks: make(map[string]*Task)}

// GetTaskStore returns the global task store.
func GetTaskStore() *TaskStore { return globalTaskStore }

func (s *TaskStore) Create(subject, description, activeForm string, metadata map[string]any) *Task {
	return s.CreateWithParent(subject, description, activeForm, metadata, "")
}

func (s *TaskStore) CreateWithParent(subject, description, activeForm string, metadata map[string]any, parentID string) *Task {
	s.mu.Lock()

	var id string
	if parentID != "" {
		// Count existing children of this parent
		childCount := 0
		for _, t := range s.tasks {
			if t.ParentID == parentID {
				childCount++
			}
		}
		id = fmt.Sprintf("%s.%d", parentID, childCount+1)
	} else {
		s.next++
		id = fmt.Sprintf("task_%d", s.next)
	}

	now := time.Now()
	t := &Task{
		ID:           id,
		ParentID:     parentID,
		Subject:      subject,
		Description:  description,
		ActiveForm:   activeForm,
		Status:       TaskStatusPending,
		Dependencies: []TaskDependency{},
		Metadata:     metadata,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if parentID != "" {
		t.Dependencies = append(t.Dependencies, TaskDependency{TargetID: parentID, Type: "parent-child"})
	}
	s.tasks[id] = t
	s.mu.Unlock()
	s.persistOnMutation()
	return t
}

func (s *TaskStore) Get(id string) (*Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	return t, ok
}

func (s *TaskStore) List() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, t)
	}
	return out
}

func (s *TaskStore) Update(id string, fn func(*Task)) bool {
	s.mu.Lock()
	t, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return false
	}
	fn(t)
	t.UpdatedAt = time.Now()
	s.mu.Unlock()
	s.persistOnMutation()
	return true
}

func (s *TaskStore) Delete(id string) bool {
	s.mu.Lock()
	_, ok := s.tasks[id]
	if ok {
		delete(s.tasks, id)
	}
	s.mu.Unlock()
	if ok {
		s.persistOnMutation()
	}
	return ok
}

func (s *TaskStore) Reset() {
	s.mu.Lock()
	s.tasks = make(map[string]*Task)
	s.next = 0
	s.mu.Unlock()
	s.persistOnMutation()
}

// GetReadyWork returns pending tasks with no open blocking dependencies.
func (s *TaskStore) GetReadyWork() []*Task {
	schedule, err := s.Schedule()
	if err != nil {
		return []*Task{}
	}
	return schedule.Ready
}

// GetSchedule returns the validated topological scheduling view.
func (s *TaskStore) GetSchedule() (TaskSchedule, error) { return s.Schedule() }

// CompactCompleted removes completed tasks and returns a summary.
func (s *TaskStore) CompactCompleted() string {
	s.mu.Lock()
	var removed []string
	for id, t := range s.tasks {
		if t.Status == TaskStatusCompleted {
			removed = append(removed, id)
			delete(s.tasks, id)
		}
	}
	s.mu.Unlock()
	s.persistOnMutation()
	if len(removed) == 0 {
		return "No completed tasks to compact."
	}
	return fmt.Sprintf("Compacted %d completed task(s): %s", len(removed), strings.Join(removed, ", "))
}

// TaskCreateTool creates a new task in the task list.
type TaskCreateTool struct{}

// TaskCreateInput is the typed input for TaskCreateTool.
type TaskCreateInput struct {
	Subject      string           `json:"subject"`
	Description  string           `json:"description"`
	ActiveForm   string           `json:"activeForm"`
	ParentID     string           `json:"parentId"`
	Dependencies []TaskDependency `json:"dependencies"`
	Metadata     map[string]any   `json:"metadata"`
}

func (TaskCreateTool) Name() string        { return "TaskCreate" }
func (TaskCreateTool) Aliases() []string   { return []string{"task_create"} }
func (TaskCreateTool) Description() string { return "Create a new task in the task list" }

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (TaskCreateTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"subject":     {Type: "string", Description: "A brief title for the task"},
			"description": {Type: "string", Description: "What needs to be done"},
			"activeForm":  {Type: "string", Description: "Present continuous form shown in spinner when in_progress (e.g., \"Running tests\")"},
			"parentId":    {Type: "string", Description: "Parent task ID for hierarchical tasks"},
			"dependencies": {Type: "array", Items: &SchemaProperty{
				Type: "object",
				Properties: map[string]SchemaProperty{
					"targetId": {Type: "string"},
					"type":     {Type: "string", Enum: []interface{}{"blocks", "related", "parent-child"}},
				},
			}, Description: "Typed dependencies"},
			"metadata": {Type: "object", Description: "Arbitrary metadata to attach to the task"},
		},
		Required: []string{"subject", "description"},
	}
}

func (TaskCreateTool) Parameters() map[string]interface{} {
	return taskCreateSchema.ToJSONSchema()
}

// taskCreateSchema is the single source of truth for TaskCreate's input schema.
var taskCreateSchema = TaskCreateTool{}.Schema()

func (TaskCreateTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[TaskCreateInput]("TaskCreate", input)
	if err != nil {
		return "", err
	}
	if p.Subject == "" {
		return "", fmt.Errorf("subject is required")
	}
	if p.Description == "" {
		return "", fmt.Errorf("description is required")
	}
	task := globalTaskStore.CreateWithParent(p.Subject, p.Description, p.ActiveForm, p.Metadata, p.ParentID)
	if len(p.Dependencies) > 0 {
		globalTaskStore.Update(task.ID, func(t *Task) {
			t.Dependencies = append(t.Dependencies, p.Dependencies...)
		})
	}
	out, _ := json.Marshal(map[string]any{
		"task": map[string]any{"id": task.ID, "subject": task.Subject, "parentId": task.ParentID},
	})
	return string(out), nil
}

// TaskGetTool retrieves a task by ID.
type TaskGetTool struct{}

// TaskGetInput is the typed input for TaskGetTool.
type TaskGetInput struct {
	TaskID string `json:"taskId"`
}

func (TaskGetTool) Name() string        { return "TaskGet" }
func (TaskGetTool) Aliases() []string   { return []string{"task_get"} }
func (TaskGetTool) Description() string { return "Get a task by ID from the task list" }

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (TaskGetTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"taskId": {Type: "string", Description: "The ID of the task to retrieve"},
		},
		Required: []string{"taskId"},
	}
}

func (TaskGetTool) Parameters() map[string]interface{} {
	return taskGetSchema.ToJSONSchema()
}

// taskGetSchema is the single source of truth for TaskGet's input schema.
var taskGetSchema = TaskGetTool{}.Schema()

func (TaskGetTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[TaskGetInput]("TaskGet", input)
	if err != nil {
		return "", err
	}
	task, ok := globalTaskStore.Get(p.TaskID)
	if !ok {
		out, _ := json.Marshal(map[string]any{"task": nil})
		return string(out), nil
	}
	out, _ := json.Marshal(map[string]any{
		"task": map[string]any{
			"id":           task.ID,
			"parentId":     task.ParentID,
			"subject":      task.Subject,
			"description":  task.Description,
			"status":       task.Status,
			"dependencies": task.Dependencies,
			"attempts":     task.Attempts,
			"maxAttempts":  task.EffectiveMaxAttempts(),
			"lastError":    task.LastError,
			"checkpoint":   task.Checkpoint,
			"owner":        task.Owner,
			"activeForm":   task.ActiveForm,
			"createdAt":    task.CreatedAt,
			"updatedAt":    task.UpdatedAt,
			"retryBackoff": task.Metadata["retryBackoffTick"],
		},
	})
	return string(out), nil
}

// TaskListTool lists all tasks.
type TaskListTool struct{}

// TaskListInput is the typed input for TaskListTool.
type TaskListInput struct {
	Action string `json:"action"`
}

func (TaskListTool) Name() string        { return "TaskList" }
func (TaskListTool) Aliases() []string   { return []string{"task_list"} }
func (TaskListTool) Description() string { return "List all tasks, ready tasks, or compact completed" }

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (TaskListTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action": {Type: "string", Enum: []interface{}{"list", "ready", "failed", "compact"}, Description: "Action: list (default), ready (pending with no blockers), failed (replan candidates), compact (remove completed)"},
		},
	}
}

func (TaskListTool) Parameters() map[string]interface{} {
	return taskListSchema.ToJSONSchema()
}

// taskListSchema is the single source of truth for TaskList's input schema.
var taskListSchema = TaskListTool{}.Schema()

func (TaskListTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	var p TaskListInput
	if len(input) > 0 && string(input) != "null" {
		decoded, err := DecodeInput[TaskListInput]("TaskList", input)
		if err != nil {
			return "", err
		}
		p = decoded
	}

	switch p.Action {
	case "ready":
		schedule, err := globalTaskStore.GetSchedule()
		if err != nil {
			return "", fmt.Errorf("validate task schedule: %w", err)
		}
		tasks := schedule.Ready
		summaries := make([]map[string]any, 0, len(tasks))
		for _, t := range tasks {
			summaries = append(summaries, map[string]any{
				"id":           t.ID,
				"subject":      t.Subject,
				"status":       t.Status,
				"owner":        t.Owner,
				"dependencies": t.Dependencies,
			})
		}
		out, _ := json.Marshal(map[string]any{"tasks": summaries, "waves": schedule.Waves})
		return string(out), nil
	case "failed":
		tasks := globalTaskStore.FailedTasks()
		summaries := make([]map[string]any, 0, len(tasks))
		for _, t := range tasks {
			summaries = append(summaries, map[string]any{
				"id":         t.ID,
				"subject":    t.Subject,
				"status":     t.Status,
				"attempts":   t.Attempts,
				"lastError":  t.LastError,
				"checkpoint": t.Checkpoint,
				"owner":      t.Owner,
			})
		}
		out, _ := json.Marshal(map[string]any{"tasks": summaries})
		return string(out), nil
	case "compact":
		summary := globalTaskStore.CompactCompleted()
		out, _ := json.Marshal(map[string]any{"result": summary})
		return string(out), nil
	default:
		tasks := globalTaskStore.List()
		summaries := make([]map[string]any, 0, len(tasks))
		for _, t := range tasks {
			summaries = append(summaries, map[string]any{
				"id":           t.ID,
				"subject":      t.Subject,
				"status":       t.Status,
				"owner":        t.Owner,
				"dependencies": t.Dependencies,
			})
		}
		out, _ := json.Marshal(map[string]any{"tasks": summaries})
		return string(out), nil
	}
}

// TaskUpdateTool updates task fields.
type TaskUpdateTool struct{}

// TaskUpdateInput is the typed input for TaskUpdateTool.
type TaskUpdateInput struct {
	TaskID       string           `json:"taskId"`
	Status       string           `json:"status"`
	Owner        string           `json:"owner"`
	Dependencies []TaskDependency `json:"dependencies"`
}

func (TaskUpdateTool) Name() string        { return "TaskUpdate" }
func (TaskUpdateTool) Aliases() []string   { return []string{"task_update"} }
func (TaskUpdateTool) Description() string { return "Update a task's status, owner, or dependencies" }

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (TaskUpdateTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"taskId": {Type: "string", Description: "The ID of the task to update"},
			"status": {Type: "string", Enum: []interface{}{"pending", "in_progress", "reviewing", "completed", "failed", "skipped", "cancelled"}, Description: "New task status"},
			"owner":  {Type: "string", Description: "Agent name to assign"},
			"dependencies": {Type: "array", Items: &SchemaProperty{
				Type: "object",
				Properties: map[string]SchemaProperty{
					"targetId": {Type: "string"},
					"type":     {Type: "string", Enum: []interface{}{"blocks", "related", "parent-child"}},
				},
			}, Description: "Replace dependencies"},
		},
		Required: []string{"taskId"},
	}
}

func (TaskUpdateTool) Parameters() map[string]interface{} {
	return taskUpdateSchema.ToJSONSchema()
}

// taskUpdateSchema is the single source of truth for TaskUpdate's input schema.
var taskUpdateSchema = TaskUpdateTool{}.Schema()

func (TaskUpdateTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[TaskUpdateInput]("TaskUpdate", input)
	if err != nil {
		return "", err
	}
	if p.TaskID == "" {
		return "", fmt.Errorf("taskId is required")
	}
	ok := globalTaskStore.Update(p.TaskID, func(t *Task) {
		if p.Status != "" {
			// Any explicit status change out of failed is a replan or terminal
			// signal: reset the retry budget and error so it starts fresh.
			if t.Status == TaskStatusFailed && TaskStatus(p.Status) != TaskStatusFailed {
				t.Attempts = 0
				t.LastError = ""
			}
			t.Status = TaskStatus(p.Status)
		}
		if p.Owner != "" {
			t.Owner = p.Owner
		}
		if p.Dependencies != nil {
			t.Dependencies = p.Dependencies
		}
	})
	if !ok {
		return "", fmt.Errorf("task %q not found", p.TaskID)
	}
	task, _ := globalTaskStore.Get(p.TaskID)
	out, _ := json.Marshal(map[string]any{
		"task": map[string]any{
			"id":      task.ID,
			"subject": task.Subject,
			"status":  task.Status,
			"owner":   task.Owner,
		},
	})
	return string(out), nil
}
