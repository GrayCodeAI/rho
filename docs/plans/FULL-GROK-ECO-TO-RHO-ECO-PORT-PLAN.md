# Full Port Plan: grok-eco → graycode-eco (Go reimplementation)

**Status:** Master long-term plan  
**Date:** 2026-07-16  
**Active execution:** [YEAR-0-ACTIVE.md](./YEAR-0-ACTIVE.md) (Year 0 control-plane track)  
**ADR:** [ADR-0003](../architecture/adr/ADR-0003-grok-behavioral-port-go-multirepo.md)  
**Meaning of “port”:** Reimplement **every Grok Build capability** in idiomatic **Go** across graycode-eco repos.  
**Not meaning:** Copy Rust crates, depend on Grok binaries, or collapse rho into a Rust monorepo.

**Source tree:** `grok-eco/grok-build` (~1.35M LOC Rust, 1 product monorepo)  
**Target tree:** `graycode-eco/*` (multi-repo Go platform + cloud/TS/Python)

**Related shorter plan:** `GROK-CLASS-CAPABILITY-LONG-HORIZON-PLAN.md` (control-plane focus).  
**This document:** **complete** crate/tool/doc/slash inventory, including small items, with owner repo + status + phase.

---

## 0. Port principles

1. **Behavior parity over code structure** — same user-visible contracts, not same file tree.  
2. **Go multi-repo stays** — map Grok crates into rho / engines / contracts / cloud / skills.  
3. **Prefer wire-first** — Rho already has partial types; complete wiring before greenfield.  
4. **Privacy-first defaults** — Grok Mixpanel/Sentry defaults become **opt-in** OTEL/privacy-safe telemetry.  
5. **Multi-provider stays** — Grok sampler/auth maps to **flux**, not a single-vendor clone.  
6. **Memory stays harrier** — Grok markdown memory maps to **harrier APIs + UX**, not a second store.
7. **Tokens stay shrike** — Grok `xai-token-estimation` is obsolete relative to shrike.
8. **Track everything** — even 70-line crates appear in the matrix (Done / Partial / Port / Skip / N/A).

### Status legend

| Status | Meaning |
|--------|---------|
| **Done** | graycode-eco already has equivalent or better |
| **Partial** | Exists but incomplete vs Grok |
| **Port** | Must reimplement in Go |
| **Skip** | Intentionally not porting (vendor/stack conflict) |
| **N/A** | Grok internal leaf; absorbed into larger port |

### Effort bands (one senior Go eng)

| Band | Meaning |
|------|---------|
| XS | &lt; 1 week |
| S | 1–2 weeks |
| M | 3–6 weeks |
| L | 2–3 months |
| XL | 3–6 months |
| XXL | 6–12+ months (TUI/parity programs) |

### Suggested calendar (full port)

| Horizon | Scope |
|---------|--------|
| **Y0 (0–6 mo)** | Control plane + trust + spawn + hooks (critical path) |
| **Y0–1 (6–12 mo)** | Plugins/marketplace, tasks/monitor, ACP, user-guide |
| **Y1–2** | Workspace depth, foreign sessions, hunks, mermaid, PTY, enterprise policy |
| **Y2–3** | Computer-hub class remote tools (if product needs), full slash parity, IDE polish |
| **Y3–5** | Hardening, multi-editor, fleet/agents at scale, continuous Grok-diff audits |

Full “every small thing green” is a **multi-year product program**, not a 10-year rewrite of Rust.  
Critical path remains **months**; **complete parity** including TUI polish and enterprise is **~2–4 years** of continuous work depending on headcount.

---

## 1. Repo mapping (where Grok monorepo goes)

```text
grok-eco/grok-build  (one Rust workspace)
        │
        ├─► rho                    product CLI/TUI/engine/tools/hooks/plugins/ACP
        ├─► eagle     pure DTOs (tool/spawn/hooks/policy)
        ├─► flux                   LLM routing/stream/retry/catalog/auth credentials
        ├─► harrier                    memory graph + dream UX
        ├─► shrike                     compression / secrets / token cost
        ├─► swift                   session capture/import/replay
        ├─► kestrel / merlin         review & live audit (no Grok 1:1)
        ├─► falcon             MCP server helpers
        ├─► starling   skills + marketplace content
        ├─► graycode-platform/apps/worker  managed policy / tenancy / usage
        ├─► sparrow/python      ACP/daemon clients
        └─► graycode-platform       web dashboard/BFF (optional)
```

