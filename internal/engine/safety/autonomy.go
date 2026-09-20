package safety

import (
	"strings"
)

// AutonomyLevel controls how much the agent can do without asking the user.
type AutonomyLevel int

const (
	// AutonomySupervised asks for permission on every tool call.
	AutonomySupervised AutonomyLevel = 0
	// AutonomyBasic auto-allows read-only tools.
	AutonomyBasic AutonomyLevel = 1
	// AutonomySemi auto-allows reads and writes, asks for Bash.
	AutonomySemi AutonomyLevel = 2
	// AutonomyFull auto-allows everything except destructive commands.
	AutonomyFull AutonomyLevel = 3
	// AutonomyYOLO never asks for permission.
	AutonomyYOLO AutonomyLevel = 4
)

// ParseAutonomyLevel converts a string name or number to an AutonomyLevel.
// Product aliases (Grok/Claude-style):
//
//	acceptEdits → semi (auto-apply edits, ask for bash)
//	dontAsk     → yolo (never prompt; still subject to PreToolUse/DryRun/spec)
func ParseAutonomyLevel(s string) AutonomyLevel {
	s = strings.TrimSpace(strings.ToLower(s))
	// normalize separators
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	switch s {
	case "0", "supervised":
		return AutonomySupervised
	case "1", "basic":
		return AutonomyBasic
	case "2", "semi", "acceptedits":
		return AutonomySemi
	case "3", "full":
		return AutonomyFull
	case "4", "yolo", "dontask":
		return AutonomyYOLO
	default:
		return AutonomySupervised
	}
}

// String returns the human-readable name of an autonomy level.
func (l AutonomyLevel) String() string {
	switch l {
	case AutonomySupervised:
		return "supervised"
	case AutonomyBasic:
		return "basic"
	case AutonomySemi:
		return "semi"
	case AutonomyFull:
		return "full"
	case AutonomyYOLO:
		return "yolo"
	default:
		return "supervised"
	}
}
