# Goose Adoption Plan

Status: Proposed

Source: `https://github.com/aaif-goose/goose` (Apache-2.0, Rust workspace,
Linux Foundation / Agentic AI Foundation)

## Executive Decision

Goose's audit against rho found that most of its runtime concepts already have
a native rho implementation (providers via flux, sessions, MCP, ACP, skills,
host-native policy, permissions). The genuinely novel, rho-relevant ideas are
adopted here in Go, without copying Rust code or adding a second execution
sandbox runtime.

## Existing Rho Capabilities

| Goose package/concept | Rho implementation | Decision |
|---|---|---|
| Provider abstraction (~36) | sibling `flux` (28 built-in + 75+ live) | Keep rho |
| Sessions (SQLite WAL) | `internal/session` JSONL+WAL+zstd + sibling `swift` (Swift) | Keep rho |
| MCP (client+server) | `internal/mcp` + sibling `falcon` | Keep rho |
| ACP | `internal/acp` | Keep rho |
| Extensions/skills | `internal/plugin`, skills registry | Keep rho |
| Execution safety | `internal/engine/safety`, path guards, trust policy | Host-native, fail-closed |
| OSV malware gate | `internal/permissions/osv_checker.go` (`CheckCommand`/`CheckPackage`) | Keep rho |
| Hints / AGENTS.md | `internal/config` AGENTS.md loader | **Adopt** @file references + subdir hints |
| Context compaction | sibling `shrike` (Shrike) + `internal/engine/compaction` | **Adopt** structured-summary retry ladder |
| Extension env safety | none (only OSV gate) | **Adopt** disallowed-env-var filter |
| Download manager | `internal/container`, `tool` | Out of scope |

## Priority Model

- **P0:** Security-relevant, bounded, rho-native adoptions.
- **P1:** High-value product improvements.
- **Defer:** Larger or cross-cutting changes needing an RFC.

## P0: Disallowed Env-Var Filter for Package/Extension Launch

### Goal

Prevent command/library hijacking when spawning `uvx`/`npx`/CLI-based extension
or MCP stdio processes by filtering dangerous environment overrides
(`PATH`, `LD_PRELOAD`, `LD_LIBRARY_PATH`, `DYLD_*`, `PYTHONPATH`,
`NODE_OPTIONS`, `GOROOT`, etc.) from the child environment.

### Scope and ownership

- Primary: `internal/env` (or `internal/permissions` next to the OSV
  checker) — a `sanitizeEnv` / `SafeEnv` helper.
- Consumers: `internal/mcp` stdio launch path and `internal/plugin` package
  execution.
- No changes to sibling `flux`.

### Required behavior

1. Define the disallowed env-var set (path/library/python/node/go hijacking
   vectors), mirroring goose `extension.rs::Envs`.
2. Provide a helper that, given a proposed env map, drops disallowed keys and
   returns the sanitized map plus the list of removed keys.
3. Apply it before spawning extension/MCP stdio subprocesses.
4. Log removed keys at debug/warn (not their values).

### Acceptance criteria

- A config that sets `PATH`/`LD_PRELOAD`/`PYTHONPATH` cannot influence the child
  process.
- Sanitization is unit-tested for the full disallowed set and for allow-listed
  benign keys.
- Existing extension launch behavior is unchanged when no disallowed keys are
  present.

> Adopted: `internal/env/sanitize.go` (`SanitizeEnv`) wired into
> `internal/plugin/bridge.go`. Unit-tested.

## P0: AGENTS.md `@file` References with Boundary + Budgets

### Goal

Let `AGENTS.md` (and other context files) reference additional files via
`@path` that get inlined into the loaded context, bounded by the git root and
strict size/depth budgets — matching goose `hints/import_files.rs`.

### Scope and ownership

- Primary: `internal/config` (the AGENTS.md loader).
- Boundary: stop imports at the git root so a context file cannot pull in files
  from outside the repository.
- Budgets: max import depth, max operations, max expanded bytes, content parse
  limit.

### Required behavior

1. Parse `@path` references in the loaded context file.
2. Resolve them relative to the context file, refusing paths outside the git
   root.
3. Inline referenced file content recursively, applying depth/operation/byte
   budgets.
4. On any budget/parse violation, fail that reference gracefully (skip) without
   failing the whole load.

### Acceptance criteria

- A context file can pull in an in-repo file and its content appears in the
  loaded context.
- A reference outside the git root is refused.
- Depth/byte/operation budgets are enforced and unit-tested.
- Existing single-file AGENTS.md behavior is unchanged.

> Adopted: `internal/config/context_refs.go` (`expandContextReferences`) wired
> into `LoadAgentsMDFrom`. Unit-tested.

## P1: Structured Compaction Overflow Retry Ladder

### Goal

Upgrade rho's context compaction to a structured summary with a progressive
tool-response-dropping retry ladder on overflow, and token-estimator-backed
accounting, matching `goose-context-management`.

### Scope and ownership

- Primary: `internal/engine/compaction` (and sibling `shrike` (Shrike) for any
  compression primitive).
- Behavior: on compaction context-overflow, drop tool responses from the middle
  outwards and retry; parse structured summaries leniently with a lossless raw
  fallback.

### Required behavior

1. Detect compaction overflow (`ContextLengthExceeded`).
2. Retry with progressive tool-response removal (`[0,10,20,50,100]%`).
3. Produce a structured summary (intent, files, errors/fixes, next step) when
   possible, falling back to raw text losslessly.
4. Estimate tokens when the provider does not report usage.

### Acceptance criteria

- Compaction succeeds where it previously overflowed by dropping tool responses.
- Structured-summary parsing is lenient and never loses content to a hard error.
- Unit tests cover the retry ladder and fallback.

## Deliberately Deferred

- Goose's SQLite session store (`usage_ledger`, token/cost schema): rho's
  JSONL/WAL + swift + cost tracker cover it; a schema migration is a larger
  change and is tracked separately.
- MCP Apps / agent-provided HTML UIs: novel but requires UI-layer design.
- ACP-as-provider wrapping other CLIs: larger provider abstraction change.
- Local-inference tool emulation / toolshim: depends on flux's local-model
  path.
- Recipe security scanner / cron recipes: rho already has schedule/cron.

## Verification

- `go test ./...` full suite.
- `make vet`, `make lint`, `rho verify`.
- Focused tests for the env filter, AGENTS.md references, and compaction retry.
- markdownlint on this document.
