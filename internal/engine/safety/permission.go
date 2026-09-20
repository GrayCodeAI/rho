package safety

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	contracts "github.com/GrayCodeAI/rho/internal/contracts/policy"
	"github.com/GrayCodeAI/rho/internal/permissions"
	"github.com/GrayCodeAI/rho/internal/tool"
)

var permissionANSIRe = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))`)

// PermissionRequest is sent from engine to TUI when a tool needs approval.
type PermissionRequest struct {
	contracts.PermissionRequest
	Response chan bool
}

// PermissionMemory stores always-allow and always-deny rules.
type PermissionMemory struct {
	mu         sync.RWMutex
	allowRules []string // patterns like "bash:go test*", "file_write:*.go"
	denyRules  []string
	allowAll   map[string]bool // tool names that are always allowed
	allowExact map[string]bool // literal tool+identity grants
	denyExact  map[string]bool // literal tool+identity denials
}

// RuleSnapshot is an immutable copy of remembered permission rules.
type RuleSnapshot struct {
	AllowRules []string
	DenyRules  []string
	AllowAll   map[string]bool
	AllowExact map[string]bool
	DenyExact  map[string]bool
}

func NewPermissionMemory() *PermissionMemory {
	return &PermissionMemory{allowAll: make(map[string]bool), allowExact: make(map[string]bool), denyExact: make(map[string]bool)}
}

// Snapshot returns a deep copy that can safely be used by one evaluation.
func (pm *PermissionMemory) Snapshot() RuleSnapshot {
	if pm == nil {
		return RuleSnapshot{AllowAll: map[string]bool{}}
	}
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	allowAll := make(map[string]bool, len(pm.allowAll))
	for name, allowed := range pm.allowAll {
		allowAll[name] = allowed
	}
	allowExact := make(map[string]bool, len(pm.allowExact))
	for key, allowed := range pm.allowExact {
		allowExact[key] = allowed
	}
	denyExact := make(map[string]bool, len(pm.denyExact))
	for key, denied := range pm.denyExact {
		denyExact[key] = denied
	}
	return RuleSnapshot{AllowRules: append([]string(nil), pm.allowRules...), DenyRules: append([]string(nil), pm.denyRules...), AllowAll: allowAll, AllowExact: allowExact, DenyExact: denyExact}
}

// NewPermissionMemoryFromSnapshot creates an independent rule store.
func NewPermissionMemoryFromSnapshot(snapshot RuleSnapshot) *PermissionMemory {
	allowAll := make(map[string]bool, len(snapshot.AllowAll))
	for name, allowed := range snapshot.AllowAll {
		allowAll[name] = allowed
	}
	allowExact := make(map[string]bool, len(snapshot.AllowExact))
	for key, allowed := range snapshot.AllowExact {
		allowExact[key] = allowed
	}
	denyExact := make(map[string]bool, len(snapshot.DenyExact))
	for key, denied := range snapshot.DenyExact {
		denyExact[key] = denied
	}
	return &PermissionMemory{allowRules: append([]string(nil), snapshot.AllowRules...), denyRules: append([]string(nil), snapshot.DenyRules...), allowAll: allowAll, allowExact: allowExact, denyExact: denyExact}
}

// ensureMapsLocked keeps PermissionMemory's exported zero value usable. The
// constructor initializes these maps for the common path, but replacement
// engines and integrations may intentionally use var pm PermissionMemory.
// Callers must hold pm.mu while invoking this helper.
func (pm *PermissionMemory) ensureMapsLocked() {
	if pm.allowAll == nil {
		pm.allowAll = make(map[string]bool)
	}
	if pm.allowExact == nil {
		pm.allowExact = make(map[string]bool)
	}
	if pm.denyExact == nil {
		pm.denyExact = make(map[string]bool)
	}
}

// Grants returns the remembered allow/deny rules as canonical permissions.Grant
// slice. allowAll entries become tool-wide allow grants; allowRules/denyRules
// become tool:pattern grants. Source is set so UnifiedGrants can rank user
// rules above auto-learned ones.
func (pm *PermissionMemory) Grants() []permissions.Grant {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var out []permissions.Grant
	for tool := range pm.allowAll {
		out = append(out, permissions.Grant{
			Tool:    tool,
			Pattern: "*",
			Allow:   true,
			Source:  permissions.SourceUserAllow,
			Label:   "from settings",
		})
	}
	for key := range pm.allowExact {
		tool, identity := splitExactKey(key)
		out = append(out, permissions.Grant{Tool: tool, Pattern: identity, Exact: true, Allow: true, Source: permissions.SourceUserAllow, Label: "from settings"})
	}
	for key := range pm.denyExact {
		tool, identity := splitExactKey(key)
		out = append(out, permissions.Grant{Tool: tool, Pattern: identity, Exact: true, Allow: false, Source: permissions.SourceUserDeny, Label: "from settings"})
	}
	for _, rule := range pm.allowRules {
		tool, pattern := parseRuleSpec(rule)
		out = append(out, permissions.Grant{
			Tool:    tool,
			Pattern: pattern,
			Allow:   true,
			Source:  permissions.SourceUserAllow,
			Label:   "from settings",
		})
	}
	for _, rule := range pm.denyRules {
		tool, pattern := parseRuleSpec(rule)
		out = append(out, permissions.Grant{
			Tool:    tool,
			Pattern: pattern,
			Allow:   false,
			Source:  permissions.SourceUserDeny,
			Label:   "from settings",
		})
	}
	// Map-backed exact and tool-wide rules must be deterministic. The permission
	// center renders this slice directly; stable ordering prevents redraws from
	// appearing to change policy and makes audit output reproducible.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Tool != out[j].Tool {
			return out[i].Tool < out[j].Tool
		}
		if out[i].Pattern != out[j].Pattern {
			return out[i].Pattern < out[j].Pattern
		}
		if out[i].Exact != out[j].Exact {
			return out[i].Exact
		}
		if out[i].Allow != out[j].Allow {
			return out[i].Allow
		}
		return out[i].Source < out[j].Source
	})
	return out
}

// Reset clears all allow/deny memory so the active rule set can be rebuilt.
func (pm *PermissionMemory) Reset() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.allowRules = nil
	pm.denyRules = nil
	pm.allowAll = make(map[string]bool)
	pm.allowExact = make(map[string]bool)
	pm.denyExact = make(map[string]bool)
}

// AlwaysAllow marks a tool as always allowed.
func (pm *PermissionMemory) AlwaysAllow(toolName string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.ensureMapsLocked()
	pm.allowAll[canonicalToolName(toolName)] = true
}

// AlwaysAllowPattern adds a pattern rule (e.g. "bash:go *").
func (pm *PermissionMemory) AlwaysAllowPattern(pattern string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.allowRules = append(pm.allowRules, normalizeRuleSpec(pattern))
}

// AlwaysAllowExact remembers one literal tool action for the session. Glob
// metacharacters in the identity are never interpreted.
func (pm *PermissionMemory) AlwaysAllowExact(toolName, identity string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.ensureMapsLocked()
	pm.allowExact[exactRuleKey(toolName, identity)] = true
}

// AlwaysDeny marks a tool as always denied.
func (pm *PermissionMemory) AlwaysDeny(toolName string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.denyRules = append(pm.denyRules, canonicalToolName(toolName)+":*")
}

// AlwaysDenyPattern adds a deny pattern rule.
func (pm *PermissionMemory) AlwaysDenyPattern(pattern string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.denyRules = append(pm.denyRules, normalizeRuleSpec(pattern))
}

// AlwaysDenyExact remembers one literal tool action for the session.
func (pm *PermissionMemory) AlwaysDenyExact(toolName, identity string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.ensureMapsLocked()
	pm.denyExact[exactRuleKey(toolName, identity)] = true
}

// AllowSpec applies an archive-style permission rule, e.g. "Bash(git:*)".
func (pm *PermissionMemory) AllowSpec(spec string) {
	toolName, pattern := parseRuleSpec(spec)
	if pattern == "" {
		pm.AlwaysAllow(toolName)
		return
	}
	pm.AlwaysAllowPattern(toolName + ":" + pattern)
}

// DenySpec applies an archive-style deny rule, e.g. "Write(*.env)".
func (pm *PermissionMemory) DenySpec(spec string) {
	toolName, pattern := parseRuleSpec(spec)
	if pattern == "" {
		pm.AlwaysDeny(toolName)
		return
	}
	pm.AlwaysDenyPattern(toolName + ":" + pattern)
}

// Check returns: true=allowed, false=denied, nil=ask user.
func (pm *PermissionMemory) Check(toolName string, summary string) *bool {
	return pm.CheckWithIdentity(toolName, summary, summary)
}

// CheckWithIdentity evaluates glob rules against the bounded display summary
// and exact rules against the complete canonical identity.
func (pm *PermissionMemory) CheckWithIdentity(toolName, summary, identity string) *bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	toolName = canonicalToolName(toolName)
	key := exactRuleKey(toolName, identity)
	if pm.denyExact[key] {
		f := false
		return &f
	}
	if pm.allowExact[key] {
		t := true
		return &t
	}

	for _, rule := range pm.denyRules {
		parts := strings.SplitN(rule, ":", 2)
		if len(parts) == 2 && parts[0] == toolName {
			if matchRulePattern(parts[1], summary) {
				f := false
				return &f
			}
		}
	}

	if pm.allowAll[toolName] {
		t := true
		return &t
	}

	for _, rule := range pm.allowRules {
		parts := strings.SplitN(rule, ":", 2)
		if len(parts) == 2 && parts[0] == toolName {
			if matchRulePattern(parts[1], summary) {
				t := true
				return &t
			}
		}
	}

	return nil // ask user
}

func exactRuleKey(toolName, identity string) string {
	return canonicalToolName(toolName) + "\x00" + identity
}

func splitExactKey(key string) (toolName, identity string) {
	parts := strings.SplitN(key, "\x00", 2)
	if len(parts) != 2 {
		return key, ""
	}
	return parts[0], parts[1]
}

type toolInvocation struct {
	name       string
	identity   string
	command    string
	hasCommand bool
	needsAsk   bool
}

// inspectToolInvocation derives the canonical identity and command risk once.
// Every caller that renders, previews, or executes a tool request must use the
// same assessment so policy and UI cannot drift.
func inspectToolInvocation(name string, args map[string]interface{}) toolInvocation {
	inv := toolInvocation{name: canonicalToolName(name), identity: name}
	switch inv.name {
	case "Bash", "PowerShell":
		if cmd, ok := args["command"].(string); ok {
			inv.command = cmd
			inv.hasCommand = true
			inv.identity = cmd
		}
	case "Write", "Edit", "NotebookEdit":
		if path, ok := pathArgument(args); ok {
			inv.identity = path
		}
	}

	// These tools either mutate directly or pass model-controlled text to a
	// live shell. Shell commands only need a prompt when their content is not
	// confidently safe; malformed input is always treated as needing approval.
	switch inv.name {
	case "Write", "Edit", "NotebookEdit", "TerminalCreate", "TerminalSend":
		inv.needsAsk = true
	case "Bash":
		inv.needsAsk = !inv.hasCommand || tool.IsSuspicious(inv.command)
	case "PowerShell":
		inv.needsAsk = !inv.hasCommand || tool.IsSuspicious(inv.command) || tool.IsPowerShellSuspicious(inv.command)
	}
	return inv
}

// toolNeedsPermission returns true for tools that modify state or need
// command-specific scrutiny before execution.
func ToolNeedsPermission(name string, args map[string]interface{}) bool {
	return inspectToolInvocation(name, args).needsAsk
}

func toolIdentity(name string, args map[string]interface{}) string {
	return inspectToolInvocation(name, args).identity
}

func toolSummary(name string, args map[string]interface{}) string {
	inv := inspectToolInvocation(name, args)
	identity := inv.identity
	canonical := inv.name
	if (canonical == "Bash" || canonical == "PowerShell") && len(identity) > 120 {
		return identity[:120] + "..."
	}
	return identity
}

// ToolIdentity returns the complete canonical action identity used by exact
// permission rules. It is never truncated.
func ToolIdentity(name string, args map[string]interface{}) string {
	return toolIdentity(name, args)
}

// ToolSummary generates a human-readable, bounded summary of what a tool call
// will do. Matching rules use this stable display form; exact rules use the
// complete ToolIdentity instead.
func ToolSummary(name string, args map[string]interface{}) string {
	return toolSummary(name, args)
}

// ActionRisk returns the risk of the concrete invocation, not merely the
// maximum capability of its tool. A shell is high-capability, but a clearly
// non-suspicious command is medium risk; malformed or suspicious shell calls
// remain high risk and therefore fail closed in presentation and inspection.
func ActionRisk(toolName string, args map[string]interface{}) RiskLevel {
	policy := ToolPolicyFor(toolName)
	canonical := canonicalToolName(toolName)
	if canonical != "Bash" && canonical != "PowerShell" {
		if policy.DefaultRisk == "" {
			return RiskMedium
		}
		return policy.DefaultRisk
	}
	inv := inspectToolInvocation(toolName, args)
	if !inv.hasCommand || strings.TrimSpace(inv.command) == "" || inv.needsAsk {
		return RiskHigh
	}
	return RiskMedium
}

// FormatPermissionDisplay builds the multi-line body shown in the TUI permission
// box. toolName and summary are display inputs; summary should remain ToolSummary
// so remembered rules still match after the user answers.
func FormatPermissionDisplay(toolName, summary string) string {
	policy := ToolPolicyFor(toolName)
	risk := string(ActionRisk(toolName, map[string]interface{}{"command": summary}))
	why := permissionWhyLine(toolName, RiskLevel(risk), policy)
	displaySummary := sanitizePermissionDisplay(summary)
	if displaySummary == "" {
		displaySummary = toolName
	}
	return fmt.Sprintf("[%s risk] %s\n%s\nEffects: %s\n%s", strings.ToUpper(risk), canonicalToolName(toolName), displaySummary, formatCapabilities(policy.Capabilities), why)
}

// sanitizePermissionDisplay protects the approval UI from terminal control
// sequences and layout spoofing in model/tool arguments. Policy matching must
// continue to use the unsanitized canonical identity; this is presentation-only.
func sanitizePermissionDisplay(summary string) string {
	clean := permissionANSIRe.ReplaceAllString(summary, "")
	var b strings.Builder
	for _, r := range clean {
		switch {
		case r == '\n' || r == '\r':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteByte(' ')
		case unicode.IsControl(r) || unicode.In(r, unicode.Cf):
			// Drop invisible/control characters, including bidi overrides.
		default:
			b.WriteRune(r)
		}
	}
	return truncatePermissionDisplay(strings.TrimSpace(b.String()), 240)
}

func truncatePermissionDisplay(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes-3]) + "..."
}

func permissionWhyLine(toolName string, risk RiskLevel, policy ToolPolicy) string {
	switch risk {
	case RiskHigh:
		if canonicalToolName(toolName) == "Bash" || canonicalToolName(toolName) == "PowerShell" {
			return "Why: shell can change your system — review the command before allowing."
		}
		return "Why: high-impact action needs your confirmation."
	case RiskMedium:
		if hasCapability(policy, CapabilityFilesystemWrite) || hasCapability(policy, CapabilityFilesystemDelete) {
			return "Why: this can modify or delete project files."
		}
		return "Why: this can change project state."
	default:
		return "Why: current autonomy settings require confirmation for this tool."
	}
}

func hasCapability(policy ToolPolicy, want Capability) bool {
	for _, c := range policy.Capabilities {
		if c == want {
			return true
		}
	}
	return false
}

func formatCapabilities(capabilities []Capability) string {
	if len(capabilities) == 0 {
		return "none declared"
	}
	values := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		values = append(values, string(capability))
	}
	return strings.Join(values, ", ")
}

func pathArgument(args map[string]interface{}) (string, bool) {
	if p, ok := args["path"].(string); ok && p != "" {
		return p, true
	}
	if p, ok := args["file_path"].(string); ok && p != "" {
		return p, true
	}
	return "", false
}

func canonicalToolName(name string) string {
	return permissions.CanonicalToolName(name)
}

func parseRuleSpec(spec string) (toolName, pattern string) {
	spec = strings.TrimSpace(spec)
	if open := strings.Index(spec, "("); open > 0 && strings.HasSuffix(spec, ")") {
		return spec[:open], spec[open+1 : len(spec)-1]
	}
	if parts := strings.SplitN(spec, ":", 2); len(parts) == 2 {
		return parts[0], parts[1]
	}
	return spec, ""
}

func normalizeRuleSpec(spec string) string {
	toolName, pattern := parseRuleSpec(spec)
	return canonicalToolName(toolName) + ":" + normalizeRulePattern(pattern)
}

func normalizeRulePattern(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	if strings.HasSuffix(pattern, ":*") {
		return strings.TrimSuffix(pattern, ":*") + " *"
	}
	return pattern
}

func matchRulePattern(pattern, summary string) bool {
	if pattern == "*" {
		return true
	}
	if matched, _ := filepath.Match(pattern, summary); matched {
		return true
	}
	if strings.HasSuffix(pattern, " *") {
		prefix := strings.TrimSuffix(pattern, " *")
		return summary == prefix || strings.HasPrefix(summary, prefix+" ")
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(summary, pattern[:len(pattern)-1])
	}
	return pattern == summary
}

// permissionAuditLog is a fixed-size ring buffer of recent permission
// decisions, surfaced via "/autonomy audit". It is safe for concurrent use.
type permissionAuditLog struct {
	mu      sync.Mutex
	entries []auditEntry
	head    int
	full    bool
}

// auditEntry records one permission decision for the audit trail.
type auditEntry struct {
	Time    time.Time
	Tool    string
	Summary string
	Outcome DecisionOutcome
	Reason  DecisionReason
}

// newPermissionAuditLog creates a ring buffer holding the most recent cap
// decisions.
func newPermissionAuditLog(cap int) *permissionAuditLog {
	if cap <= 0 {
		cap = 256
	}
	return &permissionAuditLog{entries: make([]auditEntry, cap)}
}

// record appends a decision to the ring buffer.
func (l *permissionAuditLog) record(tool, summary string, outcome DecisionOutcome, reason DecisionReason) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries[l.head] = auditEntry{
		Time:    time.Now(),
		Tool:    tool,
		Summary: summary,
		Outcome: outcome,
		Reason:  reason,
	}
	l.head++
	if l.head >= len(l.entries) {
		l.head = 0
		l.full = true
	}
}

// Recent returns the most recent n entries in chronological order (oldest
// first). If n exceeds the buffer size, the entire buffer is returned.
func (l *permissionAuditLog) Recent(n int) []auditEntry {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	size := len(l.entries)
	if !l.full {
		size = l.head
	}
	if n <= 0 || n > size {
		n = size
	}
	out := make([]auditEntry, 0, n)
	// Oldest entry index.
	start := l.head - n
	if start < 0 {
		start += len(l.entries)
	}
	for i := 0; i < n; i++ {
		idx := (start + i) % len(l.entries)
		out = append(out, l.entries[idx])
	}
	return out
}

// Format returns a human-readable audit trail for display.
func (l *permissionAuditLog) Format(n int) string {
	if l == nil {
		return "Audit log disabled."
	}
	entries := l.Recent(n)
	if len(entries) == 0 {
		return "No permission decisions recorded yet."
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Permission Audit (last %d):\n", len(entries)))
	for _, e := range entries {
		b.WriteString(fmt.Sprintf("  [%s] %s %s → %s (%s)\n",
			e.Time.Format("15:04:05"), e.Tool, e.Summary, e.Outcome, e.Reason))
	}
	return b.String()
}
