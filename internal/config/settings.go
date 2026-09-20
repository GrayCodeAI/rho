package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GrayCodeAI/rho/internal/provider/gateway"
	"github.com/GrayCodeAI/rho/internal/provider/routing"
	"github.com/GrayCodeAI/rho/internal/safewrite"
	"github.com/GrayCodeAI/rho/internal/smartrouting"
	"github.com/GrayCodeAI/rho/internal/storage"

	"github.com/GrayCodeAI/rho/internal/types"
)

func fetchModelsViaRuntime(ctx context.Context, provider string) ([]EngineModel, error) {
	return ListEngineModels(ctx, provider, false)
}

// Settings holds rho configuration.
// Rho: no API keys stored here. Secrets come from the OS secret store via flux.
type Settings struct {
	// PolicySchemaVersion versions permission/autonomy fields. Zero is
	// the legacy format and is migrated to CurrentPolicySchemaVersion on load.
	PolicySchemaVersion int `json:"policy_schema_version,omitempty"`
	// Model and Provider carry runtime host overrides only (e.g. --settings).
	// Flux owns the stored selection (SetActiveModel / SetActiveProvider);
	// values found in settings.json are dropped on load and on save.
	Model           string   `json:"model,omitempty"`
	Provider        string   `json:"provider,omitempty"`
	Theme           string   `json:"theme,omitempty"`
	AutoAllow       []string `json:"auto_allow,omitempty"`      // tools to always allow
	AllowedTools    []string `json:"allowedTools,omitempty"`    // archive-compatible allow rules
	DisallowedTools []string `json:"disallowedTools,omitempty"` // archive-compatible deny rules
	MaxBudgetUSD    float64  `json:"max_budget_usd,omitempty"`  // cost cap per session
	// Optional local token ceilings. Disabled by default (provider rate limits
	// own throughput). Set a positive value to opt in; -1 also means disabled.
	HourlyTokenLimit        int                    `json:"hourly_token_limit,omitempty"`
	DailyTokenLimit         int                    `json:"daily_token_limit,omitempty"`
	SessionTokenLimit       int                    `json:"session_token_limit,omitempty"`
	MCPServers              []MCPServerConfig      `json:"mcp_servers,omitempty"`
	CustomProviders         []CustomProviderConfig `json:"custom_providers,omitempty"`
	RepoMap                 *bool                  `json:"repo_map,omitempty"`
	RepoMapMaxTokens        int                    `json:"repo_map_max_tokens,omitempty"`
	AutoCommit              *bool                  `json:"auto_commit,omitempty"`                // auto-commit file changes
	Autonomy                int                    `json:"autonomy,omitempty"`                   // autonomy level 0-4
	AutonomyExplicit        bool                   `json:"autonomy_explicit,omitempty"`          // distinguishes persisted Supervised (0) from unset
	AutonomyOverrides       map[string]bool        `json:"autonomy_overrides,omitempty"`         // per-flag overrides (e.g. "auto_execute_bash": false)
	NeverAllow              []string               `json:"never_allow,omitempty"`                // personal hard ceiling: deny rules even YOLO can't override
	SpecAllowTests          bool                   `json:"spec_allow_tests,omitempty"`           // allow safe test commands during spec stage
	ModelRoles              *routing.ModelRoles    `json:"model_roles,omitempty"`                // per-role model overrides
	SmartRouting            *smartrouting.Config   `json:"smart_routing,omitempty"`              // cheap-simple / strong turn routing (opt-in)
	AutoCompactThresholdPct int                    `json:"auto_compact_threshold_pct,omitempty"` // token % to trigger auto-compact (default 85)
	Frugal                  bool                   `json:"frugal,omitempty"`                     // aggressive cost optimization: cascade to cheap models, lower max_tokens, earlier compaction
	Attribution             *Attribution           `json:"attribution,omitempty"`
	DeploymentRouting       *bool                  `json:"deployment_routing,omitempty"`       // use catalog deployment router when true / unset + provider.json qualifies
	GLMThinkingEnabled      *bool                  `json:"glm_thinking_enabled,omitempty"`     // global thinking fallback (Z.AI-era name); prefer ModelThinking / ThinkingEnabled
	ModelThinking           map[string]bool        `json:"model_thinking,omitempty"`           // per-model thinking; missing = provider default
	TuiMouse                *bool                  `json:"tui_mouse,omitempty"`                // TUI mouse capture; false preserves native click-drag copy
	ReplMode                *bool                  `json:"repl_mode,omitempty"`                // Start in REPL mode instead of TUI
	ScrollSpeed             int                    `json:"scroll_speed,omitempty"`             // scroll speed 1-100 (default 50)
	ScrollMode              string                 `json:"scroll_mode,omitempty"`              // auto, wheel, trackpad
	InvertScroll            bool                   `json:"invert_scroll,omitempty"`            // natural scrolling invert
	CompactMode             bool                   `json:"compact_mode,omitempty"`             // reduce outer padding
	PaginatorLines          int                    `json:"paginator_lines,omitempty"`          // scrollback buffer lines (0 = unlimited)
	PaginatorShowLineNums   *bool                  `json:"paginator_show_line_nums,omitempty"` // show line numbers in scrollback
}

