# Pi Adoption Follow-up Plan

Status: Proposed

Source: `docs/plans/pi-adoption-plan.md` — the remaining open sub-items after
the three merged Pi PRs (#226, #227, #228).

## Executive Decision

Two of the three remaining sub-items are **blocked by the rendering stack** and
cannot be adopted cleanly in rho's current UI framework:

- Bubble Tea v2 exposes **no public `Renderer` interface** (its renderer is an
  unexported type with internal methods; the only option is `WithoutRenderer`).
  A custom differential-renderer swap is therefore not a clean interface
  implementation — it would require forking the renderer internals, a major and
  non-runtime-verifiable change.
- Kitty render-loop integration depends on that renderer.

The third sub-item — **agent-runtime eval comparative + reproducibility
reporting** — is fully feasible and safe (non-TUI) and is adopted here.

## Adopted

### Agent-runtime eval: comparative + reproducibility reporting

- Compute a reproducibility hash over each run (prompt, model, provider,
  config) so identical runs can be cached and compared.
- Add a comparative report across runs/models: pass rate, token/latency/cost
  deltas, and per-run reproducibility hash.
- Wire it into the existing `evalloop` package and `rho eval loop` output.

### Scope and ownership

- Primary: `internal/features/evalloop`.
- CLI: `cmd/eval.go` loop mode.
- No changes to a local sandbox runtime (none ships in rho), `internal/session`,
  or `internal/daemon`.

### Required behavior

1. `Run` records the model, provider, prompt, and a config-version seed.
2. A reproducibility hash (SHA-256) is derived from those inputs plus the
   result transcript.
3. A `Compare` helper aggregates multiple results and reports pass-rate and
   per-metric deltas (tokens, cost, duration).
4. `rho eval loop --report` prints the comparative summary.

### Acceptance criteria

- Identical inputs produce identical reproducibility hashes.
- The comparative report surfaces token/cost/duration deltas across runs.
- Existing single-run behavior is unchanged.
- Unit tests cover hashing determinism and the comparative report.

## Deliberately Not Adopted

- **Bubble Tea renderer integration** — Bubble Tea v2's renderer is unexported;
  no clean public seam to swap in a differential renderer. The reusable
  line-diff core (`internal/tui/diff`) remains available for a future render
  engine or an upstream Bubble Tea change.
- **Kitty render-loop integration** — depends on the above; the encoding library
  (`internal/tui/kitty`) remains usable by any renderer that can emit frames.

## Verification

- `go test ./...` full suite.
- `make vet`, `make lint`, `rho verify`.
- Focused `internal/features/evalloop` tests.
- markdownlint on this document.