**There is only one Grok product repo (`grok-build`).**  
“Port all grok-eco repos” = port **all crates/capabilities inside grok-build** into the **appropriate graycode-eco repos**.

---

## 2. Master crate → graycode-eco matrix

### 2.1 Product / TUI / shell (codegen)

| Grok crate | ~LOC | Capability | Target repo | Status | Effort | Notes |
|------------|-----:|------------|-------------|--------|--------|-------|
| `xai-grok-pager` | 415k | Full TUI | rho `cmd/` | Partial | XXL | Bubble Tea already; parity of panes/modals/slash is multi-year |
| `xai-grok-pager-render` | 35k | Render pipeline | rho | Partial | L | Streaming render, blocks, media |
| `xai-grok-pager-minimal` | 5.6k | `--minimal` native scrollback | rho | Partial | M | `rho --repl` / print modes exist |
| `xai-grok-pager-bin` | 2.9k | Binary composition root | rho `cmd/rho` | Done | — | |
| `xai-grok-pager-pty-harness` | 10k | PTY test harness | rho tests | Partial | M | Test infra |
| `xai-grok-shell` | 336k | Agent runtime host | rho `internal/engine` | Partial | XXL | Largest runtime port |
| `xai-grok-shell-base` | 2.7k | Shared shell foundation | rho | Partial | S | Absorb |
| `xai-grok-shell-session-support` | 1.5k | Session support extract | rho `session` | Partial | S | Absorb |
| `xai-grok-tools` | 112k | Tool implementations | rho `internal/tool` | Partial | XL | See tool matrix §3 |
| `xai-grok-tools-api` | 0.7k | Tool API / slash wording | rho + contracts | Partial | S | |
| `xai-grok-workspace` | 77k | FS/VCS/permissions/hub | rho + sandbox + session | Partial | XL | |
| `xai-grok-workspace-types` | 9.2k | Workspace wire types | contracts | Partial | M | |
| `xai-grok-workspace-client` | 0.8k | Workspace RPC client | rho (if hub) | Port | M | Only if computer-hub ported |
| `xai-grok-agent` | 21k | Agent defs + system prompt | rho agent/personas | Partial | L | |
| `xai-grok-subagent-resolution` | 2.6k | Capability/isolation resolve | rho + contracts | Port | M | Critical |
| `xai-chat-state` | 13k | Chat state actor | rho session/engine | Partial | L | |
| `xai-grok-markdown` | 22k | Streaming MD TUI | rho markdown | Partial | L | |
| `xai-grok-markdown-core` | 1.1k | Headless MD analysis | rho | Partial | S | |
| `xai-ratatui-textarea` | 12k | Input widget | rho (Bubble Tea) | N/A | — | Different UI stack |
| `xai-ratatui-inline` | 3.7k | Inline render | rho | N/A | — | UI stack |
| `xai-fast-worktree` | 19k | CoW worktree speed | rho worktree | Port | L | Perf enhancement |
| `xai-file-utils` | 15k | Event tracking / upload | rho observability | Partial | M | Privacy opt-in |
| `xai-grok-telemetry` | 14k | Events + OTEL + Mixpanel + Sentry | rho observability | Partial | L | **Skip Mixpanel default**; keep OTEL |
| `xai-hunk-tracker` | 13k | Agent vs external hunks | rho | Port | L | |
| `xai-grok-sampling-types` | 13k | Chat API types | **flux** | Done* | — | Different shape; flux owns |
| `xai-grok-sampler` | 11k | HTTP stream + retry | **flux** | Done* | — | Do not replace flux |
| `xai-grok-update` | 11k | Auto-update | rho | Partial | M | |
| `xai-grok-mcp` | 10k | MCP client (oauth, wire) | rho `mcp` | Partial | L | |
| `xai-codebase-graph` | 9.7k | Tree-sitter graph | rho codegraph/repomap | Partial | L | |
| `xai-grok-memory` | 9.7k | Cross-session memory | **harrier** | Done* | M | Port UX only |
| `xai-grok-hooks` | 8.3k | File/HTTP hooks | rho hooks | Port | L | |
| `xai-fsnotify` | 6.7k | FS events | rho (fsnotify) | Partial | S | |
| `xai-grok-config` | 6k | Config layers + managed | rho config + cloud | Port | L | |
| `xai-grok-config-types` | 2.7k | Config DTOs | contracts/config | Partial | S | |
| `xai-grok-plugin-marketplace` | 5.3k | Marketplace | rho + community-skills | Port | L | |
| `xai-grok-shared` | 5.2k | Shared utils | rho | N/A | — | Absorb |
| `xai-grok-test-support` | 4.6k | Test harness | rho testutil | Partial | M | |
| `xai-grok-sandbox` | 3.9k | OS sandbox profiles | rho host permissions | Port | L | |
| `xai-grok-voice` | 2.7k | Streaming STT | rho | Partial | M | whisper path exists |
| `xai-acp-lib` | 2.3k | ACP protocol | rho acp | Port | L | |
| `xai-grok-mermaid` | 2.2k | Mermaid→PNG | rho | Port | M | |
| `xai-crash-handler` | 1.9k | Crash + startup detect | rho | Port | S | |
| `ptyctl` | 2.3k | Headless PTY control | rho | Port | M | |
| `ptyctl-cli` | 0.8k | PTY CLI | rho tests/tools | Optional | S | |
| `xai-tty-utils` | 1.2k | TTY-safe spawn | rho | Port | S | |
| `xai-hooks-plugins-types` | 1.2k | Hooks/plugins ACP DTOs | contracts | Port | S | |
| `xai-sqlite-journal` | 0.8k | SQLite journal mode | rho/harrier/shrike | Partial | XS | |
| `xai-system-power` | 0.7k | Sleep/wake notify | rho sleep_prevent | Partial | S | |
| `xai-grok-http` | 0.6k | Shared HTTP client | rho/netutil | Partial | XS | |
| `xai-agent-lifecycle` | 0.6k | Lifecycle hooks data | rho hooks/engine | Partial | S | |
| `xai-gix-status` | 0.6k | Fast git status | rho git tools | Partial | S | |
| `xai-grok-paths` | 0.6k | AbsPath types | rho | N/A | XS | Go path.Clean enough |
| `xai-grok-secrets` | 0.6k | Secrets helpers | shrike + flux | Partial | S | |
| `xai-grok-announcements` | 0.4k | Release announcements | rho tips/notify | Port | S | |
| `xai-grok-auth` | 0.4k | Auth seam | flux + rho auth | Partial | M | Browser OAuth optional |
| `xai-token-estimation` | 0.3k | Bytes/4 heuristic | **shrike** | Skip | — | shrike superior |
| `xai-tracing-macros` | 0.2k | Log macros | rho observability | N/A | XS | |
| `xai-grok-env` | 0.2k | Backend env presets | flux/rho | Partial | XS | |
| `xai-prompt-queue` | 0.2k | Prompt queue types | rho | Port | S | |
| `xai-mixpanel` | 0.1k | Mixpanel client | — | **Skip** | — | Privacy; OTEL opt-in |
| `xai-grok-version` | 0.1k | Version | rho VERSION | Done | — | |
| `xai-grok-models` | 0.1k | Default model IDs | flux catalog | Done* | — | |

