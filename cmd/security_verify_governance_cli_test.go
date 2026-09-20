package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GrayCodeAI/rho/internal/securitylog"
)

// withTempState runs a cli test body with RHO_STATE_DIR pointed at a temp dir
// so the real user state is never touched.
func withTempState(t *testing.T, body func(stateDir string)) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("RHO_STATE_DIR", dir)
	body(dir)
}

func runRoot(args ...string) (string, error) {
	// Reset persistent CLI flag vars so test invocations don't bleed values
	// across cases (cobra keeps the last value when a flag is omitted).
	governancePath = ""
	securitylogLimit = 0
	securitylogJSON = false
	learnLimit = 20
	learnAll = false
	learnWhat, learnWhy, learnLesson = "", "", ""
	learnCategory = "manual"
	learnClearYes = false

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return buf.String(), err
}

func TestGovernanceValidate(t *testing.T) {
	dir := t.TempDir()
	policy := filepath.Join(dir, "policy.json")
	doc := `{
		"version": 1,
		"fail_closed": true,
		"capabilities": [
			{"scope": "bash", "action": "deny", "pattern": "rm -rf *", "reason": "protect filesystem"}
		],
		"denied_tools": ["Bash"]
	}`
	if err := os.WriteFile(policy, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runRoot("governance", "validate", policy)
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "valid") {
		t.Fatalf("expected validation success, got: %s", out)
	}
}

func TestGovernanceExplainDenied(t *testing.T) {
	dir := t.TempDir()
	policy := filepath.Join(dir, "policy.json")
	doc := `{
		"version": 1,
		"fail_closed": false,
		"capabilities": [
			{"scope": "filesystem_write", "action": "deny", "pattern": "*.env", "reason": "protect secrets"}
		]
	}`
	if err := os.WriteFile(policy, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runRoot("governance", "explain", "Write", "config.local.env", "--path", policy)
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "DENY") {
		t.Fatalf("expected DENY for sensitive .env write, got: %s", out)
	}
}

func TestGovernanceExplainAllow(t *testing.T) {
	dir := t.TempDir()
	policy := filepath.Join(dir, "policy.json")
	doc := `{
		"version": 1,
		"fail_closed": false,
		"capabilities": [
			{"scope": "filesystem_read", "action": "allow", "pattern": "*.go"}
		]
	}`
	if err := os.WriteFile(policy, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runRoot("governance", "explain", "Read", "main.go", "--path", policy)
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ALLOW") {
		t.Fatalf("expected ALLOW for reading a .go file, got: %s", out)
	}
}

func TestGovernanceExplainNoPolicy(t *testing.T) {
	withTempState(t, func(string) {
		// No policy installed and no --path: should error helpfully.
		_, err := runRoot("governance", "explain", "Read")
		if err == nil {
			t.Fatal("expected error when no policy is available")
		}
		if !strings.Contains(err.Error(), "no governance policy") {
			t.Fatalf("expected 'no governance policy' error, got: %v", err)
		}
	})
}

func TestSecuritylogShowsEmpty(t *testing.T) {
	withTempState(t, func(stateDir string) {
		out, err := runRoot("securitylog")
		if err != nil {
			t.Fatalf("unexpected error: %v\n%s", err, out)
		}
		if !strings.Contains(out, "No security events recorded yet") {
			t.Fatalf("expected empty-log message, got: %s", out)
		}
		if !strings.Contains(out, stateDir) {
			t.Fatalf("expected log location %q in output, got: %s", stateDir, out)
		}

		// Empty JSON should be an array, not null.
		jsonOut, err := runRoot("securitylog", "show", "--json")
		if err != nil {
			t.Fatalf("unexpected error: %v\n%s", err, jsonOut)
		}
		if strings.TrimSpace(jsonOut) != "[]" {
			t.Fatalf("expected empty JSON array, got: %q", jsonOut)
		}
	})
}

func TestSecuritylogAppendVerifyAndShow(t *testing.T) {
	withTempState(t, func(stateDir string) {
		// Append two events via the public API.
		l, err := securitylog.New(securitylog.DefaultDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := l.Append(securitylog.SeverityInfo, "tool_exec", "wrote file", "Write", "sess-1"); err != nil {
			t.Fatal(err)
		}
		if _, err := l.Append(securitylog.SeverityWarning, "denied", "blocked", "Bash", "sess-1"); err != nil {
			t.Fatal(err)
		}
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}

		out, err := runRoot("securitylog", "verify")
		if err != nil {
			t.Fatalf("verify should pass on a valid chain: %v\n%s", err, out)
		}
		if !strings.Contains(out, "2 entries verified") {
			t.Fatalf("expected verification summary, got: %s", out)
		}

		show, err := runRoot("securitylog", "show")
		if err != nil {
			t.Fatalf("unexpected error: %v\n%s", err, show)
		}
		if !strings.Contains(show, "tool_exec") || !strings.Contains(show, "denied") {
			t.Fatalf("expected both events in show output, got: %s", show)
		}
	})
}

