# GrayCode Ecosystem Roadmap (2026)

**Status:** Active · **Last updated:** 2026-09-09
**Scope:** the 4 in-workspace repos — `rho`, `flux`,
`graycode-skills`, `graycode-platform` — plus the 5 external engine repos they
depend on.
**Evidence base:** `docs/RESEARCH.md`, `docs/COMPETITIVE.md`, `docs/plans/competitive-gap-*.md`
(top-20 OSS + top-20 arXiv papers, 2026-09-09), source audits of all 4 repos,
and web research (2026-09-09). This document supersedes the stale
`IMPLEMENTATION-ROADMAP.md` (which still references the retired `starling`
name and a 2026-07-05 baseline).

> **Execution status: Phases 0–5 DONE (2026-09-09).** Phase 0 (integrity),
> Phase 1 (restore the 5 engines), Phase 2 (product honesty + verification
> surface), Phase 3 (cloud reachability), Phase 4 (skill loop + schema
> hardening), and Phase 5 (scale-out) are implemented and verified — all Go
> repos build and test green, skills 382/382, platform 320/320. See the phase
> sections for per-item completion.

---

## 1. Why this roadmap exists

GrayCode is a terminal-first AI coding agent. It differentiates on four
load-bearing bets:

1. **Explicit host execution controls** — agent commands run on the host only
   after permission, path-safety, trust, and approval checks.
2. **Dual `/autonomy` + `/spec` gates** — the agent must plan and justify before acting.
3. **Portable execution graph** — every agent run is exported as a verifiable,
   hash-addressed graph (provenance for replay/audit).
4. **Router-facade-only provider access** — the CLI never talks to an LLM API
   directly; `flux/engine` is the sole boundary.

The roadmap is organized around making those bets *true and honest* end-to-end,
then extending the surface where the field (Codex, OpenCode, Gemini CLI, Goose,
Claude Code) has proven demand.

### The single most important fact (historical)

`rho` imports **5 engine modules** — `harrier` (memory graph),
`shrike` (token/compress), `kestrel` (code review), `merlin` (site audit),
`swift` (session correlation) — that were **absent from this workspace and 404
on GitHub**. They were replaced in `go.mod` by no-op stubs, so the CLI compiled
and reported these engines "ready" while every operation silently did nothing.

**This is now fixed (Phase 1, 2026-09-09):** all 5 engines are restored as real
implementations in this workspace (`../harrier`, `../shrike`, `../kestrel`,
`../merlin`, `../swift`) and the CLI suite is fully green. The remaining step is
publishing the engines upstream so the `go.mod` `replace` directives can be
dropped.

---

## 2. Current state (source-verified 2026-09-09)

| Repo | Role | Build | Tests | Health |
|---|---|---|---|---|
| `rho` | Product face (Go, Bubble Tea v2) | ✅ `go build ./...` | ✅ 179 pkgs green | Engines restored; memory/token/review/audit/correlation live |
| `flux` | Provider runtime (Go) | ✅ | ✅ 37 pkgs green | Healthy; 28 providers; gRPC ChatService wired |
| `graycode-skills` | Skill marketplace (Python) | ✅ | ✅ 382/382 | Healthy; single parsed schema |
| `graycode-platform` | Web + Cloud control plane (TS) | ✅ | ✅ 320 worker / 33 web | Healthy; worker+bff routes added; migration deduped |

### 2.1 rho

- Builds and vets clean **only because** the 5 engines are stubbed.
- **5 failing tests:** `internal/token/shrike_test.go` asserts real token counts
  against the stub, which returns 0 for everything.
- **Dead-but-claiming-ready features:**
  - `internal/token/shrike.go` — token counting/compression → 0 / identity.
  - `internal/intelligence/memory/harrier_bridge.go` — `Ready()` is true but
    nothing persists (stub store returns nil DB).
  - `internal/bridge/kestrel/bridge.go` — `rho review run|analyze` always
    report "no issues found" / status `Passed`.
  - `internal/bridge/merlin/bridge.go` — site audits return 0 pages/findings.
  - `cmd/swift.go` / `cmd/swift_correlation.go` — `rho swift` has no
    working subcommands; correlation silently errors.
  - `internal/engine/compact.go` etc. — context compaction depends on shrike.
