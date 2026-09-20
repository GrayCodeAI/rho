package governance

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestScopesForTool(t *testing.T) {
	scopes := ScopesForTool("Bash")
	if len(scopes) != 1 || scopes[0] != "bash" {
		t.Fatalf("expected bash scope, got %v", scopes)
	}
	scopes = ScopesForTool("Write")
	found := false
	for _, s := range scopes {
		if s == "filesystem_write" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected filesystem_write scope for Write, got %v", scopes)
	}
}

func TestPolicyDenyWinsOverProfileAllow(t *testing.T) {
	policy, err := BuildProfile("policy", Document{
		Version: 1,
		Capabilities: []Capability{
			{Scope: "bash", Action: ActionDeny},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := BuildProfile("profile", Document{
		Version: 1,
		Capabilities: []Capability{
			{Scope: "bash", Action: ActionAllow},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	e := New()
	e.policy = policy
	e.SetProfile(profile)

	d := e.Evaluate("Bash", "rm -rf /")
	if d.Allowed {
		t.Fatalf("policy deny must beat profile allow: %+v", d)
	}
	if d.Source != "policy" {
		t.Fatalf("expected source=policy, got %q", d.Source)
	}
}

func TestProfileCanNarrowButNotExpand(t *testing.T) {
	policy, err := BuildProfile("policy", Document{
		Version: 1,
		Capabilities: []Capability{
			{Scope: "bash", Action: ActionAllow},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := BuildProfile("profile", Document{
		Version: 1,
		Capabilities: []Capability{
			{Scope: "bash", Action: ActionDeny, Pattern: "git push*"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	e := New()
	e.policy = policy
	e.SetProfile(profile)

	// Policy allows bash; profile denies git push specifically.
	if d := e.Evaluate("Bash", "git push origin main"); d.Allowed {
		t.Fatalf("profile deny must win: %+v", d)
	}
	// Profile silence must not expand what policy denied.
	if d := e.Evaluate("Bash", "echo hi"); !d.Allowed {
		t.Fatalf("allowed bash should pass: %+v", d)
	}
}

func TestFailClosedDeniesUngovernedTools(t *testing.T) {
	policy, err := BuildProfile("policy", Document{
		Version:    1,
		FailClosed: true,
		Capabilities: []Capability{
			{Scope: "bash", Action: ActionAllow},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	e := New()
	e.policy = policy

	if d := e.Evaluate("Bash", "echo hi"); !d.Allowed {
		t.Fatalf("bash is governed by allow: %+v", d)
	}
	// Network scope is not governed and the policy is fail-closed.
	if d := e.Evaluate("WebFetch", "example.com"); d.Allowed {
		t.Fatalf("fail-closed must deny ungoverned tool: %+v", d)
	}
}

func TestDeniedToolsFlatList(t *testing.T) {
	policy, err := BuildProfile("policy", Document{
		Version:     1,
		DeniedTools: []string{"Browser"},
	})
	if err != nil {
		t.Fatal(err)
	}
	e := New()
	e.policy = policy

	if d := e.Evaluate("Browser", ""); d.Allowed {
		t.Fatalf("denied_tools must block Browser: %+v", d)
	}
	if d := e.Evaluate("Bash", "echo hi"); !d.Allowed {
		t.Fatalf("unlisted tool should be unaffected: %+v", d)
	}
}

func TestLoadPolicyFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "security_policy.json")
	doc := `{"version":1,"fail_closed":true,"denied_tools":["Bash"]}`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	e := New()
	if err := e.LoadPolicy(path); err != nil {
		t.Fatal(err)
	}
	if d := e.Evaluate("Bash", ""); d.Allowed {
		t.Fatalf("expected Bash denied: %+v", d)
	}
}

func TestLoadPolicyMissingFilePreservesNotExistCause(t *testing.T) {
	e := New()
	err := e.LoadPolicy(filepath.Join(t.TempDir(), "missing-policy.json"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing policy error must preserve os.ErrNotExist, got %v", err)
	}
}

func TestLoadPolicyParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := New()
	if err := e.LoadPolicy(path); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestSensitivePathDenyFloor(t *testing.T) {
	policy, err := BuildProfile("policy", Document{
		Version: 1,
		Capabilities: []Capability{
			{Scope: "filesystem_write", Action: ActionAllow},
		},
		SensitivePaths: []string{".ssh/*"},
	})
	if err != nil {
		t.Fatal(err)
	}
	e := New()
	e.policy = policy

	if d := e.Evaluate("Write", "~/.ssh/config"); d.Allowed {
		t.Fatalf("sensitive path must be denied even when write is allowed: %+v", d)
	}
	if d := e.Evaluate("Write", "src/main.go"); !d.Allowed {
		t.Fatalf("normal write should pass: %+v", d)
	}
}

func TestManagedPolicyPath(t *testing.T) {
	if ManagedPolicyPath() == "" {
		t.Fatal("expected a managed policy path")
	}
}
