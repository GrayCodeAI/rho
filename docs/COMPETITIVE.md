# rho vs Top 20 OSS — Competitive Analysis

Status: Implemented (branch `feat/competitive-analysis-top20`, 2026-09-09)
Scope: rho (this repo) vs 10 AI coding CLIs + 6 dev CLIs + 4 terminals (incl. `herdr` multiplexer)
Related: `docs/plans/toolbench-comparison-vs-top20.md` (tool-count parity), `docs/SECURITY-DEVELOPER.md` (permission model), `docs/plans/pi-adoption-plan.md` (Kitty graphics already proposed), `docs/RESEARCH.md` (top-20 research-paper comparison + implementation record)

## Methodology (no assumptions)

Verified from source in this repo:

- Tools: `cmd/chat_tools.go:41-192` — 34 essential + ~90 lazy-loaded optional (126 unique `tool.*Tool` refs; prior plan counted 69 — surface grew, mostly `spec_*`).
- Browser/Screenshot: `tool.BrowserTool{}`, `tool.ScreenshotTool{}` in essential set (`cmd/chat_tools.go:75-76`); headless Chrome via chromedp per prior plan.
- Execution: direct host execution behind the permission engine and path guard; no container backend.
- Creds: OS secret store only, no `.env`/env read — `docs/SECURITY-DEVELOPER.md:7-12`.
- Share: local deeplink only — `internal/session/export.go:801-819` returns `rho://share/<hash[:16]>`, no hosted URL.
- Custom providers: supported — `internal/config/settings.go:50` (`custom_providers`), `internal/config/engine.go:32-50`.
- Unwired backends: `internal/tool/computer_use.go:67-94` (`SetComputerBackend`, nil default), `internal/tool/media_generation.go:69-71` (`SetMediaEngine`, nil default).
- Terminal detect covers kitty/ghostty/wezterm/alacritty names (`internal/ui/icons/detect_test.go:56`); Kitty graphics protocol not implemented (see `docs/plans/pi-adoption-plan.md:25`).
- Bench infra exists (`internal/features/eval/`, `make bench`) but README publishes no numbers.

External star counts below are approximate web-search snapshots (2026-09-08), not repo-verified. Treat as order-of-magnitude traction, not exact rankings. rho is pre-release (`VERSION`: `0.0.1`, `README.md:40-44` source-build primary) — it competes on architecture, not stars.

## The 20

### A. Direct AI coding CLIs

| # | Repo | Stars~ | Lang / Lic | Provider story | Sandbox | Memory | Multi-agent | Distro |
|---|---|---|---|---|---|---|---|---|
| A1 | `anomalyco/opencode` | ~200k | TS/Bun, MIT | 75+ via Models.dev, BYOK + Copilot/Plus login | allow/ask/deny globs, `--dangerously-skip-permissions` | AGENTS.md + @-imports + /init | primary/plan/subagent + custom agents | curl/npm/brew/scoop/Desktop |
| A2 | `openclaw/openclaw` | ~388k* | TS, MIT | any + fallbacks/aliases | Docker modes off/non-main/all + approvals | SOUL.md + MEMORY.md + wiki | agents.entries routing | curl/npm/Docker/Nix |
| A3 | `NousResearch/hermes-agent` | ~240k* | Py+TS, MIT | Portal + OpenRouter/OpenAI/custom | 7 backends (local/Docker/SSH/Modal/Daytona/…) | auto-skill creation + FTS5 + SOUL.md | subagents + worktree parallel | install.sh/Desktop |
| A4 | `openai/codex` | ~121k | Rust/Ratatui, Apache-2.0 | OpenAI-first + ChatGPT sub login + custom base_url | Seatbelt/Landlock/AppContainer; read-only/workspace-write/full; net-off default | AGENTS.md + Memories + /compress | subagent delegation | npm/brew/binary/Docker |
| A5 | `google-gemini/gemini-cli` | ~105k | TS, Apache-2.0 | Gemini-first + Anthropic/OpenAI/OpenRouter | Docker/Podman + gVisor + macOS sandbox-exec profiles | GEMINI.md + save_memory + checkpoints | subagents + policy engine | npm/npx/brew/Docker |
| A6 | `earendil-works/pi` | ~100k | TS, MIT | unified OpenAI/Anthropic/Google + Ollama | none by design (trust.json; run in container yourself) | AGENTS.md/CLAUDE.md + session persist | via Extension only | npm |
| A7 | `OpenHands/OpenHands` | ~86k | Py+TS, MIT | LiteLLM any + SaaS | DockerWorkspace (rec.) / Process / Remote | events + Condenser summarizer + skills | SDK delegation | pip/Docker |
| A8 | `cline/cline` | ~65k | TS, Apache-2.0 | BYOK shared config | approvals + shadow-git checkpoints | .clinerules-bank + skills | Plan/Act + SDK subagents | npm/VSCode/binaries |
| A9 | `block/goose` | ~53k | Rust, Apache-2.0 | registry + declarative custom | prompt/allow/deny + env strip + per-ext isolation | memory MCP (store/retrieve) | orchestrator + subagents | curl/Desktop/cargo |
| A10 | `Aider-AI/aider` | ~48k | Py, Apache-2.0 | LiteLLM any + Ollama | none; auto-commit + diff/undo + lint/test fix | RepoMap (graph-ranked defs) | none (wrappable as MCP tool) | pip/pipx/Docker |

*OpenClaw/Hermes counts volatile (mirrors/forks); directionally >100k.

### B. Dev CLIs (substrate + UX bar)

