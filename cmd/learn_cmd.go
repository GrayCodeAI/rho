package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine"
	"github.com/GrayCodeAI/rho/internal/ui/icons"
	"github.com/spf13/cobra"
)

var (
	learnWhat     string
	learnWhy      string
	learnLesson   string
	learnCategory string
	learnLimit    int
	learnAll      bool
	learnClearYes bool
)

// learnCmd manages the cross-session lesson store.
var learnCmd = &cobra.Command{
	Use:   "learn",
	Short: "Manage lessons learned across sessions",
	Long: `Rho persists lessons from failures (and manual entries) so future
sessions avoid repeating them. Lessons are injected into the system prompt.

  rho learn                      List recent lessons
  rho learn add                  Add a lesson manually
  rho learn prompt <context>     Print the lesson-extraction prompt for a context
  rho learn clear                Remove all lessons`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLearnList(cmd)
	},
}

var learnAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a lesson manually",
	RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(learnWhat) == "" || strings.TrimSpace(learnLesson) == "" {
			return fmt.Errorf("--what and --lesson are required")
		}
		if learnCategory == "" {
			learnCategory = "manual"
		}
		si := engine.NewSelfImprover()
		si.Learn(strings.TrimSpace(learnWhat), strings.TrimSpace(learnWhy), strings.TrimSpace(learnLesson), strings.TrimSpace(learnCategory))
		cmd.Println(auditTint(icons.CheckBold()+" ", doneGreen) + auditTint("lesson added (category: "+learnCategory+")", textPrimary))
		return nil
	},
}

var learnPromptCmd = &cobra.Command{
	Use:   "prompt <context>",
	Short: "Print the lesson-extraction prompt for a failure context",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Println(engine.LearnPrompt(strings.Join(args, " ")))
		return nil
	},
}

var learnClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Remove all lessons",
	RunE: func(cmd *cobra.Command, args []string) error {
		si := engine.NewSelfImprover()
		n := len(si.Lessons(""))
		if n == 0 {
			cmd.Println(auditTint("no lessons to clear", textMuted))
			return nil
		}
		ok := learnClearYes
		if !ok {
			var err error
			ok, err = confirmDestructive(fmt.Sprintf("Remove all %d lesson(s)?", n))
			if err != nil {
				return err
			}
		}
		if !ok {
			cmd.Println(auditTint("Cancelled.", textMuted))
			return nil
		}
		si.Clear()
		cmd.Println(auditTint("cleared "+strconv.Itoa(n)+" lesson(s)", textPrimary))
		return nil
	},
}

func init() {
	learnAddCmd.Flags().StringVar(&learnWhat, "what", "", "what went wrong")
	learnAddCmd.Flags().StringVar(&learnWhy, "why", "", "root cause")
	learnAddCmd.Flags().StringVar(&learnLesson, "lesson", "", "what to do differently")
	learnAddCmd.Flags().StringVar(&learnCategory, "category", "manual", "code, test, design, communication, manual")
	learnCmd.Flags().IntVar(&learnLimit, "limit", 20, "max lessons to print (0 = all)")
	learnCmd.Flags().BoolVar(&learnAll, "all", false, "include all fields (also shows the why)")
	learnClearCmd.Flags().BoolVar(&learnClearYes, "yes", false, "confirm clearing all lessons")
	learnCmd.AddCommand(learnAddCmd)
	learnCmd.AddCommand(learnPromptCmd)
	learnCmd.AddCommand(learnClearCmd)
	rootCmd.AddCommand(learnCmd)
}

func runLearnList(cmd *cobra.Command) error {
	si := engine.NewSelfImprover()
	lessons := si.Lessons("")
	if len(lessons) == 0 {
		cmd.Println(auditTint("No lessons yet. Add one with: rho learn add --what ... --lesson ...", textMuted))
		return nil
	}

	// Count by category.
	cats := map[string]int{}
	for _, e := range lessons {
		cats[e.Category]++
	}
	var catSummary []string
	for cat, count := range cats {
		catSummary = append(catSummary, fmt.Sprintf("%s (%d)", cat, count))
	}
	cmd.Println(auditTint("Lesson store: "+strconv.Itoa(len(lessons))+" lesson(s) — "+strings.Join(catSummary, ", "), textPrimary))

	start := 0
	if learnLimit > 0 && len(lessons) > learnLimit {
		start = len(lessons) - learnLimit
	}
	cmd.Println()
	for _, e := range lessons[start:] {
		cmd.Printf("%s %s\n", auditTint("["+e.Category+"]", toolGold), auditTint(e.What, textPrimary))
		cmd.Printf("%s %s\n", auditTint("    lesson:", textMuted), auditTint(e.Lesson, textPrimary))
		if learnAll && e.Why != "" {
			cmd.Printf("%s %s\n", auditTint("    why:", textMuted), auditTint(e.Why, textPrimary))
		}
		cmd.Printf("%s %s\n", auditTint("    learned:", textMuted), auditTint(e.Timestamp.Format(time.RFC3339), textMuted))
	}
	return nil
}
