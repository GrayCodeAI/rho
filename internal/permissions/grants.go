// Package permissions — unified grant store.
//
// Session grant sources persist allow/deny decisions with different scopes and
// matching semantics. The types in this file unify them behind one interface
// so the permission engine consults a single precedence-ordered view.
//
// Precedence (highest first):
//  1. Deny rules win over allow rules (deny > allow).
//  2. More specific patterns win over broader ones (Bash(rm -rf *) beats Bash(*)).
//  3. Source priority: governance > hook > user-deny > user-allow.
package permissions

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// GrantSource identifies where a grant originated. Higher value = higher priority.
type GrantSource int

const (
	SourceUserAllow  GrantSource = iota // explicit user allow rule (settings.AutoAllow etc.)
	SourceUserDeny                      // explicit user deny rule
	SourceHook                          // PreToolUse decision hook
	SourceGovernance                    // admin POLICY ceiling
)

func (s GrantSource) String() string {
	switch s {
	case SourceUserAllow:
		return "memory"
	case SourceUserDeny:
		return "memory"
	case SourceHook:
		return "hook"
	case SourceGovernance:
		return "governance"
	default:
		return "unknown"
	}
}

// Grant is one canonical allow/deny rule, independent of which backend stores it.
type Grant struct {
	// Tool is the canonical tool name (e.g. "Bash", "Write"). "*" matches all.
	Tool string
	// Pattern is the argument/path pattern (e.g. "go test*", "*.md"). "*" matches all.
	Pattern string
	// Exact makes Pattern a literal identity rather than a glob.
	Exact bool
	// Allow is true for an allow grant, false for a deny grant.
	Allow bool
	// Source is where the grant came from.
	Source GrantSource
	// Scope is "project" or "global" (empty defaults to "global").
	Scope string
	// Expires is zero for session-long grants.
	Expires *time.Time
	// Label is a human-readable provenance note (e.g. "from settings.AutoAllow", "learned 42x").
	Label string
}

// Active reports whether the grant has not expired relative to now.
func (g Grant) Active(now time.Time) bool {
	return g.Expires == nil || g.Expires.After(now)
}

// Specificity returns a rough measure of how narrow the grant is. Higher = more
// specific, so it wins when two grants conflict. "*" pattern on "*" tool scores 0;
// exact tool + exact path scores high.
func (g Grant) Specificity() int {
	spec := 0
	if g.Tool != "*" && g.Tool != "" {
		spec += 10
	}
	if g.Pattern != "*" && g.Pattern != "" {
		spec += 5
	}
	if g.Exact {
		spec += 100
	}
	// Prefix patterns (trailing space before *) are slightly less specific than
	// fully exact matches.
	if strings.HasSuffix(g.Pattern, " *") || strings.HasSuffix(g.Pattern, "*") {
		spec -= 1
	}
	return spec
}

