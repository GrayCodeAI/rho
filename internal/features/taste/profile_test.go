package taste

import (
	"strings"
	"testing"
	"time"
)

func TestNewProfile(t *testing.T) {
	p := NewProfile("test-project")
	if p.ProjectID != "test-project" {
		t.Errorf("expected project ID 'test-project', got %q", p.ProjectID)
	}
	if p.Version != 1 {
		t.Errorf("expected version 1, got %d", p.Version)
	}
	if len(p.Preferences) != 0 {
		t.Errorf("expected empty preferences, got %d", len(p.Preferences))
	}
}

func TestProfile_Update_NewCategory(t *testing.T) {
	p := NewProfile("test")
	p.Update(CategoryNaming, Signal{Value: "camelCase", Confidence: 0.8})

	sig := p.Get(CategoryNaming)
	if sig.Value != "camelCase" {
		t.Errorf("expected value 'camelCase', got %q", sig.Value)
	}
	if sig.Confidence != 0.8 {
		t.Errorf("expected confidence 0.8, got %f", sig.Confidence)
	}
	if sig.SampleCount != 1 {
		t.Errorf("expected sample count 1, got %d", sig.SampleCount)
	}
}

func TestProfile_Update_ReinforceSameValue(t *testing.T) {
	p := NewProfile("test")
	p.Update(CategoryNaming, Signal{Value: "snake_case", Confidence: 0.5})
	p.Update(CategoryNaming, Signal{Value: "snake_case", Confidence: 0.6})
	p.Update(CategoryNaming, Signal{Value: "snake_case", Confidence: 0.7})

	sig := p.Get(CategoryNaming)
	if sig.Value != "snake_case" {
		t.Errorf("expected snake_case, got %q", sig.Value)
	}
	if sig.SampleCount != 3 {
		t.Errorf("expected 3 samples, got %d", sig.SampleCount)
	}
	// Confidence should have increased from reinforcement.
	if sig.Confidence <= 0.5 {
		t.Errorf("expected confidence to increase, got %f", sig.Confidence)
	}
}

func TestProfile_Update_ConflictingValue(t *testing.T) {
	p := NewProfile("test")
	// Start with strong camelCase preference.
	p.Update(CategoryNaming, Signal{Value: "camelCase", Confidence: 0.9})

	// Send many conflicting signals.
	for i := 0; i < 20; i++ {
		p.Update(CategoryNaming, Signal{Value: "snake_case", Confidence: 0.8})
	}

	// Eventually should switch to snake_case.
	sig := p.Get(CategoryNaming)
	if sig.Value != "snake_case" {
		t.Errorf("expected preference to switch to snake_case after many conflicting signals, got %q", sig.Value)
	}
}

func TestProfile_ToPromptContext_Empty(t *testing.T) {
	p := NewProfile("test")
	ctx := p.ToPromptContext()
	if ctx != "" {
		t.Errorf("expected empty context for empty profile, got %q", ctx)
	}
}

func TestProfile_ToPromptContext_WithPreferences(t *testing.T) {
	p := NewProfile("test")
	p.Preferences[CategoryNaming] = Signal{Value: "camelCase", Confidence: 0.85, SampleCount: 5}
	p.Preferences[CategoryComments] = Signal{Value: "minimal", Confidence: 0.7, SampleCount: 3}
	p.Preferences[CategoryErrorHandling] = Signal{Value: "wrapped", Confidence: 0.2, SampleCount: 1} // low confidence

	ctx := p.ToPromptContext()
	if !strings.Contains(ctx, "camelCase") {
		t.Error("expected camelCase in prompt context")
	}
	if !strings.Contains(ctx, "minimal") {
		t.Error("expected minimal in prompt context")
	}
	// Low confidence should be excluded.
	if strings.Contains(ctx, "wrapped") {
		t.Error("expected wrapped to be excluded (low confidence)")
	}
}

func TestProfile_Merge(t *testing.T) {
	p1 := NewProfile("project1")
	p1.Update(CategoryNaming, Signal{Value: "camelCase", Confidence: 0.8})

	p2 := NewProfile("project2")
	p2.Update(CategoryNaming, Signal{Value: "camelCase", Confidence: 0.9})
	p2.Update(CategoryComments, Signal{Value: "heavy", Confidence: 0.7})

	p1.Merge(p2)

	// Same value — confidence should be boosted.
	naming := p1.Get(CategoryNaming)
	if naming.Value != "camelCase" {
		t.Errorf("expected camelCase after merge, got %q", naming.Value)
	}

	// New category from p2 should be adopted.
	comments := p1.Get(CategoryComments)
	if comments.Value != "heavy" {
		t.Errorf("expected heavy comments from merge, got %q", comments.Value)
	}
	// Confidence should be reduced for merged-in preferences.
	if comments.Confidence >= 0.7 {
		t.Errorf("expected reduced confidence for merged preference, got %f", comments.Confidence)
	}
}

func TestProfile_Reset(t *testing.T) {
	p := NewProfile("test")
	p.Update(CategoryNaming, Signal{Value: "camelCase", Confidence: 0.8})
	p.Update(CategoryComments, Signal{Value: "minimal", Confidence: 0.6})

	p.Reset()

	if len(p.Preferences) != 0 {
		t.Errorf("expected empty preferences after reset, got %d", len(p.Preferences))
	}
}

func TestProfile_Summary(t *testing.T) {
	p := NewProfile("my-project")
	p.Update(CategoryNaming, Signal{Value: "snake_case", Confidence: 0.75})

	summary := p.Summary()
	if !strings.Contains(summary, "my-project") {
		t.Error("expected project name in summary")
	}
	if !strings.Contains(summary, "snake_case") {
		t.Error("expected naming preference in summary")
	}
}

func TestProfile_Get_NonexistentCategory(t *testing.T) {
	p := NewProfile("test")
	sig := p.Get("nonexistent")
	if sig.Value != "" || sig.Confidence != 0 {
		t.Errorf("expected zero Signal for nonexistent category, got %+v", sig)
	}
}

func TestProfile_Update_SetsLastUpdated(t *testing.T) {
	p := NewProfile("test")
	before := time.Now()
	p.Update(CategoryNaming, Signal{Value: "camelCase", Confidence: 0.5})
	after := time.Now()

	sig := p.Get(CategoryNaming)
	if sig.LastUpdated.Before(before) || sig.LastUpdated.After(after) {
		t.Error("expected LastUpdated to be within test bounds")
	}
}