func TestSecuritylogVerifyDetectsTampering(t *testing.T) {
	withTempState(t, func(stateDir string) {
		l, err := securitylog.New(securitylog.DefaultDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := l.Append(securitylog.SeverityInfo, "tool_exec", "original", "Write", ""); err != nil {
			t.Fatal(err)
		}
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}

		// Tamper with the log file.
		path := filepath.Join(stateDir, "securitylog", "security_events.jsonl")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		tampered := strings.Replace(string(data), "original", "TAMPERED", 1)
		if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
			t.Fatal(err)
		}

		_, err = runRoot("securitylog", "verify")
		if err == nil {
			t.Fatal("expected verification to fail after tampering")
		}
	})
}

func TestSecuritylogShowJSON(t *testing.T) {
	withTempState(t, func(stateDir string) {
		l, err := securitylog.New(securitylog.DefaultDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := l.Append(securitylog.SeverityInfo, "tool_exec", "wrote file", "Write", "sess-1"); err != nil {
			t.Fatal(err)
		}
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}

		out, err := runRoot("securitylog", "show", "--json")
		if err != nil {
			t.Fatalf("unexpected error: %v\n%s", err, out)
		}
		var events []securitylog.Event
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &events); err != nil {
			t.Fatalf("expected valid JSON, got parse error: %v\n%s", err, out)
		}
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}
		if events[0].Type != "tool_exec" {
			t.Fatalf("unexpected event type: %s", events[0].Type)
		}
	})
}

func TestVerifyCommand(t *testing.T) {
	t.Run("passes on empty", func(t *testing.T) {
		withTempState(t, func(stateDir string) {
			out, err := runRoot("verify")
			if err != nil {
				t.Fatalf("unexpected error: %v\n%s", err, out)
			}
			if !strings.Contains(out, "verification passed") {
				t.Fatalf("expected 'verification passed', got: %s", out)
			}
		})
	})

	t.Run("passes when log intact", func(t *testing.T) {
		withTempState(t, func(stateDir string) {
			l, err := securitylog.New(securitylog.DefaultDir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := l.Append(securitylog.SeverityInfo, "tool_exec", "ok", "Write", "s1"); err != nil {
				t.Fatal(err)
			}
			if err := l.Close(); err != nil {
				t.Fatal(err)
			}
			out, err := runRoot("verify")
			if err != nil {
				t.Fatalf("unexpected error: %v\n%s", err, out)
			}
			if !strings.Contains(out, "1 entries verified") {
				t.Fatalf("expected verified count, got: %s", out)
			}
		})
	})
}

func TestLearnListEmpty(t *testing.T) {
	withTempState(t, func(stateDir string) {
		out, err := runRoot("learn")
		if err != nil {
			t.Fatalf("unexpected error: %v\n%s", err, out)
		}
		if !strings.Contains(out, "No lessons yet") {
			t.Fatalf("expected empty-lessons message, got: %s", out)
		}
	})
}

func TestLearnAddListClear(t *testing.T) {
	withTempState(t, func(stateDir string) {
		out, err := runRoot("learn", "add",
			"--what", "write failed",
			"--why", "wrong encoding",
			"--lesson", "verify encoding",
			"--category", "code")
		if err != nil {
			t.Fatalf("unexpected error: %v\n%s", err, out)
		}
		if !strings.Contains(out, "lesson added") {
			t.Fatalf("expected 'lesson added', got: %s", out)
		}

		list, err := runRoot("learn")
		if err != nil {
			t.Fatalf("unexpected error: %v\n%s", err, list)
		}
		if !strings.Contains(list, "write failed") || !strings.Contains(list, "code (1)") {
			t.Fatalf("expected lesson in list output, got: %s", list)
		}

		// Duplicate add is deduped: store stays at 1.
		if _, err := runRoot("learn", "add",
			"--what", "write failed",
			"--why", "wrong encoding",
			"--lesson", "verify encoding",
			"--category", "code"); err != nil {
			t.Fatalf("duplicate add errored: %v", err)
		}
		list2, err := runRoot("learn")
		if err != nil {
			t.Fatalf("unexpected error: %v\n%s", err, list2)
		}
		if !strings.Contains(list2, "Lesson store: 1 lesson") {
			t.Fatalf("expected dedup to keep 1 lesson, got: %s", list2)
		}

		clear, err := runRoot("learn", "clear", "--yes")
		if err != nil {
			t.Fatalf("unexpected error: %v\n%s", err, clear)
		}
		if !strings.Contains(clear, "cleared 1 lesson") {
			t.Fatalf("expected clear summary, got: %s", clear)
		}
	})
}

func TestLearnPrompt(t *testing.T) {
	out, err := runRoot("learn", "prompt", "the parser crashed on line 42")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "WHAT_FAILED") || !strings.Contains(out, "WHY_FAILED") || !strings.Contains(out, "WHAT_TO_DO") {
		t.Fatalf("expected labeled fields in prompt, got: %s", out)
	}
	if !strings.Contains(out, "the parser crashed on line 42") {
		t.Fatalf("expected context echoed in prompt, got: %s", out)
	}
}
