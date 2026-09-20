package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStorageDirsRespectOverrides(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "cfg")
	stateDir := filepath.Join(t.TempDir(), "state")
	cacheDir := filepath.Join(t.TempDir(), "cache")
	t.Setenv(envConfigDir, configDir)
	t.Setenv(envStateDir, stateDir)
	t.Setenv(envCacheDir, cacheDir)

	if got := ConfigDir(); got != configDir {
		t.Fatalf("ConfigDir() = %q, want override %q", got, configDir)
	}
	if got := StateDir(); got != stateDir {
		t.Fatalf("StateDir() = %q, want override %q", got, stateDir)
	}
	if got := CacheDir(); got != cacheDir {
		t.Fatalf("CacheDir() = %q, want override %q", got, cacheDir)
	}
}

func TestProviderConfigPathUsesFluxOverrideWithoutMovingRhoSettings(t *testing.T) {
	rhoDir := filepath.Join(t.TempDir(), "rho")
	fluxDir := filepath.Join(t.TempDir(), "flux")
	t.Setenv(envConfigDir, rhoDir)
	t.Setenv(envFluxConfigDir, fluxDir)

	if got, want := ProviderConfigPath(), filepath.Join(fluxDir, "provider.json"); got != want {
		t.Fatalf("ProviderConfigPath() = %q, want FLUX_CONFIG_DIR path %q", got, want)
	}
	if got, want := SettingsPath(), filepath.Join(rhoDir, "settings.json"); got != want {
		t.Fatalf("SettingsPath() = %q, want RHO_CONFIG_DIR path %q", got, want)
	}
}

func TestProviderConfigPathDefaultsToFluxDir(t *testing.T) {
	t.Setenv(envConfigDir, filepath.Join(t.TempDir(), "rho"))
	t.Setenv(envFluxConfigDir, "  ")

	if got, want := ProviderConfigPath(), filepath.Join(mustUserConfigDir(), "flux", "provider.json"); got != want {
		t.Fatalf("ProviderConfigPath() = %q, want Flux default %q", got, want)
	}
}

func TestProjectIDIsStableAndSafe(t *testing.T) {
	root := filepath.Join(t.TempDir(), "my project")

	first := ProjectID(root)
	second := ProjectID(root)
	if first != second {
		t.Fatalf("ProjectID not stable: %q != %q", first, second)
	}
	if !strings.HasPrefix(first, "my-project-") {
		t.Fatalf("ProjectID() = %q, want sanitized base prefix", first)
	}
	if strings.ContainsAny(first, string(filepath.Separator)+" ") {
		t.Fatalf("ProjectID() = %q, contains unsafe path characters", first)
	}
}

func TestProjectStateAndCacheUseHashedProjectRoot(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	cache := filepath.Join(t.TempDir(), "cache")
	project := filepath.Join(t.TempDir(), "repo")
	t.Setenv(envStateDir, state)
	t.Setenv(envCacheDir, cache)

	id := ProjectID(project)
	if got := ProjectStateDir(project); got != filepath.Join(state, "projects", id) {
		t.Fatalf("ProjectStateDir() = %q", got)
	}
	if got := ProjectCacheDir(project); got != filepath.Join(cache, "projects", id) {
		t.Fatalf("ProjectCacheDir() = %q", got)
	}
}

func TestLegacyEnvOverridesStillResolve(t *testing.T) {
	// A pre-rename install set HAWK_CONFIG_DIR; the new build must honor it.
	legacyCfg := filepath.Join(t.TempDir(), "legacy-cfg")
	t.Setenv(envConfigDir, "")
	t.Setenv("HAWK_CONFIG_DIR", legacyCfg)
	if got := ConfigDir(); got != legacyCfg {
		t.Fatalf("ConfigDir() = %q, want legacy override %q", got, legacyCfg)
	}
}

func TestResolveAppDirIgnoresLegacyDirectory(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, legacyAppName)
	if err := os.MkdirAll(legacy, 0o750); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, appName)
	if got := resolveAppDir(root); got != want {
		t.Fatalf("resolveAppDir = %q, want current dir %q", got, want)
	}
}

func TestResolveAppDirPrefersCurrentWhenPresent(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, appName)
	if err := os.MkdirAll(current, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, legacyAppName), 0o750); err != nil {
		t.Fatal(err)
	}
	if got := resolveAppDir(root); got != current {
		t.Fatalf("resolveAppDir = %q, want current dir %q", got, current)
	}
}
