# OpenAI Codex CLI Adoption Plan

Status: Audited. Core ideas already implemented natively in rho; remaining
deltas recorded as future RFCs.

> **Superseded runtime assumptions (2026-09-20):** Rho no longer ships a
> native OS/container sandbox or a Docker execution backend. The permission
> engine, path guard, trust policy, and destructive-command hard blocks are
> the current local safety boundary. Sandbox rows below are historical design
> notes and must not be treated as current implementation work.

Source: `https://github.com/openai/codex` (Apache-2.0, Rust workspace
`codex-rs`, ~100 crates)

## Executive Decision

The audit found that every codex-rs capability relevant to rho's security and
runtime model already has a native Go implementation, several of them deeper
than codex's equivalents because they build on Rho's independent ecosystem
repositories.
No second runtime, sandbox layer, or policy engine was created.

The former native sandbox-backend status was removed with the sandbox runtime.

Three codex ideas are deliberately deferred as future RFCs; see
[Deliberately Deferred](#deliberately-deferred).

## Capability Audit

| codex-rs crate/concept | rho implementation | Decision |
|---|---|---|
| `core` agent loop | `internal/engine` | Keep rho |
| `tui`, `ansi-escape`, `terminal-detection` | Bubble Tea/Lipgloss TUI | Keep rho |
| `rollout`, `thread-store`, `history` JSONL sessions with resume/fork | `internal/session` JSONL + WAL + named checkpoints + fork + recovery + handover | Keep rho (richer) |
| `app-server-daemon`, `app-server-protocol` (JSON-RPC for IDE/desktop) | `internal/daemon` HTTP/SSE on 4590 + `internal/acp` | Keep rho |
| `mcp-server`, `codex-mcp`, `rmcp-client`, `connectors` | `internal/mcp` client+server, sibling `falcon` scaffolding | Keep rho |
| `skills`, `plugin`, `hooks` | community skill registry + structural validator, plugins, expanded lifecycle hook events | Keep rho |
| `login`, `keyring-store`, `aws-auth` | flux credential store in OS keychain across 28 providers | Keep rho (broader) |
| `model-provider(-info)`, `models-manager`, `ollama`, `lmstudio` | sibling `flux` adapters, catalog, cascade routing | Keep rho (much broader) |
| `memories`, `agent-graph-store`, `context-fragments` | sibling `harrier` (Harrier) graph memory; eventlog/graphjournal projections | Keep rho |
| `apply-patch`, `file-search`, `file-watcher`, `git-utils` | edit tools, codegraph, git tooling, watcher hooks | Keep rho |
| `external-agent-migration` | swift reads Claude Code / Codex / Gemini CLI / OpenCode / Cursor sessions | Parity |
| **`linux-sandbox`** (Landlock + seccomp-bpf) | Removed from the local product runtime | Removed |
| **macOS Seatbelt** | Removed from the local product runtime | Removed |
| Windows confinement | Removed from the local product runtime | Removed |
| **`bwrap`**, nsjail, container fallbacks | Removed from the local product runtime | Removed |
| **`network-proxy`** (egress through inspectable proxy) | Not part of the local product runtime | Removed |
| **`execpolicy`** (structured pre-exec command analysis) | Permission-engine destructive-command hard block and `NeverAllow` ceiling remain; sandbox code verification was removed | Replaced by permission/path policy |
| **`shell-escalation`** (exact re-validated widening approval) | `PermissionService.EscalatePermission` binds single-use opaque tokens to exact calls; no sandbox policy layer | Kept at permission layer |
| `sdk` (TS), `thread-manager-sample` | daemon REST/SSE API is the programmatic surface; Go SDK deferred until consumers require it | Deferred (matches fx plan) |

### Adopted in this change

- Status transparency remains limited to the effective permission and path
  policy. There is no sandbox backend or `permission.sandbox_backend` field.
- **Batch tool** (safe core of codex Code Mode): a `Batch` tool runs a list of
  read-only tool calls in a single turn, cutting agent round-trips for fan-out
  research. It reuses the existing read-only allowlist and per-call schema
  validation, so no mutation can bypass the normal tool pipeline. It delivers
  Code Mode's primary token/round-trip benefit without embedding a script
  runtime or adding a new execution authority boundary.

## Deliberately Deferred

- **Full Code Mode** (`code-mode`, `code-mode-runtime`, `v8-poc`): letting the
  model author an arbitrary script that batches tool calls into one permission-checked
  execution, including mutation and control flow. The `Batch` tool above covers
  the safe read-only fan-out case. Arbitrary-script execution still requires an
  embedded runtime, capabilities model, and output-trust threat model; track as
  a standalone RFC.
- **Agent identity signing** (`agent-identity`): rho already provides a
  per-harness anonymous user identity (`internal/identity`) and a tamper-evident
  HMAC-chained security log with session-scoped events (`internal/securitylog`).
  Signed subagent delegation chains are worth a focused design once multi-org
  delegation exists.
- **Cloud tasks client** (`cloud-tasks*`): remote task queue integration.
  Rho Cloud already provides sync/review surfaces; a queue protocol would
  duplicate that until a concrete consumer exists.

## Verification

- `go test ./...` full suite green.
- `make vet`, `make lint`, `rho verify` green.
- Repo-owned markdown passes `markdownlint-cli2 '**/*.md'` (CI scope);
  findings under sibling repositories belong to those repositories and follow
  their own contribution flow.
