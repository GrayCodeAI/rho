package cmd

import (
	"strings"
	"testing"

	commandfeature "github.com/GrayCodeAI/rho/internal/features/commands"
)

func TestSlashCommands_NotEmpty(t *testing.T) {
	t.Parallel()
	cmds := slashCommands()
	if len(cmds) == 0 {
		t.Fatal("slashCommands() should not be empty")
	}
	for _, c := range cmds {
		if !strings.HasPrefix(c, "/") {
			t.Errorf("command %q should start with /", c)
		}
	}
}

func TestSlashCommands_ContainsEssentials(t *testing.T) {
	t.Parallel()
	cmds := slashCommands()
	essential := []string{"/help", "/exit", "/clear", "/model", "/version", "/undo"}
	for _, e := range essential {
		found := false
		for _, c := range cmds {
			if c == e {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("slashCommands() missing essential command %q", e)
		}
	}
}

func TestBuiltInCommandsHaveHandlers(t *testing.T) {
	t.Helper()
	for _, name := range commandfeature.BuiltInNames() {
		trimmed := strings.TrimPrefix(name, "/")
		if _, ok := subcommandRegistry.Lookup(trimmed); !ok {
			t.Errorf("built-in command %s is advertised but has no registered handler", name)
		}
	}
}

func TestSlashSuggestions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input   string
		wantAny bool
	}{
		{"/he", true},
		{"/mo", true},
		{"/ex", true},
		{"/zzz", false},
		{"hello", false},
		{"/", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			suggestions := slashSuggestions(tt.input)
			if tt.wantAny && len(suggestions) == 0 {
				t.Errorf("slashSuggestions(%q) = empty, want results", tt.input)
			}
			if !tt.wantAny && len(suggestions) > 0 {
				t.Errorf("slashSuggestions(%q) = %v, want empty", tt.input, suggestions)
			}
		})
	}
}

func TestHasString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		values []string
		want   string
		result bool
	}{
		{[]string{"a", "b", "c"}, "b", true},
		{[]string{"a", "b", "c"}, "d", false},
		{nil, "a", false},
		{[]string{}, "a", false},
	}
	for _, tt := range tests {
		got := hasString(tt.values, tt.want)
		if got != tt.result {
			t.Errorf("hasString(%v, %q) = %v, want %v", tt.values, tt.want, got, tt.result)
		}
	}
}

func TestBranchSummary(t *testing.T) {
	// May produce output or empty depending on whether we're in a git repo.
	// It must at least be deterministic for a fixed repo state.
	summary := branchSummary()
	if summary != branchSummary() {
		t.Error("branchSummary is not deterministic for the same repo state")
	}
}

func TestFilesSummary(t *testing.T) {
	summary := filesSummary()
	if summary != filesSummary() {
		t.Error("filesSummary is not deterministic for the same repo state")
	}
}

func TestHooksSummary(t *testing.T) {
	t.Parallel()
	summary := hooksSummary()
	if summary != hooksSummary() {
		t.Error("hooksSummary is not deterministic for the same repo state")
	}
}

func TestApplySlashSuggestion(t *testing.T) {
	t.Parallel()
	result := applySlashSuggestion("/help")
	if result == "" {
		t.Error("should return non-empty")
	}
}

func TestSlashSuggestionsIncludeRegisteredCommandsAndAreDeterministic(t *testing.T) {
	first := strings.Join(slashSuggestions("/"), "\n")
	second := strings.Join(slashSuggestions("/"), "\n")
	if first != second {
		t.Fatal("slash suggestions changed between identical calls")
	}
	for _, sub := range subcommandRegistry.All() {
		if sub.Name() == "" {
			continue
		}
		found := false
		for _, suggestion := range slashSuggestions("/") {
			if strings.HasPrefix(suggestion, "/"+sub.Name()+" ") || suggestion == "/"+sub.Name() {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing suggestion for registered command /%s", sub.Name())
		}
	}
}

func TestStalenessFormatReport(t *testing.T) {
	t.Parallel()
	report := stalenessFormatReport(nil)
	_ = report
}
