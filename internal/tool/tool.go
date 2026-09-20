package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	agentcontracts "github.com/GrayCodeAI/rho/internal/contracts/agent"

	"github.com/GrayCodeAI/rho/internal/lint"
	"github.com/GrayCodeAI/rho/internal/types"
)

// AgentSpawnFn is the typed subagent entrypoint (Year 0 PACK-02).
// Implementations must honor SpawnRequest fields after Normalize.
type AgentSpawnFn func(ctx context.Context, req agentcontracts.SpawnRequest) (agentcontracts.SpawnResult, error)

// Tool is the interface every rho tool implements.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]interface{}
	Execute(ctx context.Context, input json.RawMessage) (string, error)
}

// AliasedTool can be implemented by tools that need backward-compatible wire names.
type AliasedTool interface {
	Aliases() []string
}

// ToolSchema is a typed, compile-time-safe description of a tool's input
// schema. It replaces the error-prone hand-written map[string]interface{}
// JSON-schema literals that every tool used to embed in Parameters(). A
// ToolSchema converts to the wire format via ToJSONSchema(), so the external
// FluxTool.Parameters contract (map[string]interface{}) is unchanged.
//
// Tools that don't implement SchemaProvider keep working exactly as before —
// Parameters() is still the source of truth and validation stays permissive.
type ToolSchema struct {
	// Type is the JSON-schema type, almost always "object" for tool inputs.
	Type string
	// Properties maps each input field name to its schema.
	Properties map[string]SchemaProperty
	// Required lists required field names.
	Required []string
}

// SchemaProperty describes a single tool input field.
type SchemaProperty struct {
	Type        string      `json:"type"`
	Description string      `json:"description,omitempty"`
	Default     interface{} `json:"default,omitempty"`
	// Enum, if non-nil, restricts the value to one of the listed options.
	Enum []interface{} `json:"enum,omitempty"`
	// Items describes the element type for array fields.
	Items *SchemaProperty `json:"items,omitempty"`
	// Properties describes the fields of a nested object (used together with
	// Type "object", e.g. array items that are objects with their own
	// required fields).
	Properties map[string]SchemaProperty `json:"properties,omitempty"`
	// Required lists required field names for a nested object.
	Required []string `json:"required,omitempty"`
	// Minimum/Maximum constrain numeric fields. Kept as interface{} (like
	// Default) so authors write plain literals and the wire output matches
	// hand-written schemas exactly.
	Minimum interface{} `json:"minimum,omitempty"`
	Maximum interface{} `json:"maximum,omitempty"`
	// MaxLength/MinLength constrain string fields. MaxItems/MinItems constrain
	// array fields. Plain ints (omitted when zero): zero is never a meaningful
	// bound, so authors write literals like MaxLength: 2000.
	MaxLength int `json:"maxLength,omitempty"`
	MinLength int `json:"minLength,omitempty"`
	MaxItems  int `json:"maxItems,omitempty"`
	MinItems  int `json:"minItems,omitempty"`
	// OneOf/AnyOf list alternative subschemas (e.g. array items that accept a
	// string or an object). Each branch is a full SchemaProperty. A property
	// that only carries OneOf/AnyOf leaves Type empty, and toMap omits the
	// "type" key in that case to match hand-written schemas exactly.
	OneOf []SchemaProperty `json:"oneOf,omitempty"`
	AnyOf []SchemaProperty `json:"anyOf,omitempty"`
}

// SchemaProvider is an optional interface tools can implement to expose a typed
// input schema. When a tool provides one, ValidateToolInput checks types and
// enums in addition to the required-field presence check that all tools get.
type SchemaProvider interface {
	// Schema returns the typed input schema. Implementations should derive it
	// from the same source as Parameters() so the two never diverge.
	Schema() ToolSchema
}

// ToJSONSchema converts the typed schema to the wire-format
// map[string]interface{} expected by FluxTool.Parameters.
func (s ToolSchema) ToJSONSchema() map[string]interface{} {
	props := make(map[string]interface{}, len(s.Properties))
	for name, p := range s.Properties {
		props[name] = p.toMap()
	}
	schema := map[string]interface{}{
		"type":       s.Type,
		"properties": props,
	}
	if len(s.Required) > 0 {
		schema["required"] = s.Required
	}
	return schema
}