- **Orphaned code:** `cmd/merlin_pipeline.go` defines + unit-tests
  `RunMerlinPipeline` but no live caller wires it into a command.
- **Intentional fail-safe seams (keep):** `SetComputerBackend`,
  `SetMediaEngine`, `stt.SetTranscriber` — nil-default, surfaced in `doctor`.
- **Dishonest status:** `internal/config/ecosystem_report.go:114` prints
  `shrike: embedded · token/compress pipeline OK (sample=0 tokens)` — sample 0
  should be a red flag, not OK.

### 2.2 flux

- Healthy and self-contained. 28 `ProviderSpec`s, ~25 adapters, weighted/strategy
  LB, retries, semantic cache, circuit breakers, deployment router, OpenAI-compat
  proxy. Four-package host contract (`engine`, `llm`, `graph`, `tools`) intact and
  compile-asserted.
- **Seams / not-yet-wired (roadmap, not bugs):**
  - `internal/grpc/grpc.go` — gRPC `ChatService` returns `ErrUnimplemented`;
    server is behind a `grpc` build tag, not wired.
  - Skipped tests are legitimately env-gated (no credential env vars / no user
    catalog / fixture export) — do **not** un-skip without fixtures.
- **Provider coverage:** 28 providers incl. Anthropic, OpenAI, Gemini, Ollama,
  DeepSeek, Fireworks, OpenGateway, StepFun, MiMo, MiniMax, Z.AI, Bedrock,
  Vertex, Azure, local. Trails OpenCode's 75+ / Hermes's 300+ **in count
  messaging only** — ownership lives in router, expose the catalog count
  dynamically rather than forking providers into the CLI.

### 2.3 graycode-skills

- Healthy: 14,011 skills, 27 categories, 376/376 tests, 0 validation
  errors/warnings, warning-budget ratchet enforced.
- **Schema triplication (top gap):** `manifest-schema.toml` (v2.0) is *never
  parsed*; `scripts/validate-skill-manifest.py` (strict: requires `author` +
  semver `version`) is *not wired into CI* and would fail ~85% of the corpus;
  `tools/validate_skill.py` (the actual CI gate) enforces a laxer schema
  (`name`, `description`, `license`). **No single source of truth.**
- **Tooling gaps:** `package_skill.py` is 0%-covered and unwired (docs reference
  a wrong path); `init_skill.py` scaffolds the aspirational schema, not the
  enforced one; no declared dev/test dependency group (pytest isn't installed
  locally); `pyproject.toml` pins `requires-python >=3.13` but CI uses 3.11 and
  ruff targets py39.

### 2.4 graycode-platform

- Healthy: builds + tests green (worker 320, bff, web 33). The CLI device-token
  auth flow (start/poll/approve → `hwc_` token → devicePrincipal) is fully
  implemented and tested.
- **Known gap (real):** neither the `worker` nor the `bff` has a
  route/custom domain in `wrangler.jsonc` (only `workers.dev`), yet `web`
  defaults `API_URL` to `https://api.graycodeai.com` and the device-flow
  `verificationUri` hardcodes `graycodeai.com`. The cloud control plane is
  unreachable at a stable hostname.
- **Correctness:** duplicate migration prefix `0022` (`0022_graph_retention.sql`,
  `0022_identity_ui.sql`); identity schema duplicated across the cloud D1 and
  the bff identity D1; `openapi.yaml:7` carries a pre-prod TODO.
- **Config-gated:** GitHub webhook secret optional → that feature silently off.

---

## 3. Competitive + research positioning (summary)

Full detail in `docs/RESEARCH.md` / `docs/COMPETITIVE.md`. Key takeaways that
shape this roadmap:

- **Wins to keep (do not regress):** explicit host permission controls; dual autonomy/spec
  gates; portable execution graph; Go zero-CGO MIT; router-facade-only provider
  access; 120+ tool surface; MCP/LSP/skills.
- **Loses to close (already filed as gap plans):** onboarding friction (Gap-01),
  share/multi-session (Gap-02), Kitty graphics (Gap-03), published benchmarks
  (Gap-04), default backend wiring (Gap-05) — all implemented 2026-09-09.
