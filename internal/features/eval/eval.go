package eval

// Framework for evaluating rho's coding performance against benchmarks.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/GrayCodeAI/rho/internal/intelligence/consistency"
)

// BenchmarkSuite represents a collection of benchmark tasks for evaluation.
type BenchmarkSuite struct {
	Name    string
	Tasks   []BenchmarkTask
	Results []TaskResult
}

// BenchmarkTask defines a single coding task to evaluate.
type BenchmarkTask struct {
	ID          string
	Description string
	SetupFn     func(workDir string) error
	ValidateFn  func(workDir string) (bool, string)
	Prompt      string
	TimeLimit   time.Duration
	Tags        []string
	MaxAttempts int
	Filters     []Filter
}

// TaskResult captures the outcome of running a single benchmark task.
type TaskResult struct {
	TaskID     string
	Passed     bool
	Duration   time.Duration
	TokensUsed int
	CostUSD    float64
	Attempts   int
	Error      string
}

// SuiteResult aggregates results from running an entire benchmark suite.
type SuiteResult struct {
	Suite         string
	TotalTasks    int
	Passed        int
	Failed        int
	TotalDuration time.Duration
	TotalTokens   int
	TotalCostUSD  float64
	PassRate      float64
	Results       []TaskResult
}

// Runner executes benchmark tasks against a specific model/provider.
type Runner struct {
	Model       string
	Provider    string
	MaxAttempts int
	Timeout     time.Duration
	LLM         LLMClient
	Cache       *Cache
	NoCache     bool
	Filters     []Filter
	// Samples, when > 1, makes RunConsensus run each task this many times and
	// decide the verdict by majority consensus (self-consistency, arXiv
	// 2203.11171). It has no effect on Run/RunSingle.
	Samples int
	// Progress, when non-nil, is invoked before each task runs with the
	// zero-based task index, the total task count, and the task ID. It lets
	// callers surface live per-task progress for long benchmark suites.
	Progress func(i, total int, taskID string)
}

// LLMClient is the interface for invoking an LLM during evaluation.
type LLMClient interface {
	Complete(ctx context.Context, model, prompt string) (response string, tokens int, cost float64, err error)
}

// NewRunner creates a Runner configured for the given model and provider.
func NewRunner(model, provider string) *Runner {
	return &Runner{
		Model:       model,
		Provider:    provider,
		MaxAttempts: 3,
		Timeout:     5 * time.Minute,
	}
}

// Run executes all tasks in a benchmark suite and returns aggregated results.
func (r *Runner) Run(ctx context.Context, suite *BenchmarkSuite) (*SuiteResult, error) {
	if suite == nil {
		return nil, fmt.Errorf("suite cannot be nil")
	}

	result := &SuiteResult{
		Suite:      suite.Name,
		TotalTasks: len(suite.Tasks),
		Results:    make([]TaskResult, 0, len(suite.Tasks)),
	}

	startTime := time.Now()

	for i := range suite.Tasks {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		if r.Progress != nil {
			r.Progress(i, len(suite.Tasks), suite.Tasks[i].ID)
		}

		taskResult, err := r.RunSingle(ctx, &suite.Tasks[i])
		if err != nil {
			taskResult = &TaskResult{
				TaskID: suite.Tasks[i].ID,
				Passed: false,
				Error:  err.Error(),
			}
		}

		result.Results = append(result.Results, *taskResult)
		if taskResult.Passed {
			result.Passed++
		} else {
			result.Failed++
		}
		result.TotalTokens += taskResult.TokensUsed
		result.TotalCostUSD += taskResult.CostUSD
	}

	result.TotalDuration = time.Since(startTime)
	if result.TotalTasks > 0 {
		result.PassRate = float64(result.Passed) / float64(result.TotalTasks)
	}

	// Store results back into the suite for reference.
	suite.Results = result.Results

	return result, nil
}

