package permissions

import (
	"testing"
	"time"
)

func TestCanonicalToolNameAliases(t *testing.T) {
	cases := map[string]string{
		" bash ":          "Bash",
		"power_shell":     "PowerShell",
		"file_read":       "Read",
		"web_search":      "WebSearch",
		"deps":            "DependencyAudit",
		"pty_create":      "TerminalCreate",
		"sendusermessage": "SendUserMessage",
		"tool_search":     "ToolSearch",
		"custom_tool":     "custom_tool",
	}
	for input, want := range cases {
		if got := CanonicalToolName(input); got != want {
			t.Errorf("CanonicalToolName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestUnifiedGrantsCanonicalizesLookupNames(t *testing.T) {
	grants := NewUnifiedGrants(FuncGrantStore{Fn: func() []Grant {
		return []Grant{{Tool: "WebSearch", Pattern: "*", Allow: true}}
	}})
	allowed, found := grants.Check("web_search", "anything", time.Now())
	if !found || !allowed {
		t.Fatalf("canonical alias lookup = allowed %v, found %v; want allow", allowed, found)
	}
}
