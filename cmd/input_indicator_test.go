package cmd

import (
	"testing"

	"github.com/GrayCodeAI/rho/internal/features/shellmode"
)

func TestInputIndicator_Classify(t *testing.T) {
	ind := &InputIndicator{}

	tests := []struct {
		input string
		mode  shellmode.Mode
		want  InputClass
	}{
		{"", shellmode.ModeAuto, InputClassNeutral},
		{"  ", shellmode.ModeAuto, InputClassNeutral},
		{"!ls -la", shellmode.ModeAuto, InputClassShell},
		{" !git status", shellmode.ModeAuto, InputClassShell},
		{"/config", shellmode.ModeAuto, InputClassSlash},
		{" /model gpt-4o", shellmode.ModeAuto, InputClassSlash},
		{"explain this code", shellmode.ModeAuto, InputClassAgent},
		{"ls -la", shellmode.ModeAuto, InputClassShell},
		{"explain this", shellmode.ModeShell, InputClassShell},
		{"ls -la", shellmode.ModeAgent, InputClassAgent},
	}

	for _, tt := range tests {
		got := ind.Classify(tt.input, tt.mode)
		if got != tt.want {
			t.Errorf("Classify(%q, %v) = %d, want %d", tt.input, tt.mode, got, tt.want)
		}
	}
}

func TestInputIndicator_Render(t *testing.T) {
	ind := &InputIndicator{}

	ind.Classify("", shellmode.ModeAuto)
	r := ind.Render()
	if r == "" {
		t.Error("expected non-empty render for neutral")
	}

	ind.Classify("!ls", shellmode.ModeAuto)
	r = ind.Render()
	if r == "" {
		t.Error("expected non-empty render for shell")
	}
}
