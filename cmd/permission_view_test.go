package cmd

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

func TestRenderPermissionBoxKeepsApprovalActionsReadable(t *testing.T) {
	got := renderPermissionBox("Bash [HIGH risk]\nrun git status\nWhy: execute a command", 80, time.Now().Add(time.Minute))
	for _, want := range []string{
		"[y] allow once     [n] deny once",
		"[s] allow this action for session",
		"[d] deny this tool for session",
		"[p] allow exact for project    [x] deny exact for project",
		"Esc denies",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("permission card missing %q: %q", want, got)
		}
	}
	if strings.Count(got, "allow once") != 1 {
		t.Fatalf("permission action should be rendered once: %q", got)
	}
}

func TestRenderPermissionBoxFitsNarrowTerminals(t *testing.T) {
	for _, width := range []int{24, 32, 40} {
		got := renderPermissionBox("Bash [HIGH risk]\nrun git status\nWhy: execute a command", width, time.Now().Add(time.Minute))
		for _, line := range strings.Split(got, "\n") {
			if actual := lipgloss.Width(line); actual > width {
				t.Fatalf("permission card overflows width %d: line width=%d line=%q", width, actual, line)
			}
		}
	}
}

func TestRenderApprovalBoxKeepsKeyboardActionsReadable(t *testing.T) {
	got := renderApprovalBox("network: curl https://example.com", 80, time.Now().Add(time.Minute))
	for _, want := range []string{
		"[y] approve once     [n] deny once",
		"[s] approve category for session",
		"[5] approve next 5 actions",
		"Esc denies",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("approval card missing %q: %q", want, got)
		}
	}
}

func TestRenderApprovalBoxFitsNarrowTerminals(t *testing.T) {
	for _, width := range []int{24, 32, 40} {
		got := renderApprovalBox("network: curl https://example.com", width, time.Now().Add(time.Minute))
		for _, line := range strings.Split(got, "\n") {
			if actual := lipgloss.Width(line); actual > width {
				t.Fatalf("approval card overflows width %d: line width=%d line=%q", width, actual, line)
			}
		}
	}
}

func TestRenderCredentialBoxShowsKeyboardActions(t *testing.T) {
	got := renderCredentialBox("AI wants to access GitHub (token): publish the requested change", 80, time.Now().Add(time.Minute))
	for _, want := range []string{
		"Credential access required",
		"[y] allow access     [n] deny access",
		"Credentials are never shown in the transcript",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("credential card missing %q: %q", want, got)
		}
	}
}

func TestRenderCredentialBoxFitsNarrowTerminals(t *testing.T) {
	for _, width := range []int{24, 32, 40} {
		got := renderCredentialBox("AI wants to access GitHub (token): publish the requested change", width, time.Now().Add(time.Minute))
		for _, line := range strings.Split(got, "\n") {
			if actual := lipgloss.Width(line); actual > width {
				t.Fatalf("credential card overflows width %d: line width=%d line=%q", width, actual, line)
			}
		}
	}
}
