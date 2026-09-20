package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	"github.com/GrayCodeAI/rho/internal/features/eval"
	"github.com/GrayCodeAI/rho/internal/features/evalloop"
	"github.com/GrayCodeAI/rho/internal/tool"
	"github.com/spf13/cobra"
)

var (
	evalTasks       string
	evalModel       string
	evalTags        string
	evalNoCache     bool
	evalOutput      string
	evalTaskDir     string
	evalListJSON    bool
	evalResultsJSON bool
	evalLoopPrompt  string
	evalLoopModel   string
	evalLoopReport  bool
)

var evalCmd = &cobra.Command{
	Use:   "eval",
	Short: "Evaluate model performance on coding benchmarks",
}

var evalRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run evaluation tasks",
	RunE:  runEval,
}

var evalListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available evaluation tasks",
	RunE:  runEvalList,
}

var evalResultsCmd = &cobra.Command{
	Use:   "results",
	Short: "Show past evaluation results",
	RunE:  runEvalResults,
}

var evalCacheCmd = &cobra.Command{
	Use:   "cache-clear",
	Short: "Clear the evaluation cache",
	RunE: func(_ *cobra.Command, _ []string) error {
		cache := eval.DefaultCache()
		if err := cache.Clear(); err != nil {
			return err
		}
		fmt.Println(auditTint("Cache cleared.", doneGreen))
		return nil
	},
}

var evalLoopCmd = &cobra.Command{
	Use:   "loop",
	Short: "Evaluate the agent end-to-end through its real tool loop",
	RunE:  runEvalLoop,
}

func init() {
	evalRunCmd.Flags().StringVar(&evalTasks, "tasks", "", "Comma-separated task IDs (default: all)")
	evalRunCmd.Flags().StringVar(&evalModel, "model", "", "Model to evaluate")
	evalRunCmd.Flags().StringVar(&evalTags, "tags", "", "Filter tasks by tags")
	evalRunCmd.Flags().BoolVar(&evalNoCache, "no-cache", false, "Disable result caching")
	evalRunCmd.Flags().StringVarP(&evalOutput, "output", "o", "markdown", "Output format: markdown, json")
	evalRunCmd.Flags().StringVar(&evalTaskDir, "task-dir", "", "Directory with YAML task definitions")
	evalListCmd.Flags().BoolVar(&evalListJSON, "json", false, "output tasks as JSON")
	evalResultsCmd.Flags().BoolVar(&evalResultsJSON, "json", false, "output results as JSON")
	evalLoopCmd.Flags().StringVar(&evalLoopPrompt, "prompt", "", "Task prompt to run through the agent loop")
	evalLoopCmd.Flags().StringVar(&evalLoopModel, "model", "", "Model to use (defaults to active model)")
	evalLoopCmd.Flags().BoolVar(&evalLoopReport, "report", false, "print the comparative/reproducibility report")

	evalCmd.AddCommand(evalRunCmd)
	evalCmd.AddCommand(evalListCmd)
	evalCmd.AddCommand(evalResultsCmd)
	evalCmd.AddCommand(evalCacheCmd)
	evalCmd.AddCommand(evalLoopCmd)
}