| # | Repo | Stars~ | Lang / Lic | Lesson for rho |
|---|---|---|---|---|
| B1 | `junegunn/fzf` | ~82k | Go, MIT | Pipe-first Unix design; zero-config speed |
| B2 | `jesseduffield/lazygit` | ~82k | Go, MIT | Keyboard TUI that makes hard git trivial; closest Go-TUI comp |
| B3 | `BurntSushi/ripgrep` | ~68k | Rust, MIT/Unlicense | Benchmarks in README; respects .gitignore; SIMD+parallel |
| B4 | `alacritty/alacritty` | ~65k | Rust, Apache-2.0 | Minimal fast core; delegate tabs to mux |
| B5 | `cli/cli` (`gh`) | ~46k | Go, MIT | Official CLI wins via scripting (`gh api`) + extensions |
| B6 | `herdrdev/herdr` | ~36k | Rust, Apache-2.0 | Agent multiplexer: persistent terms, detach/SSH, socket API; runs any agent as-is |

### C. Terminals + modern CLI wave (runtime layer)

| # | Repo | Stars~ | Lang / Lic | Note |
|---|---|---|---|---|
| C1 | `ghostty-org/ghostty` | ~60k | Zig, MIT | Native Metal/GL, libghostty, Kitty-graphics compat |
| C2 | `sharkdp/bat` / `starship/starship` | ~59k each | Rust, MIT/ISC | Drop-in replacements, single binary, sane defaults |
| C3 | `kovidgoyal/kitty` | ~34k | Py+C, GPL-3.0 | Image protocol others copy; GPL limits embedding |
| C4 | `wez/wezterm` | ~28k | Rust, MIT | Built-in mux + SSH mux, Lua config |

## Deep dimensions

1. **Traction.** rho has no star-moat (pre-release). Leaders won via day-1 provider-agnostic + one-liner install + Web/Desktop alongside TUI. rho already ships script/brew/npm paths (`README.md:48-59`) — keep, don't add Desktop.
2. **Language/distro.** Go+MIT+zero-CGO (`Makefile:54`, `go.mod:3`) matches `gh/fzf/lazygit` enterprise-safe profile. Avoid GPL/EUPL patterns (kitty/eza). Rust wave wins on published benchmarks — rho has `make bench` but publishes none (Gap-04).
3. **Providers.** rho routes only via `flux/engine` facade (`docs/SECURITY-DEVELOPER.md:51-56`, `ecosystem.yaml:29-30`); custom OpenAI-compat supported (`internal/config/settings.go:50`). Count messaging ("28 first-class" per README) trails OpenCode 75+ / Hermes 300+ — fix by exposing catalog count dynamically, not by forking providers into CLI (ownership lives in router per AGENTS.md).
4. **TUI/UX.** Bubble Tea v2 + vim keys + `/autonomy` + `/spec` + watch `AI!`/`AI?` + visual diff is competitive. Missing vs field: hosted share-link (ours is local `rho://` deeplink), multi-session grid (we have `mission` worktrees + daemon — unsurfaced like herdr/cmux). Gap-02.
5. **Execution safety.** Host execution keeps onboarding simple, but requires strong permission, path, approval, and audit controls. Keep those controls explicit and testable; do not add a hidden execution backend.
6. **Memory/context.** AST repomap + Harrier graph + compaction segments + relevance-prune + conversation-arc + 80% tool-result clearing exceeds most. Missing: Hermes-style auto-skill learning loop (we have curator archive + harness — surface it).
7. **Multi-agent.** `mission` worktrees + family messenger + path reservations + budgets + portable `mission-graph.json` + `graph export` (hashes only) is unique verifiable-execution story. Surface it; no new runtime needed.
8. **MCP/skills/plugins.** MCP stdio/HTTP/SSE/WS + LSP + skills search/install/audit + curator matches Goose/Gemini/Codex. Contracts live in `internal/contracts` — extensions vendor DTOs. Correct; don't regress.
9. **Media/computer-use.** Tools exist (`Browser/Screenshot/CodeMatch/SearchX/AppVerify/GenerateMedia/ComputerUse`) but media/computer/STT backends are nil-by-default seams. README notes router ships `ImageClient`/`AudioClient`; host wiring is the gap. Gap-05. Kitty graphics (image display) still missing despite terminal detection. Gap-03.
10. **Ops/determinism.** Daemon `:4590` health/ready/chat-SSE + cron + `exec --fanout N` + replay cache + circuit breaker + smart routing + harness eval is ahead of Pi minimalism and Aider single-agent. Keep; add published eval numbers (Gap-04).

## Verdict

- **Wins to keep:** explicit host permission gates + dual `/autonomy`/`/spec` gates; portable execution graph; Go zero-CGO MIT; router-facade-only provider access; 120+ tool surface (see `docs/plans/toolbench-comparison-vs-top20.md` for category parity).
- **Loses to fix (filed as plans):** Gap-01 onboarding friction; Gap-02 share/multi-session; Gap-03 Kitty graphics; Gap-04 published benchmarks; Gap-05 default backend wiring.

## Gap plans (this branch — implemented 2026-09-09)

- `docs/plans/competitive-gap-02-share-multisession.md` ✅
- `docs/plans/competitive-gap-03-kitty-graphics.md` ✅
- `docs/plans/competitive-gap-04-published-benchmarks.md` ✅
- `docs/plans/competitive-gap-05-backend-wiring.md` ✅

Each follows the adoption-plan format (Status/Source/Existing/Decision/Priority) and respects developer-first + router-ownership + fail-closed constraints. See `docs/RESEARCH.md` for the research-paper comparison and the extra fixes surfaced by enabling the build.

## Verification

- Source cites above re-checked 2026-09-08 on branch `feat/competitive-analysis-top20`.
- External stars: web-search snapshots, approximate — re-verify via GitHub API/badges before publishing.
- Docs-only change: run markdownlint + `make vet` (fast); full `make ci` before PR per `CONTRIBUTING.md:13-17`.
