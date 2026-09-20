package safety

import "testing"

func TestParseAutonomyLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected AutonomyLevel
	}{
		{"0", AutonomySupervised},
		{"supervised", AutonomySupervised},
		{"1", AutonomyBasic},
		{"basic", AutonomyBasic},
		{"2", AutonomySemi},
		{"accept-edits", AutonomySemi},
		{"3", AutonomyFull},
		{"full", AutonomyFull},
		{"4", AutonomyYOLO},
		{"dont_ask", AutonomyYOLO},
		{"unknown", AutonomySupervised},
		{"", AutonomySupervised},
	}
	for _, tt := range tests {
		if got := ParseAutonomyLevel(tt.input); got != tt.expected {
			t.Errorf("ParseAutonomyLevel(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestAutonomyLevelString(t *testing.T) {
	for level, want := range map[AutonomyLevel]string{
		AutonomySupervised: "supervised",
		AutonomyBasic:      "basic",
		AutonomySemi:       "semi",
		AutonomyFull:       "full",
		AutonomyYOLO:       "yolo",
		AutonomyLevel(99):  "supervised",
	} {
		if got := level.String(); got != want {
			t.Errorf("AutonomyLevel(%d).String() = %q, want %q", level, got, want)
		}
	}
}