### 2.2 Common crates (tool protocol / hub / compaction)

| Grok crate | ~LOC | Capability | Target | Status | Effort | Notes |
|------------|-----:|------------|--------|--------|--------|-------|
| `xai-computer-hub-sdk` | 14k | Remote tool server SDK | new or rho | Port (Y2+) | XL | Only if multi-host tools wanted |
| `xai-computer-hub-core` | 4.2k | Transport/registry | same | Port (Y2+) | L | |
| `xai-computer-hub-mcp-adapter` | 1k | MCP into hub | falcon | Port (Y2+) | M | |
| `xai-tool-protocol` | 6.6k | Wire protocol | contracts + rho | Port (Y2) | L | |
| `xai-tool-runtime` | 5.4k | Tool trait runtime | rho tool | Partial | L | |
| `xai-tool-types` | 3.6k | Spawn/task types | **contracts** | Port | M | **P0** |
| `xai-grok-compaction` | 6.8k | Compaction engine | rho engine + shrike | Partial | L | |
| `xai-circuit-breaker` | 2.2k | HTTP breaker | flux/resilience | Partial | S | |
| `xai-tracing` | 0.8k | Tracing | rho OTEL | Partial | S | |
| `xai-test-utils` | 0.4k | Hermetic git tests | rho testutil | Partial | S | |
| `xai-interjection-core` | 0.3k | Interjection messaging | rho | Port | S | Mid-turn user inject |
| `xai-proto-build` | — | Build tooling | — | Skip | — | Rust build |

