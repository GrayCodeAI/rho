package parallel

import (
	"errors"
	"testing"
)

func TestParseRequest(t *testing.T) {
	got, err := ParseRequest([]string{"parallel", "3", "Fix auth", "|", "Add tests"})
	if err != nil || got.Workers != 3 || len(got.Tasks) != 2 || got.Tasks[0] != "Fix auth" || got.Tasks[1] != "Add tests" {
		t.Fatalf("ParseRequest = %#v, %v", got, err)
	}
}

func TestParseRequestErrorsPreservePresentationKind(t *testing.T) {
	_, err := ParseRequest([]string{"parallel"})
	var parseErr *ParseError
	if !errors.As(err, &parseErr) || !parseErr.Usage {
		t.Fatalf("usage error = %#v, %v", parseErr, err)
	}
	_, err = ParseRequest([]string{"parallel", "9", "a", "|", "b"})
	if !errors.As(err, &parseErr) || parseErr.Usage {
		t.Fatalf("worker error = %#v, %v", parseErr, err)
	}
}

func TestParseRequestRejectsEmptyTask(t *testing.T) {
	if _, err := ParseRequest([]string{"parallel", "2", "first", "|", "   "}); err == nil {
		t.Fatal("expected empty task error")
	}
}
