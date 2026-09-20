// Package feature provides a minimal feature-flag system for runtime
// configuration of experimental or gated rho capabilities.
//
// Flags are registered at startup (by packages that own the feature) and read
// from the environment at init time, so no config file is required. The
// canonical environment variable form is RHO_FEATURE_<NAME> (uppercased,
// hyphens replaced with underscores). A value of "1" or "true" enables the
// flag; any other value leaves it at its default.
//
// Typical usage:
//
//	// init() registers the flag with its default and description.
//	var NewUX = feature.Register("new-ux", false, "Enable the experimental UX")
//
//	// ...in code:
//	if feature.Enabled(NewUX) {
//	    useNewUX()
//	}
package feature

import (
	"os"
	"strings"
	"sync"
)

// Flag is a handle to a registered feature flag. It is safe for concurrent
// use — the underlying store uses a RWMutex.
type Flag struct {
	name       string
	defaultVal bool
	desc       string
}

// Manager holds all registered feature flags and their resolved values.
type Manager struct {
	mu       sync.RWMutex
	values   map[string]bool
	flagInfo map[string]*flagInfo
}

// global is the singleton manager used by the package-level API.
var global = &Manager{
	values:   make(map[string]bool),
	flagInfo: make(map[string]*flagInfo),
}

type flagInfo struct {
	flag  *Flag
	value bool
}

// Register adds a feature flag with the given name, default value, and
// description to the global manager. The returned *Flag is a handle that can
// be passed to Enabled() later.
//
// Registration is idempotent: calling Register with the same name twice
// returns the existing flag without error. The first registration wins for
// the default value and description.
func Register(name string, defaultVal bool, desc string) *Flag {
	global.mu.Lock()
	defer global.mu.Unlock()

	key := normalizeKey(name)
	if info, exists := global.flagInfo[key]; exists {
		return info.flag
	}
	f := &Flag{name: name, defaultVal: defaultVal, desc: desc}
	global.flagInfo[key] = &flagInfo{flag: f, value: defaultVal}
	global.values[key] = defaultVal

	// Override from environment: RHO_FEATURE_<NAME>=1 enables.
	envVar := "RHO_FEATURE_" + strings.ReplaceAll(strings.ToUpper(key), "-", "_")
	if raw := os.Getenv(envVar); raw != "" {
		switch strings.ToLower(raw) {
		case "1", "true", "yes", "on":
			global.values[key] = true
		case "0", "false", "no", "off":
			global.values[key] = false
		}
	}

	return f
}

// Enabled reports whether the given flag is currently enabled.
// Returns false for unregistered flags.
func Enabled(f *Flag) bool {
	if f == nil {
		return false
	}
	return global.isEnabled(f)
}

// Name returns the flag's name.
func (f *Flag) Name() string {
	if f == nil {
		return ""
	}
	return f.name
}

// DefaultValue returns the flag's default value.
func (f *Flag) DefaultValue() bool {
	if f == nil {
		return false
	}
	return f.defaultVal
}

// Description returns the flag's human-readable description.
func (f *Flag) Description() string {
	if f == nil {
		return ""
	}
	return f.desc
}

// EnabledByName looks up a flag by name and reports whether it is enabled.
// Returns false for unregistered flags.
func EnabledByName(name string) bool {
	global.mu.RLock()
	defer global.mu.RUnlock()
	key := normalizeKey(name)
	if v, ok := global.values[key]; ok {
		return v
	}
	return false
}

func (m *Manager) isEnabled(f *Flag) bool {
	global.mu.RLock()
	defer global.mu.RUnlock()
	key := normalizeKey(f.name)
	if v, ok := global.values[key]; ok {
		return v
	}
	return f.defaultVal
}

// Set overrides a flag's value at runtime (e.g., for testing or dynamic
// reconfiguration). Returns false if the flag was not registered.
func Set(name string, val bool) bool {
	global.mu.Lock()
	defer global.mu.Unlock()
	key := normalizeKey(name)
	if _, ok := global.flagInfo[key]; !ok {
		return false
	}
	global.values[key] = val
	return true
}

// List returns all registered flags and their current values.
func List() map[string]bool {
	global.mu.RLock()
	defer global.mu.RUnlock()
	out := make(map[string]bool, len(global.values))
	for k, v := range global.values {
		out[k] = v
	}
	return out
}

// Info returns metadata about a registered flag, or false if unregistered.
func Info(name string) (*Flag, bool) {
	global.mu.RLock()
	defer global.mu.RUnlock()
	key := normalizeKey(name)
	if info, ok := global.flagInfo[key]; ok {
		return info.flag, true
	}
	return nil, false
}

// normalizeKey lowercases the flag name for case-insensitive lookup.
func normalizeKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