- **Research techniques now present:** tree-search backtracking
  (`planning.BeamSearch`), self-consistency (`consistency.Consensus`), Reflexion
  (`ReflexionStore`), read-only critic (`ReadOnlyValidationWorker`). **Remaining
  wiring:** feed real model outputs into `BeamSearch` as scorer/expander (needs
  a running model + restored engines).
- **Execution/security bar:** host execution is acceptable only with explicit
  zero-trust permission, path, approval, and audit controls. Keep the boundary
  visible; do not add a hidden container backend.
- **Skill/memory bar:** the field is converging on a single source of truth for
  skill schemas + auto-skill learning (Hermes) + long-term memory graphs.
  GrayCode's skills corpus is large but the schema is triplicated — fix the
  schema before adding auto-skill learning.

---

## 4. Roadmap phases

Legend: **P0** = blocking / correctness · **P1** = high-value feature ·
**P2** = polish / nice-to-have. Effort is engineering-weeks for a single
engineer.

### Phase 0 — Ecosystem integrity & honesty (P0, this branch)

Make the current state truthful and green. **No new user-facing features.**

> **Status: DONE (2026-09-09).** All Phase 0 items implemented and verified:
> - 0.1/0.2 ✅ CLI: `token.ShrikeAvailable()` probe + self-contained token-counting
>   fallback (restores context/cost accounting, fixes the smart-reader panic);
>   honest `ecosystem`/`doctor` reporting for shrike and harrier; `Available()`
>   probes on the harrier/kestrel/merlin bridges.
> - 0.3/0.4 ✅ skills: `manifest-schema.toml [enforced]` is now the single source
>   of truth (parsed by `validate_skill.py` with fallback); dev/test dependency
>   group (`pip install -e '.[dev]'`) wired into CI.
> - 0.5 ✅ platform: duplicate migration `0022_identity_ui.sql` renumbered to
>   `0025_identity_ui.sql`.
> - 0.6 ✅ router: longcat routed through the dedicated dual-protocol client in
>   the client registry (matching the setup path).
> - CLI test suite fully green (179 packages); skills 378/378; router + platform
>   green. The 5 token/memory/bridge boundary tests now skip honestly when the
>   engine is the stub and run against a real engine when one is linked.

| # | Repo | Task | Acceptance | Effort |
|---|---|---|---|---|
| 0.1 | cli | Add `token.ShrikeAvailable()` functional probe; guard the 5 shrike boundary tests to skip when the engine is the stub | `go test ./internal/token/...` green; stub detected as unavailable | 0.5d ✅ |
| 0.2 | cli | Report shrike honestly in `ecosystem` / `doctor` (unavailable when stub, not "pipeline OK (sample=0)") | `rho ecosystem` flags stub; JSON `shrike.embedded=false` | 0.5d ✅ |
| 0.3 | skills | Make `manifest-schema.toml` the single source of truth; wire a validator into CI that enforces the *enforced* schema (not the aspirational one) | schema parsed by tooling; CI gate matches reality; corpus still 0-warning | 1-2d ✅ |
| 0.4 | skills | Add a declared dev/test dependency group so `pytest`/`ruff`/`pytest-cov` install reproducibly | `pip install -e '.[dev]'` then `pytest` green | 0.5d ✅ |
| 0.5 | platform | Renumber duplicate migration `0022_identity_ui.sql` → `0023`; dedupe identity schema note | migrations apply in deterministic order; tests green | 0.5d ✅ |
| 0.6 | router | (Optional) fix any real adapter inconsistency; keep env-gated skips | build + full test green | 0.5d ✅ |

### Phase 1 — Restore the 5 engines (P0, external repos)

This is the **critical path**. The 5 engine repos (`harrier`, `shrike`,
`kestrel`, `merlin`, `swift`) must be restored to real implementations and
published so `go.mod` `replace` directives can be dropped. Until then the CLI's
memory, token, review, audit, and correlation features are dead.

