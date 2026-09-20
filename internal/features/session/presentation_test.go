package session

import (
	"strings"
	"testing"
)

func TestQuitResumeMessage(t *testing.T) {
	if got := QuitResumeMessage(""); got != "Thank you for using Rho!\n" {
		t.Fatalf("empty session message = %q", got)
	}
	if got := QuitResumeMessage("abc"); !strings.Contains(got, "rho --resume abc") {
		t.Fatalf("resume message = %q", got)
	}
}

func TestRecoveryReportEmpty(t *testing.T) {
	if got := RecoveryReport(nil); got != "No interrupted sessions found." {
		t.Fatalf("empty recovery report = %q", got)
	}
}
