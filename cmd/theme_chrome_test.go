package cmd

import "testing"

// These tests call ApplyTheme, which mutates package-level color vars. Snapshot
// and restore them with restoreThemeGlobals so a palette swap cannot leak into
// unrelated tests (e.g. TestAdaptiveNeutralsPreserveDarkAppearance). Calling
// ApplyTheme("dark") as cleanup is not equivalent: it writes the dark palette's
// values, which differ from the init-time defaults the adaptive test locks.

func TestApplyThemeTauEnablesMinimalChrome(t *testing.T) {
	restoreThemeGlobals(t)
	prev := minimalChrome
	t.Cleanup(func() { minimalChrome = prev })

	ApplyTheme("tau")
	if !minimalChrome {
		t.Fatal("ApplyTheme(tau) should enable minimal chrome")
	}
	// Tau keeps only the bottom hairline: no top rule, no side rules.
	if inputBorderStyle.GetBorderTop() || inputBorderStyle.GetBorderLeft() || inputBorderStyle.GetBorderRight() {
		t.Error("tau input border should draw only the bottom hairline")
	}
	if !inputBorderStyle.GetBorderBottom() {
		t.Error("tau input border should keep the bottom hairline")
	}
}

func TestApplyThemeDarkUsesBoxChrome(t *testing.T) {
	restoreThemeGlobals(t)
	prev := minimalChrome
	t.Cleanup(func() { minimalChrome = prev })

	ApplyTheme("dark")
	if minimalChrome {
		t.Fatal("ApplyTheme(dark) should not enable minimal chrome")
	}
	// The default chrome draws a top and bottom rule around the input.
	if !inputBorderStyle.GetBorderTop() || !inputBorderStyle.GetBorderBottom() {
		t.Error("dark input border should keep top and bottom rules")
	}
}

func TestApplyThemeUnknownIsNoop(t *testing.T) {
	restoreThemeGlobals(t)
	ApplyTheme("dark")
	before := minimalChrome
	ApplyTheme("does-not-exist")
	if minimalChrome != before {
		t.Error("ApplyTheme with an unknown name must not change state")
	}
}

func TestApplyThemeRefreshesStatusBarPalette(t *testing.T) {
	restoreThemeGlobals(t)

	ApplyTheme("tau")
	if statusCWDColor != cwdBlue {
		t.Fatalf("status cwd color = %v, want live cwd color %v", statusCWDColor, cwdBlue)
	}
}

func TestApplyThemeRefreshesRawDiffPalette(t *testing.T) {
	restoreThemeGlobals(t)
	before := ansiDone
	ApplyTheme("dracula")
	if ansiDone == before {
		t.Fatal("theme switch should update raw ANSI diff colors")
	}
	if got := DefaultDiffTheme().Added; got != ansiDone {
		t.Fatalf("diff theme added color = %q, want active ANSI color %q", got, ansiDone)
	}
}