func (p SchemaProperty) toMap() map[string]interface{} {
	m := map[string]interface{}{}
	if p.Type != "" {
		m["type"] = p.Type
	}
	if p.Description != "" {
		m["description"] = p.Description
	}
	if p.Default != nil {
		m["default"] = p.Default
	}
	if len(p.Enum) > 0 {
		m["enum"] = p.Enum
	}
	if p.Minimum != nil {
		m["minimum"] = p.Minimum
	}
	if p.Maximum != nil {
		m["maximum"] = p.Maximum
	}
	if p.MaxLength != 0 {
		m["maxLength"] = p.MaxLength
	}
	if p.MinLength != 0 {
		m["minLength"] = p.MinLength
	}
	if p.MaxItems != 0 {
		m["maxItems"] = p.MaxItems
	}
	if p.MinItems != 0 {
		m["minItems"] = p.MinItems
	}
	if len(p.OneOf) > 0 {
		branches := make([]interface{}, 0, len(p.OneOf))
		for _, b := range p.OneOf {
			branches = append(branches, b.toMap())
		}
		m["oneOf"] = branches
	}
	if len(p.AnyOf) > 0 {
		branches := make([]interface{}, 0, len(p.AnyOf))
		for _, b := range p.AnyOf {
			branches = append(branches, b.toMap())
		}
		m["anyOf"] = branches
	}
	if p.Items != nil {
		m["items"] = p.Items.toMap()
	}
	if len(p.Properties) > 0 {
		props := make(map[string]interface{}, len(p.Properties))
		for name, sub := range p.Properties {
			props[name] = sub.toMap()
		}
		m["properties"] = props
	}
	if len(p.Required) > 0 {
		m["required"] = p.Required
	}
	return m
}

// RiskLevelProvider can be implemented by tools to declare their risk level.
// Tools that don't implement it default to "medium".
type RiskLevelProvider interface {
	RiskLevel() string // "low", "medium", "high"
}

// UntrustedTool marks tools whose implementation is outside rho's built-in
// capability catalog. They remain executable, but the permission engine must
// not infer safety from registry membership alone.
type UntrustedTool interface {
	Untrusted() bool
}

// PathProtector checks whether a file path is protected (read-only).
// engine.ProtectedPaths implements this interface.
type PathProtector interface {
	IsProtected(path string) bool
}

// RetryPolicyProvider is an optional interface a tool can implement to
// customise the retry policy applied to its transient errors. Tools that
// don't implement it get tool.DefaultRetryPolicy (2 retries, 200ms→2s).
type RetryPolicyProvider interface {
	RetryPolicy() RetryPolicy
}

// TimeoutProvider can be implemented by tools to declare their execution
// budget. Tools that don't implement it get the engine's name-based fallback
// (engine.toolTimeout). Declaring a budget is zero-config: the policy reads it
// from the tool's own declaration at dispatch (DSH tool-declared timeout
// policy parity) instead of from a name lookup table.
type TimeoutProvider interface {
	// Timeout returns the maximum wall-clock duration one call may run for.
	// Returning 0 means "no declaration — use the fallback".
	Timeout() time.Duration
}

// TimeoutOf returns the tool-declared execution budget, or 0 when the tool
// does not declare one. ExecuteOne prefers this over the name-based fallback.
func TimeoutOf(t Tool) time.Duration {
	if tp, ok := t.(TimeoutProvider); ok {
		return tp.Timeout()
	}
	return 0
}

// CodeSearchResult is returned by CodeSearchFn.
type CodeSearchResult struct {
	Path      string
	StartLine int
	EndLine   int
	Content   string
	Symbol    string
	Language  string
	Score     float64
}

