# Wave 2 — high-value deepseek-harness RFC ports for rho

> **Sandbox phases are obsolete.** Rho no longer contains `internal/sandbox`,
> Docker execution, or an OS/container sandbox backend. Do not implement Phase
> 2.3 or 2.11 as written; any future policy work must target the host-native
> permission, path, trust, and environment controls.

Status: **Proposed** (RFC specs only; no code on this branch yet). Each phase below
is a self-contained feature-branch spec in the style of the Wave 1 docs, ready to
be cut into its own branch and gated.

Anchors to the parent trackers:

- [`docs/plans/dsh-harness-port-plan.md`](./dsh-harness-port-plan.md) — protocol/session
  spine (Phases 0–3, delivered). This doc builds on its event-sourced
  `internal/eventlog` spine and the "model-visible ⟺ logged" invariant.
- [`docs/plans/dsh-harness-gap-port-plan.md`](./dsh-harness-gap-port-plan.md) — feature
  surface gaps (Phases 13–19 delivered; 20–26 deferred). Wave 2 revives two
  deferred items in reduced scope: `#21` (query-only SQLite FTS, Phase 2.6) and
  `#25` (PTY terminal, Phase 2.9). The remaining deferred items
  (`#20` agent-presets, `#22` e2b, `#23` code-runtime, `#24` sdk, `#26` web UI)
  stay deferred.

Reference source: `deepseek-ai/deepseek-harness` at `dsh-v0.1.0-rc.7`
(Aug 17, 2026 clone) — same snapshot as the Wave 1 audit.

## Principles (unchanged from Wave 1)

- Port **protocols and invariants**, never Cordis.
- Everything Go-native: interfaces + a small registry + typed state, wired at the
  composition root.
- Keep new packages dependency-lean so `scripts/check-internal-layer-imports.sh`
  stays green.
- Every durable decision is a journaled fact ("model-visible ⟺ logged"); log-only
  events carry no `surfaceOp` and never appear in model history.

## Wave 2 gap inventory

| # | DSH source | Rho gap | Status |
| --- | --- | --- | --- |
| 2.1 | `guard/timeout-policy` | no per-tool declared timeout enforced at dispatch | **implemented** on `feat/dsh-harness-rfc-2.1-timeout-policy` |
| 2.2 | `compaction/compaction` tool-pairing helpers | compact strategies can cut across open tool call/result pairs | proposed |
| 2.3 | `sandbox/sandbox-policy` | obsolete: no product sandbox runtime; use host policy statements | **removed** |
| 2.4 | `subagent/subagent` delegated policy inheritance | workers can prompt / policy not fixed at delegation boundary | proposed |
| 2.5 | `skill/skill`, `skill/tool-skill` | no invocation policy split, no digest catalog, no provider seam | proposed |
| 2.6 | `session-query/*sqlite` | no SQLite FTS search over session logs | proposed |
| 2.7 | `schedule/schedule` | host cron instead of session-log-backed reminders | proposed |
| 2.8 | `acp/acp-client`, `subagent/subagent-acp|-claude-code|-codex` | ACP server only; no client / external-agent providers | proposed |
| 2.9 | `terminal/*`, `tool-terminal` | no persistent PTY tool (revives gap `#25`) | proposed |
| 2.10 | `subagent/subagent` continuable children + cold resume | mission workers are one-shot; no durable children | proposed |
| 2.11 | `sandbox/sandbox-windows-acl` | obsolete: no product sandbox runtime | **removed** |
| 2.12 | `lsp/lsp-stdio` | document LRU + unscrubbed env vs transient-open + scrubbing | proposed |

Suggested branch cut order: 2.1 → 2.2 → 2.3 → 2.4 → 2.5 → 2.6 → 2.7 → 2.8 →
2.9 → 2.10 → 2.11 → 2.12. Phases 2.3/2.4 and 2.8/2.10 have hard dependencies
(2.4 needs 2.3 events; 2.10 needs 2.8 providers and 2.3/2.4 policy replay).

---

## Phase 2.1 — Tool-declared timeout policy (`internal/tool` + `internal/engine`)

Port of DSH `guard/timeout-policy` (`dsh-tool-call-timeout-policy`) + the
`@deepseek-ai/dsh-timeout` deadline library. Zero-config: the budget is read from
the tool's own declaration, not from a policy table.