\*Done* = domain covered by rho engine with different API.

---

## 3. Tool-by-tool port matrix (Grok tools)

### 3.1 Grok Build native tools

| Grok tool | Rho equivalent | Status | Port work |
|-----------|-----------------|--------|-----------|
| `bash` / `run_terminal_command` | `Bash` | Partial | background flag, timeout, safe-bash, kill |
| `read_file` | `Read` | Partial | media/PDF page ranges parity |
| `search_replace` | `Edit` | Partial | hashline / strict range policy optional |
| `list_dir` | `LS` | Done | |
| `grep` | `Grep` | Done | |
| `web_search` | `WebSearch` | Done | |
| `web_fetch` | `WebFetch` / AgenticFetch | Partial | SSRF hardening audit |
| `todo` | `TodoWrite` | Done | |
| `task` (spawn) | `Agent` | **Port** | full schema §A |
| `task_output` / wait | bg managers | **Port** | unify + tools |
| `kill_task` | partial | **Port** | |
| `monitor` | missing | **Port** | line stream + rate limit |
| `scheduler` | `Cron*` | Partial | `/loop` UX |
| `ask_user_question` | `AskUserQuestion` | Partial | multi-option UI |
| `enter_plan_mode` | `/spec` + plan | Partial | unify |
| `exit_plan_mode` | ApproveImplementation | Partial | |
| `update_goal` | todos/mission | Partial | goal tool |
| `lsp` | `LSP` / MCPLSP | Partial | |
| `image_gen` | missing | Port / Optional | provider-backed |
| `image_edit` | missing | Port / Optional | |
| `video_gen` | missing | Port / Optional | xAI-specific → multi-provider later |
| skills invoke | `Skill` | Partial | multi-harness discovery |
| MCP meta tools | MCP tools | Partial | search_tool / use_tool patterns |

### 3.2 Codex / OpenCode compatibility tool ports (in Grok tree)

Grok vendors codex/opencode tool implementations for compatibility profiles.

| Compat surface | Rho | Action |
|----------------|------|--------|
| Codex apply_patch / read / list / grep | rho tools | Optional compat profile |
| OpenCode read/write/edit/bash/glob/grep/todo/skill | rho | Optional `compat.opencode` |
| Claude import | missing | **Port** into swift/rho |
| Cursor skills scan | missing | **Port** |

---

## 4. Slash commands & UX (pager)

Grok slash modules (port checklist → rho `/` commands or CLI):

| Grok slash | Status in rho | Action |
|------------|----------------|--------|
| help | Partial | Port completeness |
| model / effort | Partial | Port effort levels |
| theme / vim_mode | Partial | Port gaps |
| compact | Partial | |
| config_agents / personas | Partial | Port agents modal parity |
| plugins / mcps / marketplace | Partial | Port |
| plan / view_plan | Partial | Align with /spec |
| loop | Partial (cron) | Port `/loop` |
| remember / memory | Partial (harrier) | Port UX |
| voice | Partial | Streaming STT |
| dashboard | Partial (HUD) | Port |
| usage | Partial | Port |
| import_claude | Missing | Port |
| fork / rewind / resume / rename | Partial | Port gaps |
| queue | Missing | Port prompt queue |
| btw (interjection) | Missing | Port |
| always_approve / auto | Partial | Map autonomy |
| login/logout | N/A keychain | Optional OAuth |
| share | Partial | |
| export / transcript | Partial | |
| announcements / release_notes | Missing | Port |
| feedback | Partial | |
| privacy | Partial | Port |
| terminal_setup | Missing | Port |
| timestamps | Partial | |
| history / find / copy | Partial | |
| imagine / imagine_video | Missing | Optional media tools |
| session_info / recap | Partial | |
| home / cd / new / exit | Partial | |
| multiline / mouse / screen_mode | Partial | TUI polish |
| scroll_debug / gboom | Dev only | Optional |

