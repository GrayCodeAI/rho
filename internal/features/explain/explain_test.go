package explain

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeRunner struct {
	outputs map[string][]byte
	errors  map[string]error
}

func (f fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	if err := f.errors[key]; err != nil {
		return nil, err
	}
	return f.outputs[key], nil
}

func TestExplainUncommittedLine(t *testing.T) {
	runner := fakeRunner{outputs: map[string][]byte{
		"git blame -L 4,4 --porcelain file.go": []byte("0000000000000000000000000000000000000000 1 1 1\n"),
	}}
	got, err := ExplainWithRunner(context.Background(), runner, "file.go", 4)
	if err != nil || !strings.Contains(got, "uncommitted") {
		t.Fatalf("Explain = %q, %v", got, err)
	}
}

func TestExplainIncludesCommitAndDiff(t *testing.T) {
	runner := fakeRunner{outputs: map[string][]byte{
		"git blame -L 4,4 --porcelain file.go":                                          []byte("0123456789abcdef0123456789abcdef01234567 1 1 1\n"),
		"git log -1 --format=%h %s (%an, %ar) 0123456789abcdef0123456789abcdef01234567": []byte("abc123 add feature (author, 1 day ago)\n"),
		"git log -1 --format= -p -- file.go 0123456789abcdef0123456789abcdef01234567":   []byte("@@ -1 +1 @@\n-old\n+new\n"),
	}}
	got, err := ExplainWithRunner(context.Background(), runner, "file.go", 4)
	if err != nil || !strings.Contains(got, "abc123") || !strings.Contains(got, "+new") {
		t.Fatalf("Explain = %q, %v", got, err)
	}
}

func TestExplainFallsBackWhenCommitDetailsFail(t *testing.T) {
	runner := fakeRunner{
		outputs: map[string][]byte{
			"git blame -L 4,4 --porcelain file.go": []byte("0123456789abcdef0123456789abcdef01234567 1 1 1\n"),
		},
		errors: map[string]error{
			"git log -1 --format=%h %s (%an, %ar) 0123456789abcdef0123456789abcdef01234567": errors.New("missing commit"),
		},
	}
	got, err := ExplainWithRunner(context.Background(), runner, "file.go", 4)
	if err != nil || got != "Commit: 0123456 (details unavailable)" {
		t.Fatalf("Explain fallback = %q, %v", got, err)
	}
}