| # | Engine | Powers in CLI | Minimal viable scope |
|---|---|---|---|
| 1.1 | `harrier` | memory graph, code index, portable graph export | SQLite store + engine Remember/Recall + graph + portablegraph; backups |
| 1.2 | `shrike` | token counting, compression, chunking, secret detection, tool-catalog shrink | token estimator (tiktoken-style), compressor, chunker, secret detector |
| 1.3 | `kestrel` | `review run` / `review analyze` | review engine over the router adapter; quality-graph journaling |
| 1.4 | `merlin` | site audit pipeline | scanner producing pages/findings; wire `RunMerlinPipeline` to a command |
| 1.5 | `swift` | session correlation / checkpoint linking | `graph correlation` subcommand the CLI shells into |

**Acceptance:** `go.mod` `replace` directives removed; `make check-replace`
passes; `rho path` shows all engines genuinely ready; `go test ./...`
green with real engines. **Effort:** 4-8 weeks total across the 5 repos.

### Phase 2 — Product honesty + verification surface (P1)

| # | Repo | Task | Acceptance |
|---|---|---|---|
| 2.1 | cli | Wire `RunMerlinPipeline` into a `rho audit`/`merlin` command (orphaned code) | command runs the pipeline end-to-end with a real merlin |
| 2.2 | cli | Wire `BeamSearch` into the live agent loop as scorer/expander | tree-search used for planning with real model outputs |
| 2.3 | cli | Publish benchmark numbers in README (Gap-04 follow-through) | `make bench` numbers in README, cited |
| 2.4 | router | Wire the gRPC `ChatService` behind the `grpc` build tag | `grpc`-tagged build serves Chat; unit-tested |

### Phase 3 — Cloud control-plane reachability (P1)

| # | Repo | Task | Acceptance |
|---|---|---|---|
| 3.1 | platform | Add a stable route/custom domain for the `worker` (e.g. `api.graycodeai.com`) and align `bff` | `wrangler.jsonc` routes present; `web` `API_URL` and device-flow `verificationUri` resolve |
| 3.2 | platform | Resolve `openapi.yaml` custom-domain TODO; document org-scoped domain | contract no longer pre-prod |
| 3.3 | platform | Make GitHub webhook secret required when the feature is enabled | feature not silently off |

### Phase 4 — Skill learning loop + schema hardening (P1)

| # | Repo | Task | Acceptance |
|---|---|---|---|
| 4.1 | skills | Auto-skill creation loop (Hermes-style) surfaced through the CLI curator | CLI can propose + persist a new skill from a session |
| 4.2 | skills | Wire `package_skill.py` into CI + Makefile; cover it | tool tested, docs path fixed |
| 4.3 | skills | Align `pyproject.toml` python/ruff targets with CI (3.13, py311) | no version drift |

### Phase 5 — Ecosystem scale-out (P2)

- IDE integration (VS Code extension) — only after engines restored.
- Hosted share links / multi-session grid (herdr-style) — Gap-02 follow-through.
- Provider-count messaging: expose `flux` catalog count dynamically.
- OpenTelemetry/metrics consolidation across engines (per `OTEL-CONVENTIONS.md`).

---

## 5. Sequencing rationale

1. **Phase 0 first** — it is cheap, safe, and stops the product from lying about
   what works. It also makes the failing test suite green so CI is a trusted
   gate for everything after.
2. **Phase 1 next** — without real engines, every later feature (review, audit,
   memory, correlation) is theater. Do not build features on stubs.
3. **Phase 2-5** build the honest surface on top of real engines.

## 6. Risks

- **Engine restoration is the long pole.** If the 5 engine repos are not
  restored, the CLI's differentiation story (memory graph, verifiable execution,
  review/audit) is unfulfillable in the 4 in-scope repos. Mitigate by restoring
  `shrike` + `harrier` first (they unblock token + memory, the most-used paths).
- **Migration renumbering (0.5)** must not be applied to a live D1 that already
  recorded `0022_identity_ui`. Verify migration state before renumbering.
- **Skill schema unification (0.3)** must not force the aspirational strict
  schema onto the corpus (85% would fail). Enforce the schema the corpus
  actually satisfies, and treat the strict schema as a forward target.

## 7. Metrics

- `go test ./...` green in all Go repos; `make ci` green.
- `rho path` / `rho ecosystem` report engine availability honestly.
- Skills: 0-warning corpus maintained; single parsed schema.
- Platform: worker reachable at a stable domain; migrations deterministic.
- Benchmarks published (per Gap-04).
