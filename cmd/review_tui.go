package cmd

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

var reviewTUICmd = &cobra.Command{
	Use:   "tui",
	Short: "Interactive review queue",
	RunE:  runReviewTUI,
}

func init() {
	reviewCmd.AddCommand(reviewTUICmd)
}

// TUI model
type reviewTUIModel struct {
	reviews  []*ReviewRecord
	cursor   int
	expanded bool // show findings for selected review
	width    int
	height   int
	store    *ReviewStore
	quitting bool
}

type reviewsLoadedMsg []*ReviewRecord

func runReviewTUI(_ *cobra.Command, _ []string) error {
	projectDir, _ := os.Getwd()
	store, err := OpenReviewStore(projectDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}

	m := reviewTUIModel{store: store}
	p := tea.NewProgram(m)
	_, err = p.Run()
	_ = store.Close()
	return err
}

func (m reviewTUIModel) Init() tea.Cmd {
	return func() tea.Msg {
		reviews, _ := m.store.ListAll(50)
		return reviewsLoadedMsg(reviews)
	}
}

func (m reviewTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case reviewsLoadedMsg:
		m.reviews = msg

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc":
			if m.expanded {
				m.expanded = false
			} else {
				m.quitting = true
				return m, tea.Quit
			}
		case "j", "down":
			if m.cursor < len(m.reviews)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "enter", " ":
			m.expanded = !m.expanded
		case "c":
			// Close selected review.
			if m.cursor < len(m.reviews) {
				r := m.reviews[m.cursor]
				_ = m.store.SetStatus(r.ID, ReviewStatusClosed)
				return m, m.reload()
			}
		case "f":
			// Fix selected review (exit TUI, run fix).
			if m.cursor < len(m.reviews) {
				m.quitting = true
				return m, tea.Sequence(tea.Quit, func() tea.Msg { return nil })
			}
		case "r":
			return m, m.reload()
		case "g":
			m.cursor = 0
		case "G":
			if len(m.reviews) > 0 {
				m.cursor = len(m.reviews) - 1
			}
		}
	}
	return m, nil
}

func (m reviewTUIModel) reload() tea.Cmd {
	return func() tea.Msg {
		reviews, _ := m.store.ListAll(50)
		return reviewsLoadedMsg(reviews)
	}
}

func (m reviewTUIModel) View() tea.View {
	if m.quitting {
		return tea.View{}
	}

	header := lipgloss.NewStyle().Bold(true).Foreground(rhoColor)
	dim := lipgloss.NewStyle().Faint(true)
	selected := lipgloss.NewStyle().Bold(true).Background(bgCode).Foreground(textPrimary)

	var b strings.Builder
	b.WriteString(header.Render("  rho review") + dim.Render("  j/k:nav  enter:expand  c:close  f:fix  r:refresh  q:quit") + "\n")
	b.WriteString(strings.Repeat("─", reviewMin(m.width, 80)) + "\n")

	if len(m.reviews) == 0 {
		b.WriteString("\n  No reviews yet. Run 'rho review init' to get started.\n")
		v := tea.View{Content: b.String()}
		v.AltScreen = true
		return v
	}

	// Calculate visible area.
	listHeight := m.height - 4
	if m.expanded {
		listHeight = reviewMin(listHeight/2, 15)
	}

	start := 0
	if m.cursor >= listHeight {
		start = m.cursor - listHeight + 1
	}

	for i := start; i < len(m.reviews) && i < start+listHeight; i++ {
		r := m.reviews[i]
		icon := statusIcon(r.Status)
		line := fmt.Sprintf(" %s #%-3d %s  %-7s  %d findings  %s",
			icon, r.ID, r.SHA[:8], r.Status, len(r.Findings), r.CreatedAt.Format("Jan 02 15:04"))

		if i == m.cursor {
			b.WriteString(selected.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	// Expanded detail view.
	if m.expanded && m.cursor < len(m.reviews) {
		r := m.reviews[m.cursor]
		b.WriteString("\n" + strings.Repeat("─", reviewMin(m.width, 80)) + "\n")
		b.WriteString(fmt.Sprintf("  Review #%d — %s [%s]\n\n", r.ID, r.SHA[:8], r.MaxSeverity))

		if len(r.Findings) == 0 {
			b.WriteString("  No findings — clean " + icons.CheckBold() + "\n")
		} else {
			remaining := m.height - listHeight - 6
			for i, f := range r.Findings {
				if i >= remaining {
					b.WriteString(fmt.Sprintf("  ... and %d more\n", len(r.Findings)-i))
					break
				}
				sev := f.Severity.String()
				b.WriteString(fmt.Sprintf("  [%s] %s:%d — %s\n", sev, f.File, f.Line, f.Message))
			}
		}
	}

	v := tea.View{Content: b.String()}
	v.AltScreen = true
	return v
}

func reviewMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
