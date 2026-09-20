package cmd

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"path/filepath"

	"github.com/GrayCodeAI/rho/internal/harness"
	"github.com/spf13/cobra"
)

var (
	harnessOutDir string
	harnessFormat string
	harnessFix    bool
)

var harnessCmd = &cobra.Command{
	Use:   "harness [review|fix]",
	Short: "Audit workspace AI agent harness, work loop dimensions, and generation reports",
	Long: `Evaluate the workspace AI coding agent harness across 5 dimensions:
  1. Feedforward Guidance (AGENTS.md, ZERO.md, specs, skills)
  2. Feedback Sensors (linters, test suites, hooks)
  3. Task Understanding (spec clarity, acceptance criteria)
  4. Step Planning & Execution (execution graphs, step reproducibility)
  5. Verification & Safeguards (permission checks and policy)

Generates self-contained HTML (report.html), Markdown (report.md), and JSON (findings.json).
Use --fix to automatically repair missing AGENTS.md, skills, or spec directories.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}

		// Live progress over the slow evaluation and report-writing stages.
		// TTY-aware: animates on a terminal, prints clean static lines when
		// piped. Harness writes reports to files (not stdout), so progress
		// never corrupts structured output. --fix inserts a "Repairing
		// harness" step between evaluation and the report writes.
		fixing := harnessFix || (len(args) > 0 && args[0] == "fix")
		steps := []string{"Evaluating workspace", "Writing markdown", "Writing HTML", "Writing JSON"}
		reportBase := 1
		if fixing {
			steps = []string{"Evaluating workspace", "Repairing harness", "Writing markdown", "Writing HTML", "Writing JSON"}
			reportBase = 2
		}
		prog := NewCLIProgress("Harness", steps)
		defer prog.Abort()
		step := func(i int) {
			if prog != nil {
				prog.StartStep(i)
			}
		}
		done := func(i int) {
			if prog != nil {
				prog.CompleteStep(i)
			}
		}
		finish := func() {
			if prog != nil {
				prog.Done()
			}
		}

		ctx := context.Background()
		opts := harness.EvaluateOptions{
			TargetPath: targetDir,
			OutputDir:  harnessOutDir,
		}

		step(0)
		report, err := harness.EvaluateWorkspace(ctx, targetDir, opts)
		if err != nil {
			return fmt.Errorf("harness evaluation failed: %w", err)
		}
		done(0)

		if fixing {
			step(1)
			fixResult, fixErr := harness.FixWorkspaceHarness(ctx, targetDir, report)
			if fixErr != nil {
				return fmt.Errorf("harness auto-fix failed: %w", fixErr)
			}
			fmt.Printf("%s\n", auditTint("[FIX] Rho Harness Auto-Repair Results:", warnAmber))
			for _, repair := range fixResult.RepairsPerformed {
				fmt.Printf("%s\n", auditTint("   + "+repair, doneGreen))
			}
			// Re-evaluate workspace after fix
			report, _ = harness.EvaluateWorkspace(ctx, targetDir, opts)
			done(1)
		}

		outDir := harnessOutDir
		if outDir == "" {
			outDir = filepath.Join(targetDir, ".rho", "harness")
		}

		if mkdirErr := os.MkdirAll(outDir, 0o750); mkdirErr != nil {
			return fmt.Errorf("failed to create harness output directory: %w", mkdirErr)
		}

		// Write Markdown report
		step(reportBase)
		mdPath := filepath.Join(outDir, "report.md")
		mdContent := harness.RenderMarkdown(report)
		if writeErr := os.WriteFile(mdPath, []byte(mdContent), 0o640); writeErr != nil { // #nosec G306 -- report is intentionally group-readable
			return fmt.Errorf("failed to write report.md: %w", writeErr)
		}
		done(reportBase)

		// Write HTML report
		step(reportBase + 1)
		htmlPath := filepath.Join(outDir, "report.html")
		htmlContent := harness.RenderHTML(report)
		if writeErr := os.WriteFile(htmlPath, []byte(htmlContent), 0o640); writeErr != nil { // #nosec G306 -- report is intentionally group-readable
			return fmt.Errorf("failed to write report.html: %w", writeErr)
		}
		done(reportBase + 1)

		// Write JSON report
		step(reportBase + 2)
		jsonPath := filepath.Join(outDir, "findings.json")
		jsonContent, renderErr := harness.RenderJSON(report)
		if renderErr != nil {
			return fmt.Errorf("failed to serialize findings.json: %w", renderErr)
		}
		done(reportBase + 2)
		if writeErr := os.WriteFile(jsonPath, jsonContent, 0o640); writeErr != nil { // #nosec G306 -- report is intentionally group-readable
			return fmt.Errorf("failed to write findings.json: %w", writeErr)
		}

		// Journal quality observation to Rho execution graph
		_ = harness.JournalHarnessReport(report, "")

		finish()
		fmt.Printf("%s\n", auditTint("[RHO] Rho Harness Evaluation Complete", rhoColor))
		fmt.Printf("   %s : %s (%s)\n",
			auditTint("Overall Score", textPrimary),
			auditTint(fmt.Sprintf("%d/100", report.OverallScore), textPrimary),
			auditTint(report.OverallStatus, harnessStatusColor(report.OverallStatus)))
		fmt.Printf("   %s : %d prioritized issues\n", auditTint("Findings", textPrimary), len(report.Findings))
		fmt.Printf("   %s : %s\n", auditTint("HTML Report", textPrimary), htmlPath)
		fmt.Printf("   %s : %s\n", auditTint("Markdown", textPrimary), mdPath)
		fmt.Printf("   %s : %s\n", auditTint("JSON Findings", textPrimary), jsonPath)

		return nil
	},
}

func init() {
	harnessCmd.Flags().StringVar(&harnessOutDir, "out-dir", "", "Directory to save harness reports (default: .rho/harness)")
	harnessCmd.Flags().StringVar(&harnessFormat, "format", "all", "Report output format (html, markdown, json, all)")
	harnessCmd.Flags().BoolVar(&harnessFix, "fix", false, "Automatically repair missing harness assets (AGENTS.md, skills, specs)")
}

// harnessStatusColor maps the harness health status to a semantic theme color.
func harnessStatusColor(status string) color.Color {
	switch status {
	case "EXCELLENT", "GOOD":
		return doneGreen
	case "NEEDS_IMPROVEMENT":
		return warnAmber
	case "POOR":
		return errorCoral
	default:
		return textPrimary
	}
}
