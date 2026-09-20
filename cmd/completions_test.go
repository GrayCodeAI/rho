package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/GrayCodeAI/flux/catalog/registry"
)

func TestNewCompletionGenerator(t *testing.T) {
	g := NewCompletionGenerator()
	if g == nil {
		t.Fatal("NewCompletionGenerator returned nil")
	}
	if len(g.Commands) == 0 {
		t.Error("Commands should not be empty")
	}
	if len(g.Flags) == 0 {
		t.Error("Flags should not be empty")
	}
	if len(g.SlashCommands) == 0 {
		t.Error("SlashCommands should not be empty")
	}
	if len(g.Models) == 0 {
		t.Error("Models should not be empty")
	}
	if len(g.Providers) == 0 {
		t.Error("Providers should not be empty")
	}
	generated := make(map[string]bool, len(g.SlashCommands))
	for _, name := range g.SlashCommands {
		generated[name] = true
	}
	for _, name := range slashCommands() {
		if !generated[name] {
			t.Fatalf("shell completion slash commands drifted from the dispatch catalog: missing %q", name)
		}
	}
}

func TestGenerateBashContainsFunctionDefinition(t *testing.T) {
	g := NewCompletionGenerator()
	bash := g.GenerateBash()

	if !strings.Contains(bash, "_rho_completions()") {
		t.Error("Bash completion should contain _rho_completions() function definition")
	}
	if !strings.Contains(bash, "complete -F _rho_completions rho") {
		t.Error("Bash completion should register the completion function with 'complete'")
	}
}

func TestGenerateBashContainsSubcommands(t *testing.T) {
	g := NewCompletionGenerator()
	bash := g.GenerateBash()

	expectedCmds := []string{"exec", "daemon", "mission", "search", "agent", "doctor", "config", "sessions", "tools", "skills"}
	for _, cmd := range expectedCmds {
		if !strings.Contains(bash, cmd) {
			t.Errorf("Bash completion should contain subcommand %q", cmd)
		}
	}
}

func TestGenerateBashContainsProviderChoices(t *testing.T) {
	g := NewCompletionGenerator()
	bash := g.GenerateBash()

	if !strings.Contains(bash, "--provider") {
		t.Error("Bash completion should contain --provider flag")
	}
	for _, p := range g.Providers {
		if !strings.Contains(bash, p) {
			t.Errorf("Bash completion should contain provider %q", p)
		}
	}
}

func TestGenerateBashContainsSlashCommands(t *testing.T) {
	g := NewCompletionGenerator()
	bash := g.GenerateBash()

	slashSamples := []string{"/help", "/model", "/config", "/cost", "/diff", "/commit", "/undo", "/focus", "/pin", "/compact", "/clear"}
	for _, sc := range slashSamples {
		if !strings.Contains(bash, sc) {
			t.Errorf("Bash completion should contain slash command %q", sc)
		}
	}
}

func TestGenerateBashFilePathCompletion(t *testing.T) {
	g := NewCompletionGenerator()
	bash := g.GenerateBash()

	if !strings.Contains(bash, "compgen -f") {
		t.Error("Bash completion should include file path completion (compgen -f)")
	}
	if !strings.Contains(bash, "compgen -d") {
		t.Error("Bash completion should include directory completion (compgen -d)")
	}
}

func TestGenerateZshContainsCompdefHeader(t *testing.T) {
	g := NewCompletionGenerator()
	zsh := g.GenerateZsh()

	if !strings.HasPrefix(zsh, "#compdef rho") {
		t.Error("Zsh completion should start with #compdef rho header")
	}
}

func TestGenerateZshContainsFunction(t *testing.T) {
	g := NewCompletionGenerator()
	zsh := g.GenerateZsh()

	if !strings.Contains(zsh, "_rho()") {
		t.Error("Zsh completion should contain _rho() function")
	}
	if !strings.Contains(zsh, "_arguments") {
		t.Error("Zsh completion should use _arguments for flag completion")
	}
	if !strings.Contains(zsh, "_describe") {
		t.Error("Zsh completion should use _describe for subcommand completion")
	}
}