// ToolContext carries session-level functions for tools that need them.
type ToolContext struct {
	AgentSpawnFn        AgentSpawnFn
	AskUserFn           func(question string) (string, error)
	CodeSearchFn        func(ctx context.Context, query string, limit int) ([]CodeSearchResult, error)
	RefreshCodeIndexFn  func(ctx context.Context) error
	CommitMessageChatFn func(ctx context.Context, prompt string) (string, error)
	AvailableTools      []Tool
	// Registry is optional; when set, ToolSearch select: promotes tools onto
	// the lazy model-visible surface for subsequent LLM turns.
	Registry           *Registry
	AllowedDirectories []string
	AutoCommit         bool
	Protected          PathProtector
	Attribution        *types.Attribution
	SettingsGet        func(key string) (string, bool)
	SettingsSet        func(key, value string) error
	// SpecSlugGet/SpecSlugSet let the Specify/Plan/Tasks tools read and
	// write the active spec workflow's directory slug without any
	// package-level state — each session supplies its own closures over
	// its own PermissionEngine.SpecSlug field.
	SpecSlugGet func() string
	SpecSlugSet func(string)
	// BackgroundManager tracks background sub-agents. If nil, background
	// mode is not available.
	BackgroundManager *BackgroundAgentManager
	// ReadOnlyBash, when true, enforces the explore/plan bash allowlist
	// (ExploreBashAllowed) on every Bash invocation.
	ReadOnlyBash bool
	// WorkingDir, when set, is used as cmd.Dir for Bash and as the preferred
	// workspace root for path tools (subagent worktree isolation).
	WorkingDir string
	// TaskExecutor, when set, arms the TaskRun tool: it runs one task from the
	// store (e.g. by spawning a sub-agent). Nil disables TaskRun.
	TaskExecutor TaskExecutorFunc
	// Lint configures the optional post-write auto-lint cycle. The zero value
	// (Enabled=false) keeps linting off so users are not surprised.
	Lint lint.Config
}

// ctxKey is the context key for ToolContext.
type ctxKey struct{}

// WithToolContext attaches a ToolContext to a context.
func WithToolContext(ctx context.Context, tc *ToolContext) context.Context {
	return context.WithValue(ctx, ctxKey{}, tc)
}

// GetToolContext retrieves the ToolContext from a context.
func GetToolContext(ctx context.Context) *ToolContext {
	if tc, ok := ctx.Value(ctxKey{}).(*ToolContext); ok {
		return tc
	}
	return nil
}

// ReadOnlyTools is the canonical allowlist of tool names whose execution is
// side-effect-free and therefore safe to run in parallel with each other
// within a single agent turn. Consumers (engine/stream.go classifyToolCalls,
// engine/stream.go safeSnapshotTools) MUST go through this set instead of
// redefining it inline. To add a new read-only tool, append its canonical
// name here AND ensure canonicalToolName normalises all of its aliases.
var ReadOnlyTools = map[string]bool{
	"Read":       true,
	"Grep":       true,
	"Glob":       true,
	"LS":         true,
	"WebSearch":  true,
	"WebFetch":   true,
	"ToolSearch": true,
	"ToolHealth": true,
	"Outline":    true,
	"SmartRead":  true,
	"CodeSearch": true,
	"CodeGraph":  true,
	"Impact":     true,
	"GitHistory": true,
	"GitHub":     true,
}

// IsReadOnly reports whether the given (possibly-aliased) tool name is in
// the read-only allowlist. It first canonicalises the name so an LLM-emitted
// alias like "read" or "file_read" still classifies correctly.
func IsReadOnly(name string) bool {
	return ReadOnlyTools[canonicalForReadOnly(name)]
}

