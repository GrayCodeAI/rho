package feature

import (
	"os"
	"testing"
)

func TestRegisterAndGet(t *testing.T) {
	f := Register("test-flag-1", false, "a test flag")
	if f == nil {
		t.Fatal("expected non-nil flag")
	}
	if Enabled(f) {
		t.Error("expected flag disabled by default")
	}
}

func TestRegisterIdempotent(t *testing.T) {
	f1 := Register("test-flag-2", false, "first registration")
	f2 := Register("test-flag-2", true, "duplicate registration")
	if f1 != f2 {
		t.Error("expected same flag handle for duplicate registration")
	}
	// First registration wins for the default.
	if f1.defaultVal != false {
		t.Error("expected first registration default to win")
	}
}

func TestEnabledByName(t *testing.T) {
	Register("test-flag-3", true, "enabled by default")
	if !EnabledByName("test-flag-3") {
		t.Error("expected test-flag-3 enabled")
	}
	if EnabledByName("nonexistent-flag") {
		t.Error("expected nonexistent flag to be disabled")
	}
}

func TestSet(t *testing.T) {
	f := Register("test-flag-4", false, "settable flag")
	if Enabled(f) {
		t.Error("expected disabled initially")
	}
	if !Set("test-flag-4", true) {
		t.Fatal("Set should return true for registered flag")
	}
	if !Enabled(f) {
		t.Error("expected enabled after Set")
	}
	if !Set("test-flag-4", false) {
		t.Fatal("Set should return true")
	}
	if Enabled(f) {
		t.Error("expected disabled after Set(false)")
	}
	// Set on unregistered flag returns false.
	if Set("nonexistent", true) {
		t.Error("Set should return false for unregistered flag")
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("RHO_FEATURE_ENV_OVERRIDE_TEST", "1")
	// Reset the global manager to pick up the env var.
	// Since Register is idempotent, we need to use a fresh flag name.
	f := Register("env-override-test", false, "env override test")
	if !Enabled(f) {
		t.Error("expected flag enabled by env override RHO_ENABLE_TELEMETRY=1")
	}
}

func TestEnvOverrideFalse(t *testing.T) {
	t.Setenv("RHO_FEATURE_ENV_OVERRIDE_FALSE", "false")
	f := Register("env-override-false", true, "env override false test")
	if Enabled(f) {
		t.Error("expected flag disabled by env override to false")
	}
}

func TestEnvOverrideTrue(t *testing.T) {
	t.Setenv("RHO_FEATURE_ENV_OVERRIDE_TRUE", "true")
	f := Register("env-override-true", false, "env override true test")
	if !Enabled(f) {
		t.Error("expected flag enabled by env override to true")
	}
}

func TestList(t *testing.T) {
	Register("list-test-1", true, "test")
	Register("list-test-2", false, "test")
	flags := List()
	if len(flags) < 2 {
		t.Errorf("expected at least 2 flags, got %d", len(flags))
	}
}

func TestInfo(t *testing.T) {
	f := Register("info-test", true, "info test flag")
	got, ok := Info("info-test")
	if !ok {
		t.Fatal("expected flag to be found")
	}
	if got != f {
		t.Error("expected same flag handle")
	}
	if got.name != "info-test" {
		t.Errorf("expected name 'info-test', got %q", got.name)
	}
	if got.desc != "info test flag" {
		t.Errorf("expected desc 'info test flag', got %q", got.desc)
	}

	_, ok = Info("nonexistent-info")
	if ok {
		t.Error("expected false for unregistered flag")
	}
}

func TestEnabledNil(t *testing.T) {
	if Enabled(nil) {
		t.Error("expected nil flag to return false")
	}
}

func TestNormalizeKey(t *testing.T) {
	if normalizeKey("  My-Flag  ") != "my-flag" {
		t.Error("expected normalized key to be lowercase and trimmed")
	}
}

func TestDefaultDaemonFlags(t *testing.T) {
	// Ensure the default daemon feature flags are registered and have
	// sensible defaults.
	if !Enabled(MetricsEndpoint) {
		t.Error("expected MetricsEndpoint to be enabled by default")
	}
	if !Enabled(SecurityHeaders) {
		t.Error("expected SecurityHeaders to be enabled by default")
	}
	if !Enabled(AuditLog) {
		t.Error("expected AuditLog to be enabled by default")
	}
	if Enabled(CORS) {
		t.Error("expected CORS to be disabled by default")
	}
}

func init() {
	// Ensure no stale env vars from other tests interfere.
	os.Unsetenv("RHO_FEATURE_TELEMETRY_OTEL")
}
