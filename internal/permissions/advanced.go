package permissions

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

// Pre-compiled safe/unsafe patterns for performance.
var (
	safeGitRe    = regexp.MustCompile(`^git\s+(status|log|diff|show|branch)\b`)
	safeLsRe     = regexp.MustCompile(`^ls(\s+|$)`)
	safeCatRe    = regexp.MustCompile(`^cat\s+`)
	safeEchoRe   = regexp.MustCompile(`^echo(\s+|$)`)
	safePwdRe    = regexp.MustCompile(`^pwd(\s+|$)`)
	safeCdRe     = regexp.MustCompile(`^cd(\s+|$)`)
	safeGoRe     = regexp.MustCompile(`^go\s+(version|env|mod)\b`)
	safeNodeRe   = regexp.MustCompile(`^node\s+--version`)
	safePythonRe = regexp.MustCompile(`^python3?\s+--version`)

	unsafeRmRe   = regexp.MustCompile(`rm\s+-rf\s+/`)
	unsafeCurlRe = regexp.MustCompile(`curl\s+.*\|\s*(sh|bash)`)
	unsafeWgetRe = regexp.MustCompile(`wget\s+.*\|\s*(sh|bash)`)
	unsafeEvalRe = regexp.MustCompile(`eval\s+`)
	unsafeSudoRe = regexp.MustCompile(`sudo\s+`)

	// Read-only git subcommands that are safe to auto-approve.
	safeGitSubcommands = map[string]bool{
		"status": true,
		"log":    true,
		"diff":   true,
		"show":   true,
		"branch": true,
	}
)

// BypassKillswitch controls the scoped, time-bounded break-glass grant. It is
// intentionally not directly enableable without a justification; callers
// should go through PermissionService, which is the single policy authority.
type BypassKillswitch struct {
	enabled bool
	// grant is the structured bypass (scope + expiry + reason). When nil the
	// bypass behaves as legacy (all categories, session-long).
	grant *BypassGrant
	mu    sync.RWMutex
}

