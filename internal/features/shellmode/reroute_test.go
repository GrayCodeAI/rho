package shellmode

import "testing"

func TestRerouteCandidate(t *testing.T) {
	tests := []struct {
		cmd      string
		stderr   string
		exitCode int
		want     bool
	}{
		// Should reroute: NL + error pattern (2+ markers)
		{"kill the process on the port", "kill: the: command not found", 1, true},
		{"go ahead and fix the tests", "go ahead: unknown command", 1, true},
		{"make sure the tests pass", "make: *** No rule to make target 'sure'. Stop.", 2, true},
		// Should NOT reroute: success
		{"echo the quick brown fox", "", 0, false},
		// Should NOT reroute: signal (killed)
		{"sleep 100", "", 137, false},
		// Should NOT reroute: no NL markers (only flags/paths)
		{"ls -la /tmp", "ls: /tmp: No such file or directory", 1, false},
		// Should NOT reroute: no error pattern
		{"go ahead and fix", "some random output", 1, false},
	}

	for _, tt := range tests {
		got := RerouteCandidate(tt.cmd, tt.stderr, tt.exitCode)
		if got != tt.want {
			t.Errorf("RerouteCandidate(%q, %q, %d) = %v, want %v",
				tt.cmd, tt.stderr, tt.exitCode, got, tt.want)
		}
	}
}

func TestHasNLMarkers(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"kill the process on the port", true},
		{"fix my code please", true},
		{"ls -la", false},
		{"git status", false},
		{"single", false},
		{"git push the-branch", false}, // "the-branch" is one word with dash
	}
	for _, tt := range tests {
		got := hasNLMarkers(tt.input)
		if got != tt.want {
			t.Errorf("hasNLMarkers(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
