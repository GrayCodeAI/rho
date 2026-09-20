// auto_detect.go — Detect OS appearance for auto-theme.
//
// Supports macOS, Linux (XDG), and Windows.

package theme

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// AutoThemeConfig holds auto-theme settings.
type AutoThemeConfig struct {
	Enabled    bool
	DarkTheme  string
	LightTheme string
}

// DetectOSTheme returns the OS-level appearance preference.
// Returns "dark", "light", or empty string if undetermined.
func DetectOSTheme() string {
	switch runtime.GOOS {
	case "darwin":
		return detectMacOSTheme()
	case "linux":
		return detectLinuxTheme()
	case "windows":
		return detectWindowsTheme()
	default:
		return ""
	}
}

// detectMacOSTheme uses the system preference.
func detectMacOSTheme() string {
	cmd := exec.Command("sh", "-c", "defaults read -g AppleInterfaceStyle 2>/dev/null")
	out, err := cmd.Output()
	if err != nil {
		return "" // Default/no preference
	}
	if strings.TrimSpace(string(out)) == "Dark" {
		return "dark"
	}
	return "light"
}

// detectLinuxTheme uses XDG Desktop Portal or env vars.
func detectLinuxTheme() string {
	// Try org.freedesktop.portal.Settings via dbus
	if portal := readPortalSetting(); portal != "" {
		return portal
	}
	// Fallback: check GTK_THEME
	if gtk := os.Getenv("GTK_THEME"); gtk != "" {
		if strings.Contains(strings.ToLower(gtk), "dark") {
			return "dark"
		}
	}
	return ""
}

// detectWindowsTheme reads system registry.
func detectWindowsTheme() string {
	cmd := exec.Command("powershell", "-Command", `(Get-ItemProperty -Path 'HKCU:\\Control Panel\\Personal' -Name 'AppsUseLightTheme' -ErrorAction SilentlyContinue).AppsUseLightTheme`)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	if strings.TrimSpace(string(out)) == "0" {
		return "dark"
	}
	return "light"
}

// readPortalSetting attempts to read via D-Bus.
func readPortalSetting() string {
	// Full portal implementation would use dbus bindings
	// For now, check flatpak portal env
	if portal := os.Getenv("XDG_CURRENT_DESKTOP"); portal != "" {
		// Could use dbus to query org.freedesktop.portal.Settings
	}
	return ""
}

// WatchOSTheme monitors for OS appearance changes and invokes cb with the
// detected theme ("dark"/"light") on each poll tick. It runs until ctx is
// cancelled; the ticker is stopped and the goroutine exits on cancellation,
// so callers can start and stop the watcher without leaking the ticker.
func WatchOSTheme(ctx context.Context, cb func(theme string)) {
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current := DetectOSTheme()
				if current != "" && cb != nil {
					cb(current)
				}
			}
		}
	}()
}

// ApplyThemePreference decides which theme to apply based on the preference.
// It returns the theme name that should be applied (e.g., "dark", "light", or a specific theme name).
// If preference is "auto", it detects the OS theme preference and returns "dark" or "light".
// If detection fails, it returns "dark" as the default.
func ApplyThemePreference(preference string) string {
	preference = strings.ToLower(strings.TrimSpace(preference))
	if preference == "auto" || preference == "system" {
		detected := DetectOSTheme()
		if detected != "" {
			return detected
		}
		// Default to dark if detection fails
		return "dark"
	}
	// If preference is "dark" or "light", return as-is
	if preference == "dark" || preference == "light" {
		return preference
	}
	// Otherwise return the specific theme name (or empty if unknown)
	return preference
}

// ResolveAutoTheme returns the theme name to actually apply when auto-detection is enabled.
// It takes the saved preference and returns the resolved theme name.
// If darkThemeOverride or lightThemeOverride are set, they are used instead of defaults.
func ResolveAutoTheme(preference, darkThemeOverride, lightThemeOverride string) string {
	resolved := ApplyThemePreference(preference)

	// If we got "dark" or "light" and there's an override, apply it
	if resolved == "dark" && darkThemeOverride != "" {
		return darkThemeOverride
	}
	if resolved == "light" && lightThemeOverride != "" {
		return lightThemeOverride
	}

	return resolved
}
