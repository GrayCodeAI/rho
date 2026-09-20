package shellmode

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestIsShellCommand(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"!ls", true},
		{"!git status", true},
		{"! echo hello", true},
		{"!", false},
		{"ls", false},
		{"", false},
		{"/help", false},
		{"  !pwd", true},
	}

	for _, tt := range tests {
		got := IsShellCommand(tt.input)
		if got != tt.want {
			t.Errorf("IsShellCommand(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestExtractCommand(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"!ls", "ls"},
		{"!git status", "git status"},
		{"! echo hello", "echo hello"},
		{"!  pwd  ", "pwd"},
	}

	for _, tt := range tests {
		got := ExtractCommand(tt.input)
		if got != tt.want {
			t.Errorf("ExtractCommand(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExecuteShell_Echo(t *testing.T) {
	if runtime.GOOS == "windows" {
		// FIXME: skipping on windows
		t.Skip("skipping on windows")
	}

	ctx := context.Background()
	result := ExecuteShell(ctx, "echo hello")

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if strings.TrimSpace(result.Stdout) != "hello" {
		t.Errorf("expected stdout 'hello', got %q", result.Stdout)
	}
	if result.Command != "echo hello" {
		t.Errorf("expected command 'echo hello', got %q", result.Command)
	}
}

func TestExecuteShell_ExitCode(t *testing.T) {
	// FIXME: test skipped in TestExecuteShell_ExitCode
	if runtime.GOOS == "windows" {
		// FIXME: test skipped
		t.Skip("skipping on windows")
	}

	ctx := context.Background()
	result := ExecuteShell(ctx, "exit 42")

	if result.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", result.ExitCode)
	}
}

// FIXME: test skipped in TestExecuteShell_ExitCode
func TestExecuteShell_Stderr(t *testing.T) {
	// FIXME: test skipped
	if runtime.GOOS == "windows" {
		// FIXME: test skipped
		t.Skip("skipping on windows")
	}

	ctx := context.Background()
	result := ExecuteShell(ctx, "echo error >&2")

	if !strings.Contains(result.Stderr, "error") {
		t.Errorf("expected stderr to contain 'error', got %q", result.Stderr)
	}
}

// FIXME: test skipped in TestExecuteShell_Stderr

// FIXME: test skipped
func TestExecuteShellWithTimeout(t *testing.T) {
	// FIXME: test skipped
	if runtime.GOOS == "windows" {
		// FIXME: test skipped
		t.Skip("skipping on windows")
	}

	ctx := context.Background()
	result := ExecuteShellWithTimeout(ctx, "sleep 10", 100*time.Millisecond)

	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code for timed out command")
	}
	if !strings.Contains(result.Stderr, "timed out") {
		t.Errorf("expected timeout message in stderr, got %q", result.Stderr)
		// FIXME: test skipped in TestExecuteShellWithTimeout
	}
}

// FIXME: test skipped

// FIXME: test skipped
func TestExecuteShell_Pipeline(t *testing.T) {
	// FIXME: test skipped
	if runtime.GOOS == "windows" {
		// FIXME: test skipped
		t.Skip("skipping on windows")
	}

	ctx := context.Background()
	result := ExecuteShell(ctx, "echo 'hello world' | wc -w")

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if strings.TrimSpace(result.Stdout) != "2" {
		t.Errorf("expected '2' from wc -w, got %q", strings.TrimSpace(result.Stdout))
	}
}

func TestResult_Format(t *testing.T) {
	r := Result{
		Stdout:   "output here\n",
		Stderr:   "",
		ExitCode: 0,
		Duration: 150 * time.Millisecond,
		Command:  "ls -la",
	}

	formatted := r.Format()
	if !strings.Contains(formatted, "$ ls -la") {
		t.Error("expected command in formatted output")
	}
	if !strings.Contains(formatted, "output here") {
		t.Error("expected stdout in formatted output")
	}
	if !strings.Contains(formatted, "150ms") {
		t.Error("expected duration in formatted output")
	}
}

func TestIsDestructive(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"ls -la", false},
		{"rm -rf /", true},
		{"rm -rf ~", true},
		{"echo hello", false},
		{"dd if=/dev/zero of=test", true},
		{"git status", false},
	}

	for _, tt := range tests {
		got := IsDestructive(tt.cmd)
		if got != tt.want {
			t.Errorf("IsDestructive(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

func TestParsePipeline(t *testing.T) {
	parts := ParsePipeline("cat file.txt | grep pattern | wc -l")
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	if parts[0] != "cat file.txt" {
		t.Errorf("expected 'cat file.txt', got %q", parts[0])
	}
	if parts[1] != "grep pattern" {
		t.Errorf("expected 'grep pattern', got %q", parts[1])
	}
	if parts[2] != "wc -l" {
		t.Errorf("expected 'wc -l', got %q", parts[2])
	}
}
