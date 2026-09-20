package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/GrayCodeAI/rho/internal/provider/routing"
)

func TestMergeSettings_ModelOverride(t *testing.T) {
	t.Parallel()
	base := Settings{Model: "gpt-4", Provider: "openai"}
	override := Settings{Model: "claude-sonnet-4-20250514"}
	merged := MergeSettings(base, override)
	if merged.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model override, got %q", merged.Model)
	}
	if merged.Provider != "openai" {
		t.Error("provider should be preserved from base")
	}
}

func TestMergeSettings_ProviderOverride(t *testing.T) {
	t.Parallel()
	base := Settings{Provider: "openai"}
	override := Settings{Provider: "anthropic"}
	merged := MergeSettings(base, override)
	if merged.Provider != "anthropic" {
		t.Errorf("expected provider override, got %q", merged.Provider)
	}
}

func TestMergeSettings_ThemeOverride(t *testing.T) {
	t.Parallel()
	base := Settings{Theme: "dark"}
	override := Settings{Theme: "light"}
	merged := MergeSettings(base, override)
	if merged.Theme != "light" {
		t.Errorf("expected theme override, got %q", merged.Theme)
	}
}

func TestMergeSettings_BudgetOverride(t *testing.T) {
	t.Parallel()
	base := Settings{MaxBudgetUSD: 10.0}
	override := Settings{MaxBudgetUSD: 25.0}
	merged := MergeSettings(base, override)
	if merged.MaxBudgetUSD != 25.0 {
		t.Errorf("expected budget 25.0, got %f", merged.MaxBudgetUSD)
	}
}

func TestMergeSettings_BudgetZeroNotOverridden(t *testing.T) {
	t.Parallel()
	base := Settings{MaxBudgetUSD: 10.0}
	override := Settings{MaxBudgetUSD: 0}
	merged := MergeSettings(base, override)
	if merged.MaxBudgetUSD != 10.0 {
		t.Errorf("expected budget 10.0 (zero should not override), got %f", merged.MaxBudgetUSD)
	}
}

func TestMergeSettings_AutoAllowOverride(t *testing.T) {
	t.Parallel()
	base := Settings{AutoAllow: []string{"Bash"}}
	override := Settings{AutoAllow: []string{"Read", "Write"}}
	merged := MergeSettings(base, override)
	if len(merged.AutoAllow) != 2 {
		t.Errorf("expected 2 auto_allow, got %d", len(merged.AutoAllow))
	}
}

func TestMergeSettings_MCPServersOverride(t *testing.T) {
	t.Parallel()
	base := Settings{}
	override := Settings{
		MCPServers: []MCPServerConfig{
			{Name: "test-server", Command: "test-cmd"},
		},
	}
	merged := MergeSettings(base, override)
	if len(merged.MCPServers) != 1 {
		t.Errorf("expected 1 MCP server, got %d", len(merged.MCPServers))
	}
	if merged.MCPServers[0].Name != "test-server" {
		t.Errorf("expected server name 'test-server', got %q", merged.MCPServers[0].Name)
	}
}

func TestMergeSettings_RepoMapOverride(t *testing.T) {
	t.Parallel()
	base := Settings{}
	override := Settings{RepoMap: BoolPtr(false)}
	merged := MergeSettings(base, override)
	if merged.RepoMap == nil || *merged.RepoMap != false {
		t.Error("expected RepoMap=false")
	}
}

func TestMergeSettings_PersistsExplicitSupervised(t *testing.T) {
	base := Settings{Autonomy: 2, AutonomyExplicit: true}
	merged := MergeSettings(base, Settings{AutonomyExplicit: true, Autonomy: 0})
	if merged.Autonomy != 0 || !merged.AutonomyExplicit {
		t.Fatalf("merged autonomy = %d explicit=%v, want 0/true", merged.Autonomy, merged.AutonomyExplicit)
	}
}

func TestMergeSettings_ModelRolesOverride(t *testing.T) {
	t.Parallel()
	base := Settings{}
	override := Settings{
		ModelRoles: &routing.ModelRoles{
			Planner:  "claude-opus",
			Explorer: "claude-haiku",
			Coder:    "claude-sonnet",
		},
	}
	merged := MergeSettings(base, override)
	if merged.ModelRoles == nil {
		t.Fatal("expected ModelRoles to be set")
	}
	if merged.ModelRoles.Planner != "claude-opus" {
		t.Errorf("expected planner 'claude-opus', got %q", merged.ModelRoles.Planner)
	}
	if merged.ModelRoles.Explorer != "claude-haiku" {
		t.Errorf("expected explorer 'claude-haiku', got %q", merged.ModelRoles.Explorer)
	}
}

func TestNormalizeSettingKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected string
	}{
		{"model", "model"},
		{"Model", "model"},
		{"max_budget_usd", "maxbudgetusd"},
		{"max-budget-usd", "maxbudgetusd"},
		{"  AUTO_ALLOW  ", "autoallow"},
		{"allowedTools", "allowedtools"},
	}

	for _, tt := range tests {
		got := normalizeSettingKey(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeSettingKey(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestNormalizeProviderName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected string
	}{
		{"OpenAI", "openai"},
		{"anthropic", "anthropic"},
		{"  ANTHROPIC  ", "anthropic"},
		{"google_ai", "google-ai"},
	}

	for _, tt := range tests {
		got := normalizeProviderName(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeProviderName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestActiveProviderID_XiaomiAliases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected string
	}{
		{"xiaomi_mimo_token_plan", "xiaomi_mimo_token_plan"},
		{"xiaomi-mimo-token-plan", "xiaomi_mimo_token_plan"},
		{"XIAOMI_MIMO_TOKEN_PLAN", "xiaomi_mimo_token_plan"},
		{"xiaomi_mimo_payg", "xiaomi_mimo_payg"},
		{"xiaomi-mimo-payg", "xiaomi_mimo_payg"},
		{"xai", "grok"},
	}

	for _, tt := range tests {
		got := ActiveProviderID(tt.input)
		if got != tt.expected {
			t.Errorf("ActiveProviderID(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestSplitSettingList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected []string
	}{
		{"Bash,Read,Write", []string{"Bash", "Read", "Write"}},
		{"Bash Read Write", []string{"Bash", "Read", "Write"}},
		{"Bash, Read, Write", []string{"Bash", "Read", "Write"}},
		{"  Bash  ,  Read  ", []string{"Bash", "Read"}},
		{"", nil},
	}

	for _, tt := range tests {
		got := splitSettingList(tt.input)
		if len(got) != len(tt.expected) {
			t.Errorf("splitSettingList(%q) = %v (len %d), want %v (len %d)", tt.input, got, len(got), tt.expected, len(tt.expected))
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("splitSettingList(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestBoolPtr(t *testing.T) {
	t.Parallel()
	truePtr := BoolPtr(true)
	if truePtr == nil || !*truePtr {
		t.Error("BoolPtr(true) should return pointer to true")
	}
	falsePtr := BoolPtr(false)
	if falsePtr == nil || *falsePtr {
		t.Error("BoolPtr(false) should return pointer to false")
	}
}

func TestSettings_UnmarshalJSON_CamelCase(t *testing.T) {
	t.Parallel()
	data := []byte(`{"autoAllow":["Bash","Read"],"maxBudgetUSD":25.0,"customHeaders":{"X-Test":"val"}}`)

	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if len(s.AutoAllow) != 2 {
		t.Errorf("expected 2 auto_allow, got %d", len(s.AutoAllow))
	}
	if s.MaxBudgetUSD != 25.0 {
		t.Errorf("expected budget 25.0, got %f", s.MaxBudgetUSD)
	}
}

func TestSettings_UnmarshalJSON_SnakeCase(t *testing.T) {
	t.Parallel()
	data := []byte(`{"auto_allow":["Bash"],"max_budget_usd":10.0}`)

	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if len(s.AutoAllow) != 1 {
		t.Errorf("expected 1 auto_allow, got %d", len(s.AutoAllow))
	}
	if s.MaxBudgetUSD != 10.0 {
		t.Errorf("expected budget 10.0, got %f", s.MaxBudgetUSD)
	}
}

func TestActiveProviderID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected string
	}{
		{"anthropic", "anthropic"},
		{"openai", "openai"},
		{"xai", "grok"},
		{"XAI", "grok"},
		{"zai_coding", "zai_coding"},
		{"z-ai-coding", "zai_coding"},
		{"zai_payg", "zai_payg"},
		{"z-ai-payg", "zai_payg"},
	}

	for _, tt := range tests {
		got := ActiveProviderID(tt.input)
		if got != tt.expected {
			t.Errorf("ActiveProviderID(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestProviderCredentialEnvAliases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		provider string
		hasAlias bool
	}{
		{"anthropic", true},
		{"gemini", true},
		{"google", true},
		{"grok", false},
		{"xai", false},
		{"xiaomi_mimo_payg", false},
		{"openai", false},
	}

	for _, tt := range tests {
		aliases := providerCredentialEnvAliases(tt.provider)
		if tt.hasAlias && len(aliases) == 0 {
			t.Errorf("expected aliases for %s", tt.provider)
		}
		if !tt.hasAlias && len(aliases) != 0 {
			t.Errorf("expected no aliases for %s, got %v", tt.provider, aliases)
		}
	}
}

func TestReadSettingsOverride_InlineJSON(t *testing.T) {
	t.Parallel()
	var s Settings
	err := readSettingsOverride(`{"theme":"dark"}`, &s)
	if err != nil {
		t.Fatalf("readSettingsOverride failed: %v", err)
	}
	if s.Theme != "dark" {
		t.Errorf("expected theme 'dark', got %q", s.Theme)
	}
}

func TestReadSettingsOverride_EmptyString(t *testing.T) {
	t.Parallel()
	var s Settings
	err := readSettingsOverride("", &s)
	if err != nil {
		t.Fatalf("readSettingsOverride should not error on empty string: %v", err)
	}
}

func TestReadSettingsOverride_WhitespaceOnly(t *testing.T) {
	t.Parallel()
	var s Settings
	err := readSettingsOverride("   ", &s)
	if err != nil {
		t.Fatalf("readSettingsOverride should not error on whitespace: %v", err)
	}
}

func TestApiKeyProviderFromSettingKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input          string
		expectProvider string
		expectOk       bool
	}{
		{"apikey.openai", "openai", true},
		{"apikey:anthropic", "anthropic", true},
		{"apikey.xai", "xai", true},
		{"model", "", false},
		{"apikey.", "", false},
	}

	for _, tt := range tests {
		provider, ok := apiKeyProviderFromSettingKey(tt.input)
		if ok != tt.expectOk {
			t.Errorf("apiKeyProviderFromSettingKey(%q): ok = %v, want %v", tt.input, ok, tt.expectOk)
		}
		if ok && provider != tt.expectProvider {
			t.Errorf("apiKeyProviderFromSettingKey(%q): provider = %q, want %q", tt.input, provider, tt.expectProvider)
		}
	}
}

func TestValidationError_Error(t *testing.T) {
	t.Parallel()
	e := ValidationError{Field: "model", Message: "cannot contain spaces", Value: "gpt 4"}
	errStr := e.Error()
	if !strings.Contains(errStr, "model") {
		t.Error("error should contain field name")
	}
	if !strings.Contains(errStr, "gpt 4") {
		t.Error("error should contain value")
	}

	e2 := ValidationError{Field: "apiKey", Message: "missing"}
	errStr2 := e2.Error()
	if strings.Contains(errStr2, "got:") {
		t.Error("error without value should not contain 'got:'")
	}
}

func TestValidationResult_Error(t *testing.T) {
	t.Parallel()
	r := ValidationResult{Valid: true}
	if r.Error() != "" {
		t.Error("valid result should have empty error string")
	}

	r2 := ValidationResult{
		Valid: false,
		Errors: []ValidationError{
			{Field: "model", Message: "invalid"},
			{Field: "budget", Message: "negative"},
		},
	}
	errStr := r2.Error()
	if !strings.Contains(errStr, "model") || !strings.Contains(errStr, "budget") {
		t.Error("error should contain all field names")
	}
}

func TestPolicySchemaVersionMigratesLegacySettings(t *testing.T) {
	var s Settings
	if err := json.Unmarshal([]byte(`{"autonomy":0}`), &s); err != nil {
		t.Fatal(err)
	}
	if s.PolicySchemaVersion != 0 {
		t.Fatalf("raw unmarshal should preserve legacy zero, got %d", s.PolicySchemaVersion)
	}
	merged := MergeSettings(Settings{}, s)
	if merged.PolicySchemaVersion != CurrentPolicySchemaVersion {
		t.Fatalf("migrated schema version = %d, want %d", merged.PolicySchemaVersion, CurrentPolicySchemaVersion)
	}
}