// BypassGrant is a structured bypass with scope, expiry, and justification.
// Scope is a list of tool categories ("bash", "network", "filesystem"); empty
// means all categories. ExpiresAt is zero for session-long. Reason is a
// required justification surfaced in audit logs.
type BypassGrant struct {
	Enabled   bool      `json:"enabled"`
	Scope     []string  `json:"scope,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	Reason    string    `json:"reason,omitempty"`
}

// NewBypassKillswitch creates a new bypass killswitch.
func NewBypassKillswitch() *BypassKillswitch {
	return &BypassKillswitch{}
}

// Disable disables the bypass killswitch.
func (b *BypassKillswitch) Disable() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.enabled = false
	b.grant = nil
}

// IsEnabled reports whether a grant is currently active. Expiry is enforced
// by PermissionService.BypassState and the permission evaluator.
func (b *BypassKillswitch) IsEnabled() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.enabled
}

// EnableScoped enables the bypass for the given scope with an optional expiry.
// A non-empty reason is required for audit. If expiresAt is zero, the bypass
// lasts for the session. Passing an empty scope enables all categories.
func (b *BypassKillswitch) EnableScoped(scope []string, expiresAt time.Time, reason string) bool {
	reason = strings.TrimSpace(reason)
	if reason == "" || (!expiresAt.IsZero() && !expiresAt.After(time.Now())) {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.enabled = true
	b.grant = &BypassGrant{
		Enabled:   true,
		Scope:     normalizeBypassScope(scope),
		ExpiresAt: expiresAt,
		Reason:    reason,
	}
	return true
}

// Grant returns a copy of the current bypass grant (nil if unset).
func (b *BypassKillswitch) Grant() *BypassGrant {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.grant == nil {
		return nil
	}
	g := *b.grant
	if len(b.grant.Scope) > 0 {
		g.Scope = append([]string(nil), b.grant.Scope...)
	}
	return &g
}

// IsExpired reports whether a time-bound bypass has expired. A session-long
// bypass (zero ExpiresAt) never expires.
func (g *BypassGrant) IsExpired(now time.Time) bool {
	return !g.ExpiresAt.IsZero() && !now.Before(g.ExpiresAt)
}

// Covers reports whether the bypass covers a tool category. Empty scope means
// all categories.
func (g *BypassGrant) Covers(category string) bool {
	if len(g.Scope) == 0 {
		return true
	}
	category = normalizeBypassCategory(category)
	for _, s := range g.Scope {
		if normalizeBypassCategory(s) == category {
			return true
		}
	}
	return false
}

func normalizeBypassCategory(category string) string {
	return strings.ToLower(strings.TrimSpace(category))
}

func normalizeBypassScope(scope []string) []string {
	if len(scope) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(scope))
	out := make([]string, 0, len(scope))
	for _, category := range scope {
		category = normalizeBypassCategory(category)
		if category == "" {
			continue
		}
		if _, ok := seen[category]; ok {
			continue
		}
		seen[category] = struct{}{}
		out = append(out, category)
	}
	return out
}

// toolCategory maps a tool name to a bypass scope category. Local to the
// permissions package (does not import safety to avoid a cycle).
// ToolCategory maps a tool name to a bypass scope category. Exported so the
// permission engine can scope bypass grants without importing safety.
func ToolCategory(toolName string) string {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "bash", "powershell", "power_shell":
		return "bash"
	case "webfetch", "web_search", "websearch", "browser", "screenshot", "download", "dependencyaudit", "dependency_audit", "dependency-audit", "deps", "github", "gh":
		return "network"
	case "write", "file_write", "edit", "file_edit", "structurededit", "multiedit", "fileedit", "notebookedit", "notebook_edit", "delete":
		return "filesystem"
	default:
		return "other"
	}
}

// Classifier classifies commands as safe or dangerous.
type Classifier struct {
	safePatterns   []*regexp.Regexp
	unsafePatterns []*regexp.Regexp
}

// NewClassifier creates a new permission classifier.
func NewClassifier() *Classifier {
	return &Classifier{
		safePatterns: []*regexp.Regexp{
			safeGitRe,
			safeLsRe,
			safeCatRe,
			safeEchoRe,
			safePwdRe,
			safeCdRe,
			safeGoRe,
			safeNodeRe,
			safePythonRe,
		},
		unsafePatterns: []*regexp.Regexp{
			unsafeRmRe,
			unsafeCurlRe,
			unsafeWgetRe,
			unsafeEvalRe,
			unsafeSudoRe,
		},
	}
}

// Classify classifies a command as safe, unsafe, or unknown.
// Compound commands (cd && git -C <path> status) are safe only when every
// segment is independently safe. This keeps the fast path useful for common
// agent commands without treating a mixed command as automatically trusted.
func (c *Classifier) Classify(command string) string {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return "unknown"
	}
	for _, re := range c.unsafePatterns {
		if re.MatchString(cmd) {
			return "unsafe"
		}
	}
	segments := splitSafeCommandSegments(cmd)
	if len(segments) == 0 {
		return "unknown"
	}
	for _, seg := range segments {
		if !c.isSafeSegment(seg) {
			return "unknown"
		}
	}
	return "safe"
}

func (c *Classifier) isSafeSegment(segment string) bool {
	seg := strings.TrimSpace(unwrapShell(segment))
	if seg == "" {
		return true
	}
	if isSafeGitCommand(seg) {
		return true
	}
	for _, re := range c.safePatterns {
		if re.MatchString(seg) {
			return true
		}
	}
	return false
}

// splitSafeCommandSegments splits on && / ; while ignoring quoted separators.
func splitSafeCommandSegments(command string) []string {
	var (
		parts   []string
		current strings.Builder
		quote   rune
		escaped bool
	)
	flush := func() {
		part := strings.TrimSpace(current.String())
		current.Reset()
		if part != "" {
			parts = append(parts, part)
		}
	}
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			current.WriteRune(r)
			escaped = true
			continue
		}
		if quote != 0 {
			current.WriteRune(r)
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			current.WriteRune(r)
			continue
		}
		if r == ';' {
			flush()
			continue
		}
		if r == '&' && i+1 < len(runes) && runes[i+1] == '&' {
			flush()
			i++
			continue
		}
		current.WriteRune(r)
	}
	flush()
	return parts
}

// isSafeGitCommand reports whether cmd is a read-only git invocation, including
// forms like `git -C /abs/path status` and `/usr/bin/git status --porcelain`.
func isSafeGitCommand(cmd string) bool {
	tokens := tokenize(strings.TrimSpace(cmd))
	start := 0
	for start < len(tokens) && envPrefixRe.MatchString(tokens[start]) {
		start++
	}
	if start >= len(tokens) {
		return false
	}
	if normalizePath(tokens[start]) == "env" {
		start++
		for start < len(tokens) && envPrefixRe.MatchString(tokens[start]) {
			start++
		}
		if start >= len(tokens) {
			return false
		}
	}
	if normalizePath(stripQuotes(tokens[start])) != "git" {
		return false
	}
	i := start + 1
	for i < len(tokens) {
		tok := stripQuotes(tokens[i])
		switch {
		case tok == "-C" || tok == "-c":
			if i+1 >= len(tokens) {
				return false
			}
			i += 2
		case tok == "--git-dir" || tok == "--work-tree":
			if i+1 >= len(tokens) {
				return false
			}
			i += 2
		case strings.HasPrefix(tok, "--git-dir=") || strings.HasPrefix(tok, "--work-tree="):
			i++
		case strings.HasPrefix(tok, "-"):
			// Other global flags (e.g. --no-pager) take no path argument.
			i++
		default:
			return safeGitSubcommands[tok]
		}
	}
	return false
}