---

## 5. User-guide docs (port as rho user-guide)

| Grok doc | Port to |
|----------|---------|
| 01–05 essentials | `rho/docs/user-guide/` |
| 06 theming | same |
| 07 MCP | same + mcp-servers.md merge |
| 08 skills | same |
| 09 plugins | same |
| 10 hooks | same |
| 11 custom models | flux + rho |
| 12 project rules AGENTS.md | exists Partial |
| 13 memory | harrier UX |
| 14 headless | CLI flags doc |
| 15 agent/ACP | acp doc |
| 16 subagents | spawn doc |
| 17 sessions | session doc |
| 18 sandbox | sandbox.toml doc |
| 19 plan mode | /spec doc |
| 20 background tasks | taskruntime doc |
| 21 terminal support | same |
| 22 permissions | autonomy+pipeline doc |
| 23 dashboard | same |
| 24 monitoring usage | cost/usage + cloud |

**Effort:** M continuous across phases (write as features land).

---

## 6. Small systems often missed (explicit)

These are “small” crates/modules but required for **full** port claims:

| Item | Grok home | Rho action | Effort |
|------|-----------|-------------|--------|
| Folder trust | `workspace/folder_trust.rs` | **Port** `internal/trust` | M |
| envrc / direnv load | `workspace/envrc.rs` | **Port** | S |
| Foreign sessions (Claude/Codex) | `foreign_sessions/` | **Port** via swift | L |
| Managed config sync | `shell/managed_config` | **Port** via graycode-cloud | L |
| Claude import | `shell/claude_import*` | **Port** | M |
| Prompt queue | `xai-prompt-queue` | **Port** | S |
| Interjection (“btw”) | `xai-interjection-core` | **Port** | S |
| Announcements | `xai-grok-announcements` | **Port** | S |
| Crash handler | `xai-crash-handler` | **Port** | S |
| Auto-update | `xai-grok-update` | Complete Partial | M |
| Mermaid PNG | `xai-grok-mermaid` | **Port** (Go/SVG or subprocess) | M |
| Voice streaming | `xai-grok-voice` | Upgrade Partial | M |
| PTY control | `ptyctl` | **Port** for harness/tools | M |
| TTY-safe child spawn | `xai-tty-utils` | **Port** | S |
| Sleep/wake on auth | `xai-system-power` | Complete Partial | S |
| Hashline edit protocol | tools hashline | Optional Port | M |
| Leader/cluster multi-agent UI | pager leader_cluster | Optional (mission covers) | L |
| Relay / remote agent WS | shell relay/remote | Port for ACP serve | L |
| Upload / share pipeline | shell upload | Partial + privacy | M |
| Heap profile | shell heap_profile | Optional dev | S |
| Diag server | workspace diag_server | Optional | S |
| Preview supervisor | workspace | Optional | M |
| Hub auth/channel/server | workspace hub* | Y2 computer-hub | XL |
| Campaigns / version_overrides config | config | Port if enterprise | M |
| MDM macOS managed prefs | config macos_managed | Optional enterprise | M |
| Signed requirements fail-closed | config signed_policy | Port with cloud | L |
| Skills ignore/disabled/paths | skills config | Port | S |
| Compat cursor/claude toggles | config compat | Port | S |
| Safe-bash allowlist | permissions docs | Port | M |
| dontAsk / acceptEdits modes | permissions | Port | M |
| Dashboard view | pager views/dashboard | Port polish | M |
| Notifications system | pager notifications | Partial | S |
| Tips | pager tips | Partial | XS |
| Client identity | pager | Partial | XS |
| Inline media ffmpeg | pager | Optional | M |
| Hyperlink routing | pager | Port | S |
| Config TOML live edit | pager | Port | S |
| Goal classifier | shell session | Port | S |
| Swift classifier | shell | map to rho swift | S |
| Repo changes tracking | shell session | Partial | M |
| Active sessions multi | shell | Partial | M |
| MCP doctor | shell mcp_doctor | Port | S |
| Bundle / extensions suggest | shell extensions | Partial | M |
| Instrumentation / merlin modules | shell | map observability | S |

---

## 7. Year-by-year program (full port)

### Year 0 — Foundation (months 0–12)

