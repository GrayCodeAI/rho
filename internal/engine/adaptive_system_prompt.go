package engine

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/GrayCodeAI/rho/internal/provider/routing"
)

// PromptBuildContext provides situational context for building a system prompt.
type PromptBuildContext struct {
	Task        string
	Language    string
	ProjectType string
	Model       string
	TurnCount   int
	HasMemory   bool
	HasGoals    bool
}

// PromptSection represents a named section of the system prompt with priority and
// optional dynamic generation logic.
type PromptSection struct {
	Name        string
	Content     string
	Priority    int           // 1 = highest priority, higher numbers = lower priority
	Tokens      int           // pre-computed token count (0 means recalculate)
	Conditional func() bool   // if non-nil, section is included only when this returns true
	Dynamic     func() string // if non-nil, content is generated at build time
}

// SystemPromptBuilder constructs a system prompt by assembling prioritized sections
// within a token budget. It complements AdaptivePrompt by handling structural composition
// rather than learned user preferences.
type SystemPromptBuilder struct {
	BasePrompt string
	Sections   []PromptSection
	MaxTokens  int
	mu         sync.RWMutex
}

// NewSystemPromptBuilder creates a SystemPromptBuilder with a base prompt and token limit.
func NewSystemPromptBuilder(basePrompt string, maxTokens int) *SystemPromptBuilder {
	return &SystemPromptBuilder{
		BasePrompt: basePrompt,
		MaxTokens:  maxTokens,
	}
}

// AddSection appends a prompt section. If a section with the same name exists, it is replaced.
func (b *SystemPromptBuilder) AddSection(section PromptSection) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, s := range b.Sections {
		if s.Name == section.Name {
			b.Sections[i] = section
			return
		}
	}
	b.Sections = append(b.Sections, section)
}

// RemoveSection removes a section by name.
func (b *SystemPromptBuilder) RemoveSection(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, s := range b.Sections {
		if s.Name == name {
			b.Sections = append(b.Sections[:i], b.Sections[i+1:]...)
			return
		}
	}
}

// Build assembles the final system prompt by evaluating conditionals, calling dynamic
// generators, sorting by priority, and fitting sections within the token budget.
func (b *SystemPromptBuilder) Build(ctx PromptBuildContext) string {
	b.mu.RLock()
	sections := make([]PromptSection, len(b.Sections))
	copy(sections, b.Sections)
	base := b.BasePrompt
	maxTokens := b.MaxTokens
	b.mu.RUnlock()

	// Evaluate conditionals and resolve dynamic content
	var active []PromptSection
	for _, s := range sections {
		if s.Conditional != nil && !s.Conditional() {
			continue
		}
		if s.Dynamic != nil {
			s.Content = s.Dynamic()
		}
		if s.Content == "" {
			continue
		}
		if s.Tokens == 0 {
			s.Tokens = EstimateStringTokens(s.Content)
		}
		active = append(active, s)
	}

	// Sort by priority (lower number = higher priority)
	sort.SliceStable(active, func(i, j int) bool {
		return active[i].Priority < active[j].Priority
	})

	// Fit within token budget
	baseTokens := EstimateStringTokens(base)
	remaining := maxTokens - baseTokens
	if remaining < 0 {
		remaining = 0
	}

	var included []PromptSection
	for _, s := range active {
		if s.Tokens <= remaining {
			included = append(included, s)
			remaining -= s.Tokens
		}
	}

	return FormatPrompt(base, included)
}

// AdaptForTask adjusts section priorities based on the task type, returning the builder
// for method chaining. Modifies the builder in place.
func (b *SystemPromptBuilder) AdaptForTask(task string) *SystemPromptBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	lower := strings.ToLower(task)

	for i := range b.Sections {
		switch {
		case containsAny(lower, "debug", "fix", "error", "bug", "crash"):
			// Debugging: boost conventions, add error patterns
			if b.Sections[i].Name == "conventions" {
				b.Sections[i].Priority = promptMaxInt(1, b.Sections[i].Priority-1)
			}
		case containsAny(lower, "review", "audit", "check"):
			// Code review: boost safety
			if b.Sections[i].Name == "safety" {
				b.Sections[i].Priority = 1
			}
		case containsAny(lower, "implement", "create", "build", "add", "write"):
			// Implementation: boost examples and architecture
			if b.Sections[i].Name == "examples" {
				b.Sections[i].Priority = promptMaxInt(1, b.Sections[i].Priority-1)
			}
		}
	}

	// Add task-specific sections
	switch {
	case containsAny(lower, "debug", "fix", "error", "bug", "crash"):
		b.addSectionLocked(PromptSection{
			Name:     "error_patterns",
			Content:  "When debugging, systematically narrow the problem: check logs, reproduce, isolate the minimal failing case, and verify the fix does not regress.",
			Priority: 3,
		})
	case containsAny(lower, "review", "audit", "check"):
		b.addSectionLocked(PromptSection{
			Name:     "review_checklist",
			Content:  "Review checklist: correctness, edge cases, security, performance, readability, test coverage, backward compatibility.",
			Priority: 3,
		})
	case containsAny(lower, "implement", "create", "build", "add", "write"):
		b.addSectionLocked(PromptSection{
			Name:     "architecture",
			Content:  "Follow existing architectural patterns. Keep changes minimal and focused. Write tests alongside implementation.",
			Priority: 3,
		})
	}

	return b
}

