package shellmode

import "testing"

func TestClassifyInput(t *testing.T) {
	tests := []struct {
		input string
		want  Classification
	}{
		// Empty
		{"", ClassNeutral},
		{"   ", ClassNeutral},
		// Agent words
		{"why", ClassAgent},
		{"explain", ClassAgent},
		{"thanks", ClassAgent},
		{"hello", ClassAgent},
		{"refactor", ClassAgent},
		// Shell reserved words → agent
		{"do we have a way to install?", ClassAgent},
		{"in the codebase where is auth?", ClassAgent},
		// Valid commands → shell
		{"ls", ClassShell},
		{"git status", ClassShell},
		{"echo hello world", ClassShell},
		// Multi-word, first not a command → agent
		{"fix the bug in auth", ClassAgent},
		{"what files are here", ClassAgent},
		// Single unknown word → agent (conversational)
		{"asdfgh", ClassAgent},
		// Genuine shell usage of an ambiguous-word command, with real shell
		// syntax evidence (flags/paths) → shell.
		{"find . -name *.go", ClassShell},
		{"kill -9 12345", ClassShell},
		{"make -C build", ClassShell},
		{"sort file.txt", ClassShell},
		// Bare single-word ambiguous command → shell (unambiguous either way).
		{"kill", ClassShell},
	}

	for _, tt := range tests {
		got := ClassifyInput(tt.input)
		if got != tt.want {
			t.Errorf("ClassifyInput(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

// TestClassifyInput_AmbiguousWordsWithoutShellSyntax is a dedicated
// regression suite for the bug found in this session: ClassifyInput used
// to trust isValidCommand(firstWord) alone, so any ordinary English
// sentence that happened to start with a word that's also a real Unix
// binary (make, test, find, kill, time, sort, diff, patch, more, less,
// man, who, date, file, which, look...) was misclassified as a shell
// command instead of a chat message. Confirmed empirically: 18 of 20 such
// sentences were misclassified before this fix. The fix requires real
// shell-syntax evidence (flags/paths/operators) for these ambiguous
// command names before trusting them as shell.
func TestClassifyInput_AmbiguousWordsWithoutShellSyntax(t *testing.T) {
	sentences := []string{
		"make sure this works",
		"test the login flow",
		"find the bug in this file",
		"time to refactor this",
		"kill the old branch",
		"man this is annoying",
		"more context please",
		"less verbose next time",
		"true or false, does this work",
		"date the commit properly",
		"file a bug report",
		"look at this closely",
		"expr yourself clearly",
		"link this to the issue",
		"mount an argument for this",
		"sort out the imports",
		"diff this against main",
		"patch up the tests",
	}
	for _, s := range sentences {
		if got := ClassifyInput(s); got != ClassAgent {
			t.Errorf("ClassifyInput(%q) = %d, want ClassAgent(%d) — ambiguous command word without shell syntax must route to agent", s, got, ClassAgent)
		}
	}
}

func TestHasShellSyntaxEvidence(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"find . -name *.go", true},
		{"kill -9 12345", true},
		{"find the bug in this file", false},
		{"make sure this works", false},
		{"cat file.txt", true},
		{"echo hello | grep world", true},
		{"look at this closely", false},
	}
	for _, tt := range tests {
		if got := hasShellSyntaxEvidence(tt.input); got != tt.want {
			t.Errorf("hasShellSyntaxEvidence(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
