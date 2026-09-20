# Rho feature modules

This directory is the migration boundary for product features.

Each feature owns its capability, domain behavior, and tests in a dedicated
folder. The CLI (`cmd/`) is a delivery adapter and composition root; feature
packages must not import `cmd` or depend on TUI model internals. A feature may
depend on stable packages under `internal/`, but the dependency must point
inward toward domain or platform abstractions, never sideways through another
feature's implementation.

Features also must not import Bubble Tea, Bubbles, or Lipgloss. They may expose
data or side-effect APIs; `cmd` turns those APIs into UI commands and models.

New work belongs here when it has a coherent user-facing capability. Existing
files in `cmd/` and `internal/` move only as part of a vertical slice with
tests; bulk file moves without package-boundary work are prohibited because
Go directories are package boundaries.
