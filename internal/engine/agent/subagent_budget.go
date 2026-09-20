package agent

// subagent_budget.go implements mode-based budget tracking and tool allowlists
// sub-agent implementation.
// It builds on SubAgentMode/constants declared in agent.go.

// SubAgentConfig holds configuration for sub-agent budget and depth limits.
// Zero-value int fields receive defaults via DefaultSubAgentConfig().
type SubAgentConfig struct {
	ExploreMaxTurns int // turn budget for explore-mode sub-agents
	GeneralMaxTurns int // turn budget for general-mode sub-agents
	MaxDepth        int // maximum nesting depth (1 = sub-agents cannot spawn children)
}

// DefaultSubAgentConfig returns a SubAgentConfig with sane production defaults.
func DefaultSubAgentConfig() SubAgentConfig {
	return SubAgentConfig{
		ExploreMaxTurns: DefaultExploreTurns, // 15
		GeneralMaxTurns: DefaultGeneralTurns, // 20
		MaxDepth:        1,
	}
}

// ModeToolAllowlist maps each sub-agent mode to the tool names it can use.
// Explore mode is restricted to read-only tools; general mode has full access.
var ModeToolAllowlist = map[SubAgentMode][]string{
	SubAgentExplore: {
		"Read",
		"Grep",
		"Glob",
		"LS",
		"Bash", // read-only commands only (enforced by the permission pipeline)
	},
	SubAgentPlan: {
		"Read",
		"Grep",
		"Glob",
		"LS",
		"Bash", // read-only commands only (enforced by the permission pipeline)
	},
	SubAgentGeneral: {
		"Read",
		"Grep",
		"Glob",
		"LS",
		"Bash",
		"Write",
		"Edit",
		"Agent",
		"MultiAgent",
	},
}

// SubAgentBudget tracks turn usage against a mode-derived limit.
type SubAgentBudget struct {
	Mode      SubAgentMode
	TurnsUsed int
	MaxTurns  int
}

// NewSubAgentBudget creates a budget tracker for the given mode and config.
func NewSubAgentBudget(mode SubAgentMode, cfg SubAgentConfig) *SubAgentBudget {
	max := DefaultTurnsForMode(mode)
	if mode == SubAgentExplore && cfg.ExploreMaxTurns > 0 {
		max = cfg.ExploreMaxTurns
	}
	if mode == SubAgentGeneral && cfg.GeneralMaxTurns > 0 {
		max = cfg.GeneralMaxTurns
	}
	return &SubAgentBudget{
		Mode:     mode,
		MaxTurns: max,
	}
}

// Tick increments the turn counter by one.
func (b *SubAgentBudget) Tick() {
	b.TurnsUsed++
}

// ShouldSynthesize returns true when the sub-agent has exceeded its turn budget
// and should produce a final synthesis response instead of continuing tool use.
func (b *SubAgentBudget) ShouldSynthesize() bool {
	return b.TurnsUsed >= b.MaxTurns
}

// Remaining returns how many turns are left before synthesis is triggered.
func (b *SubAgentBudget) Remaining() int {
	r := b.MaxTurns - b.TurnsUsed
	if r < 0 {
		return 0
	}
	return r
}

// FilterToolsForMode returns only the tool names from available that are
// permitted for the given mode. Tools not in the allowlist are excluded.
func FilterToolsForMode(mode SubAgentMode, available []string) []string {
	allowed, ok := ModeToolAllowlist[mode]
	if !ok {
		// Unknown mode — deny all tools as a safe default.
		return nil
	}
	allowSet := make(map[string]bool, len(allowed))
	for _, t := range allowed {
		allowSet[t] = true
	}
	var filtered []string
	for _, t := range available {
		if allowSet[t] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}
