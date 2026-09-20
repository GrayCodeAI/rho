package cmd

import (
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// BlockSection represents a collapsible section in the conversation.
type BlockSection struct {
	Title     string
	Content   string
	Collapsed bool
	Kind      BlockKind // controls styling
}

// BlockKind categorizes the section type for styling.
type BlockKind int

const (
	BlockToolUse  BlockKind = iota // tool calls and results
	BlockThinking                  // LLM reasoning
	BlockDiff                      // code diffs
	BlockTest                      // test output
	BlockReview                    // review findings
	BlockPlan                      // planning sections
)

// BlockStyle returns the lipgloss style for a block kind.
func BlockStyle(kind BlockKind) (titleStyle, contentStyle lipgloss.Style) {
	switch kind {
	case BlockToolUse:
		return lipgloss.NewStyle().Foreground(infoSky).Bold(true),
			lipgloss.NewStyle().Foreground(textMuted)
	case BlockThinking:
		return lipgloss.NewStyle().Foreground(costViolet).Bold(true),
			lipgloss.NewStyle().Foreground(textMuted).Italic(true)
	case BlockDiff:
		return lipgloss.NewStyle().Foreground(successTeal).Bold(true),
			lipgloss.NewStyle().Foreground(textPrimary)
	case BlockTest:
		return lipgloss.NewStyle().Foreground(doneGreen).Bold(true),
			lipgloss.NewStyle().Foreground(textPrimary)
	case BlockReview:
		return lipgloss.NewStyle().Foreground(warnAmber).Bold(true),
			lipgloss.NewStyle().Foreground(textPrimary)
	case BlockPlan:
		return lipgloss.NewStyle().Foreground(hudBorderPurple).Bold(true),
			lipgloss.NewStyle().Foreground(textPrimary)
	default:
		return lipgloss.NewStyle().Bold(true),
			lipgloss.NewStyle().Foreground(textPrimary)
	}
}

// RenderBlockSection renders a collapsible block section.
func RenderBlockSection(block BlockSection, width int) string {
	if width < 40 {
		width = 80
	}
	boxWidth := width - 4
	if boxWidth > 100 {
		boxWidth = 100
	}

	titleStyle, contentStyle := BlockStyle(block.Kind)

	var b strings.Builder

	// Collapse indicator
	indicator := "▼"
	if block.Collapsed {
		indicator = "▶"
	}

	titleLine := fmt.Sprintf("  %s %s", indicator, block.Title)
	b.WriteString(titleStyle.Render(titleLine))
	b.WriteString("\n")

	if !block.Collapsed && block.Content != "" {
		b.WriteString("\n")
		content := wrapText(block.Content, boxWidth-2, 2)
		for _, line := range strings.Split(content, "\n") {
			b.WriteString(contentStyle.Render("  " + line))
			b.WriteString("\n")
		}
	}

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(borderDim).
		Width(boxWidth)

	return borderStyle.Render(strings.TrimRight(b.String(), "\n"))
}

// DetectBlockKind infers the block kind from content.
func DetectBlockKind(title, content string) BlockKind {
	lower := strings.ToLower(title + " " + content)
	if strings.Contains(lower, "test") || strings.Contains(lower, "PASS") || strings.Contains(lower, "FAIL") {
		return BlockTest
	}
	if strings.Contains(lower, "diff") || strings.Contains(lower, "+++") || strings.Contains(lower, "---") {
		return BlockDiff
	}
	if strings.Contains(lower, "thinking") || strings.Contains(lower, "reasoning") {
		return BlockThinking
	}
	if strings.Contains(lower, "review") || strings.Contains(lower, "finding") {
		return BlockReview
	}
	if strings.Contains(lower, "plan") || strings.Contains(lower, "approach") {
		return BlockPlan
	}
	return BlockToolUse
}

// CollapsibleBlocks manages a collection of collapsible sections.
type CollapsibleBlocks struct {
	Blocks []BlockSection
}

// NewCollapsibleBlocks creates a new block manager.
func NewCollapsibleBlocks() *CollapsibleBlocks {
	return &CollapsibleBlocks{}
}

// Add appends a new block section.
func (cb *CollapsibleBlocks) Add(title, content string, kind BlockKind) {
	cb.Blocks = append(cb.Blocks, BlockSection{
		Title:     title,
		Content:   content,
		Collapsed: false,
		Kind:      kind,
	})
}

// Toggle collapses/expands the block at the given index.
func (cb *CollapsibleBlocks) Toggle(idx int) {
	if idx >= 0 && idx < len(cb.Blocks) {
		cb.Blocks[idx].Collapsed = !cb.Blocks[idx].Collapsed
	}
}

// CollapseAll collapses all blocks.
func (cb *CollapsibleBlocks) CollapseAll() {
	for i := range cb.Blocks {
		cb.Blocks[i].Collapsed = true
	}
}

// ExpandAll expands all blocks.
func (cb *CollapsibleBlocks) ExpandAll() {
	for i := range cb.Blocks {
		cb.Blocks[i].Collapsed = false
	}
}

// RenderAll renders all blocks to a string.
func (cb *CollapsibleBlocks) RenderAll(width int) string {
	var b strings.Builder
	for _, block := range cb.Blocks {
		b.WriteString(RenderBlockSection(block, width))
		b.WriteString("\n")
	}
	return b.String()
}

// renderBlockMessage converts a message into a block-style display for the
// conversation view. Used when block display mode is active.
func renderBlockMessage(role, content string, width int) string {
	switch role {
	case "tool_use":
		block := BlockSection{Title: content, Content: "", Collapsed: true, Kind: BlockToolUse}
		return RenderBlockSection(block, width)
	case "tool_result":
		block := BlockSection{Title: "Result", Content: content, Collapsed: false, Kind: BlockToolUse}
		return RenderBlockSection(block, width)
	case "thinking":
		block := BlockSection{Title: "Reasoning", Content: content, Collapsed: true, Kind: BlockThinking}
		return RenderBlockSection(block, width)
	default:
		return "" // not a block-eligible message
	}
}
