package review

import (
	"context"
	"strings"
	"testing"
)

func TestParseFindingsJSONAndFallback(t *testing.T) {
	got := ParseFindings("prefix [{\"file\":\"a.go\",\"line\":3,\"severity\":\"high\",\"message\":\"bug\",\"fix\":\"fix it\"}] suffix", "bugs")
	if len(got) != 1 || got[0].File != "a.go" || got[0].Concern != "bugs" {
		t.Fatalf("JSON findings = %#v", got)
	}
	got = ParseFindings("The code has an issue", "style")
	if len(got) != 1 || got[0].Severity != "medium" {
		t.Fatalf("fallback findings = %#v", got)
	}
}

func TestRunDeduplicatesAndSorts(t *testing.T) {
	concerns := []Concern{{Name: "security", Prompt: "security"}, {Name: "bugs", Prompt: "bugs"}}
	findings, report := Run(context.Background(), []string{"a.go"}, concerns, func(_ context.Context, prompt string) (string, error) {
		if strings.Contains(prompt, "security") {
			return `[{"file":"a.go","line":2,"severity":"low","message":"same"}]`, nil
		}
		return `[{"file":"a.go","line":1,"severity":"critical","message":"critical"},{"file":"a.go","line":2,"severity":"low","message":"same"}]`, nil
	})
	if len(findings) != 2 || findings[0].Severity != "critical" || !strings.Contains(report, "2 issue(s) total") {
		t.Fatalf("findings=%#v report=%q", findings, report)
	}
}
