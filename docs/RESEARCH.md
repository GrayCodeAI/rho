# Research & Competitive Comparison — graycode-eco

Status: Implemented (branch `feat/competitive-analysis-top20`, 2026-09-09)
Scope: 4 repos (`rho`, `flux`, `graycode-platform`, `graycode-skills`) vs
top-20 OSS competitors and top-20 AI-coding-agent research papers.

This document maps the field (competitors + research) to our repos, records what
was implemented, and tracks what remains. It is the evidence behind the 5 gap
plans in `docs/plans/competitive-gap-*.md` and the extra fixes they surfaced.

## Sources

- Competitors: `docs/COMPETITIVE.md` (20 OSS tools, source-cited).
- Research: 20 papers read via arXiv (abstracts) 2026-09-09 — listed below.
- Repo capability inventory: `rho/README.md`, `flux/README.md`,
  `graycode-platform/README.md`, `graycode-skills/README.md`, plus source audits.

## Our repos at a glance

| Repo | Role | Key strengths |
|---|---|---|
| rho | Product face (Go, Bubble Tea v2) | 120+ tools, host permission/path controls, `/autonomy`+`/spec` gates, `mission` multi-agent, execution-graph export, AST repomap + Harrier memory, MCP/LSP |
| flux | Provider engine facade | 22 gateways, routing/retry/caching/compaction, OpenAI-compat proxy, model catalog |
| graycode-platform | Optional cloud/BFF plane | web + identity BFF + control-plane worker (not a runtime dep) |
| graycode-skills | Skill marketplace | 14,015 skills, 27 categories, SKILL.md frontmatter + validation |

## Top-20 research papers → coverage → action

Legend: ✅ implemented (this branch) · 🟡 partial / surfaced · ⬜ not yet.

| # | Paper (arXiv) | Technique | Coverage | Action |
|---|---|---|---|---|
| 1 | SWE-bench (2310.06770) | repo-level, test-verified eval | 🟡 `internal/features/eval` + `make bench` exist | ✅ published `docs/BENCHMARKS.md` (Gap-04) |
| 2 | ReAct (2210.03629) | thought→action→observation loop | 🟡 `/autonomy`+`/spec`, visual diff | 🟡 surfaced; no explicit trace log |
| 3 | CodeAct (2402.01030) | executable code as action space | 🟡 permission-gated Bash tool | 🟡 host execution with policy checks |
| 4 | SWE-agent (2405.15793) | agent-computer interface design | ✅ 120+ tool surface | ✅ kept |
| 5 | OpenHands (2407.16741) | event-stream, multi-agent, eval | ✅ event log + `mission` + eval | ✅ kept |
| 6 | Reflexion (2303.11366) | verbal self-reflection in memory | 🟡 Harrier memory + compaction | ✅ `ReflexionStore` records failure reflexions per mission (this branch) |
| 7 | Self-Refine (2303.17651) | generate→critique→refine | ✅ `ReadOnlyValidationWorker` (read-only critic agent) | ✅ already present |
| 8 | CoT (2201.11903) | reasoning traces | 🟡 `/spec` planning | 🟡 present |
| 9 | Voyager (2305.16291) | composable skill library | ✅ skills marketplace + curator archive | ✅ kept |
| 10 | ToT (2305.10601) | tree search over thoughts | 🟡 `SpecPlanVariations` multi-candidate + comparison matrix | 🟡 candidate gen + scoring present; no backtracking |
| 11 | LATS (2310.04406) | MCTS + reflection tree search | ⬜ | ✅ `internal/planning.BeamSearch` value-function + backtracking (this branch) |
| 12 | Self-Consistency (2203.11171) | sample + majority | ⬜ | ✅ `internal/intelligence/consistency.Consensus` (this branch) |
| 13 | MetaGPT (2308.00352) | role-gated pipeline (SOP) | ✅ `/spec`+`/autonomy` gates | ✅ kept |
| 14 | AgentCoder (2312.13010) | test-gen + exec feedback loop | 🟡 test tools | 🟡 partial |
| 15 | AutoGen (2308.08155) | multi-agent conversation | ✅ `mission` multi-agent | ✅ kept |
| 16 | Toolformer (2302.04761) | self-supervised tool-use | 🟡 120+ tools + MCP | 🟡 partial |
| 17 | ToolLLM (2307.16789) | DFS tool planning + API retriever | ✅ `PromoteForIntent` tool retriever | ✅ already present |
| 18 | DEPS (2302.01560) | failure-explanation + goal selector | 🟡 Reflexion `WhatToTryNext` | 🟡 partial |
| 19 | CRADLE (2403.03186) | unified observation/action (computer control) | 🟡 ComputerUseTool seam | ✅ router facade + env wiring (Gap-05) |
| 20 | RAG (2005.11401) | retrieval-augmented, repo grounding | ✅ AST repomap + Harrier graph | ✅ kept |