**Theme:** Model can do what Grok’s agent control plane does; secure by default.

| Quarter | Deliverables | Repos |
|---------|--------------|-------|
| **Q1** | Contracts spawn DTOs; wire explore/plan/general; stop hardcoded explore; explore bash hard gate; unify taskruntime; structured SpawnResult | contracts, rho |
| **Q2** | sandbox.toml; folder trust; safe-bash; permission pipeline hooks-first; PreToolUse deny | rho |
| **Q3** | File+HTTP hooks; vendor aliases; multi-harness skills; multi-component plugins; marketplace MVP | rho, community-skills |
| **Q4** | Monitor/Wait/Kill; /loop; structured AskUser; plan/spec alignment; user-guide 01–12; crash handler; announcements; prompt queue; interjection | rho |

**Exit Year 0:** Contributor-ready “Grok-class agent controls” without claiming full TUI parity.

### Year 1 — Integration & product depth (months 12–24)

| Quarter | Deliverables |
|---------|--------------|
| **Q5** | ACP session/load/resume; richer updates; OpenAPI; sdk-go/python fields |
| **Q6** | Managed policy (signed) graycode-cloud → rho; IT tier; config layer order |
| **Q7** | Foreign session import (Claude/Codex); envrc; hunk tracker; fast worktree; mermaid render |
| **Q8** | Voice streaming upgrade; update checker parity; MCP doctor; slash parity batch 1; user-guide 13–24 |

### Year 2 — Workspace / hub / polish (months 24–36)

| Focus | Items |
|-------|--------|
| Computer hub (optional product decision) | tool-protocol, hub server/client, remote tools, MCP adapter |
| TUI parity program | dashboard, modals, mouse, minimal mode, notifications polish |
| Compat | Claude/Cursor/OpenCode/Codex profiles |
| Media tools | image_gen/edit/video if multi-provider available |
| Relay/WS agent serve | remote IDE/web |
| Performance | CoW worktrees, git status, compaction quality |

### Year 3–5 — Continuous parity

- Diff audit bot: scan Grok public releases → open rho issues  
- Enterprise: MDM, SCIM (cloud), fleet policies  
- Full editor suite (VS Code/Zed via ACP)  
- Hardening, fuzz, SLSA releases  
- Deprecate temporary adapters  

---

## 8. Phase implementation packs (engineering)

Each pack is a shippable program of work with Definition of Done.

### PACK-00: Inventory & gates (1–2 weeks)

- [ ] Freeze this matrix in repo  
- [ ] ADR: “Full behavioral port, Go multi-repo”  
- [ ] Feature-flag registry  
- [ ] CI job: `docs/plans` status table lint (optional later)

### PACK-01: Contracts (2–3 weeks) — **start here**

**Repo:** `eagle`

- [ ] `agent` package: CapabilityMode, IsolationMode, SubagentType, SpawnRequest, SpawnResult  
- [ ] Hook event name constants  
- [ ] Tests for aliases and validation  
- [ ] Version release  

**DoD:** rho can import types; no engine deps.

### PACK-02: Spawn control plane (6–8 weeks)

**Repo:** `rho`

- [ ] Change `AgentSpawnFn` to SpawnRequest/Result  
- [ ] Agent tool full schema (type, capability, isolation, resume, cwd, model, thoroughness, description, background)  
- [ ] Wire plan/general (not only explore)  
- [ ] Worktree isolation lifecycle  
- [ ] Transcript resume_from  
- [ ] MultiAgent typed tasks  
- [ ] Explore bash AST allowlist  
- [ ] Unify BackgroundAgentManager / BackgroundRunner / BackgroundAgentPool → `taskruntime`  
- [ ] E2E + race tests  

**DoD:** Model can spawn plan agent that cannot Write; resume works.

### PACK-03: Trust & sandbox (5–7 weeks)

- [ ] `internal/trust` folder trust store + CLI  
- [ ] Gate project hooks/MCP/LSP/plugins  
- [ ] sandbox.toml loader (additive project merge)  
- [ ] Profiles: off, workspace, read-only, strict, devbox, custom  
- [ ] Deny globs fail-closed  
- [ ] OS matrix tests  

### PACK-04: Hooks complete (4 weeks)