// runEvalLoop runs the agent end-to-end through its real tool loop in an
// isolated temp directory and prints a JSON report with the transcript path.
func runEvalLoop(cmd *cobra.Command, _ []string) error {
	if strings.TrimSpace(evalLoopPrompt) == "" {
		return fmt.Errorf("--prompt is required")
	}
	settings := rhoconfig.LoadGlobalSettings()
	ctx := context.Background()

	gw, err := rhoconfig.NewFluxEngineForSettings(settings)
	if err != nil {
		return fmt.Errorf("eval loop: build engine client: %w", err)
	}
	model := strings.TrimSpace(evalLoopModel)
	if model == "" {
		model = strings.TrimSpace(rhoconfig.ActiveModel(ctx))
	}
	if model == "" {
		model = strings.TrimSpace(settings.Model)
	}

	workDir, err := os.MkdirTemp("", "rho-eval-loop-*")
	if err != nil {
		return fmt.Errorf("eval loop: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	cfg := evalloop.DefaultConfig()
	runtime := evalloop.NewSessionRuntime(gw.ChatClient(), "eval", model, tool.NewRegistry(), cfg)

	// The agent loop is the slow part; show a TTY-only animated indicator.
	// It clears before the JSON report prints, so piped and structured
	// output stay pure.
	prog := NewCLIProgress("Eval", []string{"Running agent loop"})
	defer prog.Abort()
	prog.StartStep(0)
	result, err := runtime.Run(ctx, workDir, evalLoopPrompt)
	if err != nil {
		return fmt.Errorf("eval loop: %w", err)
	}
	prog.CompleteStep(0)
	prog.Done()

	transcriptPath := ""
	if len(result.Transcript) > 0 {
		transcriptPath = filepath.Join(workDir, "transcript.json")
		_ = os.WriteFile(transcriptPath, result.Transcript, 0o600) // #nosec G304 -- path is the isolated eval temp dir
	}

	report := map[string]any{
		"model":           model,
		"output":          result.Output,
		"events":          len(result.Events),
		"tokens_used":     result.TokensUsed,
		"cost_usd":        result.CostUSD,
		"duration":        result.Duration.String(),
		"transcript_path": transcriptPath,
		"repro_hash":      result.ReproHash,
	}
	if evalLoopReport {
		cmp := evalloop.Compare([]evalloop.Result{result})
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), evalloop.FormatComparison(cmp)); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(append(data, '\n'))
	return err
}

func runEval(_ *cobra.Command, _ []string) error {
	// Load tasks
	goSuite := eval.GoTasks()
	tasks := goSuite.Tasks

	// Load YAML tasks if directory specified
	if evalTaskDir != "" {
		yamlTasks, err := eval.LoadTasksFromYAML(evalTaskDir)
		if err != nil {
			return fmt.Errorf("loading YAML tasks: %w", err)
		}
		tasks = append(tasks, yamlTasks...)
	}

	// Filter by task IDs
	if evalTasks != "" {
		ids := strings.Split(evalTasks, ",")
		idSet := make(map[string]bool)
		for _, id := range ids {
			idSet[strings.TrimSpace(id)] = true
		}
		var filtered []eval.BenchmarkTask
		for _, t := range tasks {
			if idSet[t.ID] {
				filtered = append(filtered, t)
			}
		}
		tasks = filtered
	}

	// Filter by tags
	if evalTags != "" {
		tags := strings.Split(evalTags, ",")
		tagSet := make(map[string]bool)
		for _, t := range tags {
			tagSet[strings.TrimSpace(t)] = true
		}
		var filtered []eval.BenchmarkTask
		for _, t := range tasks {
			for _, tag := range t.Tags {
				if tagSet[tag] {
					filtered = append(filtered, t)
					break
				}
			}
		}
		tasks = filtered
	}

	if len(tasks) == 0 {
		return fmt.Errorf("no tasks matched the given filters")
	}

	modelName := evalModel
	if modelName == "" {
		modelName = "default"
	}

	fmt.Printf("%s\n", auditTint(fmt.Sprintf("Running %d tasks with model %s...", len(tasks), modelName), textPrimary))

	suite := &eval.BenchmarkSuite{Name: "rho-eval", Tasks: tasks}
	runner := eval.NewRunner(modelName, "")
	runner.NoCache = evalNoCache
	if !evalNoCache {
		runner.Cache = eval.DefaultCache()
	}
	runner.Filters = []eval.Filter{eval.ExtractCodeBlock("go")}

	// Animate one step per benchmark task. The eval runner invokes the
	// callback before each task, so we close the previous step and open the
	// next. Quiet mode suppresses the animation entirely.
	var prog *CLIProgress
	if !IsQuiet() {
		names := make([]string, len(tasks))
		for i := range tasks {
			names[i] = tasks[i].ID
		}
		prog = NewCLIProgress("Eval", names)
		defer prog.Abort()
		runner.Progress = func(i, _ int, _ string) {
			if i > 0 {
				prog.CompleteStep(i - 1)
			}
			prog.StartStep(i)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	result, err := runner.Run(ctx, suite)
	if err != nil {
		return err
	}
	if prog != nil {
		prog.CompleteStep(len(tasks) - 1)
		prog.Done()
	}

	// Compute reproducibility hash
	hash := eval.ComputeHash(tasks)

	// Save results
	store := eval.DefaultResultStore()
	path, err := store.Save(result, model, "", hash)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", auditTint(fmt.Sprintf("Warning: failed to save results: %v", err), warnAmber))
	} else {
		fmt.Printf("%s\n", auditTint("Results saved to: ", doneGreen)+auditTint(path, textPrimary))
	}

	// Group results
	groups := eval.GroupTasks(tasks, eval.DefaultGroups())
	groupResults := eval.AggregateGroupResults(groups, result.Results)

	// Output
	switch evalOutput {
	case "json":
		type jsonOutput struct {
			*eval.SuiteResult
			Groups []eval.GroupResult `json:"groups,omitempty"`
		}
		out := jsonOutput{SuiteResult: result, Groups: groupResults}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
	default:
		fmt.Println(eval.GenerateReport(result))
		if len(groupResults) > 0 {
			fmt.Println("## Group Results")
			fmt.Println("| Group | Pass Rate |")
			fmt.Println("|-------|-----------|")
			for _, gr := range groupResults {
				if gr.Total > 0 {
					fmt.Printf("| %s | %.0f%% (%d/%d) |\n", gr.Name, gr.PassRate*100, gr.Passed, gr.Total)
				}
			}
		}
	}

	return nil
}

func runEvalList(_ *cobra.Command, _ []string) error {
	suite := eval.GoTasks()
	tasks := suite.Tasks

	if evalTaskDir != "" {
		yamlTasks, err := eval.LoadTasksFromYAML(evalTaskDir)
		if err != nil {
			return err
		}
		tasks = append(tasks, yamlTasks...)
	}

	if evalListJSON {
		// BenchmarkTask carries func fields (SetupFn/ValidateFn) that
		// encoding/json cannot marshal; project to the display-safe fields.
		type jsonTask struct {
			ID          string   `json:"id"`
			Description string   `json:"description"`
			Prompt      string   `json:"prompt"`
			TimeLimit   float64  `json:"time_limit_seconds"`
			Tags        []string `json:"tags"`
			MaxAttempts int      `json:"max_attempts"`
		}
		view := make([]jsonTask, len(tasks))
		for i, t := range tasks {
			view[i] = jsonTask{
				ID:          t.ID,
				Description: t.Description,
				Prompt:      t.Prompt,
				TimeLimit:   t.TimeLimit.Seconds(),
				Tags:        t.Tags,
				MaxAttempts: t.MaxAttempts,
			}
		}
		out, err := json.MarshalIndent(view, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling tasks: %w", err)
		}
		fmt.Println(string(out))
		return nil
	}

	fmt.Printf("%s\n\n", auditTint(fmt.Sprintf("Available tasks (%d):", len(tasks)), textPrimary))
	fmt.Println("| ID | Description | Tags |")
	fmt.Println("|----|-------------|------|")
	for _, t := range tasks {
		tags := strings.Join(t.Tags, ", ")
		desc := truncateWithEllipsis(t.Description, 53)
		fmt.Printf("| %s | %s | %s |\n", t.ID, desc, tags)
	}
	return nil
}

func runEvalResults(_ *cobra.Command, _ []string) error {
	store := eval.DefaultResultStore()
	files, err := store.List()
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Println(auditTint("No saved results found.", textMuted))
		return nil
	}

	if evalResultsJSON {
		var allResults []eval.StoredResult
		for _, f := range files {
			r, err := store.Load(f)
			if err != nil {
				continue
			}
			allResults = append(allResults, *r)
		}
		out, err := json.MarshalIndent(allResults, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling results: %w", err)
		}
		fmt.Println(string(out))
		return nil
	}

	fmt.Printf("%s\n\n", auditTint(fmt.Sprintf("Saved results (%d):", len(files)), textPrimary))
	for _, f := range files {
		r, err := store.Load(f)
		if err != nil {
			continue
		}
		fmt.Printf("  %s  %s  %s  %s\n",
			auditTint(r.Timestamp.Format("2006-01-02 15:04"), textMuted),
			auditTint(r.Model, textPrimary),
			auditTint(r.Suite, textPrimary),
			auditTint(fmt.Sprintf("%.0f%% (%d/%d)", r.Summary.PassRate*100, r.Summary.Passed, r.Summary.TotalTasks), doneGreen))
	}
	return nil
}
