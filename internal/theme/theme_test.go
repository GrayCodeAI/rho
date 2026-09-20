package theme

import (
	"strconv"
	"strings"
	"testing"
)

// TestThemeRegistryNotEmpty ensures all themes are registered.
func TestThemeRegistryNotEmpty(t *testing.T) {
	if len(themeRegistry) == 0 {
		t.Fatal("themeRegistry should not be empty")
	}
}

// TestThemeRegistryCount ensures the registry matches the expected theme set.
func TestThemeRegistryCount(t *testing.T) {
	if len(themeRegistry) != 20 {
		t.Errorf("themeRegistry has %d themes, want 20", len(themeRegistry))
	}
}

// TestDarkPaletteDefault ensures the dark palette is the default.
func TestDarkPaletteDefault(t *testing.T) {
	entry := themeRegistry[0]
	if entry.Name != "dark" {
		t.Errorf("first theme should be 'dark', got %q", entry.Name)
	}
	if !entry.IsDark {
		t.Error("dark theme should have IsDark=true")
	}
}

// TestThemeByNameExists ensures ThemeByName works.
func TestThemeByNameExists(t *testing.T) {
	m := ThemeByName()
	if m == nil {
		t.Fatal("ThemeByName should not return nil")
	}
	if _, ok := m["dark"]; !ok {
		t.Error("ThemeByName should contain 'dark' theme")
	}
	delete(m, "dark")
	if _, ok := ThemeByName()["dark"]; !ok {
		t.Error("ThemeByName must return an isolated copy")
	}
}

// TestGetThemeEntryExists ensures GetThemeEntry works.
func TestGetThemeEntryExists(t *testing.T) {
	entry := GetThemeEntry("dark")
	if entry.Name != "dark" {
		t.Errorf("GetThemeEntry('dark') returned wrong name: %q", entry.Name)
	}
	if !entry.IsDark {
		t.Error("dark theme should have IsDark=true")
	}
}

// TestLookupThemeExists ensures LookupTheme works.
func TestLookupThemeExists(t *testing.T) {
	entry, ok := LookupTheme("dark")
	if !ok {
		t.Error("LookupTheme('dark') should return ok=true")
	}
	if entry.Name != "dark" {
		t.Errorf("LookupTheme('dark') returned wrong name: %q", entry.Name)
	}
}

// TestLookupThemeNormalizesInput ensures picker/config input is forgiving.
func TestLookupThemeNormalizesInput(t *testing.T) {
	for _, input := range []string{"dark", " DARK ", "Dark"} {
		entry, ok := LookupTheme(input)
		if !ok {
			t.Errorf("LookupTheme(%q) should return ok=true", input)
		}
		if entry.Name != "dark" {
			t.Errorf("LookupTheme(%q) returned wrong name: %q", input, entry.Name)
		}
	}
}

func TestApplyThemePreferenceNormalizesInput(t *testing.T) {
	if got := ApplyThemePreference("  DARK "); got != "dark" {
		t.Fatalf("ApplyThemePreference normalized result = %q, want dark", got)
	}
}

// TestLookupThemeNotFound ensures LookupTheme returns ok=false for unknown themes.
func TestLookupThemeNotFound(t *testing.T) {
	_, ok := LookupTheme("nonexistent")
	if ok {
		t.Error("LookupTheme('nonexistent') should return ok=false")
	}
}

// TestThemeNames ensures ThemeNames returns all theme names.
func TestThemeNames(t *testing.T) {
	names := ThemeNames()
	if len(names) != 20 {
		t.Errorf("ThemeNames returned %d names, want 20", len(names))
	}
	// Check all expected themes are present
	seen := make(map[string]bool)
	for _, name := range names {
		seen[name] = true
	}
	expected := []string{
		"dark", "dracula", "nord", "gruvbox", "tokyo-night",
		"catppuccin", "one-dark", "solarized-dark", "rose-pine", "everforest",
		"monokai", "kanagawa", "ayu", "palenight", "github-dark",
		"github-light", "light", "solarized-light", "minimal", "tau",
	}
	for _, exp := range expected {
		if !seen[exp] {
			t.Errorf("expected theme %q not found in ThemeNames", exp)
		}
	}
}

// TestIsDarkTheme ensures IsDarkTheme works.
func TestIsDarkTheme(t *testing.T) {
	if !IsDarkTheme("dark") {
		t.Error("IsDarkTheme('dark') should return true")
	}
	if IsDarkTheme("light") {
		t.Error("IsDarkTheme('light') should return false")
	}
}

// TestDarkPaletteHasValidColors ensures the dark palette has valid hex colors.
func TestDarkPaletteHasValidColors(t *testing.T) {
	p := darkPalette
	if p.Panel == "" {
		t.Error("dark palette Panel should not be empty")
	}
	if !strings.HasPrefix(p.Panel, "#") {
		t.Errorf("dark palette Panel %q should start with #", p.Panel)
	}
}

// TestAllThemesHaveValidPalette ensures all themes have valid hex colors.
func TestAllThemesHaveValidPalette(t *testing.T) {
	for _, entry := range themeRegistry {
		p := entry.Palette
		// Check required fields are non-empty
		fields := []string{p.Panel, p.PromptBg, p.Line, p.Ink, p.Accent, p.Green}
		for _, field := range fields {
			if field == "" {
				t.Errorf("theme %q has empty palette field", entry.Name)
			}
			// Check it starts with #
			if !strings.HasPrefix(field, "#") {
				t.Errorf("theme %q palette field %q should start with #", entry.Name, field)
			}
			// Check it's a valid hex color (7 chars)
			if len(field) != 7 {
				t.Errorf("theme %q palette field %q should be 7 chars (e.g., #RRGGBB), got %d", entry.Name, field, len(field))
			}
		}
	}
}

func TestAllThemesMeetCoreContrastInvariants(t *testing.T) {
	for _, entry := range themeRegistry {
		p := entry.Palette
		if ratio := contrastRatio(p.Ink, p.Panel); ratio < 4.5 {
			t.Errorf("theme %q ink/panel contrast %.2f is below 4.5", entry.Name, ratio)
		}
		if ratio := contrastRatio(p.OnAccent, p.Accent); ratio < 4.5 {
			t.Errorf("theme %q on-accent/accent contrast %.2f is below 4.5", entry.Name, ratio)
		}
		if ratio := contrastRatio(p.AddInk, p.AddBg); ratio < 3 {
			t.Errorf("theme %q add-ink/add-bg contrast %.2f is below 3", entry.Name, ratio)
		}
		if ratio := contrastRatio(p.DelInk, p.DelBg); ratio < 3 {
			t.Errorf("theme %q del-ink/del-bg contrast %.2f is below 3", entry.Name, ratio)
		}
	}
}

func contrastRatio(foreground, background string) float64 {
	foregroundLuminance := relativeLuminance(foreground)
	backgroundLuminance := relativeLuminance(background)
	if foregroundLuminance < backgroundLuminance {
		foregroundLuminance, backgroundLuminance = backgroundLuminance, foregroundLuminance
	}
	return (foregroundLuminance + 0.05) / (backgroundLuminance + 0.05)
}

func relativeLuminance(hexColor string) float64 {
	parse := func(pair string) float64 {
		value, _ := strconv.ParseUint(pair, 16, 8)
		channel := float64(value) / 255
		if channel <= 0.03928 {
			return channel / 12.92
		}
		return ((channel + 0.055) / 1.055) * ((channel + 0.055) / 1.055)
	}
	return 0.2126*parse(hexColor[1:3]) + 0.7152*parse(hexColor[3:5]) + 0.0722*parse(hexColor[5:7])
}
