package cmd

import (
	"testing"

	"github.com/GrayCodeAI/rho/internal/engine/safety"
)

func TestInteractiveOverlaysZeroValuesRenderSafely(t *testing.T) {
	t.Run("autonomy", func(t *testing.T) {
		picker := &AutonomyPicker{}
		picker.Open(safety.AutonomySupervised)
		if got := picker.Render(80); got == "" {
			t.Fatal("zero-value autonomy picker rendered empty content")
		}
	})

	t.Run("spec", func(t *testing.T) {
		picker := &SpecPicker{}
		picker.Open(safety.SpecStageNone)
		if got := picker.Render(80); got == "" {
			t.Fatal("zero-value spec picker rendered empty content")
		}
	})

	t.Run("command palette", func(t *testing.T) {
		palette := &CommandPalette{}
		palette.Open()
		if got := palette.Render(80); got == "" {
			t.Fatal("zero-value command palette rendered empty content")
		}
	})
}
