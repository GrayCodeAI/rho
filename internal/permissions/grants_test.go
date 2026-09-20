package permissions

import (
	"testing"
	"time"
)

func TestUnifiedGrants_DenyBeatsAllow(t *testing.T) {
	now := time.Now()
	store := FuncGrantStore{Fn: func() []Grant {
		return []Grant{
			{Tool: "Bash", Pattern: "*", Allow: true, Source: SourceUserAllow},
			{Tool: "Bash", Pattern: "rm -rf *", Allow: false, Source: SourceUserDeny},
		}
	}}
	u := NewUnifiedGrants(store)

	// Broad allow matches, but deny is more specific and deny > allow.
	allowed, found := u.Check("Bash", "rm -rf /tmp", now)
	if !found {
		t.Fatal("expected a grant to match")
	}
	if allowed {
		t.Fatal("deny should beat allow for rm -rf")
	}

	// A safe command should still be allowed by the broad allow.
	allowed, found = u.Check("Bash", "git status", now)
	if !found {
		t.Fatal("expected match for git status")
	}
	if !allowed {
		t.Fatal("git status should be allowed")
	}
}

func TestUnifiedGrants_SourcePriority(t *testing.T) {
	now := time.Now()
	store := FuncGrantStore{Fn: func() []Grant {
		return []Grant{
			// Auto-learned allow (low priority).
			{Tool: "Bash", Pattern: "deploy *", Allow: true, Source: SourceUserAllow},
			// User deny (higher priority).
			{Tool: "Bash", Pattern: "deploy *", Allow: false, Source: SourceUserDeny},
		}
	}}
	u := NewUnifiedGrants(store)

	allowed, found := u.Check("Bash", "deploy prod", now)
	if !found || allowed {
		t.Fatal("user deny should beat auto-learned allow")
	}
}

func TestUnifiedGrants_ExpiredIgnored(t *testing.T) {
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)
	store := FuncGrantStore{Fn: func() []Grant {
		return []Grant{
			{Tool: "Bash", Pattern: "*", Allow: false, Source: SourceUserDeny, Expires: &yesterday},
		}
	}}
	u := NewUnifiedGrants(store)

	_, found := u.Check("Bash", "anything", now)
	if found {
		t.Fatal("expired grant should not match")
	}
}

func TestUnifiedGrants_ToolWildcard(t *testing.T) {
	now := time.Now()
	store := FuncGrantStore{Fn: func() []Grant {
		return []Grant{
			{Tool: "*", Pattern: "*.env", Allow: false, Source: SourceUserDeny},
		}
	}}
	u := NewUnifiedGrants(store)

	allowed, found := u.Check("Write", ".env", now)
	if !found || allowed {
		t.Fatal("wildcard tool deny should block Write(*.env)")
	}
}

func TestUnifiedGrants_MultiStore(t *testing.T) {
	now := time.Now()
	s1 := FuncGrantStore{Fn: func() []Grant {
		return []Grant{{Tool: "Read", Pattern: "*", Allow: true, Source: SourceUserAllow}}
	}}
	s2 := FuncGrantStore{Fn: func() []Grant {
		return []Grant{{Tool: "Bash", Pattern: "go test*", Allow: true, Source: SourceUserAllow}}
	}}
	u := NewUnifiedGrants(s1, s2)

	if allowed, found := u.Check("Read", "anything", now); !found || !allowed {
		t.Fatal("Read should be allowed from store 1")
	}
	if allowed, found := u.Check("Bash", "go test ./...", now); !found || !allowed {
		t.Fatal("Bash go test should be allowed from store 2")
	}
	// Write has no grant.
	if _, found := u.Check("Write", "foo.go", now); found {
		t.Fatal("Write should have no matching grant")
	}
}

func TestUnifiedGrants_AllDedup(t *testing.T) {
	now := time.Now()
	store := FuncGrantStore{Fn: func() []Grant {
		return []Grant{
			{Tool: "Bash", Pattern: "git*", Allow: true, Source: SourceUserAllow},
			{Tool: "Bash", Pattern: "git*", Allow: true, Source: SourceUserAllow}, // dup
		}
	}}
	u := NewUnifiedGrants(store)
	all := u.All(now)
	count := 0
	for _, g := range all {
		if g.Tool == "Bash" && g.Pattern == "git*" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected dedup to 1, got %d", count)
	}
}
