package workspace

import (
	"errors"
	"strings"
	"testing"
)

func TestDiffReportWithUsesCachedDiffWhenWorkingTreeIsEmpty(t *testing.T) {
	got, err := DiffReportWith(func(args ...string) (string, error) {
		key := strings.Join(args, " ")
		switch key {
		case "diff --stat", "diff":
			return "", nil
		case "diff --cached --stat":
			return "1 file changed", nil
		case "diff --cached":
			return "@@ -1 +1 @@", nil
		default:
			t.Fatalf("unexpected git args: %s", key)
			return "", nil
		}
	})
	if err != nil || !strings.Contains(got, "1 file changed") || !strings.Contains(got, "@@ -1 +1 @@") {
		t.Fatalf("DiffReportWith = %q, %v", got, err)
	}
}

func TestDiffReportWithReturnsEmptyForCleanTree(t *testing.T) {
	got, err := DiffReportWith(func(args ...string) (string, error) { return "", nil })
	if err != nil || got != "" {
		t.Fatalf("clean DiffReportWith = %q, %v", got, err)
	}
}

func TestDiffReportWithPropagatesGitErrors(t *testing.T) {
	want := errors.New("not a repository")
	got, err := DiffReportWith(func(args ...string) (string, error) {
		if strings.Join(args, " ") == "diff --stat" {
			return "", want
		}
		return "", nil
	})
	if !errors.Is(err, want) || got != "" {
		t.Fatalf("error DiffReportWith = %q, %v", got, err)
	}
}

func TestDiffReportWithLimitsLargeOutput(t *testing.T) {
	large := strings.Repeat("x", 10001)
	got, err := DiffReportWith(func(args ...string) (string, error) {
		if strings.HasSuffix(strings.Join(args, " "), "--stat") {
			return "stat", nil
		}
		return large, nil
	})
	if err != nil || !strings.Contains(got, "diff too large") || strings.Contains(got, large) {
		t.Fatalf("large DiffReportWith = %q, %v", got, err)
	}
}
