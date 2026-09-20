package execution

import (
	"context"
	"strings"
	"testing"
)

func TestBuildTestFixPromptBoundsOutput(t *testing.T) {
	got := BuildTestFixPrompt(strings.Repeat("FAIL: some test output\n", 300), 2, 3)
	if !strings.Contains(got, "round 2/3") || !strings.Contains(got, "omitted") {
		t.Fatalf("prompt = %q", got)
	}
}

func TestRunTestFirstUsesNonNilContext(t *testing.T) {
	called := false
	result := RunTestFirst(TestFirstConfig{TestCmd: "false", MaxRounds: 1}, func(ctx context.Context, _ string) (string, error) {
		called = true
		if ctx == nil {
			t.Fatal("chat context must not be nil")
		}
		return "", nil
	})
	if !called || result.Passed || result.Rounds != 1 {
		t.Fatalf("result = %#v", result)
	}
}
