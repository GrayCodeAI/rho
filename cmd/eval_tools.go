package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	"github.com/GrayCodeAI/rho/internal/features/eval"
	"github.com/GrayCodeAI/rho/internal/types"
	"github.com/spf13/cobra"
)

var evalToolsOutput string

func init() {
	evalToolsCmd.Flags().StringVarP(&evalToolsOutput, "output", "o", "markdown", "Output format: markdown, json")
	evalCmd.AddCommand(evalToolsCmd)
}

var evalToolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Evaluate tool selection: trigger confusion matrix + payload accuracy",
	Long: "Run a model-in-the-loop tool-use evaluation. Each case is a prompt with an " +
		"expected tool (or none). Triggering (did the model call a tool when it should) " +
		"is scored as a confusion matrix, separately from payload accuracy (right tool + args).",
	RunE: runEvalTools,
}

// defaultToolUseCases is a small built-in set exercising clear positive and
// negative tool-trigger situations against rho's standard tools.
func defaultToolUseCases() []eval.ToolUseCase {
	return []eval.ToolUseCase{
		{
			ID:       "read-existing-file",
			Prompt:   "Show me the contents of go.mod.",
			Expected: &eval.ExpectedCall{Tool: "Read"},
		},
		{
			ID:       "list-directory",
			Prompt:   "What files are in the cmd directory?",
			Expected: &eval.ExpectedCall{Tool: "LS"},
		},
		{
			ID:       "run-command",
			Prompt:   "Run the test suite for this project.",
			Expected: &eval.ExpectedCall{Tool: "Bash"},
		},
		{
			ID:       "search-code",
			Prompt:   "Find every place that defines an http handler in this repo.",
			Expected: &eval.ExpectedCall{Tool: "Grep"},
		},
		{
			// Negative case: a pure-knowledge question needs no tool.
			ID:       "no-tool-trivia",
			Prompt:   "In one sentence, what does the SOLID 'S' stand for?",
			Expected: nil,
		},
		{
			// Negative case: a greeting needs no tool.
			ID:       "no-tool-greeting",
			Prompt:   "Say hello.",
			Expected: nil,
		},
	}
}

func runEvalTools(cmd *cobra.Command, _ []string) error {
	settings := rhoconfig.LoadSettings()

	registry, err := defaultRegistry(settings)
	if err != nil {
		return fmt.Errorf("building tool registry: %w", err)
	}
	systemPrompt, err := buildSystemPrompt()
	if err != nil {
		return err
	}
	modelName, providerName := effectiveModelAndProvider(settings)
	sess, err := newConfiguredRhoSession(settings, providerName, modelName, systemPrompt, registry, nil)
	if err != nil {
		return err
	}

	tools := registry.FluxTools()

	// caller performs one tool-aware turn and reports the first tool the model
	// chose (if any). It does not execute the tool — we are scoring selection,
	// not effects.
	caller := func(ctx context.Context, c eval.ToolUseCase) (eval.ObservedCall, error) {
		resp, err := sess.Chat(ctx, []types.FluxMessage{
			{Role: "user", Content: c.Prompt},
		}, types.ChatOptions{Model: model, Tools: tools})
		if err != nil {
			return eval.ObservedCall{}, err
		}
		if resp == nil || len(resp.ToolCalls) == 0 {
			return eval.ObservedCall{}, nil // no tool called
		}
		tc := resp.ToolCalls[0]
		return eval.ObservedCall{Tool: tc.Name, Args: tc.Arguments}, nil
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
	defer cancel()

	cmd.Printf("%s\n", auditTint(fmt.Sprintf("Evaluating tool selection on %d cases with model %s...", len(defaultToolUseCases()), model), textPrimary))
	report := eval.ScoreToolUse(ctx, defaultToolUseCases(), caller)

	switch evalToolsOutput {
	case "json":
		data, _ := json.MarshalIndent(report, "", "  ")
		cmd.Println(string(data))
	default:
		cmd.Println(report.Markdown())
	}
	return nil
}
