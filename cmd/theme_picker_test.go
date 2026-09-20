package cmd

import (
	"strings"
	"testing"
)

func TestBuildThemeChoicesUsesRegistryOrderAndAutoEntry(t *testing.T) {
	choices := buildThemeChoices()
	if len(choices) != 21 {
		t.Fatalf("theme choices = %d, want 21 palettes plus auto", len(choices))
	}
	if choices[0].Name != "dark" || choices[len(choices)-1].Name != "auto" {
		t.Fatalf("theme choice bounds = %q ... %q", choices[0].Name, choices[len(choices)-1].Name)
	}
}

func TestThemePickerPaletteUsesSelectedTheme(t *testing.T) {
	dark := themePickerPalette("dark")
	tau := themePickerPalette("tau")
	if dark.Panel == tau.Panel || dark.Accent == tau.Accent {
		t.Fatal("theme picker palette did not change with selected theme")
	}
	if got := themePickerPalette("does-not-exist"); got.Panel != dark.Panel {
		t.Fatal("unknown theme should fall back to dark palette")
	}
	if preview := renderThemePreview("tau"); !strings.Contains(preview, "Panel:") {
		t.Fatalf("theme preview missing panel row: %q", preview)
	}
}

func TestThemePickerZeroValueIsUsable(t *testing.T) {
	picker := &ThemePicker{}
	picker.Open()
	if picker.Selected() == nil {
		t.Fatal("zero-value theme picker did not initialize a selection")
	}
	if view := picker.View(); view.Content == "" {
		t.Fatal("zero-value theme picker rendered empty content")
	}
}
