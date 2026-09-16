package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

type todoItem struct {
	ID       int    `json:"id"`
	Task     string `json:"task"`
	Status   string `json:"status,omitempty"`
	Priority string `json:"priority,omitempty"`
	Done     bool   `json:"done"`
}

var (
	todoMu    sync.Mutex
	todoItems []todoItem
	todoNext  = 1
)

type TodoWriteTool struct{}

// TodoWriteInput is the typed input for TodoWriteTool.
type TodoWriteInput struct {
	Action string      `json:"action"`
	Task   string      `json:"task"`
	ID     int         `json:"id"`
	Todos  []todoInput `json:"todos"`
}

func (TodoWriteTool) Name() string      { return "TodoWrite" }
func (TodoWriteTool) Aliases() []string { return []string{"todo"} }
func (TodoWriteTool) Description() string {
	return "Manage a task list. Actions: add, complete, list, remove."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (TodoWriteTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action": {Type: "string", Enum: []interface{}{"add", "complete", "list", "remove"}, Description: "Action to perform"},
			"task":   {Type: "string", Description: "Task description (for add)"},
			"id":     {Type: "integer", Description: "Task ID (for complete/remove)"},
			"todos": {
				Type:        "array",
				Description: "Archive-compatible full todo list replacement",
				Items: &SchemaProperty{
					Type: "object",
					Properties: map[string]SchemaProperty{
						"content":  {Type: "string"},
						"task":     {Type: "string"},
						"status":   {Type: "string"},
						"priority": {Type: "string"},
					},
				},
			},
		},
	}
}

func (TodoWriteTool) Parameters() map[string]interface{} {
	return todoWriteSchema.ToJSONSchema()
}

// todoWriteSchema is the single source of truth for TodoWrite's input schema.
var todoWriteSchema = TodoWriteTool{}.Schema()

func (TodoWriteTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[TodoWriteInput]("TodoWrite", input)
	if err != nil {
		return "", err
	}
	todoMu.Lock()
	defer todoMu.Unlock()

	if p.Todos != nil {
		todoItems = todoItems[:0]
		todoNext = 1
		for _, in := range p.Todos {
			task := strings.TrimSpace(in.Content)
			if task == "" {
				task = strings.TrimSpace(in.Task)
			}
			if task == "" {
				continue
			}
			status := strings.TrimSpace(in.Status)
			done := status == "completed" || status == "done"
			todoItems = append(todoItems, todoItem{
				ID:       todoNext,
				Task:     task,
				Status:   status,
				Priority: strings.TrimSpace(in.Priority),
				Done:     done,
			})
			todoNext++
		}
		return fmt.Sprintf("Updated todo list (%d items):\n%s", len(todoItems), formatTodoItems()), nil
	}

	switch p.Action {
	case "add":
		if p.Task == "" {
			return "", fmt.Errorf("task is required for add")
		}
		todoItems = append(todoItems, todoItem{ID: todoNext, Task: p.Task})
		todoNext++
		return fmt.Sprintf("Added task #%d: %s", todoNext-1, p.Task), nil
	case "complete":
		for i := range todoItems {
			if todoItems[i].ID == p.ID {
				todoItems[i].Done = true
				return fmt.Sprintf("Completed task #%d", p.ID), nil
			}
		}
		return "", fmt.Errorf("task #%d not found", p.ID)
	case "remove":
		for i := range todoItems {
			if todoItems[i].ID == p.ID {
				todoItems = append(todoItems[:i], todoItems[i+1:]...)
				return fmt.Sprintf("Removed task #%d", p.ID), nil
			}
		}
		return "", fmt.Errorf("task #%d not found", p.ID)
	case "list":
		if len(todoItems) == 0 {
			return "No tasks.", nil
		}
		return formatTodoItems(), nil
	default:
		return "", fmt.Errorf("unknown action: %s", p.Action)
	}
}

type todoInput struct {
	Content  string `json:"content"`
	Task     string `json:"task"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

func formatTodoItems() string {
	var b strings.Builder
	for _, t := range todoItems {
		mark := "[ ]"
		if t.Done {
			mark = "[x]"
		}
		extra := ""
		if t.Status != "" || t.Priority != "" {
			var parts []string
			if t.Status != "" {
				parts = append(parts, "status="+t.Status)
			}
			if t.Priority != "" {
				parts = append(parts, "priority="+t.Priority)
			}
			extra = " (" + strings.Join(parts, ", ") + ")"
		}
		_, _ = fmt.Fprintf(&b, "%s #%d: %s%s\n", mark, t.ID, t.Task, extra)
	}
	return strings.TrimRight(b.String(), "\n")
}