// RunSingle executes a single benchmark task in an isolated temporary directory.
func (r *Runner) RunSingle(ctx context.Context, task *BenchmarkTask) (*TaskResult, error) {
	if task == nil {
		return nil, fmt.Errorf("task cannot be nil")
	}

	timeout := r.Timeout
	if task.TimeLimit > 0 {
		timeout = task.TimeLimit
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result := &TaskResult{
		TaskID:   task.ID,
		Attempts: 0,
	}

	var lastErr error

	maxAttempts := r.MaxAttempts
	if task.MaxAttempts > 0 {
		maxAttempts = task.MaxAttempts
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			result.Error = fmt.Sprintf("context cancelled after %d attempts: %v", attempt-1, ctx.Err())
			return result, nil
		default:
		}

		result.Attempts = attempt

		// Run each attempt in a closure so its work directory is removed when
		// the attempt finishes. Deferring os.RemoveAll directly in the retry
		// loop would accumulate every attempt's directory until RunTask returns.
		passed, attemptErr := func() (bool, error) {
			// Create isolated work directory for this attempt.
			workDir, err := os.MkdirTemp("", fmt.Sprintf("rho-eval-%s-*", task.ID))
			if err != nil {
				return false, fmt.Errorf("failed to create temp dir: %w", err)
			}
			defer func() { _ = os.RemoveAll(workDir) }()

			// Run setup to create the initial buggy/incomplete code.
			startTime := time.Now()
			if err := task.SetupFn(workDir); err != nil {
				return false, fmt.Errorf("setup failed: %w", err)
			}

			// Invoke LLM to fix/complete the code
			if r.LLM != nil {
				var llmResponse string
				if !r.NoCache && r.Cache != nil {
					if entry := r.Cache.Get(r.Model, task.Prompt); entry != nil {
						llmResponse = entry.Response
						result.TokensUsed += entry.Tokens
						result.CostUSD += entry.CostUSD
						goto applyResponse
					}
				}
				{
					resp, tokens, cost, err := r.LLM.Complete(ctx, r.Model, task.Prompt)
					if err != nil {
						return false, fmt.Errorf("LLM call failed: %w", err)
					}
					llmResponse = resp
					result.TokensUsed += tokens
					result.CostUSD += cost
					if !r.NoCache && r.Cache != nil {
						_ = r.Cache.Put(r.Model, task.Prompt, resp, tokens, cost)
					}
				}
			applyResponse:
				// Apply filters to extract code from response
				filters := r.Filters
				if len(filters) == 0 {
					filters = task.Filters
				}
				filtered := ApplyFilters(llmResponse, filters...)
				// Write solution to work directory
				ext := ".go"
				_ = os.WriteFile(filepath.Join(workDir, "solution"+ext), []byte(filtered), 0o600)
			}

			passed, msg := task.ValidateFn(workDir)
			result.Duration = time.Since(startTime)

			if passed {
				result.Passed = true
				return true, nil
			}

			return false, fmt.Errorf("validation failed: %s", msg)
		}()

		if attemptErr != nil {
			lastErr = attemptErr
			continue
		}
		if passed {
			return result, nil
		}
	}

	if lastErr != nil {
		result.Error = lastErr.Error()
	}
	return result, nil
}

// RunConsensus runs a task Samples times (independent LLM calls) and decides
// the final verdict by majority consensus over the per-sample pass/fail
// results — self-consistency (Wang et al., ICLR 2023, arXiv 2203.11171).
// Sampling N diverse solutions and taking the majority is more reliable than a
// single greedy run. It returns a TaskResult whose Passed is the consensus and
// whose cost/tokens/duration aggregate the samples.
func (r *Runner) RunConsensus(ctx context.Context, task *BenchmarkTask) (*TaskResult, error) {
	if task == nil {
		return nil, fmt.Errorf("task cannot be nil")
	}
	n := r.Samples
	if n <= 0 {
		n = 3
	}
	result := &TaskResult{TaskID: task.ID, Attempts: n}
	verdicts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		single, err := r.RunSingle(ctx, task)
		if err != nil {
			return nil, err
		}
		verdict := "FAIL"
		if single.Passed {
			verdict = "PASS"
		}
		verdicts = append(verdicts, verdict)
		result.TokensUsed += single.TokensUsed
		result.CostUSD += single.CostUSD
		result.Duration += single.Duration
	}
	consensusVerdict, _ := consistency.Consensus(verdicts)
	result.Passed = consensusVerdict == "PASS"
	return result, nil
}