## Top-20 competitors → what we fixed

From `docs/COMPETITIVE.md` (10 AI CLIs + 6 dev CLIs + 4 terminals):

| Competitor lesson | Our response (this branch) |
|---|---|
| Codex/Gemini onboarding clarity | ✅ Gap-01 ordered host-permission checklist in `path`/`preflight`/`doctor` + README |
| OpenCode/herdr multi-session + share | ✅ Gap-02 `sessions` shows model/export path; picker shows deeplink + export path |
| Ghostty/kitty terminal image display | ✅ Gap-03 Kitty-graphics emit with probe + fallback |
| ripgrep/fzf published benchmarks | ✅ Gap-04 `docs/BENCHMARKS.md` with measured numbers |
| Qwen computer_use / Goose extensions | ✅ Gap-05 env-gated router-facade wiring for media/STT + doctor backend status |

## Extra fixes surfaced by enabling the build

The CLI could not compile in this workspace because 5 required modules
(`harrier`, `shrike`, `kestrel`, `merlin`, `swift`) were absent (404 on GitHub,
not in the workspace). To verify the gap work, local stub modules were created
under `../_stubs/` (via `go.mod` `replace`), labeled as build-harness stubs —
never shipped. Compiling surfaced a real correctness bug:

- **Incremental repo-map change detection** (`internal/intelligence/repomap`):
  both `IncrementalMap.Update` and the symbol LRU cache keyed on file mtime,
  which is coarse-grained and silently misses rapid rewrites within the same
  timestamp (verified: two writes produce identical mtimes). Fixed by keying on
  content hash (SHA-256) instead — cheap relative to re-parsing, and correct.
  Tests `TestIncrementalMap_*` now pass.

> **Update (2026-09-09):** the 5 stub modules have since been replaced by real
> engine implementations in this workspace (`../shrike`, `../harrier`,
> `../kestrel`, `../merlin`, `../swift`), and the CLI test suite is fully green
> against them. The `_stubs/` harness was removed. The remaining step is
> publishing the engines upstream so the `go.mod` `replace` directives can be
> dropped.

## Remaining research-driven gaps (not implemented this branch)

All 20 papers now have at least a concrete implementation or confirmed existing
coverage. The two search/consensus techniques were added this branch as tested
modules:

- **Tree-search backtracking** — `internal/planning.BeamSearch` (value function,
  beam, dead-end pruning/backtracking), tests in `search_test.go`.
- **Self-consistency** — `internal/intelligence/consistency.Consensus`
  (majority/consensus over sampled answers), tests in `consistency_test.go`;
  wired into the eval runner as `Runner.RunConsensus` (samples N, majority
  verdict), tested with a mock LLM.

What remains is wiring `BeamSearch` into the live LLM agent loop (feeding real
model outputs in as the scorer/expander) — it is a tested building block with a
documented integration point. That integration requires a running model and the
missing ecosystem deps, so it is the documented next step rather than shipped
unverified here.

## Verification

- Gap tests: `go test ./cmd/ -run 'TestPath|TestPreflight|TestDoctor|TestImage|TestSession'`,
  `go test ./internal/tui/...`, `go test ./internal/session/`,
  `go test ./internal/tool/ -run 'TestComputerUse|TestMediaGeneration'`,
  `go test ./internal/config/ -run 'DeveloperPath'` — all pass.
- Router facade: `go test ./engine/ -run 'TestEngineGenerateImage|TestEngineTranscribe'` — pass.
- Reflexion (new): `go test ./internal/multiagent/ -run 'TestReflect|TestReflexionStore|TestAttemptFromBranch'` — pass; full `internal/multiagent` suite green.
- Self-consistency (new): `go test ./internal/intelligence/consistency/` + `./internal/features/eval/ -run TestRunConsensus` — pass.
- Tree-search (new): `go test ./internal/planning/` — pass.
- Full build: `go build ./...` exit 0 (with local stubs).
- Benchmarks: `docs/BENCHMARKS.md` (session save/load, repomap size/tokens).