const CurrentPolicySchemaVersion = 1

// ToolPreset maps a named preset to a list of allowed tools.
type ToolPreset struct {
	Name  string   `json:"name"`
	Tools []string `json:"tools"` // nil means all tools allowed
}

// builtinToolPresets defines the built-in tool presets.
var builtinToolPresets = map[string]ToolPreset{
	"minimal": {
		Name:  "minimal",
		Tools: []string{"Read", "Write", "Edit", "Grep", "Glob", "Bash"},
	},
	"readonly": {
		Name:  "readonly",
		Tools: []string{"Read", "Grep", "Glob"},
	},
	"full": {
		Name:  "full",
		Tools: nil, // nil means all tools allowed
	},
}

// ToolPresetByName returns a built-in tool preset by name.
func ToolPresetByName(name string) (ToolPreset, bool) {
	p, ok := builtinToolPresets[name]
	return p, ok
}

// Attribution controls how rho identifies itself in git commits.
type Attribution = types.Attribution

// CustomProviderConfig defines a user-specified OpenAI-compatible provider.
type CustomProviderConfig struct {
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	APIKeyEnv string `json:"api_key_env,omitempty"`
	Model     string `json:"model,omitempty"`
}

// UnmarshalJSON accepts both Go-era snake_case keys and archive-style camelCase keys.
func (s *Settings) UnmarshalJSON(data []byte) error {
	type alias Settings
	aux := struct {
		*alias
		AutoAllowCamel       []string          `json:"autoAllow,omitempty"`
		MaxBudgetUSDCamel    float64           `json:"maxBudgetUSD,omitempty"`
		MCPServersCamel      []MCPServerConfig `json:"mcpServers,omitempty"`
		AllowedToolsSnake    []string          `json:"allowed_tools,omitempty"`
		DisallowedToolsSnake []string          `json:"disallowed_tools,omitempty"`
	}{
		alias: (*alias)(s),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if len(s.AutoAllow) == 0 {
		s.AutoAllow = aux.AutoAllowCamel
	}
	if s.MaxBudgetUSD == 0 {
		s.MaxBudgetUSD = aux.MaxBudgetUSDCamel
	}
	if len(s.MCPServers) == 0 {
		s.MCPServers = aux.MCPServersCamel
	}
	if len(s.AllowedTools) == 0 {
		s.AllowedTools = aux.AllowedToolsSnake
	}
	if len(s.DisallowedTools) == 0 {
		s.DisallowedTools = aux.DisallowedToolsSnake
	}
	return nil
}

// MCPServerConfig defines an MCP server to connect at startup.
type MCPServerConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Type    string            `json:"type,omitempty"`    // "stdio" (default), "sse", "http", "websocket"
	URL     string            `json:"url,omitempty"`     // for sse/http/websocket transports
	Headers map[string]string `json:"headers,omitempty"` // custom headers for sse/http/websocket
	// ClientID is an OAuth client_id pre-registered with the server's
	// authorization server. When empty, dynamic client registration (RFC
	// 7591) is attempted if the server advertises a registration_endpoint.
	ClientID string `json:"client_id,omitempty"`
}

func globalSettingsPath() string {
	return storage.SettingsPath()
}

