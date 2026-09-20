package cmd

import (
	"fmt"
	"image/color"

	"github.com/GrayCodeAI/rho/internal/engine/safety"

	lipgloss "charm.land/lipgloss/v2"
)

// Five host-execution autonomy tiers (Scout → Builder → Operator → Autonomous → Always Ask).
// Supervised ("Always Ask") is included in the Ctrl+L cycle but requires a
// deliberate double-press to land on (see chat_update.go ctrl+l handling) so
// repeated key-presses can't accidentally drop the user into max-friction mode.
var autonomyTiers = []safety.AutonomyLevel{
	safety.AutonomyBasic,
	safety.AutonomySemi,
	safety.AutonomyFull,
	safety.AutonomyYOLO,
	safety.AutonomySupervised,
}

var autonomyTierNames = []string{
	"Scout",
	"Builder",
	"Operator",
	"Autonomous",
	"Always Ask",
}

// DefaultAutonomy is the default host-execution permission tier.
const DefaultAutonomy = safety.AutonomySemi

// yoloConfirmToken is the exact string a user must type (case-insensitive) to
// confirm entry into YOLO ("Autonomous") unattended mode via the picker.
const yoloConfirmToken = "continue"

func autonomyTierName(level safety.AutonomyLevel) string {
	if level == safety.AutonomySupervised {
		return "Always Ask"
	}
	for i, l := range autonomyTiers {
		if l == level {
			return autonomyTierNames[i]
		}
	}
	return "Builder"
}

func autonomyTierIndex(level safety.AutonomyLevel) int {
	for i, l := range autonomyTiers {
		if l == level {
			return i
		}
	}
	return 1 // default Builder
}

// nextAutonomyTier returns the next tier in the Ctrl+L cycle. It skips
// Supervised ("Always Ask") — repeated Ctrl+L wraps YOLO → Basic. Use
// nextAutonomyTierIncludingSupervised when the user explicitly confirms they
// want the cautious tier.
func nextAutonomyTier(level safety.AutonomyLevel) safety.AutonomyLevel {
	idx := autonomyTierIndex(level)
	for {
		idx = (idx + 1) % len(autonomyTiers)
		if autonomyTiers[idx] != safety.AutonomySupervised {
			return autonomyTiers[idx]
		}
	}
}

// nextAutonomyTierIncludingSupervised returns the next tier with Supervised
// included in the cycle (used after the user confirms via double-press).
func nextAutonomyTierIncludingSupervised(level safety.AutonomyLevel) safety.AutonomyLevel {
	return autonomyTiers[(autonomyTierIndex(level)+1)%len(autonomyTiers)]
}

// isSupervisedPending reports whether the next regular cycle step would land
// on Supervised (i.e. the current tier is YOLO). The UI uses this to prompt
// for confirmation.
func isSupervisedPending(level safety.AutonomyLevel) bool {
	return level == safety.AutonomyYOLO
}

// autonomyTierDescription is short copy shown when the user changes tier (ctrl+L).
func autonomyTierDescription(level safety.AutonomyLevel) string {
	switch level {
	case safety.AutonomySupervised:
		return "Prompts for permission on every tool call"
	case safety.AutonomyBasic:
		return "Explore only — edits and commands ask first"
	case safety.AutonomySemi:
		return "File changes auto-approve — commands ask first"
	case safety.AutonomyFull:
		return "Commands auto-run — risky actions ask first"
	case safety.AutonomyYOLO:
		return "Minimal prompts — only the highest-risk actions stop"
	default:
		return "File changes auto-approve — commands ask first"
	}
}

func autonomyTierColor(level safety.AutonomyLevel) color.Color {
	switch level {
	case safety.AutonomySupervised:
		return textDisabled
	case safety.AutonomyBasic:
		return tierInspect
	case safety.AutonomySemi:
		return tierEdit
	case safety.AutonomyFull:
		return tierRun
	case safety.AutonomyYOLO:
		return tierTrust
	default:
		return tierEdit
	}
}

func autonomyTierStyle(level safety.AutonomyLevel) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(autonomyTierColor(level)).Bold(true).Inline(true)
}

func renderAutonomyTierLabel(level safety.AutonomyLevel) string {
	return autonomyTierStyle(level).Render(autonomyTierName(level))
}

func formatAutonomyTierMessage(level safety.AutonomyLevel) string {
	return fmt.Sprintf("Autonomy %s — %s", renderAutonomyTierLabel(level), autonomyTierDescription(level))
}

func autonomyFromSettings(n int) safety.AutonomyLevel {
	switch n {
	case 1:
		return safety.AutonomyBasic
	case 2:
		return safety.AutonomySemi
	case 3:
		return safety.AutonomyFull
	case 4:
		return safety.AutonomyYOLO
	default:
		return 0
	}
}