// matchPattern returns true when the argument/path matches the grant's pattern.
// Supports filepath.Match globs plus the trailing-space prefix convention
// ("go *" matches "go test" and "go test ./...").
func grantMatchPattern(pattern, target string) bool {
	if pattern == "*" || pattern == "" {
		return true
	}
	if matched, _ := filepath.Match(pattern, target); matched {
		return true
	}
	// Prefix match: "go *" → prefix "go "
	if strings.HasSuffix(pattern, " *") {
		prefix := strings.TrimSuffix(pattern, " *")
		return target == prefix || strings.HasPrefix(target, prefix+" ")
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(target, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == target
}

// GrantStore is the interface every grant backend implements so UnifiedGrants
// can consult them uniformly.
type GrantStore interface {
	// Grants returns the store's current rules as canonical Grant slice. Expired
	// grants may be omitted; UnifiedGrants filters again defensively.
	Grants() []Grant
}

// FuncGrantStore adapts a plain func() []Grant to the GrantStore interface.
// Use it when wrapping an ad-hoc source without changing that source's API.
type FuncGrantStore struct {
	Fn func() []Grant
}

// Grants calls the wrapped function.
func (f FuncGrantStore) Grants() []Grant {
	if f.Fn == nil {
		return nil
	}
	return f.Fn()
}

// UnifiedGrants merges multiple GrantStores into one precedence-ordered view.
// It is the single source of truth the permission engine consults for remembered
// allow/deny decisions.
type UnifiedGrants struct {
	mu     sync.RWMutex
	stores []GrantStore
}

// NewUnifiedGrants wraps the given stores. Order does not matter — grants are
// re-sorted by precedence at evaluation time.
func NewUnifiedGrants(stores ...GrantStore) *UnifiedGrants {
	return &UnifiedGrants{stores: append([]GrantStore(nil), stores...)}
}

// AddStore appends another store (e.g. one that became available after setup).
func (u *UnifiedGrants) AddStore(s GrantStore) {
	if u == nil || s == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.stores = append(u.stores, s)
}

func (u *UnifiedGrants) storeSnapshot() []GrantStore {
	if u == nil {
		return nil
	}
	u.mu.RLock()
	defer u.mu.RUnlock()
	return append([]GrantStore(nil), u.stores...)
}

// collect gathers all active grants from every store, sorted by precedence:
// deny before allow, then higher source priority, then higher specificity.
func (u *UnifiedGrants) collect(now time.Time) []Grant {
	var all []Grant
	for _, s := range u.storeSnapshot() {
		if s == nil {
			continue
		}
		for _, g := range s.Grants() {
			if !g.Active(now) {
				continue
			}
			all = append(all, g)
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		// Deny grants sort before allow grants.
		if all[i].Allow != all[j].Allow {
			return !all[i].Allow
		}
		// Higher source priority first.
		if all[i].Source != all[j].Source {
			return all[i].Source > all[j].Source
		}
		// More specific first.
		return all[i].Specificity() > all[j].Specificity()
	})
	return all
}

// grantPrecedes reports whether candidate outranks current. Equal-ranked
// grants retain the existing store order, matching the stable sort used by All.
func grantPrecedes(candidate, current Grant) bool {
	if candidate.Allow != current.Allow {
		return !candidate.Allow
	}
	if candidate.Source != current.Source {
		return candidate.Source > current.Source
	}
	return candidate.Specificity() > current.Specificity()
}

// Check evaluates a tool call against the unified grant set. Returns:
//   - allowed=true, found=true  → an allow grant matched
//   - allowed=false, found=true → a deny grant matched
//   - found=false               → no grant matched; caller decides (ask user)
//
// Because grants are precedence-sorted, the first matching grant wins: a deny
// always beats a allow, and a specific user-deny beats a broad auto-learned allow.
func (u *UnifiedGrants) Check(toolName, summary string, now time.Time) (allowed bool, found bool) {
	return u.CheckWithIdentity(toolName, summary, summary, now)
}

// CheckWithIdentity matches glob grants against the display summary and exact
// grants against the complete canonical action identity.
func (u *UnifiedGrants) CheckWithIdentity(toolName, summary, identity string, now time.Time) (allowed bool, found bool) {
	toolName = CanonicalToolName(toolName)
	var winner Grant
	for _, store := range u.storeSnapshot() {
		if store == nil {
			continue
		}
		for _, g := range store.Grants() {
			if !g.Active(now) {
				continue
			}
			if g.Tool != "*" && g.Tool != toolName {
				continue
			}
			if g.Exact && g.Pattern != identity {
				continue
			}
			if !g.Exact && !grantMatchPattern(g.Pattern, summary) {
				continue
			}
			if !found || grantPrecedes(g, winner) {
				winner = g
				found = true
			}
		}
	}
	if !found {
		return false, false
	}
	return winner.Allow, true
}

// All returns every active grant (deduplicated by tool+pattern+allow), labeled
// with source. Used for the user-facing "/autonomy rules" view.
func (u *UnifiedGrants) All(now time.Time) []Grant {
	seen := make(map[string]bool)
	var out []Grant
	for _, g := range u.collect(now) {
		allow := "deny"
		if g.Allow {
			allow = "allow"
		}
		matchKind := "glob"
		if g.Exact {
			matchKind = "exact"
		}
		key := g.Tool + "|" + g.Pattern + "|" + matchKind + "|" + allow
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, g)
	}
	return out
}
