package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine/safety"

	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	mission "github.com/GrayCodeAI/rho/internal/multiagent"
	"github.com/GrayCodeAI/rho/internal/observability/logger"
	"github.com/GrayCodeAI/rho/internal/tool"
	"github.com/GrayCodeAI/rho/internal/ui/icons"
	"github.com/spf13/cobra"
)

var (
	missionWorkers int
	missionModel   string
	missionAuto    string
	missionTimeout time.Duration
	missionDryRun  bool
)

var missionCmd = &cobra.Command{
	Use:   "mission [prompt]",
	Short: "Run a multi-agent mission (parallel feature execution)",
	Long: `Decompose a task into features and execute them in parallel git worktrees.

Each feature runs in its own worktree with a full engine session.
Results are committed on separate branches for review/merge.

Examples:
  rho mission "Add auth, rate limiting, and logging to the API"
  rho mission --workers 6 "Refactor the database layer into 3 services"
  rho mission --model claude-sonnet-4-6 "Add tests for all untested packages"
  rho mission --from-tasks`,
	Args: cobra.ArbitraryArgs,
	RunE: runMission,
}

func init() {
	missionCmd.Flags().IntVar(&missionWorkers, "workers", 4, "Max parallel workers")
	missionCmd.Flags().StringVarP(&missionModel, "model", "m", "", "Model for workers")
	missionCmd.Flags().StringVar(&missionAuto, "auto", "full", "Autonomy level for workers")
	missionCmd.Flags().DurationVar(&missionTimeout, "timeout", 30*time.Minute, "Mission timeout")
	missionCmd.Flags().BoolVar(&missionDryRun, "dry-run", false, "Plan only, don't execute workers")
}

