package eval

// TaskGroup defines a named collection of tasks with aggregated metrics.
type TaskGroup struct {
	Name  string
	Tags  []string // tasks matching any of these tags belong to this group
	Tasks []BenchmarkTask
}

// GroupResult holds aggregated metrics for a task group.
type GroupResult struct {
	Name     string  `json:"name"`
	Total    int     `json:"total"`
	Passed   int     `json:"passed"`
	PassRate float64 `json:"pass_rate"`
}

// DefaultGroups returns the standard task groupings.
func DefaultGroups() []TaskGroup {
	return []TaskGroup{
		{Name: "bug-fixing", Tags: []string{"bug-fix", "nil-safety", "concurrency"}},
		{Name: "implementation", Tags: []string{"implementation", "algorithm", "networking"}},
		{Name: "refactoring", Tags: []string{"refactoring", "design-pattern"}},
	}
}

// GroupTasks assigns tasks to groups based on tag matching.
func GroupTasks(tasks []BenchmarkTask, groups []TaskGroup) []TaskGroup {
	result := make([]TaskGroup, len(groups))
	for i, g := range groups {
		result[i] = TaskGroup{Name: g.Name, Tags: g.Tags}
		tagSet := make(map[string]bool)
		for _, t := range g.Tags {
			tagSet[t] = true
		}
		for _, task := range tasks {
			for _, tag := range task.Tags {
				if tagSet[tag] {
					result[i].Tasks = append(result[i].Tasks, task)
					break
				}
			}
		}
	}
	return result
}

// AggregateGroupResults computes pass rates per group from task results.
func AggregateGroupResults(groups []TaskGroup, results []TaskResult) []GroupResult {
	resultMap := make(map[string]*TaskResult)
	for i := range results {
		resultMap[results[i].TaskID] = &results[i]
	}

	var out []GroupResult
	for _, g := range groups {
		gr := GroupResult{Name: g.Name, Total: len(g.Tasks)}
		for _, task := range g.Tasks {
			if r, ok := resultMap[task.ID]; ok && r.Passed {
				gr.Passed++
			}
		}
		if gr.Total > 0 {
			gr.PassRate = float64(gr.Passed) / float64(gr.Total)
		}
		out = append(out, gr)
	}
	return out
}