func TestGenerateZshContainsSubcommands(t *testing.T) {
	g := NewCompletionGenerator()
	zsh := g.GenerateZsh()

	expectedCmds := []string{"exec", "daemon", "mission", "search", "agent", "doctor", "config", "sessions", "tools", "skills"}
	for _, cmd := range expectedCmds {
		if !strings.Contains(zsh, cmd) {
			t.Errorf("Zsh completion should contain subcommand %q", cmd)
		}
	}
}

func TestGenerateZshContainsProviders(t *testing.T) {
	g := NewCompletionGenerator()
	zsh := g.GenerateZsh()

	for _, p := range g.Providers {
		if !strings.Contains(zsh, p) {
			t.Errorf("Zsh completion should contain provider %q", p)
		}
	}
}

func TestGenerateZshContainsSlashCommands(t *testing.T) {
	g := NewCompletionGenerator()
	zsh := g.GenerateZsh()

	slashSamples := []string{"/help", "/model", "/config", "/cost", "/diff", "/commit"}
	for _, sc := range slashSamples {
		if !strings.Contains(zsh, sc) {
			t.Errorf("Zsh completion should contain slash command %q", sc)
		}
	}
}

func TestGenerateFishContainsCompleteDirectives(t *testing.T) {
	g := NewCompletionGenerator()
	fish := g.GenerateFish()

	if !strings.Contains(fish, "complete -c rho") {
		t.Error("Fish completion should contain 'complete -c rho' directives")
	}
}

func TestGenerateFishContainsSubcommands(t *testing.T) {
	g := NewCompletionGenerator()
	fish := g.GenerateFish()

	expectedCmds := []string{"exec", "daemon", "mission", "search", "agent", "doctor", "config", "sessions", "tools", "skills"}
	for _, cmd := range expectedCmds {
		if !strings.Contains(fish, cmd) {
			t.Errorf("Fish completion should contain subcommand %q", cmd)
		}
	}
}

func TestGenerateFishContainsProviders(t *testing.T) {
	g := NewCompletionGenerator()
	fish := g.GenerateFish()

	for _, p := range g.Providers {
		if !strings.Contains(fish, p) {
			t.Errorf("Fish completion should contain provider %q", p)
		}
	}
}

func TestGenerateFishContainsDescriptions(t *testing.T) {
	g := NewCompletionGenerator()
	fish := g.GenerateFish()

	// Each subcommand should have a description via -d flag
	if !strings.Contains(fish, "-d '") {
		t.Error("Fish completion should contain descriptions (-d flag)")
	}
}

func TestGenerateFishContainsSlashCommands(t *testing.T) {
	g := NewCompletionGenerator()
	fish := g.GenerateFish()

	slashSamples := []string{"/help", "/model", "/config", "/cost", "/diff", "/commit"}
	for _, sc := range slashSamples {
		if !strings.Contains(fish, sc) {
			t.Errorf("Fish completion should contain slash command %q", sc)
		}
	}
}