// AdaptForModel adjusts verbosity and section limits based on the target model.
func (b *SystemPromptBuilder) AdaptForModel(model string) *SystemPromptBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch routing.CostTierOf(model) {
	case routing.CostTierExpensive:
		b.MaxTokens = b.MaxTokens * 12 / 10
	case routing.CostTierCheap:
		b.MaxTokens = b.MaxTokens * 7 / 10
		for i := range b.Sections {
			if b.Sections[i].Name == "examples" {
				b.Sections[i].Priority = 10
			}
		}
	}

	return b
}

// EstimateStringTokens provides a rough token count for content using the ~4 chars per token heuristic.
// This avoids external dependencies and is suitable for budget estimation.
func EstimateStringTokens(content string) int {
	if content == "" {
		return 0
	}
	// Approximate: 1 token per 4 characters for English text
	return (len(content) + 3) / 4
}

// FormatPrompt assembles the base prompt and included sections into a final string.
func FormatPrompt(base string, sections []PromptSection) string {
	var b strings.Builder
	if base != "" {
		b.WriteString(base)
		b.WriteString("\n\n")
	}

	for i, s := range sections {
		b.WriteString("## ")
		b.WriteString(s.Name)
		b.WriteString("\n")
		b.WriteString(s.Content)
		if i < len(sections)-1 {
			b.WriteString("\n\n")
		}
	}

	return strings.TrimSpace(b.String())
}

// DiffPrompts shows what changed between two prompt versions line by line.
func DiffPrompts(old, new string) string {
	oldLines := strings.Split(old, "\n")
	newLines := strings.Split(new, "\n")

	oldSet := make(map[string]struct{}, len(oldLines))
	for _, l := range oldLines {
		oldSet[l] = struct{}{}
	}
	newSet := make(map[string]struct{}, len(newLines))
	for _, l := range newLines {
		newSet[l] = struct{}{}
	}

	var diff strings.Builder
	// Lines removed
	for _, l := range oldLines {
		if _, ok := newSet[l]; !ok && strings.TrimSpace(l) != "" {
			diff.WriteString(fmt.Sprintf("- %s\n", l))
		}
	}
	// Lines added
	for _, l := range newLines {
		if _, ok := oldSet[l]; !ok && strings.TrimSpace(l) != "" {
			diff.WriteString(fmt.Sprintf("+ %s\n", l))
		}
	}

	result := diff.String()
	if result == "" {
		return "(no changes)"
	}
	return strings.TrimRight(result, "\n")
}

// DefaultSections returns the built-in prompt sections that rho uses.
func DefaultSections(ctx PromptBuildContext) []PromptSection {
	sections := []PromptSection{
		{
			Name:     "identity",
			Content:  "You are rho, an AI coding agent. You help developers write, debug, review, and refactor code. You operate inside the user's repository with access to tools for file manipulation, shell commands, and code search.",
			Priority: 1,
		},
		{
			Name:     "safety",
			Content:  "Permission and safety rules:\n- Never execute destructive commands without explicit user approval.\n- Never modify files outside the project directory unless instructed.\n- Never expose secrets, credentials, or tokens.\n- Ask before making large-scale changes.\n- Respect .gitignore and permission boundaries.",
			Priority: 1,
		},
		{
			Name:     "tools",
			Content:  "You have access to tools for: reading files, writing files, running shell commands, searching code, and managing git. Use tools deliberately and explain your reasoning.",
			Priority: 2,
		},
		{
			Name:     "project",
			Content:  "Follow project-specific conventions documented in AGENTS.md. Respect existing code style, patterns, and architecture.",
			Priority: 2,
			Conditional: func() bool {
				return ctx.ProjectType != ""
			},
		},
		{
			Name:     "conventions",
			Content:  "Follow established coding conventions: consistent naming, proper error handling, clear comments for complex logic, and idiomatic patterns for the language in use.",
			Priority: 3,
			Conditional: func() bool {
				return ctx.Language != ""
			},
		},
		{
			Name:     "memory",
			Content:  "Use relevant memories from prior sessions to maintain continuity and apply learned context.",
			Priority: 3,
			Conditional: func() bool {
				return ctx.HasMemory
			},
		},
		{
			Name:     "goals",
			Content:  "Track progress toward stated objectives. Report completion of sub-goals and remaining work.",
			Priority: 4,
			Conditional: func() bool {
				return ctx.HasGoals
			},
		},
		{
			Name:     "examples",
			Content:  "Refer to successful prior interactions for style and approach guidance.",
			Priority: 5,
			Conditional: func() bool {
				return ctx.TurnCount < 5 // examples most useful early in conversation
			},
		},
	}

	return sections
}

// addSectionLocked adds or replaces a section (caller must hold the lock).
func (b *SystemPromptBuilder) addSectionLocked(section PromptSection) {
	for i, s := range b.Sections {
		if s.Name == section.Name {
			b.Sections[i] = section
			return
		}
	}
	b.Sections = append(b.Sections, section)
}

// containsAny returns true if s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// promptMaxInt returns the larger of two ints.
func promptMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
