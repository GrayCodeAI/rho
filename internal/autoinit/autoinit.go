// Package autoinit performs a one-time, automatic codebase-analysis pass the
// first time rho runs in a project that has no context files (AGENTS.md /
// RHO.md / CONTEXT.md). It mirrors the behaviour of the `init-deep` skill but
// is gated so it runs at most once per project and can be disabled entirely.
//
// The package is intentionally additive and self-contained: it only inspects
// the filesystem to decide whether to run, writes a marker file once a run has
// been attempted, and delegates the actual analysis to a caller-supplied
// runner. Callers (the cmd layer) wire the runner to whatever drives the
// init analysis (the init-deep skill / `rho init`). When no runner is wired,
// MaybeRun is a no-op beyond gating, so importing the package never changes
// behaviour on its own.
package autoinit

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/GrayCodeAI/rho/internal/config"
	"github.com/GrayCodeAI/rho/internal/storage"
)

// markerName is the file written under the project's user-state directory once an
// auto-init run has been attempted. Its presence (regardless of run outcome)
// prevents repeated attempts on subsequent invocations.
const markerName = "auto-init.done"

// disableEnv, when set to a truthy value ("1", "true", "yes", "on"), disables
// auto-init globally. This is the kill switch for users/CI that never want the
// behaviour.
const disableEnv = "RHO_DISABLE_AUTO_INIT"

// contextFiles are the project-level context files whose presence means the
// project already has context and auto-init should be skipped. This matches
// the convention files recognized elsewhere in rho.
var contextFiles = []string{"AGENTS.md", "RHO.md", "CONTEXT.md"}

// Runner performs the actual codebase analysis for a project rooted at root.
// It is supplied by the caller so this package carries no dependency on the
// engine, skills, or cmd layers.
type Runner func(ctx context.Context, root string) error

// Options controls a MaybeRun invocation.
type Options struct {
	// Root is the project directory to analyze. Required.
	Root string
	// Run performs the analysis. When nil, MaybeRun gates and writes the
	// marker (if it would have run) but performs no analysis.
	Run Runner
	// Force ignores the "already has context" check but still respects the
	// marker file and the disable env var. Useful for an explicit re-init.
	Force bool
	// disableEnvValue overrides the environment lookup; used by tests. When
	// empty, the real environment is consulted.
	disableEnvValue string
}

// Decision reports what MaybeRun did, for logging and tests.
type Decision struct {
	// Ran is true when the runner was invoked.
	Ran bool
	// Skipped is a short reason when the runner was not invoked.
	Skipped string
}

// Disabled reports whether auto-init is disabled via the environment.
func Disabled() bool {
	return isTruthy(config.Getenv(disableEnv))
}

// HasContext reports whether root already contains a project context file.
func HasContext(root string) bool {
	for _, name := range contextFiles {
		if fileExists(filepath.Join(root, name)) {
			return true
		}
	}
	return false
}

// MarkerPath returns the path to the auto-init marker file for root.
func MarkerPath(root string) string {
	return filepath.Join(storage.ProjectStateDir(root), markerName)
}

// HasRun reports whether auto-init has already been attempted for root.
func HasRun(root string) bool {
	return fileExists(MarkerPath(root))
}

// MaybeRun runs the auto-init analysis at most once for opts.Root, subject to
// the gating rules:
//
//  1. Disabled via RHO_DISABLE_AUTO_INIT  -> skip.
//  2. Marker file already present          -> skip.
//  3. Project already has a context file   -> mark + skip (unless Force).
//  4. Otherwise                            -> run, then mark.
//
// The marker is written whenever a run is attempted (even on runner error) so
// a failing analysis is not retried on every invocation. Marker write failures
// are returned before running because otherwise auto-init can retry forever.
func MaybeRun(ctx context.Context, opts Options) (Decision, error) {
	if opts.Root == "" {
		return Decision{Skipped: "no project root"}, nil
	}

	disabled := opts.disableEnvValue
	if disabled == "" {
		disabled = config.Getenv(disableEnv)
	}
	if isTruthy(disabled) {
		return Decision{Skipped: "disabled via " + disableEnv}, nil
	}

	if HasRun(opts.Root) {
		return Decision{Skipped: "already attempted (marker present)"}, nil
	}

	if !opts.Force && HasContext(opts.Root) {
		// Project already has context: record the marker so we never probe
		// again, and skip the analysis.
		if err := writeMarker(opts.Root); err != nil {
			return Decision{Skipped: "project already has context"}, err
		}
		return Decision{Skipped: "project already has context"}, nil
	}

	// Write the marker before running so a crash mid-analysis does not cause a
	// retry storm on every subsequent invocation.
	if err := writeMarker(opts.Root); err != nil {
		return Decision{Skipped: "marker write failed"}, err
	}

	if opts.Run == nil {
		return Decision{Skipped: "no runner configured"}, nil
	}

	if err := opts.Run(ctx, opts.Root); err != nil {
		return Decision{Ran: true}, err
	}
	return Decision{Ran: true}, nil
}

func writeMarker(root string) error {
	dir := storage.ProjectStateDir(root)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, markerName), []byte("auto-init attempted\n"), 0o600)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