- `tool.Tool` gains an optional `TimeoutProvider` interface (`Timeout()
  time.Duration`, 0 = no declaration) — the rho-native form of DSH's
  `ToolDefinition.timeoutMs`, consistent with the existing optional-provider
  pattern (`RiskLevelProvider`, `RetryPolicyProvider`, `SchemaProvider`).
  `tool.TimeoutOf(t Tool) time.Duration` reads it.
- Owning tools declare a budget: `WebFetchTool` declares `30s` so the engine's
  dispatch deadline matches its own 30s HTTP deadline. Bash/WebSearch keep the
  name-based fallback (`engine.toolTimeout`), which still applies to undeclaring
  tools.
- Enforcement is at the **dispatch point in `ExecuteOne`** — the faithful port
  of DSH's `tools/execute` around-dispatch listener, not a `StagePreExecute`
  pipeline registration: a pre-execute interceptor cannot wrap the execution
  context (its `next()` delegates to remaining interceptors only; the raw
  `tool.Execute` runs after the pipeline returns). `ExecuteOne` resolves the
  tool, picks the declared budget (winning over the fallback), derives
  `context.WithTimeout`, and executes through the existing `RetryExecutor`.
- If the imposed deadline wins, the result is a structured `TOOL_TIMEOUT`
  error: `tool call timed out after <ms>ms (code TOOL_TIMEOUT)` — not a raw
  `context deadline exceeded`. A `DeadlineExceeded` the tool produced on its
  own (while the outer budget is still live) keeps the generic error
  vocabulary: `timedOut` requires `toolCtx.Err() == DeadlineExceeded`.

Tests: `internal/tool/timeout_test.go` (TimeoutOf: declared / zero-declaration /
undeclaring); `internal/engine/tool_timeout_test.go` through `ExecuteOne`
(declared budget fires and produces the structured result; undeclaring tool
completes via the fallback; internal deadline error is not mislabelled
TOOL_TIMEOUT).

## Phase 2.2 — Compaction tool-pairing boundaries (`internal/engine/compact` + `internal/eventlog`)

Port of DSH `compaction/compaction` surface contract + the
`toolPairingBalancedBefore` / `toolPairingBalancedAfter` helpers. Rho's
`internal/engine/compact/` already does LLM summarization
(`strategy.go`, `micro.go`, `session_memory.go`) and `internal/eventlog` already
journals `tool.call` / `tool.result` / `session.compacted` (Wave 1 Phase 0). The
gap is **where a cut is legal**.

- `internal/eventlog/boundary.go` gains `ToolPairingBalancedAt(log, seq) bool`
  and `SafeCompactionRange(log, start, end) (ok bool, reason string)`:
  - A cut is legal only when no open `tool.call` lacks its matching `tool.result`
    across the edge (no unanswered assistant tool call crosses the cut).
  - A live bracket cannot cross a `turn/start` / `turn/end` (both are already
    journaled per Wave 1 Phase 3); reject ranges that do.
- `compact/strategy.go` validates every candidate range with
  `SafeCompactionRange` before summarization; a rejected range is skipped, never
  force-cut.
- Keep the DSH surface rules: only `message.user`, `message.assistant`,
  `tool.result` are surface nodes; `session.compacted` is log-only; the replace
  lands a fresh high-seq summary node; shadowed events remain in the raw log for
  deterministic replay.
- DSH's durable lock-bracket pattern (start → summary → replace → end) maps onto
  the existing `session.compacted` append; a crash between start and end must
  leave a detectable orphaned bracket (an unmatched start with no end), which
  blocks subsequent compaction until adoption repairs it.

Tests: pairing parity; unanswered-call rejection at the cut; reject range crossing
turn boundaries; replace generation rebuilds membership; orphaned-bracket
detection and adoption on load; deterministic replay after replace.

## Phase 2.3 — Sandbox mode vocabulary + model-facing policy (`internal/sandbox` + `internal/eventlog` + `internal/engine`)

> **Obsolete specification — retained for historical provenance only.** Do not
> implement this phase. Rho's current product boundary is host-native
> permission, path, trust, and environment policy; there is no sandbox mode or
> `internal/sandbox` package to extend.

Port of DSH `sandbox/sandbox-policy` semantics
(`SandboxMode` read-only / workspace-write / danger-full-access,
`ctx.sandboxPolicy.resolve()`, `setSandboxMode`, the `sandbox:policy` context
contribution). Rho already has the vocabulary — `internal/sandbox/mode.go`
`Mode` strict / workspace / off — so this phase ports the **policy resolution
and durability** around it.

