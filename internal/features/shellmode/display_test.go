package shellmode

import (
	"strconv"
	"strings"
	"testing"
)

func TestFormatInteractiveResultTrimsAndFormatsExit(t *testing.T) {
	got := FormatInteractiveResult(Result{Stdout: "output\n", ExitCode: 7})
	if got.Output != "output" || got.ExitError != "" {
		t.Fatalf("display = %#v", got)
	}
	got = FormatInteractiveResult(Result{ExitCode: 7})
	if got.ExitError != "exit code: 7" {
		t.Fatalf("empty display error = %#v", got)
	}
}

func TestFormatInteractiveResultBoundsLongOutput(t *testing.T) {
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "line-" + strconv.Itoa(i) + strings.Repeat("x", 100)
	}
	got := FormatInteractiveResult(Result{Stdout: strings.Join(lines, "\n")})
	if !strings.Contains(got.Output, "line-0") || !strings.Contains(got.Output, "line-49") || !strings.Contains(got.Output, "omitted") {
		t.Fatalf("bounded output = %q", got.Output)
	}
}