func runMission(_ *cobra.Command, args []string) error {
	prompt, err := missionPrompt(args)
	if err != nil {
		return err
	}

	cwd, _ := os.Getwd()
	baseBranch := getCurrentBranch(cwd)

	settings := rhoconfig.LoadSettings()
	effectiveModel, effectiveProvider := effectiveModelAndProvider(settings)
	if missionModel != "" {
		effectiveModel = missionModel
	}

	autonomy := safety.ParseAutonomyLevel(missionAuto)

	cfg := mission.Config{
		MaxWorkers:    missionWorkers,
		WorkerModel:   effectiveModel,
		RepoDir:       cwd,
		BaseBranch:    baseBranch,
		AutonomyLevel: int(autonomy),
	}

	m := mission.New(prompt, cfg)
	// Clean up the mission's temp directory when the command finishes,
	// whether it succeeded or failed. Without this, /tmp/rho-missions/
	// accumulates one directory per run indefinitely (C6 fix).
	defer func() { _ = m.Cleanup() }()

	ctx, cancel := context.WithTimeout(context.Background(), missionTimeout)
	defer cancel()

	var waves [][]string
	if missionFromTasks {
		fmt.Printf("%s\n", auditTint(fmt.Sprintf("Mission %s: loading validated task graph...", m.ID), textPrimary))
		features, taskWaves, err := missionFeaturesFromTasks(tool.GetTaskStore(), m.ID)
		if err != nil {
			return fmt.Errorf("task graph: %w", err)
		}
		m.Features = features
		waves = taskWaves
	} else {
		fmt.Printf("%s\n", auditTint(fmt.Sprintf("Mission %s: planning...", m.ID), textPrimary))
		planFn := func(ctx context.Context, p string) ([]mission.Feature, error) {
			return planWithLLM(ctx, p, effectiveProvider, effectiveModel, settings)
		}
		if err := m.Plan(ctx, planFn); err != nil {
			return fmt.Errorf("planning: %w", err)
		}
	}

	fmt.Printf("%s\n", auditTint(fmt.Sprintf("Mission %s: %d features planned", m.ID, len(m.Features)), textPrimary))
	for i, f := range m.Features {
		fmt.Printf("%s\n", auditTint(fmt.Sprintf("  %d. %s", i+1, f.Description), textPrimary))
	}
	fmt.Println()

	if missionDryRun {
		fmt.Println(auditTint("(dry-run: not executing workers)", textMuted))
		return nil
	}

	// Build system prompt for workers
	systemPrompt, _ := buildSystemPrompt()

	// Run features in parallel
	workerFn := mission.EngineWorker(effectiveProvider, effectiveModel, systemPrompt)
	if missionFromTasks {
		workerFn = graphTrackingWorker(tool.GetTaskStore(), workerFn)
	}

	var prog *CLIProgress
	if !IsQuiet() {
		prog = NewCLIProgress("Mission", []string{fmt.Sprintf("Executing %d features with %d workers", len(m.Features), cfg.MaxWorkers)})
		defer prog.Abort()
		prog.StartStep(0)
	}
	var runErr error
	if missionFromTasks {
		runErr = m.RunStaged(ctx, workerFn, mission.WithExecutionWaves(waves))
	} else {
		runErr = m.Run(ctx, workerFn)
	}
	if runErr != nil {
		if prog != nil {
			prog.FailStep(0, runErr.Error())
		}
		return runErr
	}
	if prog != nil {
		prog.CompleteStep(0)
		prog.Done()
	}

	// Print results
	fmt.Println()
	fmt.Println(auditTint(m.Summary(), textPrimary))
	fmt.Println()
	for _, f := range m.Features {
		status := icons.CheckBold() + " "
		statusColor := doneGreen
		if f.Status == mission.FeatureFailed {
			status = icons.CloseThick() + " "
			statusColor = errorCoral
		}
		branch := f.Branch
		if f.Handoff != nil && f.Handoff.CommitID != "" {
			branch += " (" + f.Handoff.CommitID[:7] + ")"
		}
		fmt.Printf("  %s %s\n", auditTint(status, statusColor), auditTint(f.Description, textPrimary)+auditTint(" — "+branch, textMuted))
	}
	if missionFromTasks && len(m.WaveJoins) > 0 {
		fmt.Println()
		fmt.Println(auditTint("Wave joins:", textPrimary))
		for _, join := range m.WaveJoins {
			fmt.Printf("  %s\n", auditTint(fmt.Sprintf("%d. %s", join.Wave, join.Summary), textMuted))
		}
	}

	// Propagate feature failures as a non-zero exit so CI sees a real
	// failure instead of green: Mission.Run historically returned nil even
	// when every feature failed (H9), so `rho mission` exited 0.
	failed := 0
	for _, f := range m.Features {
		if f.Status == mission.FeatureFailed {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("mission %s: %d/%d features failed", m.ID, failed, len(m.Features))
	}

	return nil
}

func planWithLLM(ctx context.Context, prompt, provider, model string, settings rhoconfig.Settings) ([]mission.Feature, error) {
	planPrompt := fmt.Sprintf(
		"Decompose this task into independent features that can be implemented in parallel.\n\n"+
			"Task: %s\n\n"+
			"Return a numbered list of features. Each feature should be:\n"+
			"- Independent (can be implemented without the others)\n"+
			"- Specific (clear what to implement)\n"+
			"- Testable (has clear success criteria)\n\n"+
			"Format: one feature per line, numbered. Just the descriptions, no extra text.",
		prompt,
	)

	registry, _ := defaultRegistry(settings)
	sess, err := newConfiguredRhoSession(settings, provider, model, planPrompt, registry, logger.New(io.Discard, logger.Error))
	if err != nil {
		return nil, err
	}
	_ = sess.SetMaxTurns(1)
	sess.PermSvc().SetPermissionFn(func(req safety.PermissionRequest) {
		if req.Response != nil {
			req.Response <- true
		}
	})

	sess.AddUser(planPrompt)
	events, err := sess.Stream(ctx)
	if err != nil {
		return nil, err
	}

	var response strings.Builder
	for ev := range events {
		if ev.Type == "content" {
			response.WriteString(ev.Content)
		}
	}

	return parseFeatures(response.String()), nil
}

func parseFeatures(text string) []mission.Feature {
	var features []mission.Feature
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Strip numbering: "1. ", "1) ", "- "
		for _, prefix := range []string{"- ", "* "} {
			line = strings.TrimPrefix(line, prefix)
		}
		if len(line) > 2 && line[0] >= '0' && line[0] <= '9' {
			idx := strings.IndexAny(line, ".)")
			if idx > 0 && idx < 4 {
				line = strings.TrimSpace(line[idx+1:])
			}
		}
		if line == "" {
			continue
		}
		features = append(features, mission.Feature{
			Description: line,
		})
	}
	return features
}

func getCurrentBranch(dir string) string {
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "main"
	}
	return strings.TrimSpace(string(out))
}