- New eventlog type `sandbox.mode` (log-only, no surfaceOp) carrying
  `{mode, source}` where `source ∈ {user, delegation}` (`delegation` is
  Phase 2.4). Closed vocabulary invariant: a forged value outside
  `{strict, workspace, off}` fails loud on load (same pattern as the Wave 1
  event validation).
- `internal/sandbox/policy.go`:
  - `SandboxPolicy.OverrideOf(session)` — fold the latest `sandbox.mode` event
    (last switch wins) on the session's own log.
  - `SetSandboxMode(session, mode)` — THE write path; appends exactly one event.
  - `Resolve(session, defaultMode)` — explicit override ?? fold(events) ??
    deployment default; workspace root is the canonicalized session cwd
    (canonicalization precedes lexical normalization so `symlink/..` agrees with
    process cwd resolution).
- Model-facing statement (Phase 2.3's port of `sandbox:policy`): one concise
  durable context message on the first request and on each effective policy
  change; unchanged requests add nothing. `workspace` carries only the canonical
  workspace path (no host-dependent temp paths — summarize them). Rendered by
  the prompt assembler exactly like DSH's three templates, but with rho's
  vocabulary:
  - `strict` — read-only; do not refuse a required modification from this policy
    alone; try the tool and follow denial/escalation guidance.
  - `workspace` — may modify under `<workspace root>`; some platform temp areas
    writable.
  - `off` — file sandbox does not restrict modifications.
- Engine wiring: `PermissionService` / the agent loop consult `Resolve` per
  enforcement point; the daemon's persist path replays `sandbox.mode` events so
  the override survives restart.

Tests: fold (last switch wins), override persistence across reload, forged-value
rejection, canonicalization (symlink/.. parity), context statement emitted only
on first request / effective change, KV-cache stability (unchanged system prompt
across mode switches).

## Phase 2.4 — Delegated subagent policy inheritance (`internal/multiagent`)

Port of DSH `subagent/subagent` `captureDelegatedPolicyOverrides` /
`appendDelegatedPolicyOverrides` + the `subagent:delegation` context statement.
Depends on Phase 2.3 (`sandbox.mode` events).

- In `internal/multiagent/worker.go` (EngineWorker) and the mission spawn path:
  1. Capture the parent session's explicit sandbox override at the delegation
     boundary.
  2. Append it onto the child's own log as a `sandbox.mode` event with
     `source: delegation` during unpublished setup (after any fork seed) — so
     fresh policy wins stale seed state and the child's effective policy is
     reconstructable from its own log alone. The deployment default is never
     copied: an unswitched parent stamps nothing.
  3. Pin the child's approval policy to **never** regardless of the parent's —
     a delegated child acts only within its inherited sandbox scope and every
     ask is rejected deterministically instead of waiting on a prompt no one is
     watching. Reuse `multiagent/approval.go`'s gate in deny-by-default mode for
     workers (the typed `RequestResponse` gate already exists; this is the
     wiring + policy).
- Model-facing statement (port of `subagent:delegation`): a scoped runtime
  context message telling the child the scope is fixed and a task needing wider
  access ends with a reported limitation, not retries.
- Policy is fixed at spawn: a parent switch after creation never retroactively
  changes the child (cold-resume replay semantics land with Phase 2.10).

Tests: child log reconstructs policy from its own events; approval denied
deterministically with no prompt; parent switch after spawn has no effect;
unswitched parent → child follows deployment default dynamically; wider-access
task ends with a limitation result.

## Phase 2.5 — Skill invocation policy + digest catalog + provider seam (`internal/plugin` + `internal/tool`)

Port of DSH `skill/skill` (registry + invocation policy + provider seam) and
`skill/tool-skill` (catalog digest + replacement + canonical rendering).

- `internal/plugin/registry.go` — `SkillEntry` gains
  `Invocation { ModelInvocable, UserInvocable bool }`; one discovery result
  serves model-facing tools, human commands, and trusted internal callers
  without conflating catalogs. Existing entries default to
  `{true, true}` (backward compatible).
- Provider seam: `SkillProvider` interface (`List(cwd)`, `Get(name)`) +
  `Register(provider) → disposer`, with a runtime provider for embedded/bundled
  skills and the existing community-registry installs as the filesystem
  provider. Layer semantics: host rows in the global layer; per-scope layers
  shadow the global on duplicate names (nearest wins), rank only within a layer.
- Digest catalog (port of `tool-skill`):
  - Digest = hash of the sorted `(name, description)` pairs of
    model-invocable skills for the session cwd.
  - Published as a durable user-role context message only when the digest
    changes; an empty catalog publishes a tombstone that retires older names.
  - No digest change → no republished catalog (token savings on every pre-step).
- `tool.SkillTool` returns canonical `{name, provider, resourceBase?, content}`
  (port of `renderSkillContent`); `modelInvocable=false` → distinct error;
  `userInvocable` does not restrict the model-facing tool. `skill` tool
  visibility participates in the digest.

Tests: policy filtering on both surfaces; digest stability across unchanged
sessions; replacement-on-change; tombstone; provider shadowing; cwd-scoped
discovery; runtime provider registration/disposal.

## Phase 2.6 — SQLite FTS session query (`internal/sessionquery`)

Revives gap `#21` in **query-only** scope (no migration of the JSONL store; the
JSONL file stays the source of truth). Port of DSH
`session-query/session-query-sqlite` semantics over rho's session logs.

- New `internal/sessionquery` package:
  - FTS5 index (external content or contentless) over the session store,
    populated by a background incremental indexer that tails new/rewritten
    `.jsonl` files. Index is derived and rebuildable — never authority.
  - `Search(sessionID | workspace, query, filters, page)` returns bounded,
    redacted results (apply `shrike.SecretDetector` + the session export redaction
    path at the read boundary).
  - Workspace authorization: callers may only search sessions they own /
    the current workspace (mirror of DSH's authorized-read contract).
- Model-facing `SessionQueryTool` following the existing tool shape
  (Name/Aliases/Parameters/Execute), with byte-bounded result pages so results
  re-entering context are capped (same cap philosophy as the jobs tool,
  Wave 1 Phase 15).

Tests: tokenization/FTS queries, authorization denial, paging, redaction of
secrets, index rebuild from scratch, incremental tail after session rewrite.

## Phase 2.7 — Session-log-backed schedule (`internal/schedule` + tool)

Port of DSH `schedule/schedule`: reminders whose durable state lives in the
session log; no external cron; due work enters the same conversation through the
agent's ordinary follow-up queue. This is an alternative to — not a replacement
for — the existing host cron (`CronCreate`/`CronDelete` tools); it is the
**in-conversation** primitive.

- New eventlog types (all log-only, no surfaceOp): `schedule.create`,
  `schedule.update`, `schedule.delete`, `schedule.due`.
- New `internal/schedule` package:
  - `Fold` over the events → view (create/list/delete model-facing tools:
    `ScheduleCreate`, `ScheduleList`, `ScheduleDelete`).
  - A process-local timer owner keys on the live root agent of the session;
    cold sessions resume overdue work when they become live again; never
    implies an external notification channel.
  - Due delivery goes through the engine's follow-up/inbox seam
    (`Session.Followup`-style), so the reminder arrives in-conversation with
    full session context.
- Wire the model-facing tools into the tool registry; leave the existing
  `CronCreate`/`CronDelete` untouched (out-of-conversation scheduling remains
  useful).

Tests: durable record, view fold, due delivery via follow-up queue, cold-session
resume of overdue work, no external channel, create/list/delete round-trip.

## Phase 2.8 — ACP client + external-agent subagent providers (`internal/acp` + `internal/multiagent`)

Port of DSH `acp/` (client side) + `subagent/subagent-acp`, `subagent-claude-code`,
`subagent-codex`. Rho has the ACP **server** (Wave 1 `internal/acp/server.go` —
initialize, session/new, session/prompt, streamed updates,
session/request_permission); this phase adds the client so rho's mission mode
can **delegate to** other ACP agents, Claude Code, and Codex.

- `internal/acp/client.go`:
  - Newline-delimited JSON-RPC 2.0 over stdio (mirror the framing of
    `internal/mcp` and the ACP server — same `rpcMessage`/`rpcError` types).
  - `Client.Connect(cmd, args)` spawns and handshakes (`initialize`);
    `NewSession()`, `Prompt(sessionID, msgs)`, `Cancel(sessionID)`,
    `RequestPermission` callback for client-routed approvals (map to
    `multiagent/approval.go`'s gate).
  - Timeout + teardown contract: a failed handshake cleans up the child
    process; every path disposes the process tree (see Phase 2.12 tree-kill
    helper).
- `internal/multiagent` provider registry (port of `SubagentRuntime`):
  - `RegisterProvider(name, provider) → disposer`; `Start(name, req)`;
    `List()`; provider `capabilities` (`outputSchema`, `depthLimit`,
    `toolFilter`, `persona`) so unsupported requests reject before child
    creation.
  - Providers: `acp` (generic), `claude-code`, `codex` — each resolves its CLI,
    launches via the client, and adapts results to the existing
    `Handoff`/worker result contract.
  - One-shot semantics first (`SubagentRun` = disposable foreground delegation
    with one result, no cold-resume); continuable children land in Phase 2.10.

Tests: wire framing parity with the server; initialize handshake failure
cleanup; prompt/cancel lifecycle; structured output (`outputSchema`); provider
capability rejection; disposal idempotence; process-tree cleanup on exit.

## Phase 2.9 — Persistent PTY terminal tool (`internal/terminal`)

Revives gap `#25`. Port of DSH `terminal/pty` (backend registry, branded ids,
exact-session ownership, awaited cleanup) + `terminal/terminal-bash` (readiness
detection, bounded state, sandbox policy) + `terminal/tool-terminal` (six
model-facing tools). Complement to the one-shot bash tool: state persists across
tool calls and supports interactive stdin.

- New `internal/terminal` package:
  - `TerminalStore` — backend registry, branded ids (`terminal-<N>`), exact
    session ownership (a terminal belongs to the session that created it;
    cross-session access denied), awaited cleanup on owner release.
  - Local backend over the sandbox executor: PTY allocation with zero-CGO on
    Linux (`syscall`-based pty or a maintained pure-Go pty; verify
    `github.com/creack/pty`'s CGO status before adoption — zero-CGO is a hard
    rho constraint); Windows via `ConPTY` later, outside this phase.
  - Sandbox enforcement: the backend applies the resolved Phase 2.3 policy
    (mode + workspace root) before exec, fail-closed.
  - Bounded reads: per-read byte cap; no unbounded buffering in model-facing
    results.
- Six tools: `TerminalCreate`, `TerminalSend`, `TerminalRead`,
  `TerminalList`, `TerminalResize`, `TerminalKill` — following the existing
  tool shape (Name/Aliases/Parameters/Execute).

Tests: lifecycle, ownership enforcement (cross-session deny), bounded reads,
readiness detection, cleanup on owner release, sandbox deny, cancellation
terminates the PTY tree.

## Phase 2.10 — Continuable subagents + cold resume (`internal/multiagent` + `internal/eventlog`)

Port of DSH `subagent/subagent` continuable children: durable child sessions
with FIFO follow-up and **cold resume** from the persisted log. Depends on
Phase 2.8 providers and Phase 2.3/2.4 policy replay.

- New eventlog type `subagent.descriptor` (log-only): versioned payload with
  `{provider, mode, label, parentSession, resolved model, persona?, toolFilter?}`
  — explicit fields, never a merge-extensible options object. Persisted
  `SessionHeader.delegationDepth` is authoritative and monotone (runtime options
  may deepen, never lower).
- `internal/multiagent` continuable path:
  - `StartContinuable(spec)` establishes a durable child and delivers the
    initial prompt without waiting for the turn to start; any pre-publication
    failure rolls the child back entirely.
  - Resident child = one process-local **Activation** (one residency epoch for
    a reconstructed child Agent); the Agent inbox is the only turn queue.
  - `Followup(parent, childID, content)` — FIFO next turn; routing by
    residency: running enqueues, waiting wakes, absent **cold-resumes** from
    the persisted Session + folded descriptor.
  - Follow-up authority comes from the exact live direct parent recorded in the
    child's durable header, rechecked at cold resume; stale/self-targeting/
    non-ancestor callers reject with `UNAUTHORIZED`.
  - Cold resume replays Phase 2.3/2.4 delegation events instead of re-capturing
    the parent — a parent switch after creation never retroactively changes a
    durable child.
- `ListChildren`/`ListDescendants` read the session store directly (live-only
  enumeration when persistence is absent).

Tests: descriptor validation; cold-resume reconstruction; FIFO follow-up order;
authority rejection (stale / self-targeting / non-ancestor); depth
monotonicity; policy replay on cold resume; dispose idempotence; rollback on
pre-publication failure.

## Phase 2.11 — Native Windows ACL sandbox (`internal/sandbox`)

> **Obsolete specification — retained for historical provenance only.** Do not
> implement this phase. Rho no longer provides a local OS/container sandbox or
> Docker fallback on any platform.

The original proposal ported DSH `sandbox/sandbox-windows-acl` and assumed a
cross-platform confinement backend. That assumption is no longer part of
Rho's local product architecture.

- `internal/sandbox/windows_acl.go` (`//go:build windows`):
  - Implements the same `Sandbox` interface as `landlock.go`
    (`Apply`, `AddReadOnlyPath`, `AddReadWritePath`, `Available`).
  - Deny ACEs via `golang.org/x/sys/windows` security descriptors on paths
    outside (project dir + scratch dir); read-only vs read-write via ACE
    access masks.
  - `Available` probes the API surface (Win7+ ACL semantics) and returns false
    when confinement cannot be guaranteed — fail-closed preserved.
- `landlock_other.go` shrinks to darwin/other only (macOS seatbelt deferred,
  unchanged stub).
- CI: cross-compile gate (`GOOS=windows GOARCH=amd64 go build ./...`) plus a
  Windows runner gate exercising `Apply`/`AddReadOnlyPath`/`AddReadWritePath`
  against a scratch tree.

Tests: availability detection; Apply + path rules on the Windows CI runner;
deny enforcement (write outside project fails); read-only vs read-write
distinction; fail-closed when unavailable; cross-compile green on non-Windows
hosts.

## Phase 2.12 — LSP transient-open + env scrubbing (`internal/lsp`)

Port of DSH `lsp/lsp-stdio` operational semantics. `internal/lsp/` currently has
`client.go`/`manager.go`/`config.go`; this phase audits and aligns the client
lifecycle with DSH's:
- **Transient-open sequence**: per query — resolve and byte-bound the source
  through rho's fs, `textDocument/didOpen` (version 1, full text), the
  requested request, then `textDocument/didClose` in `finally`. No document
  cache, no LRU, no `didChange`; a failed/canceled `didOpen` write terminates
  the server instance before the pool can reuse it.
- **Per-workspace queue**: serialize each source-read/open/query/close lifecycle
  through one abortable per-workspace queue so queued calls read current source
  only when their turn starts; distinct workspaces run in parallel.
- **Retry-once**: if the pooled transport fails before/during a read-only
  query, await disposal and retry once on a fresh server process.
- **Tree-kill on shutdown failure**: after protocol shutdown fails, terminate
  the server's descendant tree (POSIX process-group signaling; Windows
  `taskkill /T /F`), then confirm quiescence by tree-liveness wait. Reuse this
  helper in Phase 2.8's ACP client teardown.
- **Env scrubbing**: before spawn, scrub ambient env vars matching
  `KEY`/`PASSWORD`/`SECRET`/`TOKEN` — reuse `shrike.SecretDetector` /
  `shrike.IsSensitiveFilename` from the Shrike repository; explicit config values
  merge after the scrub.
- `initialize.processId` stays `null` (another PID namespace must not monitor
  the host process — same reasoning as DSH).

Tests: transient-open lifecycle (didOpen v1 → request → didClose always runs);
queue serialization per workspace; retry-once-on-fresh-process; tree-kill both
platforms (POSIX group on linux CI, taskkill contract on Windows CI); env
scrubbing (KEY/PASSWORD/SECRET/TOKEN absent from child env); processId null.

---

## Gates (each phase)

`gofmt`/`go vet`/`go test -race` on the touched packages, then `make lint` and
`rho verify`, run **twice** (second pass re-runs the full touched suite after
any fixes). `scripts/check-internal-layer-imports.sh` must stay green. No direct
commits to `main` — each phase is its own feature branch
(`feat/dsh-harness-rfc-<phase>`), opened from `main`, with a PR and green CI.

## New dependencies (projected)

| Phase | Dependency | Why |
| --- | --- | --- |
| 2.6 | none (stdlib `database/sql` + modernc sqlite or `mattn`? decide in phase) | FTS5 index; zero-CGO requirement favors `modernc.org/sqlite` |
| 2.9 | pty allocation (verify zero-CGO; fallback: pure-Go `syscall` on linux) | PTY backend |
| 2.11 | `golang.org/x/sys` (already indirect) | Windows security descriptors |

Every new direct dependency must be justified in the phase PR and pass the
dependency-lean review.