func TestGenerateJSONIsValidJSON(t *testing.T) {
	g := NewCompletionGenerator()
	jsonStr, err := g.GenerateJSON()
	if err != nil {
		t.Fatalf("GenerateJSON returned error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		t.Fatalf("GenerateJSON should produce valid JSON, got error: %v", err)
	}

	// Check required top-level fields
	requiredFields := []string{"name", "version", "commands", "global_flags", "slash_commands", "providers", "models"}
	for _, field := range requiredFields {
		if _, ok := result[field]; !ok {
			t.Errorf("JSON should contain field %q", field)
		}
	}
}

func TestGenerateJSONContainsCommands(t *testing.T) {
	g := NewCompletionGenerator()
	jsonStr, err := g.GenerateJSON()
	if err != nil {
		t.Fatalf("GenerateJSON returned error: %v", err)
	}

	var result struct {
		Commands []struct {
			Name string `json:"name"`
		} `json:"commands"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	cmdNames := make(map[string]bool)
	for _, cmd := range result.Commands {
		cmdNames[cmd.Name] = true
	}

	expected := []string{"exec", "daemon", "mission", "search", "agent", "doctor", "preflight", "path", "recover", "config", "sessions", "tools", "skills"}
	for _, name := range expected {
		if !cmdNames[name] {
			t.Errorf("JSON should contain command %q", name)
		}
	}
}

func TestGenerateJSONContainsProviders(t *testing.T) {
	g := NewCompletionGenerator()
	jsonStr, err := g.GenerateJSON()
	if err != nil {
		t.Fatalf("GenerateJSON returned error: %v", err)
	}

	var result struct {
		Providers []string `json:"providers"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	providerSet := make(map[string]bool)
	for _, p := range result.Providers {
		providerSet[p] = true
	}
	for _, p := range registry.All() {
		if !providerSet[p.ProviderID] {
			t.Errorf("JSON should contain provider %q", p.ProviderID)
		}
	}
}

func TestGenerateJSONContainsSlashCommands(t *testing.T) {
	g := NewCompletionGenerator()
	jsonStr, err := g.GenerateJSON()
	if err != nil {
		t.Fatalf("GenerateJSON returned error: %v", err)
	}

	var result struct {
		SlashCommands []string `json:"slash_commands"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if len(result.SlashCommands) < 50 {
		t.Errorf("JSON should contain many slash commands, got %d", len(result.SlashCommands))
	}

	scSet := make(map[string]bool)
	for _, sc := range result.SlashCommands {
		scSet[sc] = true
	}
	samples := []string{"/help", "/model", "/config", "/cost", "/diff", "/commit", "/undo", "/focus", "/pin", "/compact", "/clear"}
	for _, sc := range samples {
		if !scSet[sc] {
			t.Errorf("JSON should contain slash command %q", sc)
		}
	}
}

func TestInstallCompletionBash(t *testing.T) {
	path, err := InstallCompletion("bash")
	if err != nil {
		t.Fatalf("InstallCompletion(bash) returned error: %v", err)
	}
	if path == "" {
		t.Error("InstallCompletion(bash) returned empty path")
	}
	if !strings.Contains(path, "rho") {
		t.Errorf("Bash install path should contain 'rho', got %q", path)
	}
	// Should be a bash-related path
	if !strings.Contains(path, "bash") && !strings.Contains(path, "completion") {
		t.Errorf("Bash install path should be bash-related, got %q", path)
	}
}

func TestInstallCompletionZsh(t *testing.T) {
	path, err := InstallCompletion("zsh")
	if err != nil {
		t.Fatalf("InstallCompletion(zsh) returned error: %v", err)
	}
	if path == "" {
		t.Error("InstallCompletion(zsh) returned empty path")
	}
	if !strings.Contains(path, "_rho") {
		t.Errorf("Zsh install path should contain '_rho', got %q", path)
	}
}

func TestInstallCompletionFish(t *testing.T) {
	path, err := InstallCompletion("fish")
	if err != nil {
		t.Fatalf("InstallCompletion(fish) returned error: %v", err)
	}
	if path == "" {
		t.Error("InstallCompletion(fish) returned empty path")
	}
	if !strings.Contains(path, "rho.fish") {
		t.Errorf("Fish install path should contain 'rho.fish', got %q", path)
	}
	if !strings.Contains(path, "fish") {
		t.Errorf("Fish install path should be fish-related, got %q", path)
	}
}

func TestInstallCompletionUnsupportedShell(t *testing.T) {
	_, err := InstallCompletion("tcsh")
	if err == nil {
		t.Error("InstallCompletion should return error for unsupported shell")
	}
	if !strings.Contains(err.Error(), "unsupported shell") {
		t.Errorf("Error should mention 'unsupported shell', got: %v", err)
	}
}

func TestAllProvidersPresent(t *testing.T) {
	g := NewCompletionGenerator()

	providerSet := make(map[string]bool)
	for _, p := range g.Providers {
		providerSet[p] = true
	}
	for _, p := range registry.All() {
		if !providerSet[p.ProviderID] {
			t.Errorf("Provider %q should be present in CompletionGenerator.Providers", p.ProviderID)
		}
	}
}

func TestAllProvidersInBashCompletion(t *testing.T) {
	g := NewCompletionGenerator()
	bash := g.GenerateBash()

	for _, p := range registry.All() {
		if !strings.Contains(bash, p.ProviderID) {
			t.Errorf("Bash completion should contain provider %q", p.ProviderID)
		}
	}
}

func TestAllProvidersInZshCompletion(t *testing.T) {
	g := NewCompletionGenerator()
	zsh := g.GenerateZsh()

	for _, p := range registry.All() {
		if !strings.Contains(zsh, p.ProviderID) {
			t.Errorf("Zsh completion should contain provider %q", p.ProviderID)
		}
	}
}

func TestAllProvidersInFishCompletion(t *testing.T) {
	g := NewCompletionGenerator()
	fish := g.GenerateFish()

	for _, p := range registry.All() {
		if !strings.Contains(fish, p.ProviderID) {
			t.Errorf("Fish completion should contain provider %q", p.ProviderID)
		}
	}
}

func TestCompletionJSONCommand(t *testing.T) {
	preserveCLICompilerVersionState(t)
	SetVersion("test-version")

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"completion", "json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute() error = %v", err)
	}

	var result struct {
		Version  string `json:"version"`
		Commands []struct {
			Name string `json:"name"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("completion json output should be valid JSON: %v\n%s", err, buf.String())
	}
	if result.Version != "test-version" {
		t.Fatalf("completion json version = %q, want test-version", result.Version)
	}
	foundPath := false
	for _, cmd := range result.Commands {
		if cmd.Name == "path" {
			foundPath = true
			break
		}
	}
	if !foundPath {
		t.Fatal("completion json should include the path command")
	}
}

func TestGenerateBashProviderAfterFlag(t *testing.T) {
	g := NewCompletionGenerator()
	bash := g.GenerateBash()

	// Verify it completes providers specifically after --provider
	if !strings.Contains(bash, "case \"$prev\" in") {
		t.Error("Bash completion should have case statement for prev word")
	}
	if !strings.Contains(bash, "--provider)") {
		t.Error("Bash completion should have --provider case")
	}
}

func TestGenerateBashSlashCommandTrigger(t *testing.T) {
	g := NewCompletionGenerator()
	bash := g.GenerateBash()

	// Verify slash command completion is triggered by /
	if !strings.Contains(bash, `"$cur" == /*`) {
		t.Error("Bash completion should trigger slash command completion when cur starts with /")
	}
}

func TestCommandInfoHasDescriptions(t *testing.T) {
	g := NewCompletionGenerator()
	for _, cmd := range g.Commands {
		if cmd.Description == "" {
			t.Errorf("Command %q should have a description", cmd.Name)
		}
	}
}

func TestFlagInfoHasTypes(t *testing.T) {
	g := NewCompletionGenerator()
	validTypes := map[string]bool{"string": true, "bool": true, "int": true}
	for _, f := range g.Flags {
		if !validTypes[f.Type] {
			t.Errorf("Flag %q has invalid type %q (expected string, bool, or int)", f.Name, f.Type)
		}
	}
}

func TestSlashCommandsStartWithSlash(t *testing.T) {
	g := NewCompletionGenerator()
	for _, sc := range g.SlashCommands {
		if !strings.HasPrefix(sc, "/") {
			t.Errorf("Slash command %q should start with /", sc)
		}
	}
}

func TestGenerateZshProviderChoices(t *testing.T) {
	g := NewCompletionGenerator()
	zsh := g.GenerateZsh()

	// The provider completion function should list all providers
	if !strings.Contains(zsh, "_rho_providers()") {
		t.Error("Zsh completion should contain _rho_providers() function")
	}
}

func TestGenerateFishProviderFlag(t *testing.T) {
	g := NewCompletionGenerator()
	fish := g.GenerateFish()

	// Should have a dedicated provider completion line
	if !strings.Contains(fish, "provider") {
		t.Error("Fish completion should contain provider flag completion")
	}
}
