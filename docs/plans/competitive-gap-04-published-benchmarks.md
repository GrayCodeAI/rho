# Gap-04: Published Benchmarks (Docs from Existing Infra)

Status: Implemented (2026-09-09)
Source: field comparison vs ripgrep/fzf/alacritty (numbers in README), Aider RepoMap token budgets

Constraint: docs-only. No new benchmark framework; infra already exists.

## Existing rho capabilities (verified)

- `make bench` (`go test -bench=. -benchmem -count=3`) in `Makefile:100-101`.
- `internal/bench/suite.go` (eco suite runner + report formatter).
- `internal/features/eval/` (model benchmark tasks, runner, CSV export).
- Session load/save benchmarks (`internal/session/benchmark_test.go`).

## Decision

Adopt: a repeatable report workflow + published table in README/docs.

Do not adopt: new eval harness, SWE-bench claims, provider-funded comparisons.

## Priority model

- P0: fixed command + env (`make bench` subset: session save/load 100/1000, repomap size/tokens) recorded with machine + commit.
- P1: `docs/BENCHMARKS.md` table (TUI-independent, no latency theater): session save/load, repomap tokens, tool-catalog size before/after `RHO_TOOL_SHRINK=1`.
- P2: CI artifact (optional): nightly `bench` JSON upload; never gate releases on it.

## Steps

1. Run the P0 subset locally; capture `go test -bench` output + commit SHA.
2. Write `docs/BENCHMARKS.md` with method, hardware, commit, raw output link.
3. Link from README performance-adjacent section; keep claims to measured numbers only.

## Verification

- Commands used are existing `make`/`go test` targets (no code change).
- Markdown passes markdownlint config (line-length disabled).
- `make vet` clean (no code touched).
