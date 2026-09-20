package taste

import (
	"sync"
	"time"
)

// Outcome represents the user's response to a code proposal.
type Outcome int

const (
	OutcomeAccept Outcome = iota
	OutcomeEdit
	OutcomeReject
)

func (o Outcome) String() string {
	switch o {
	case OutcomeAccept:
		return "accept"
	case OutcomeEdit:
		return "edit"
	case OutcomeReject:
		return "reject"
	default:
		return "unknown"
	}
}

// Proposal tracks what the agent proposed and how the user responded.
type Proposal struct {
	ID         string
	Proposed   string
	Final      string
	Outcome    Outcome
	Signals    []DiffSignal
	RecordedAt time.Time
}

// DiffSignal represents a detected style difference between proposed and final code.
type DiffSignal struct {
	Category string
	Proposed string
	Actual   string
}

// Collector observes agent output vs user final version and extracts style signals.
type Collector struct {
	mu        sync.Mutex
	proposals map[string]*Proposal
	profile   *Profile
}

// NewCollector creates a new taste collector bound to a profile.
func NewCollector(profile *Profile) *Collector {
	return &Collector{
		proposals: make(map[string]*Proposal),
		profile:   profile,
	}
}

// RecordProposal stores what the agent suggested.
func (c *Collector) RecordProposal(id string, proposed string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.proposals[id] = &Proposal{
		ID:         id,
		Proposed:   proposed,
		RecordedAt: time.Now(),
	}
}

// RecordOutcome marks the user's decision on a proposal.
func (c *Collector) RecordOutcome(id string, outcome Outcome) {
	c.mu.Lock()
	defer c.mu.Unlock()

	prop, ok := c.proposals[id]
	if !ok {
		return
	}
	prop.Outcome = outcome

	if outcome == OutcomeAccept {
		// Acceptance reinforces detected patterns in the proposed code.
		signals := detectCodeSignals(prop.Proposed)
		for _, sig := range signals {
			c.profile.Update(sig.Category, Signal{
				Value:      sig.Proposed,
				Confidence: 0.6,
			})
		}
	} else if outcome == OutcomeReject {
		// Rejection weakens confidence in detected patterns.
		signals := detectCodeSignals(prop.Proposed)
		for _, sig := range signals {
			existing := c.profile.Get(sig.Category)
			if existing.Value == sig.Proposed {
				c.profile.Update(sig.Category, Signal{
					Value:      "unknown",
					Confidence: 0.2,
				})
			}
		}
	}
}

// RecordEdit stores what the user actually used and computes diff signals.
func (c *Collector) RecordEdit(id string, final string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	prop, ok := c.proposals[id]
	if !ok {
		return
	}
	prop.Final = final
	prop.Outcome = OutcomeEdit
	prop.Signals = ComputeDiff(prop.Proposed, final)

	// Feed signals to profile.
	for _, sig := range prop.Signals {
		c.profile.Update(sig.Category, Signal{
			Value:      sig.Actual,
			Confidence: 0.7,
		})
	}
}

// ComputeDiff analyzes the nature of changes between proposed and final code.
func ComputeDiff(proposed, final string) []DiffSignal {
	var signals []DiffSignal

	proposedNaming := DetectNamingStyle(proposed)
	finalNaming := DetectNamingStyle(final)
	if proposedNaming != finalNaming && finalNaming != "unknown" {
		signals = append(signals, DiffSignal{
			Category: CategoryNaming,
			Proposed: proposedNaming,
			Actual:   finalNaming,
		})
	}

	proposedComments := DetectCommentDensity(proposed)
	finalComments := DetectCommentDensity(final)
	if categorizeCommentDensity(proposedComments) != categorizeCommentDensity(finalComments) {
		signals = append(signals, DiffSignal{
			Category: CategoryComments,
			Proposed: categorizeCommentDensity(proposedComments),
			Actual:   categorizeCommentDensity(finalComments),
		})
	}

	proposedErr := DetectErrorPattern(proposed)
	finalErr := DetectErrorPattern(final)
	if proposedErr != finalErr && finalErr != "unknown" {
		signals = append(signals, DiffSignal{
			Category: CategoryErrorHandling,
			Proposed: proposedErr,
			Actual:   finalErr,
		})
	}

	proposedAbstraction := DetectAbstractionLevel(proposed)
	finalAbstraction := DetectAbstractionLevel(final)
	if proposedAbstraction != finalAbstraction && finalAbstraction != "unknown" {
		signals = append(signals, DiffSignal{
			Category: CategoryAbstraction,
			Proposed: proposedAbstraction,
			Actual:   finalAbstraction,
		})
	}

	return signals
}

// detectCodeSignals extracts style signals from a single code block.
func detectCodeSignals(code string) []DiffSignal {
	var signals []DiffSignal

	if naming := DetectNamingStyle(code); naming != "unknown" {
		signals = append(signals, DiffSignal{Category: CategoryNaming, Proposed: naming})
	}
	if errStyle := DetectErrorPattern(code); errStyle != "unknown" {
		signals = append(signals, DiffSignal{Category: CategoryErrorHandling, Proposed: errStyle})
	}
	if abstraction := DetectAbstractionLevel(code); abstraction != "unknown" {
		signals = append(signals, DiffSignal{Category: CategoryAbstraction, Proposed: abstraction})
	}

	return signals
}

// categorizeCommentDensity converts a numeric density into a label.
func categorizeCommentDensity(density float64) string {
	switch {
	case density < 0.05:
		return "minimal"
	case density < 0.15:
		return "moderate"
	default:
		return "heavy"
	}
}

// PendingCount returns the number of proposals awaiting resolution.
func (c *Collector) PendingCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	for _, p := range c.proposals {
		if p.Outcome == OutcomeAccept && p.Final == "" && p.Proposed != "" {
			// Still pending initial outcome
		} else if p.Final == "" && p.Outcome == OutcomeEdit {
			count++
		}
	}
	return count
}

// RecentSignals returns the diff signals from the last N interactions.
func (c *Collector) RecentSignals(n int) []DiffSignal {
	c.mu.Lock()
	defer c.mu.Unlock()

	var all []DiffSignal
	// Collect from most recent proposals.
	type timestamped struct {
		t       time.Time
		signals []DiffSignal
	}
	var items []timestamped
	for _, p := range c.proposals {
		if len(p.Signals) > 0 {
			items = append(items, timestamped{t: p.RecordedAt, signals: p.Signals})
		}
	}
	// Sort by time descending.
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].t.After(items[i].t) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}

	for _, item := range items {
		all = append(all, item.signals...)
		if len(all) >= n {
			break
		}
	}
	if len(all) > n {
		all = all[:n]
	}
	return all
}

// Cleanup removes proposals older than the given duration.
func (c *Collector) Cleanup(maxAge time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for id, p := range c.proposals {
		if p.RecordedAt.Before(cutoff) {
			delete(c.proposals, id)
		}
	}
}
