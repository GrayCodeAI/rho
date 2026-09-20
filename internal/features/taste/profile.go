// Package taste implements a learning system that observes user coding preferences
// and builds a style profile over time to improve agent output alignment.
package taste

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// Category constants for preference tracking.
const (
	CategoryNaming        = "naming"
	CategoryComments      = "comments"
	CategoryErrorHandling = "error_handling"
	CategoryAbstraction   = "abstraction"
	CategoryTesting       = "testing"
	CategoryFormatting    = "formatting"
)

// AllCategories returns all supported preference categories.
func AllCategories() []string {
	return []string{
		CategoryNaming,
		CategoryComments,
		CategoryErrorHandling,
		CategoryAbstraction,
		CategoryTesting,
		CategoryFormatting,
	}
}

// Signal represents a single preference observation with confidence tracking.
type Signal struct {
	Value       string    `json:"value"`
	Confidence  float64   `json:"confidence"`
	SampleCount int       `json:"sample_count"`
	LastUpdated time.Time `json:"last_updated"`
	Decay       float64   `json:"decay"`
}

// DefaultDecay is the standard exponential decay rate for signals.
const DefaultDecay = 0.95

// Profile holds a user's coding style preferences, keyed by category.
type Profile struct {
	mu          sync.RWMutex
	Preferences map[string]Signal `json:"preferences"`
	ProjectID   string            `json:"project_id,omitempty"`
	Version     int               `json:"version"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// NewProfile creates a fresh empty profile.
func NewProfile(projectID string) *Profile {
	now := time.Now()
	return &Profile{
		Preferences: make(map[string]Signal),
		ProjectID:   projectID,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// Update records a new signal for a category using exponential moving average with decay.
func (p *Profile) Update(category string, signal Signal) {
	p.mu.Lock()
	defer p.mu.Unlock()

	existing, ok := p.Preferences[category]
	if !ok {
		// First observation — accept directly.
		if signal.Decay == 0 {
			signal.Decay = DefaultDecay
		}
		if signal.SampleCount == 0 {
			signal.SampleCount = 1
		}
		signal.LastUpdated = time.Now()
		p.Preferences[category] = signal
		p.UpdatedAt = time.Now()
		return
	}

	// Exponential moving average: blend new value in.
	decay := existing.Decay
	if decay == 0 {
		decay = DefaultDecay
	}

	existing.SampleCount++

	if signal.Value == existing.Value {
		// Same preference reinforced — increase confidence.
		existing.Confidence = existing.Confidence*decay + (1-decay)*1.0
		existing.Confidence = math.Min(existing.Confidence, 1.0)
	} else {
		// Different preference — decrease confidence of existing.
		existing.Confidence *= decay
		// If confidence drops below threshold, switch to new value.
		if existing.Confidence < 0.3 || signal.Confidence > existing.Confidence {
			existing.Value = signal.Value
			existing.Confidence = signal.Confidence
		}
	}

	existing.LastUpdated = time.Now()
	existing.Decay = decay
	p.Preferences[category] = existing
	p.UpdatedAt = time.Now()
}

// Get returns the current preference signal for a category.
func (p *Profile) Get(category string) Signal {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Preferences[category]
}

// ToPromptContext serializes the top preferences into a system prompt fragment.
func (p *Profile) ToPromptContext() string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.Preferences) == 0 {
		return ""
	}

	// Sort by confidence (descending) and filter low-confidence.
	type entry struct {
		category string
		signal   Signal
	}
	var entries []entry
	for cat, sig := range p.Preferences {
		if sig.Confidence >= 0.4 && sig.SampleCount >= 2 {
			entries = append(entries, entry{category: cat, signal: sig})
		}
	}

	if len(entries) == 0 {
		return ""
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].signal.Confidence > entries[j].signal.Confidence
	})

	var b strings.Builder
	b.WriteString("User style preferences (learned from past interactions):\n")
	for _, e := range entries {
		b.WriteString(fmt.Sprintf("- %s: %s (confidence: %.0f%%)\n",
			e.category, e.signal.Value, e.signal.Confidence*100))
	}
	return b.String()
}

// Merge blends another profile into this one, useful for team taste sharing.
func (p *Profile) Merge(other *Profile) {
	if other == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	other.mu.RLock()
	defer other.mu.RUnlock()

	for cat, otherSig := range other.Preferences {
		existing, ok := p.Preferences[cat]
		if !ok {
			// New category from other — adopt with reduced confidence.
			otherSig.Confidence *= 0.7
			p.Preferences[cat] = otherSig
			continue
		}

		if existing.Value == otherSig.Value {
			// Same preference — boost confidence.
			existing.Confidence = math.Min(1.0, existing.Confidence+otherSig.Confidence*0.3)
			existing.SampleCount += otherSig.SampleCount
			p.Preferences[cat] = existing
		} else {
			// Conflicting — keep stronger one with slightly reduced confidence.
			if otherSig.Confidence > existing.Confidence {
				otherSig.Confidence *= 0.8
				p.Preferences[cat] = otherSig
			}
		}
	}
	p.UpdatedAt = time.Now()
}

// Reset clears all preferences.
func (p *Profile) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Preferences = make(map[string]Signal)
	p.UpdatedAt = time.Now()
}

// Summary returns a human-readable summary of the profile.
func (p *Profile) Summary() string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.Preferences) == 0 {
		return "No taste preferences recorded yet."
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Taste Profile (project: %s, version: %d)\n", p.ProjectID, p.Version))
	b.WriteString(fmt.Sprintf("Created: %s | Updated: %s\n\n", p.CreatedAt.Format("2006-01-02"), p.UpdatedAt.Format("2006-01-02")))

	for _, cat := range AllCategories() {
		sig, ok := p.Preferences[cat]
		if !ok {
			continue
		}
		confidence := fmt.Sprintf("%.0f%%", sig.Confidence*100)
		samples := fmt.Sprintf("%d samples", sig.SampleCount)
		b.WriteString(fmt.Sprintf("  %-16s %s (%s, %s)\n", cat+":", sig.Value, confidence, samples))
	}
	return b.String()
}
