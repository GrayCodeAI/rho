package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

const (
	appName          = "rho"
	legacyAppName    = "hawk"
	envConfigDir     = "RHO_CONFIG_DIR"
	envFluxConfigDir = "FLUX_CONFIG_DIR"
	envStateDir      = "RHO_STATE_DIR"
	envCacheDir      = "RHO_CACHE_DIR"
	projectIDHashLen = 12
)

// legacyEnvDir reads a legacy HAWK_* override (e.g. HAWK_STATE_DIR) so an
// existing Hawk install keeps resolving to the same location after the rename.
func legacyEnvDir(legacyKey string) string {
	return strings.TrimSpace(os.Getenv(legacyKey))
}

// resolveAppDir returns the current Rho app directory under root. Legacy
// environment-variable overrides remain supported explicitly, but an
// auto-detected Hawk directory must not silently become Rho's active state:
// it produces confusing paths and can be unwritable after the rebrand.
func resolveAppDir(root string) string {
	return filepath.Join(root, appName)
}

// ConfigDir returns the per-user configuration directory for Rho.
func ConfigDir() string {
	if dir := cleanEnvDir(envConfigDir); dir != "" {
		return dir
	}
	if dir := legacyEnvDir("HAWK_CONFIG_DIR"); dir != "" {
		return dir
	}
	return resolveAppDir(mustUserConfigDir())
}

// StateDir returns the per-user state directory for durable runtime data.
func StateDir() string {
	if dir := cleanEnvDir(envStateDir); dir != "" {
		return dir
	}
	if dir := legacyEnvDir("HAWK_STATE_DIR"); dir != "" {
		return dir
	}
	if dir := cleanEnvDir("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, appName)
	}
	// State lives under the config root in both the current and legacy layout,
	// so resolve the app dir once and append "state".
	return filepath.Join(resolveAppDir(mustUserConfigDir()), "state")
}

// CacheDir returns the per-user cache directory for disposable data.
func CacheDir() string {
	if dir := cleanEnvDir(envCacheDir); dir != "" {
		return dir
	}
	if dir := legacyEnvDir("HAWK_CACHE_DIR"); dir != "" {
		return dir
	}
	return resolveAppDir(mustUserCacheDir())
}

func SettingsPath() string {
	return filepath.Join(ConfigDir(), "settings.json")
}

func ProviderConfigPath() string {
	// Flux owns provider routing state and resolves it from FLUX_CONFIG_DIR,
	// defaulting to its own directory under the user config root.
	if dir := cleanEnvDir(envFluxConfigDir); dir != "" {
		return filepath.Join(dir, "provider.json")
	}
	return filepath.Join(mustUserConfigDir(), "flux", "provider.json")
}

func SessionsDir() string {
	return filepath.Join(StateDir(), "sessions")
}

func PlansDir(projectRoot string) string {
	return filepath.Join(ProjectStateDir(projectRoot), "plans")
}

func DaemonRunDir() string {
	return filepath.Join(StateDir(), "run")
}

func WorkspaceSnapshotsDir() string {
	return filepath.Join(StateDir(), "snapshots")
}

func PersonasDir() string {
	return filepath.Join(StateDir(), "agents")
}

func TasteDir() string {
	return filepath.Join(StateDir(), "taste")
}

func RepoMapCacheDir(projectRoot string) string {
	return filepath.Join(ProjectCacheDir(projectRoot), "repomap")
}

func ProjectStateDir(projectRoot string) string {
	return filepath.Join(StateDir(), "projects", ProjectID(projectRoot))
}

func ProjectCacheDir(projectRoot string) string {
	return filepath.Join(CacheDir(), "projects", ProjectID(projectRoot))
}

// ProjectID returns a stable, filesystem-safe identifier for a project path.
func ProjectID(projectRoot string) string {
	if projectRoot == "" {
		projectRoot = "."
	}
	abs, err := filepath.Abs(projectRoot)
	if err == nil {
		projectRoot = abs
	}
	projectRoot = filepath.Clean(projectRoot)
	base := filepath.Base(projectRoot)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "project"
	}
	base = sanitizeName(base)
	sum := sha256.Sum256([]byte(projectRoot))
	return base + "-" + hex.EncodeToString(sum[:])[:projectIDHashLen]
}

func cleanEnvDir(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func mustUserConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		// Never crash the CLI at init because the environment is broken
		// (e.g. unset HOME in a cron/daemon context). Fall back to a
		// stable, writable location under the OS temp dir so the process
		// still functions; the effective paths are also overridable via
		// RHO_CONFIG_DIR / RHO_STATE_DIR / RHO_CACHE_DIR.
		return filepath.Join(os.TempDir(), "rho-config")
	}
	return dir
}

func mustUserCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		return filepath.Join(os.TempDir(), "rho-cache")
	}
	return dir
}

func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	out := strings.Trim(b.String(), ".-")
	if out == "" {
		return "project"
	}
	return out
}
