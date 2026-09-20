// Package parallel owns request validation for multi-agent execution.
package parallel

import (
	"fmt"
	"strconv"
	"strings"
)

// Request is the validated, execution-ready parallel-agent request.
type Request struct {
	Workers int
	Tasks   []string
}

// ParseError preserves whether the caller should present a usage hint or an
// invalid-input error, without depending on a particular UI framework.
type ParseError struct {
	Usage bool
	Text  string
}

func (e *ParseError) Error() string { return e.Text }

// ParseRequest validates /parallel arguments and splits the task list on '|'.
func ParseRequest(parts []string) (Request, error) {
	if len(parts) < 3 {
		return Request{}, &ParseError{
			Usage: true,
			Text:  "Usage: /parallel <N> <task1> | <task2> | ...\nExample: /parallel 3 Fix auth bug | Add logging | Update tests",
		}
	}
	workers, err := strconv.Atoi(parts[1])
	if err != nil || workers < 1 || workers > 8 {
		return Request{}, &ParseError{Text: "Worker count must be 1-8"}
	}

	taskDescs := strings.Split(strings.Join(parts[2:], " "), "|")
	for i := range taskDescs {
		taskDescs[i] = strings.TrimSpace(taskDescs[i])
	}
	if len(taskDescs) < 2 {
		return Request{}, &ParseError{Text: "Need at least 2 tasks separated by |"}
	}
	for _, task := range taskDescs {
		if task == "" {
			return Request{}, &ParseError{Text: "Parallel tasks cannot be empty"}
		}
	}
	return Request{Workers: workers, Tasks: taskDescs}, nil
}

// Summary returns a stable human-readable launch summary.
func (r Request) Summary() string {
	return fmt.Sprintf("%d workers, %d tasks", r.Workers, len(r.Tasks))
}