// settingsCache memoizes the raw settings file bytes for the process
// lifetime, keyed on (path, mtime, size) so external edits and SaveGlobal are
// always picked up while repeated startup loads skip the disk read. Only raw
// bytes are cached — each call unmarshals a fresh value, because callers
// (e.g. MergeSettings) mutate reference fields like ModelRoles in place.
var settingsCache struct {
	sync.Mutex
	valid   bool
	path    string
	modTime time.Time
	size    int64
	data    []byte
}

func readSettingsFileCached(path string) ([]byte, error) {
	fi, statErr := os.Stat(path)
	settingsCache.Lock()
	defer settingsCache.Unlock()
	if statErr == nil && settingsCache.valid && settingsCache.path == path &&
		settingsCache.modTime.Equal(fi.ModTime()) && settingsCache.size == fi.Size() {
		return settingsCache.data, nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is the rho global settings path from internal storage config, not external input
	if err == nil && statErr == nil {
		settingsCache.valid = true
		settingsCache.path = path
		settingsCache.modTime = fi.ModTime()
		settingsCache.size = fi.Size()
		settingsCache.data = data
	} else {
		settingsCache.valid = false
	}
	return data, err
}

// invalidateSettingsCache forces the next readSettingsFileCached to re-read
// from disk. SaveGlobal calls it after writing so a same-second, same-size
// write cannot be served from stale mtime/size cache (Phase 3 fix).
func invalidateSettingsCache() {
	settingsCache.Lock()
	settingsCache.valid = false
	settingsCache.Unlock()
}

// LoadGlobalSettings loads only Rho's user config settings.json.
func LoadGlobalSettings() Settings {
	var s Settings
	path := globalSettingsPath()
	if data, err := readSettingsFileCached(path); err == nil {
		if err := json.Unmarshal(data, &s); err != nil {
			slog.Warn("failed to parse settings", "path", path, "error", err)
		}
	}
	// Flux owns the stored model/provider selection. Stale values left in
	// settings.json must not act as a host override.
	s = stripHostModelSelection(s)
	if s.PolicySchemaVersion == 0 {
		s.PolicySchemaVersion = CurrentPolicySchemaVersion
	}
	return s
}

// LoadSettings loads settings from user config, overlaid with any
// project-scoped settings discovered by walking up from the current working
// directory looking for .rho/settings.json.
func LoadSettings() Settings {
	s := LoadGlobalSettings()
	if project := findProjectSettings(); project != nil {
		// Project settings provide repository-safe defaults only. Private
		// authority stays in the user profile or explicit runtime overrides.
		s = MergeSettings(s, projectSafeSettings(*project))
	}
	if s.PolicySchemaVersion == 0 {
		s.PolicySchemaVersion = CurrentPolicySchemaVersion
	}
	return s
}

// projectSafeSettings strips fields that could grant private authority when
// loaded from a repository. Project configuration may influence bounded,
// repository-local behavior but cannot select credentials, grant permissions,
// or register external providers and integrations.
func projectSafeSettings(project Settings) Settings {
	project.Model = ""
	project.Provider = ""
	project.AutoAllow = nil
	project.AllowedTools = nil
	project.DisallowedTools = nil
	project.NeverAllow = nil
	project.MCPServers = nil
	project.CustomProviders = nil
	project.DeploymentRouting = nil
	project.ModelThinking = nil
	project.GLMThinkingEnabled = nil
	return project
}

// findProjectSettings walks up from the current working directory looking for
// a .rho/settings.json file. Returns nil if none is found. The nearest
// ancestor wins (no recursive merge — a project settings file fully shadows
// global settings for its fields).
func findProjectSettings() *Settings {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	dir := cwd
	for {
		candidate := filepath.Join(dir, ".rho", "settings.json")
		if data, err := os.ReadFile(candidate); err == nil { // #nosec G304 -- path derived from cwd walk-up
			var s Settings
			if err := json.Unmarshal(data, &s); err == nil {
				return &s
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil // reached filesystem root
		}
		dir = parent
	}
}

// LoadSettingsWithOverride loads normal settings plus a JSON object or JSON file override.
func LoadSettingsWithOverride(override string) (Settings, error) {
	s := LoadSettings()
	if override == "" {
		return s, nil
	}
	var extra Settings
	if err := readSettingsOverride(override, &extra); err != nil {
		return s, err
	}
	return MergeSettings(s, extra), nil
}

func readSettingsOverride(source string, out *Settings) error {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil
	}
	if strings.HasPrefix(source, "{") {
		return json.Unmarshal([]byte(source), out)
	}
	data, err := os.ReadFile(source) // #nosec G304 -- source is a file path explicitly supplied by the invoking user via CLI/API override, same trust level as the user
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// MergeSettings applies override fields on top of base using project-style precedence.
func MergeSettings(base, override Settings) Settings {
	if base.PolicySchemaVersion == 0 {
		base.PolicySchemaVersion = CurrentPolicySchemaVersion
	}
	if override.PolicySchemaVersion > base.PolicySchemaVersion {
		base.PolicySchemaVersion = override.PolicySchemaVersion
	}
	if override.Model != "" {
		base.Model = override.Model
	}
	if override.Provider != "" {
		base.Provider = override.Provider
	}
	if override.Theme != "" {
		base.Theme = override.Theme
	}
	if override.MaxBudgetUSD > 0 {
		base.MaxBudgetUSD = override.MaxBudgetUSD
	}
	if override.HourlyTokenLimit != 0 {
		base.HourlyTokenLimit = override.HourlyTokenLimit
	}
	if override.DailyTokenLimit != 0 {
		base.DailyTokenLimit = override.DailyTokenLimit
	}
	if override.SessionTokenLimit != 0 {
		base.SessionTokenLimit = override.SessionTokenLimit
	}
	if len(override.AutoAllow) > 0 {
		base.AutoAllow = override.AutoAllow
	}
	if len(override.AllowedTools) > 0 {
		base.AllowedTools = append(base.AllowedTools, override.AllowedTools...)
	}
	if len(override.DisallowedTools) > 0 {
		base.DisallowedTools = append(base.DisallowedTools, override.DisallowedTools...)
	}
	if len(override.MCPServers) > 0 {
		base.MCPServers = override.MCPServers
	}
	if len(override.CustomProviders) > 0 {
		base.CustomProviders = override.CustomProviders
	}
	if override.RepoMap != nil {
		base.RepoMap = override.RepoMap
	}
	if override.RepoMapMaxTokens > 0 {
		base.RepoMapMaxTokens = override.RepoMapMaxTokens
	}
	if override.AutoCommit != nil {
		base.AutoCommit = override.AutoCommit
	}
	if override.AutonomyExplicit || override.Autonomy != 0 {
		base.Autonomy = override.Autonomy
		base.AutonomyExplicit = true
	}
	if override.AutoCompactThresholdPct > 0 {
		base.AutoCompactThresholdPct = override.AutoCompactThresholdPct
	}
	if override.DeploymentRouting != nil {
		base.DeploymentRouting = override.DeploymentRouting
	}
	if override.GLMThinkingEnabled != nil {
		base.GLMThinkingEnabled = override.GLMThinkingEnabled
	}
	if len(override.ModelThinking) > 0 {
		if base.ModelThinking == nil {
			base.ModelThinking = make(map[string]bool, len(override.ModelThinking))
		}
		for modelID, enabled := range override.ModelThinking {
			base.ModelThinking[modelID] = enabled
		}
	}
	if override.ModelRoles != nil {
		if base.ModelRoles == nil {
			base.ModelRoles = override.ModelRoles
		} else {
			if override.ModelRoles.Planner != "" {
				base.ModelRoles.Planner = override.ModelRoles.Planner
			}
			if override.ModelRoles.Explorer != "" {
				base.ModelRoles.Explorer = override.ModelRoles.Explorer
			}
			if override.ModelRoles.Coder != "" {
				base.ModelRoles.Coder = override.ModelRoles.Coder
			}
			if override.ModelRoles.Reviewer != "" {
				base.ModelRoles.Reviewer = override.ModelRoles.Reviewer
			}
			if override.ModelRoles.Commit != "" {
				base.ModelRoles.Commit = override.ModelRoles.Commit
			}
		}
	}
	return base
}

// SaveGlobal saves settings to the global config file.
func SaveGlobal(s Settings) error {
	s = stripHostModelSelection(s)
	dir := filepath.Dir(globalSettingsPath())
	_ = os.MkdirAll(dir, 0o750)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// safewrite keeps the previous 0600 mode (per-user config, unreadable to
	// other local users) while making the write atomic and symlink-resistant.
	if err := safewrite.WriteFile(globalSettingsPath(), data); err != nil {
		return err
	}
	// Invalidate the in-process byte cache so subsequent loads within the
	// same second see the new file (the cache is also keyed on mtime/size,
	// which can be identical for a same-size write).
	invalidateSettingsCache()
	return nil
}

// SettingValue returns a display-safe value for a supported setting key.
func SettingValue(s Settings, key string) (string, bool) {
	normalized := normalizeSettingKey(key)
	// Rho: API key status comes from OS secret store, not settings file
	if provider, ok := apiKeyProviderFromSettingKey(normalized); ok {
		return EnvKeyStatus(provider), true
	}
	switch normalized {
	case "model":
		return ActiveModel(context.Background()), true
	case "provider":
		return ActiveProvider(context.Background()), true
	case "apikey":
		return EnvKeyStatus(s.Provider), true
	case "apikeys":
		return AllEnvKeyStatus(), true
	case "theme":
		return s.Theme, true
	case "autoallow":
		return strings.Join(s.AutoAllow, ", "), true
	case "allowedtools":
		return strings.Join(s.AllowedTools, ", "), true
	case "disallowedtools":
		return strings.Join(s.DisallowedTools, ", "), true
	case "maxbudgetusd":
		if s.MaxBudgetUSD == 0 {
			return "", true
		}
		return strconv.FormatFloat(s.MaxBudgetUSD, 'f', -1, 64), true
	case "hourlytokenlimit":
		if s.HourlyTokenLimit == 0 {
			return "", true
		}
		return strconv.Itoa(s.HourlyTokenLimit), true
	case "dailytokenlimit":
		if s.DailyTokenLimit == 0 {
			return "", true
		}
		return strconv.Itoa(s.DailyTokenLimit), true
	case "sessiontokenlimit":
		if s.SessionTokenLimit == 0 {
			return "", true
		}
		return strconv.Itoa(s.SessionTokenLimit), true
	case "mcpservers":
		data, _ := json.Marshal(s.MCPServers)
		return string(data), true
	case "deploymentrouting":
		return DeploymentRoutingLabel(s), true
	case "glmthinking", "glmthinkingenabled":
		if s.GLMThinkingEnabled == nil {
			return "default", true
		}
		if *s.GLMThinkingEnabled {
			return "true", true
		}
		return "false", true
	case "tuimouse":
		if s.TuiMouse == nil {
			return "default (on)", true
		}
		if *s.TuiMouse {
			return "true", true
		}
		return "false", true
	default:
		return "", false
	}
}

// SetGlobalSetting updates a supported scalar/list setting in Rho user config.
// Rho: API keys are NOT stored in settings.json. Use /config and the OS secret store.
func SetGlobalSetting(key, value string) error {
	s := LoadGlobalSettings()
	normalized := normalizeSettingKey(key)
	// Rho: reject API key persistence to disk
	if _, ok := apiKeyProviderFromSettingKey(normalized); ok {
		return fmt.Errorf("API keys are not stored in settings.json. Save via /config (%s)", CredentialStoreName())
	}
	if normalized == "apikey" {
		return fmt.Errorf("API keys are not stored in settings.json. Save via /config (%s)", CredentialStoreName())
	}
	switch normalized {
	case "model":
		return SetActiveModel(context.Background(), value)
	case "provider":
		return SetActiveProvider(context.Background(), value)
	case "theme":
		s.Theme = value
	case "autoallow":
		s.AutoAllow = splitSettingList(value)
	case "allowedtools":
		s.AllowedTools = splitSettingList(value)
	case "disallowedtools":
		s.DisallowedTools = splitSettingList(value)
	case "maxbudgetusd":
		amount, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid max budget: %w", err)
		}
		s.MaxBudgetUSD = amount
	case "hourlytokenlimit":
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("invalid hourly_token_limit: %w", err)
		}
		s.HourlyTokenLimit = n
	case "dailytokenlimit":
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("invalid daily_token_limit: %w", err)
		}
		s.DailyTokenLimit = n
	case "sessiontokenlimit":
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("invalid session_token_limit: %w", err)
		}
		s.SessionTokenLimit = n
	case "deploymentrouting":
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "on":
			enabled := true
			s.DeploymentRouting = &enabled
		case "0", "false", "no", "off":
			enabled := false
			s.DeploymentRouting = &enabled
		default:
			return fmt.Errorf("deployment_routing must be true or false")
		}
	case "glmthinking", "glmthinkingenabled":
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "on":
			enabled := true
			s.GLMThinkingEnabled = &enabled
		case "0", "false", "no", "off":
			enabled := false
			s.GLMThinkingEnabled = &enabled
		case "default", "null", "nil", "":
			s.GLMThinkingEnabled = nil
		default:
			return fmt.Errorf("glm_thinking must be true, false, or default")
		}
	case "tuimouse":
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "on", "enable":
			enabled := true
			s.TuiMouse = &enabled
		case "0", "false", "no", "off", "disable":
			enabled := false
			s.TuiMouse = &enabled
		case "default", "null", "nil", "":
			s.TuiMouse = nil
		default:
			return fmt.Errorf("tui_mouse must be true, false, or default")
		}
	case "scrollspeed":
		speed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || speed < 1 || speed > 100 {
			return fmt.Errorf("scroll_speed must be a number between 1 and 100")
		}
		s.ScrollSpeed = speed
	case "scrollmode":
		mode := strings.ToLower(strings.TrimSpace(value))
		switch mode {
		case "auto", "wheel", "trackpad":
			s.ScrollMode = mode
		default:
			return fmt.Errorf("scroll_mode must be auto, wheel, or trackpad")
		}
	case "invertscroll", "invert_scroll":
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "on":
			s.InvertScroll = true
		case "0", "false", "no", "off":
			s.InvertScroll = false
		default:
			return fmt.Errorf("invert_scroll must be true or false")
		}
	case "compactmode", "compact_mode":
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "on":
			s.CompactMode = true
		case "0", "false", "no", "off":
			s.CompactMode = false
		default:
			return fmt.Errorf("compact_mode must be true or false")
		}
	case "paginatorlines", "paginator_lines":
		lines, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || lines < 0 {
			return fmt.Errorf("paginator_lines must be a non-negative number")
		}
		s.PaginatorLines = lines
	case "paginatorshowlinenumbers", "paginator_show_line_nums":
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "on":
			enabled := true
			s.PaginatorShowLineNums = &enabled
		case "0", "false", "no", "off":
			enabled := false
			s.PaginatorShowLineNums = &enabled
		default:
			return fmt.Errorf("paginator_show_line_nums must be true or false")
		}
	default:
		return fmt.Errorf("unsupported setting key %q", key)
	}
	return SaveGlobal(s)
}

func normalizeSettingKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, "_", "")
	key = strings.ReplaceAll(key, "-", "")
	return key
}

func normalizeProviderName(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	provider = strings.ReplaceAll(provider, "_", "-")
	return provider
}

func apiKeyProviderFromSettingKey(normalized string) (string, bool) {
	for _, prefix := range []string{"apikey.", "apikey:"} {
		if strings.HasPrefix(normalized, prefix) {
			provider := normalizeProviderName(strings.TrimPrefix(normalized, prefix))
			return provider, provider != ""
		}
	}
	return "", false
}

func splitSettingList(value string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }) {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// BoolPtr returns a pointer to the given bool value.
func BoolPtr(b bool) *bool { return &b }

// ─────────────────────────────────────────────────────────────
// Rho: API keys from OS secret store only (no .env)
// ─────────────────────────────────────────────────────────────

// ProviderAPIKeyEnv returns the API key env var for a provider (registry first for setup gateways).
func ProviderAPIKeyEnv(provider string) string {
	if env := SetupGatewayCredentialEnv(provider); env != "" {
		return env
	}
	return ""
}

// EnvKeyStatus returns set, empty, or local from the OS credential store.
func EnvKeyStatus(provider string) string {
	engine, err := newFluxEngine()
	if err != nil {
		return "empty"
	}
	status, err := engine.CredentialStatus(context.Background(), provider)
	if err != nil {
		return "empty"
	}
	if status.Configured {
		return "set"
	}
	if strings.TrimSpace(status.EnvVar) == "" {
		return "local"
	}
	return "empty"
}

// AllEnvKeyStatus returns a comma-separated summary of providers with credentials set.
func AllEnvKeyStatus() string {
	var parts []string
	for _, p := range AllCatalogProviders() {
		if EnvKeyStatus(p) == "set" {
			parts = append(parts, p+":set")
		}
	}
	if len(parts) == 0 {
		return "(none set)"
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func providerCredentialEnvAliases(provider string) []string {
	primary := strings.TrimSpace(ProviderAPIKeyEnv(provider))
	seen := map[string]bool{}
	var out []string
	engine, _ := newFluxEngine()
	var aliases []string
	if engine != nil {
		aliases = engine.CredentialEnvKeys(provider)
	}
	for _, env := range aliases {
		env = strings.TrimSpace(env)
		if env == "" || env == primary || seen[env] {
			continue
		}
		seen[env] = true
		out = append(out, env)
	}
	return out
}

// ─────────────────────────────────────────────────────────────
// Live model catalog fetch from flux
// ─────────────────────────────────────────────────────────────

// FetchModelsForProvider returns models from the flux catalog (dynamic; no rho hardcoded lists).
// RefreshModelCatalogV1 is the explicit network refresh boundary.
func FetchModelsForProvider(provider string) ([]EngineModel, error) {
	provider = gateway.NormalizeProviderID(provider)
	if provider == "" {
		return nil, fmt.Errorf("no provider specified")
	}
	ctx := context.Background()
	models, err := fetchModelsViaRuntime(ctx, provider)
	if err == nil && len(models) > 0 {
		return models, nil
	}
	if refreshErr := TryAutoRefreshCatalog(ctx); refreshErr == nil {
		return fetchModelsViaRuntime(ctx, provider)
	}
	if err != nil {
		return nil, err
	}
	// Custom OpenAI-compatible providers: single model from settings, not rho catalog data.
	for _, cp := range LoadSettings().CustomProviders {
		if gateway.NormalizeProviderID(cp.Name) != provider {
			continue
		}
		if id := strings.TrimSpace(cp.Model); id != "" {
			return []EngineModel{{
				ID:          id,
				DisplayName: id,
			}}, nil
		}
	}
	return nil, fmt.Errorf("no models found for provider %s in flux catalog (check API keys; rho will refresh automatically on next start)", provider)
}

// FetchModelsForProviderWithSettings resolves cached models using one
// invocation's effective settings, including its custom gateways.
func FetchModelsForProviderWithSettings(ctx context.Context, settings Settings, provider string) ([]EngineModel, error) {
	provider = gateway.NormalizeProviderID(provider)
	if provider == "" {
		return nil, fmt.Errorf("no provider specified")
	}
	engine, engineErr := NewFluxEngineForSettings(settings)
	if engineErr != nil {
		return nil, engineErr
	}
	models, err := engine.ListModels(ctx, provider, false)
	if err == nil && len(models) > 0 {
		return models, nil
	}
	if refreshed, refreshErr := engine.ListModels(ctx, provider, true); refreshErr == nil && len(refreshed) > 0 {
		return refreshed, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no models found for provider %s in flux catalog", provider)
}

func refreshModelCatalog(ctx context.Context, _ bool) (gateway.CatalogSnapshot, error) {
	engine, err := newFluxEngine()
	if err != nil {
		return gateway.CatalogSnapshot{}, err
	}
	return engine.RefreshCatalog(ctx, "")
}

// RefreshModelCatalogV1 asks flux to refresh the remote catalog and provider APIs using env API keys.
func RefreshModelCatalogV1(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	result, err := refreshModelCatalog(ctx, true)
	if err != nil {
		return "", err
	}
	return formatCatalogSnapshot(result), nil
}

// RefreshModelCatalogV1WithSettings refreshes through an invocation-scoped
// engine, so --settings custom gateways participate without global state.
func RefreshModelCatalogV1WithSettings(ctx context.Context, settings Settings) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	engine, err := NewFluxEngineForSettings(settings)
	if err != nil {
		return "", err
	}
	result, err := engine.RefreshCatalog(ctx, "")
	if err != nil {
		return "", err
	}
	return formatCatalogSnapshot(result), nil
}