// canonicalForReadOnly is a small case-folded map lookup; it intentionally
// duplicates a subset of engine.canonicalToolName to avoid an import cycle
// (tool → engine would be a cycle because engine imports tool). The map
// below MUST be kept in sync with engine.permission_session_methods.canonicalToolName.
func canonicalForReadOnly(name string) string {
	switch strings.ToLower(name) {
	case "read", "file_read":
		return "Read"
	case "grep":
		return "Grep"
	case "glob":
		return "Glob"
	case "ls":
		return "LS"
	case "websearch", "web_search":
		return "WebSearch"
	case "webfetch", "web_fetch":
		return "WebFetch"
	case "toolsearch", "tool_search":
		return "ToolSearch"
	case "toolhealth", "tool_health", "tools_health":
		return "ToolHealth"
	case "outline":
		return "Outline"
	case "smartread", "smart_reader", "smart-reader":
		return "SmartRead"
	case "codesearch", "code_search":
		return "CodeSearch"
	case "codegraph", "code_graph":
		return "CodeGraph"
	case "impact":
		return "Impact"
	case "githistory", "git_history", "git-history":
		return "GitHistory"
	case "github", "gh":
		return "GitHub"
	}
	return name
}

// Registry holds all registered tools.
type Registry struct {
	mu      sync.RWMutex
	tools   map[string]Tool
	primary []Tool
	// modelVisible, when non-nil, restricts FluxTools to the listed primary
	// names (lazy model surface). Get/Execute still reach every registered tool.
	modelVisible map[string]bool
}

// NewRegistry creates a registry with the given tools.
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		r.tools[t.Name()] = t
		r.primary = append(r.primary, t)
		if aliased, ok := t.(AliasedTool); ok {
			for _, alias := range aliased.Aliases() {
				if alias != "" {
					if existing, exists := r.tools[alias]; exists && existing.Name() != t.Name() {
						fmt.Fprintf(os.Stderr, "warning: tool alias %q already registered to %s, overriding with %s\n", alias, existing.Name(), t.Name())
					}
					r.tools[alias] = t
				}
			}
		}
	}
	return r
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// PrimaryTools returns the model-visible tools registered in this registry.
func (r *Registry) PrimaryTools() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, len(r.primary))
	copy(out, r.primary)
	return out
}

// Filter returns a new Registry containing only tools whose names are in the allowlist.
func (r *Registry) Filter(allow []string) *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	set := make(map[string]bool, len(allow))
	for _, name := range allow {
		set[name] = true
	}
	var filtered []Tool
	for _, t := range r.primary {
		if set[t.Name()] {
			filtered = append(filtered, t)
		}
	}
	return NewRegistry(filtered...)
}

// FluxTools converts model-visible tools to Rho runtime tool definitions.
// When lazy model surface is enabled, only promoted/essential tools are listed.
func (r *Registry) FluxTools() []types.FluxTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]types.FluxTool, 0, len(r.primary))
	for _, t := range r.primary {
		if r.modelVisible != nil && !r.modelVisible[t.Name()] {
			continue
		}
		out = append(out, types.FluxTool{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	return out
}

// Execute runs a tool by name with the given JSON input. Input is validated
// against the tool's declared schema before dispatch (H5).
func (r *Registry) Execute(ctx context.Context, name string, input json.RawMessage) (string, error) {
	r.mu.RLock()
	t, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	if err := ValidateToolInput(t, input); err != nil {
		return "", err
	}
	return t.Execute(ctx, input)
}

// Register adds a tool to the registry after creation.
// Returns error if a tool with the same name already exists (unless it's an alias).
func (r *Registry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[t.Name()]; exists {
		return fmt.Errorf("tool %q already registered", t.Name())
	}
	r.addLocked(t)
	return nil
}

// Unregister removes the tool registered under name (primary or alias) and all of
// its aliases. It returns false when no such tool exists. Callers this way get a
// manual disposer equivalent for the mutating surfaces that predate the
// disposer-returning interception seams and do not have to change their Register
// signatures.
func (r *Registry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	found := r.tools[name]
	if found == nil {
		return false
	}
	for n, t := range r.tools {
		if t == found {
			delete(r.tools, n)
		}
	}
	for i, t := range r.primary {
		if t == found {
			r.primary = append(r.primary[:i], r.primary[i+1:]...)
			break
		}
	}
	return true
}

func (r *Registry) addLocked(t Tool) {
	r.tools[t.Name()] = t
	r.primary = append(r.primary, t)
	if aliased, ok := t.(AliasedTool); ok {
		for _, alias := range aliased.Aliases() {
			if alias != "" {
				r.tools[alias] = t
			}
		}
	}
}
