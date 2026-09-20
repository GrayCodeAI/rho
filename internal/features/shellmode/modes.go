package shellmode

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/GrayCodeAI/rho/internal/storage"
)

// Mode represents the REPL routing mode.
type Mode int

const (
	ModeAuto  Mode = iota // smart routing (default)
	ModeShell             // everything to shell
	ModeAgent             // everything to AI
)

// String returns the display name of the mode.
func (m Mode) String() string {
	switch m {
	case ModeShell:
		return "shell"
	case ModeAgent:
		return "agent"
	default:
		return "auto"
	}
}

// ModeManager handles mode state and toggling.
type ModeManager struct {
	mu      sync.Mutex
	current Mode
}

// NewModeManager creates a manager starting in auto mode.
func NewModeManager() *ModeManager {
	return &ModeManager{current: ModeAuto}
}

// Current returns the active mode.
func (mm *ModeManager) Current() Mode {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	return mm.current
}

// Set changes to a specific mode and persists it.
func (mm *ModeManager) Set(m Mode) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.current = m
	mm.persist()
}

// Toggle cycles through modes: auto → shell → agent → auto.
func (mm *ModeManager) Toggle() Mode {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.current = (mm.current + 1) % 3
	mm.persist()
	return mm.current
}

func (mm *ModeManager) persist() {
	dir := storage.StateDir()
	_ = os.MkdirAll(dir, 0o750)
	_ = os.WriteFile(filepath.Join(dir, "mode"), []byte(mm.current.String()), 0o600)
}

// LoadPersistedMode restores mode from disk.
func (mm *ModeManager) LoadPersistedMode() {
	data, err := os.ReadFile(filepath.Join(storage.StateDir(), "mode"))
	if err != nil {
		return
	}
	if m, ok := ParseMode(strings.TrimSpace(string(data))); ok {
		mm.mu.Lock()
		mm.current = m
		mm.mu.Unlock()
	}
}

// ParseMode converts a string to a Mode.
func ParseMode(s string) (Mode, bool) {
	switch s {
	case "auto", "a":
		return ModeAuto, true
	case "shell", "s":
		return ModeShell, true
	case "agent", "ai":
		return ModeAgent, true
	default:
		return ModeAuto, false
	}
}

// ClassifyWithMode applies mode override to classification.
// In shell/agent mode, the mode takes precedence over auto-detection.
func (mm *ModeManager) ClassifyWithMode(input string) Classification {
	mm.mu.Lock()
	mode := mm.current
	mm.mu.Unlock()

	switch mode {
	case ModeShell:
		return ClassShell
	case ModeAgent:
		return ClassAgent
	default:
		return ClassifyInput(input)
	}
}