- [ ] Event set expansion  
- [ ] Claude/Cursor aliases  
- [ ] File discovery paths  
- [ ] HTTP runner  
- [ ] Inject into PermissionEngine before autonomy  
- [ ] Plugin env RHO_PLUGIN_ROOT/DATA  

### PACK-05: Extensions (8–10 weeks)

- [ ] Multi-component plugin layout  
- [ ] Discovery scopes  
- [ ] Marketplace multi-source install  
- [ ] community-skills index schema for plugins  
- [ ] Multi-harness skill scan  
- [ ] Audit hardening  

### PACK-06: Task tools & loop (4 weeks)

- [ ] GetTaskOutput, WaitTasks, KillTask tools  
- [ ] Monitor tool (line processor, rate limit, auto-kill)  
- [ ] `/loop` over cron with caps/expiry  
- [ ] Shell background demote hotkey (optional TUI)  

### PACK-07: Plan & AskUser (3 weeks)

- [ ] Structured AskUserQuestion UI  
- [ ] enter/exit plan alignment with /spec  
- [ ] update_goal tool if needed  

### PACK-08: Small UX systems batch (4 weeks)

- [ ] Crash handler  
- [ ] Announcements  
- [ ] Prompt queue  
- [ ] Interjection (btw)  
- [ ] MCP doctor  
- [ ] Terminal setup slash  
- [ ] Privacy / usage docs  

### PACK-09: ACP + SDKs (7 weeks)

- [ ] session/load, richer updates  
- [ ] OpenAPI fields  
- [ ] sdk-go + sdk-python  
- [ ] Optional WS serve/relay  

### PACK-10: Enterprise policy (6 weeks)

- [ ] Signed managed policy schema (cloud)  
- [ ] rho apply layers  
- [ ] fail-open vs fail-closed  
- [ ] IT non-excludable tier end-to-end  

### PACK-11: Session ecosystem (6 weeks)

- [ ] Foreign sessions import (swift)
- [ ] Claude import CLI  
- [ ] envrc load  
- [ ] Hunk attribution tracker  
- [ ] Fast worktree (CoW where available)  

### PACK-12: Media & mermaid (4–6 weeks)

- [ ] Mermaid render path  
- [ ] Image gen/edit if providers allow  
- [ ] Video optional  
- [ ] Inline media handling  

### PACK-13: Voice & power (3 weeks)

- [ ] Streaming STT pipeline  
- [ ] Sleep/wake aware auth refresh  

### PACK-14: TUI parity program (ongoing XL)

Track Grok pager features against rho Bubble Tea:

- [ ] Dashboard view parity  
- [ ] Agents/personas modal  
- [ ] Plugins/marketplace modal  
- [ ] Minimal mode  
- [ ] Mouse/scroll polish  
- [ ] Notifications  
- [ ] Theme completeness  
- [ ] Slash completeness audit (table §4)  

### PACK-15: Computer hub (optional Y2 XL)

Only if product requires remote tool hosts:

- [ ] tool-protocol Go  
- [ ] hub server/client  
- [ ] MCP adapter  
- [ ] workspace RPC  

### PACK-16: Docs full port (parallel)

- [x] `docs/user-guide/01` … `24` (completed July 2026)
- [ ] Architecture notes stay separate  

### PACK-17: Continuous parity (forever)

- [ ] Quarterly grok-build public tree diff  
- [ ] Issue auto-filing for new Grok features  
- [ ] Security review cadence  

---

## 9. Per graycode-eco repo full ownership checklist

### `rho` (majority)

Port/finish: spawn, tools, sandbox, trust, hooks, plugins, marketplace client, TUI, ACP, headless, slash, taskruntime, hunks, mermaid, voice, crash, announcements, queue, interjection, plan, ask user, update, PTY harness, envrc, permissions pipeline, user-guide.

### `eagle`

Port: spawn/capability/isolation types, hook events, optional sandbox policy DTO, tool protocol types (later).

### `flux`

Absorb: sampling/stream/retry/circuit-breaker lessons; catalog already ahead; managed deployment hooks; **do not** become Grok-only auth.

### `harrier`

Port memory **UX**: dream, toggle priority, flush; keep graph store.

### `shrike`

Keep; optionally align display token estimates; secrets scanning already stronger.

### `swift`

Foreign session import; share/export; session indexing parity with Grok session features that are capture-oriented.

