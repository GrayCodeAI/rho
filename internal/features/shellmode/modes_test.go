package shellmode

import "testing"

func TestModeManager_Toggle(t *testing.T) {
	mm := NewModeManager()
	if mm.Current() != ModeAuto {
		t.Fatal("expected auto as default")
	}
	m := mm.Toggle()
	if m != ModeShell {
		t.Errorf("first toggle: got %v, want shell", m)
	}
	m = mm.Toggle()
	if m != ModeAgent {
		t.Errorf("second toggle: got %v, want agent", m)
	}
	m = mm.Toggle()
	if m != ModeAuto {
		t.Errorf("third toggle: got %v, want auto", m)
	}
}

func TestModeManager_ClassifyWithMode(t *testing.T) {
	mm := NewModeManager()

	// Auto mode — uses detection
	mm.Set(ModeAuto)
	if mm.ClassifyWithMode("explain this") != ClassAgent {
		t.Error("auto mode should detect 'explain' as agent")
	}

	// Shell mode — always shell
	mm.Set(ModeShell)
	if mm.ClassifyWithMode("explain this") != ClassShell {
		t.Error("shell mode should force shell")
	}

	// Agent mode — always agent
	mm.Set(ModeAgent)
	if mm.ClassifyWithMode("ls -la") != ClassAgent {
		t.Error("agent mode should force agent")
	}
}

func TestParseMode(t *testing.T) {
	tests := []struct {
		input string
		want  Mode
		ok    bool
	}{
		{"auto", ModeAuto, true},
		{"shell", ModeShell, true},
		{"agent", ModeAgent, true},
		{"ai", ModeAgent, true},
		{"s", ModeShell, true},
		{"a", ModeAuto, true},
		{"invalid", ModeAuto, false},
	}
	for _, tt := range tests {
		got, ok := ParseMode(tt.input)
		if ok != tt.ok || got != tt.want {
			t.Errorf("ParseMode(%q) = (%v, %v), want (%v, %v)", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}
