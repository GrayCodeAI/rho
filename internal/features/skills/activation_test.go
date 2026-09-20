package skills

import (
	"strings"
	"testing"

	"github.com/GrayCodeAI/rho/internal/plugin"
)

func TestResolveInvocationBuildsPromptAndArguments(t *testing.T) {
	got, err := ResolveInvocation("/rho:review", "/rho:review focus on auth", nil, []plugin.SmartSkill{{
		Name: "review", Invoke: "/rho:review", Content: "Review the code.",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Arguments != "focus on auth" || !strings.Contains(got.Prompt, "[User request]: focus on auth") {
		t.Fatalf("activation = %#v", got)
	}
}

func TestResolveInvocationReportsConflicts(t *testing.T) {
	active := map[string]plugin.SmartSkill{
		"one": {Name: "one", Chain: plugin.SkillChain{Conflicts: []string{"two"}}},
	}
	got, err := ResolveInvocation("/rho:two", "/rho:two", active, []plugin.SmartSkill{{
		Name: "two", Invoke: "/rho:two", Content: "two", Chain: plugin.SkillChain{Conflicts: []string{"one"}},
	}})
	if err != nil || len(got.Conflicts) == 0 || got.Conflicts[0] != "one" {
		t.Fatalf("activation conflicts = %#v, %v", got, err)
	}
}

func TestResolveInvocationMissingSkill(t *testing.T) {
	if _, err := ResolveInvocation("/rho:nope", "/rho:nope", nil, nil); err == nil {
		t.Fatal("expected missing skill error")
	}
}