### `kestrel` / `merlin`

No direct Grok crates; keep peer engines; ensure rho composition matches Grok “review/merlin” product moments if any.

### `falcon`

Liveness, shared OAuth helpers, hub MCP adapter if hub lands.

### `starling`

Marketplace content + multi-component packages + validation.

### `graycode-platform/apps/worker` (deployed as `graycode-cloud`)

Managed policy, usage ledger (exists), entitlements, SSO/SCIM (exists primitives).

### `sparrow` / `robin`

Spawn options, plugins meta, permission/sandbox, stream formats.

### `graycode-platform`

Dashboard/usage/marketplace web UI only.

---

## 10. Explicit Skip list (still “accounted for”)

| Grok thing | Why skip / transform |
|------------|----------------------|
| Entire Rust monorepo layout | Wrong language/org model |
| Mixpanel default | Privacy |
| Sentry hard-dependency | Opt-in error reporting |
| SpaceXAI-only browser auth as sole path | Multi-provider + keychain |
| `xai-token-estimation` | shrike wins |
| Replace harrier with markdown memory files | Graph better |
| Replace flux with xai-grok-sampler | Multi-provider better |
| ratatui widgets | Bubble Tea stack |
| `xai-proto-build` | Rust build |
| Closed contribution policy | Rho is open |

Skipping is **documented completion**, not unfinished work.

---

## 11. Tracking spreadsheet schema (use in GitHub Projects)

Columns:

1. ID (e.g. PACK-02-07)  
2. Grok source (crate/path)  
3. Capability name  
4. Target repo  
5. Target package  
6. Status (Done/Partial/Port/Skip)  
7. Pack  
8. Effort  
9. Priority (P0–P3)  
10. Owner  
11. PR links  
12. Tests (unit/integ/OS)  
13. User-guide section  
14. Feature flag  
15. Notes  

Import every row from §2, §3, §4, §6.

---

## 12. Definition of “full port complete”

Declare complete when:

1. Every row in §2 is Done, Partial-accepted (with written gap &lt; 10% behavior), or Skip.  
2. Every tool in §3 is Done or Explicitly Optional.  
3. Every slash in §4 is Done or Explicitly Dev-only.  
4. User-guide 01–24 exists and matches behavior.  
5. ACP multi-turn + permissions works in at least one external editor.  
6. Folder trust + sandbox deny globs pass security tests on macOS+Linux.  
7. Marketplace can install a multi-component plugin.  
8. Subagent explore/plan/general + resume + worktree isolation work.  
9. No second memory/token/provider stack duplicated against harrier/shrike/flux.
10. Continuous parity process (PACK-17) running for 2 releases.

---

## 13. Resource model for “all including small things”

| Team size | Time to Year-0 exit | Time to full port claim |
|-----------|---------------------|-------------------------|
| 1 senior | ~12 mo | ~4–5 years |
| 2 seniors | ~7–9 mo | ~2.5–3.5 years |
| 3–4 (core+cloud+skills) | ~6 mo | ~2–3 years |

**Bottleneck:** TUI parity (pager 415k LOC class) and optional computer-hub — not the spawn contracts.

---

## 14. Immediate next 90 days (actionable)

| Week | Work |
|------|------|
| 1–2 | PACK-01 contracts + ADR |
| 3–8 | PACK-02 spawn control plane |
| 9–12 | Start PACK-03 trust/sandbox (parallel safe-bash) |

Do **not** start marketplace or computer-hub before PACK-02/03.

---

## 15. Relationship to shorter plan

| Document | Scope |
|----------|--------|
| `GROK-CLASS-CAPABILITY-LONG-HORIZON-PLAN.md` | Agent control + trust + hooks + marketplace focus (~12–18 mo) |
| **This document** | **Every crate, tool, slash, small system** → multi-year full port |

Execute shorter plan as **Year 0** of this master plan.

---

## 16. One-line summary

**Port all of grok-eco into graycode-eco = reimplement Grok Build’s full capability surface in Go across graycode-eco repos, map each crate to an owner engine, skip vendor/privacy conflicts, wire Rho’s existing partial systems first, and run a multi-year program ending in behavioral parity—not a Rust code transplant.**

---

*Maintainers: update Status column as PRs merge; re-run crate inventory when grok-build updates.*
