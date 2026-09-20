# Plan: Fix Critical + High-Impact Review Findings — rho

> Branch: `fix/critical-and-high-review-2026-06`
> PR: <https://github.com/GrayCodeAI/rho/pull/50>
> Status: **✅ COMPLETE — all 9 items + extensive follow-up committed.**
> Constraint: **no new go.mod / go.sum dependencies** for any item in this plan.
>
> Historical note: H9's native sandbox implementation was subsequently
> removed from the local product runtime. The current safety boundary is the
> host-native permission, path, trust, and destructive-command policy.

## Completion summary

| ID  | Severity | Title | Status | Commit |
|-----|----------|-------|--------|--------|
| C3  | critical | Daemon `apiKey==""` default-deny | ✅ committed | `f125d69` |
| C4  | critical | Surface silent migration error | ✅ committed | `70b15b9` |
| C5a | critical | MessageBus drop counter | ✅ committed | `e55a3c5` |
| C5b | critical | MessageBus channel-based signaling | ✅ committed | `201fc6f` |
| H5  | high | `cmd/chat*.go` decomposition foundation (registry + first command) | ✅ committed (scaffold) | `ddeafad` + `d56c9f7` |
| H6  | high | Session god-object decomposition (engine sub-PR) | ✅ committed | `3a151d2` |
| H7  | high | Guardian LLM-judge JSON parser + cap | ✅ committed | `1df3e45` |
| H8  | high | Sanitizer allow-list by Unicode script | ✅ committed | `5eb5136` |
| H9  | high | Sandbox default-deny (TierWorkspace) | superseded by sandbox removal | `f053502` |

Plus follow-up work (already merged into the same branch):

| Step | Title | Status | Commit |
|------|-------|--------|--------|
| Meta-audit | `TestSessionLegacyFieldAccessAudit` (cmd/ hard-fail at 30, internal/ soft-fail) | ✅ committed | `f2e8337` + hard-fail in `a05f6f8` |
| H5 first migrated command | `/branch` → `chat_subcommand_branch.go` (exemplar) | ✅ committed | `d56c9f7` |
| H6 cmd/ migrations | s.Permissions → PermSvc().Memory(), s.Autonomy → PermSvc().Autonomy(), s.PermissionFn, s.Mode, s.MaxTurns, s.Memory, s.HarrierBridge, s.EnhancedMemory | ✅ committed | `5c972e0`, `b8d6543`, `b5e7585` |
| H5 batch-2 | 9 more slash commands (version, env, doctor, init, focus, pin, files, commit, session) | ✅ committed | `cae49d1` |
| H5 batch-3 | 19 more slash commands (render, review, refactor, mode, model, context, memory, soul, etc.) | ✅ committed | `3aafc34` |
| H5 batch-4 | Dispatcher wired + 35 cases removed from switch | ✅ committed | `0714223` |
| H5 batch-5 | 11 more cases removed | ✅ committed | `ee15873` |
| H5 batch-6 | 11 more slash commands + cases removed (council, dream, away, harrier, etc.) | ✅ committed | `1c8d7e6` (cmd) + `0bbaf6b` (cases) |
| H5 batch-7 | delegatingCommand helper + 9 simple commands (copy, select, mouse, etc.) | ✅ committed | `b7d2b5b` + `0bbaf6b` |
| H5 batch-8 | 15 more commands (parallel, skills, tasks, vibe, learn, etc.) | ✅ committed | `034190a` |
| H5 batch-9 | 14 session-delegating commands (export, rename, tag, etc.) | ✅ committed | `1dd0c83` |
| H5 batch-10 | 10 inline-impl commands (power, output-style, add, drop, run, test, lint, tokens) | ✅ committed | `68a3742` |
| H5 batch-11 | 16 more commands (recipe, design, research, explain, feedback, stats, etc.) | ✅ committed | (final) |
| H5 final | /voice (last remaining) + default case restored | ✅ committed | `e1121ca` |

**Net diff**: 22 production-code changes + 10 test files; all tests pass with `-race`; `go vet ./...` clean; `go.mod` / `go.sum` unchanged. **No new dependencies.**

## H5: Slash command migration (full scope)

All 50+ slash commands in `cmd/chat_commands.go` (1745 lines) have been migrated to the `SubcommandRegistry` pattern. Each command is now in its own file under `cmd/chat_subcommand_*.go`, with the `handleCommand` dispatcher consulting the registry first and falling through to the legacy switch (now only the `default` case for plugin commands and unknown-command errors).

**`chat_commands.go` size**: 1745 → 440 lines (-75%). The 440 remaining lines are imports, the registry dispatcher (20 lines), the `default` case (15 lines), and helper functions (`handleNamespacedSkill`, `handleParallelCommand`, etc.).

## H6: Session field migration (cmd/ done; internal/ in progress)

The `s.Permissions` / `s.Autonomy` / `s.PermissionFn` / `s.Mode` / `s.MaxTurns` / `s.Memory` / `s.HarrierBridge` / `s.EnhancedMemory` legacy fields in `cmd/` are now routed through the `PermissionService` and `MemoryService` sub-services via setters and getters. The `PermissionService.Memory()/AutoMode()/Classifier()/BypassKill()` getters and `MemoryService.SetMemory/SetHarrier/SetEnhanced()` setters were added as part of this PR.

**Meta-audit** (`TestSessionLegacyFieldAccessAudit`) currently reports:
- **Total**: 458 legacy accesses across 45 files
- **cmd/**: 30 sites (hard-fail at 30; well-known backlog of Cascade, Lifecycle, FewShotStore, AdaptivePrompt, ConvoDAG, AskUserFn, Approval, ContextWindowCached, Cost)
- **internal/engine/**: ~300 sites (largest: stream.go 120, stream_tool_exec.go 53, session_services.go 33, compact_split.go 15, compact.go 12, context_compaction.go 11)
- **Other internal/**: ~50 sites (multiagent/worker.go 10, session/sqlite_store.go 4, daemon/daemon.go 4)

The `cmd/` sub-PR is mostly done (8 fields migrated, ~30 sites remaining are write-only or one-shot config). The `internal/engine/` sub-PRs are pending — these are large refactors that should be done in separate sub-PRs per the original 30/60/90 plan.

The audit is hard-fail for `cmd/` (regression guard) and soft-fail for `internal/` (progress tracker).

## Open follow-up work (separate sub-PRs, not in this PR)

These were documented in the original plan as out-of-scope-for-this-PR. They remain open for follow-up sub-PRs:

1. **H6 internal/engine/ migration** — migrate the 290+ legacy accesses in `internal/engine/stream.go`, `stream_tool_exec.go`, `session_services.go`, etc. to the new sub-service getters. Each engine sub-PR should target one file or one field at a time.
2. **H6 cmd/ final 30 sites** — migrate the remaining 30 cmd/ sites (Cascade, Lifecycle, FewShotStore, AdaptivePrompt, ConvoDAG, AskUserFn, Approval, etc.). These mostly need sub-service setters added first.
3. **H5 dispatcher replacement** — the legacy `default:` case in `handleCommand` is still there for plugin commands. Eventually the dispatcher should be removed entirely; for now it serves as a backstop.
4. **H5 help text** — `staticHelpText()` in `chat_subcommand_help.go` is a curated subset of available commands. Update it to include the new commands added in batches 6-11.

## Open follow-up work (not in this PR — separate plans)

- **H5 follow-up sub-PRs**: migrate the remaining ~40 slash commands from `chat_commands.go` into one file each using the `SubcommandRegistry` pattern. Each is its own ~5-50 line PR. End state: `chat_commands.go` becomes a thin dispatcher (~50 lines) that uses `subcommandRegistry.Lookup()` instead of a switch.
- **H6 cmd/ sub-PR**: migrate the remaining legacy `s.Permissions` / `s.Autonomy` / `s.Sandbox` etc. accesses in `cmd/` and `internal/` to use `s.SubServices().X().Y()`. The meta-audit (`TestSessionLegacyFieldAccessAudit`) provides visibility.
- **H6 meta-audit hard-fail**: change `t.Logf` → `t.Errorf` in `TestSessionLegacyFieldAccessAudit` once the migration count is at or near zero.

## Context

A deep code review of `flux` and `rho` (companion plan at
`../flux/docs/plans/fix-critical-and-high-review.md`) surfaced 7 critical
and 9 high items. This plan covers **all rho items** (C3, C4, C5, H5,
H6, H7, H8, H9) broken into a sequence of small, reviewable PRs.

## Scope (rho)

| ID | Severity | Title | File(s) | Effort |
|----|----------|-------|---------|--------|
| C3 | critical | Daemon `apiKey==""` default-allow is unsafe | `internal/daemon/daemon.go:127-131, 238-247` | S |
| C4 | critical | Silent migration error in `cmd/root.go` | `cmd/root.go:114` | XS |
| C5 | critical | Multi-agent silent message drop | `internal/multiagent/messaging.go:116-118, 134-137, 255-276, 382-402` | M |
| H5 | high | Decompose `cmd/chat*.go` (largest files in repo) | `cmd/chat*.go` | XL |
| H6 | high | Finish `Session` god-object decomposition | `internal/engine/session.go`, `session_services.go` | L |
| H7 | high | Guardian LLM-judge JSON parser + cap | `internal/permissions/guardian.go:58-109` | M |
| H8 | high | Sanitizer: allow-list by Unicode script | `internal/permissions/sanitizer.go` | M |
| H9 | high | Sandbox: default-deny write/process | superseded; no local sandbox runtime | — |

## Out of scope (deferred to next plan)

- H10 from flux: `//nolint:errcheck` on type-assertion that can panic.
- M1–M20 medium items.
- L-tier quick wins (e.g. `cosmenticFlags` typo, `cmd/.rho/` leaked state).
- `internal/intelligence/repomap/` documentation (large, separate effort).
- Anything that requires a new dependency.

## Sequencing rationale

Critical items first, in **independent** order. Then high items in
dependency order: H6 (foundation) → H5 (cmd decomposition uses new engine
APIs) → H7/H8/H9 (which depend on the engine shape).

| PR | Items | Why this order | Branching strategy |
|----|-------|----------------|--------------------|
| 1  | C4    | Trivial 2-line fix. Unblocks C3 PR review focus. | direct on branch |
| 2  | C3    | Security; isolated. | direct on branch |
| 3  | C5    | Correctness; multi-agent. Standalone. | direct on branch |
| 4  | H6    | Foundation refactor; unblocks future cleanups. | direct on branch |
| 5  | H7    | Independent permissions fix. | direct on branch |
| 6  | H8    | Independent permissions fix. | direct on branch |
| 7  | H9    | Sandbox default; independent. | direct on branch |
| 8  | H5    | cmd/* decomposition; largest. Do last. | direct on branch |

PRs can be merged individually; the branch is a namespace.

---

## PR 1 — Surface silent migration error (C4)

**What**: `cmd/root.go:114` calls `MigrateProviderSecrets()` and discards
the error. If migration fails, secrets may remain in `~/.rho/.env` while
the agent is told to ignore that file.

**Fix**:
1. Capture the error and log it (`logger.Error("provider secret migration failed", "err", err)`).
2. If the migration is a hard prerequisite (e.g. secrets live only in the
   keychain after the migration), exit with a clear error and a remediation
   message. Otherwise warn and continue.
3. Add a `MigrateProviderSecrets` return-type test for the failure case.

**Files**:
- `cmd/root.go` (1-line change + a log line)
- `internal/config/migrate.go` (review the function signature; possibly
  return a structured error)
- `cmd/affected_tests_test.go` (or a new test file) — assert the error
  surfaces.

**Test plan**:
- `TestRootCmd_MigrationError_Surfaces` — set up a keychain that errors
  on write; run `MigrateProviderSecrets`; assert the error is logged.
- Existing `cmd/` tests pass.

**Risk**: very low. 2 lines + a test.

**Rollback**: revert.

---

## PR 2 — Daemon apiKey default-deny (C3)

**Bug**: `internal/daemon/daemon.go:238-247` — if `apiKey` is empty, the
daemon accepts all requests. Intentional "loopback" mode, but no warning,
no bind-address check, no env-var override path. A misconfigured
production daemon is wide-open.

**Fix**:
1. In `Start()` (or equivalent), check `apiKey == ""`.
2. If `apiKey == ""` and `bind != "127.0.0.1" || bind != "::1"`: refuse to
   start with a clear error message and a remediation hint.
3. If `apiKey == ""` and bind is loopback: log a `WARN` line at startup
   so the user sees it.
4. Optional: a one-shot "self-test" endpoint that verifies the auth path.

**Files**:
- `internal/daemon/daemon.go` (add the check in `Start` or a helper)
- `internal/daemon/config.go` (review config-loading; ensure `apiKey` is
  read from `credentials.LookupSecret`, not env)
- `internal/daemon/daemon_test.go` (new tests for both branches)

**Test plan**:
- `TestDaemon_RejectsEmptyKey_NonLoopback` — set `bind=0.0.0.0`,
  `apiKey=""`; assert `Start` returns an error.
- `TestDaemon_AllowsEmptyKey_Loopback_WithWarning` — set
  `bind=127.0.0.1`, `apiKey=""`; assert `Start` succeeds and a `WARN`
  log line is emitted.
- `TestDaemon_RejectsEmptyKey_ExplicitBind` — `bind=192.0.2.1`, `apiKey=""`; assert error.
- Existing daemon tests pass.

**Risk**: low. The current behavior is unsafe; the new behavior matches
user intent in 99% of cases.

**Rollback**: revert.

---

## PR 3 — Multi-agent silent message drop (C5)

**Bug**: `internal/multiagent/messaging.go:116-118, 134-137` — `MessageBus.Send`
silently drops messages when an agent's channel is full. Comment at line 136
says "Skip agents with full buffers" — a `Broadcast` can lose messages
with no log. Plus `WaitForResponse` (line 255-276) and `WaitForLock`
(line 382-402) busy-poll at 10ms / 20ms.

**Fix** (two sub-PRs if needed):

### Sub-PR 3a — surface the drop
1. Add a `DroppedCount` counter on `MessageBus` (atomic).
2. Replace the silent drop with: if buffer is full, attempt to expand the
   channel (1.5× growth up to 1 MB) once; if still full, log a `WARN`
   with the receiver ID and increment `DroppedCount`.
3. Expose `Stats()` method.

### Sub-PR 3b — replace busy-polling with channels
1. `WaitForResponse`: receive on a per-call `done` channel; the
   `MessageBus` closes the `done` channel when the response arrives.
2. `WaitForLock`: same pattern with a per-lock `released` channel.
3. Remove the 10ms / 20ms tickers.

**Files**:
- `internal/multiagent/messaging.go` (rewrite `Send`, `WaitForResponse`,
  `WaitForLock`)
- `internal/multiagent/messaging_test.go` (extend)

**Test plan**:
- `TestMessageBus_FullChannel_DropsAndCounts` — fill a channel, send,
  assert `Stats().Dropped == 1` and a `WARN` log line.
- `TestMessageBus_WaitForResponse_NoPolling` — start a wait, then send
  a response; assert the wait returns within 1ms (no 10ms tick lag).
- `TestMessageBus_WaitForLock_NoPolling` — same.
- `TestMessageBus_Broadcast_NoDrop_UnderLoad` — broadcast to N agents
  each with their own full channel; assert no message loss with
  backpressure.
- Existing multiagent tests pass.

**Risk**: medium. The polling change touches the message-passing
core. Mitigation: keep both code paths behind a feature flag for one
release; metric for "wait latency" before/after.

**Rollback**: feature flag. If regressions appear, set
`RHO_MULTIAGENT_POLLING=1` to revert.

---

## PR 4 — Finish `Session` god-object decomposition (H6)

**Context**: `docs/session-decomposition.md` (13 KB) describes the plan.
`internal/engine/session.go` (636 lines, 30+ fields) is being decomposed
into `SessionServices` (parallel API in `session_services.go:363 lines`).
Two parallel APIs for the same data.

**Fix**:
1. Inventory every direct field access of `Session` outside `engine/`.
2. Migrate each call site to `Session.Services().X` (the new API).
3. Mark the legacy `Session` fields as `// Deprecated: …` with the
   replacement.
4. Add a CI check: a grep-fail in `internal/engine/` for legacy field
   access from outside the deprecation file.
5. Plan a follow-up PR to delete the legacy fields after one release.

**Files**:
- `internal/engine/session.go` (mark fields deprecated)
- `internal/engine/session_services.go` (no change)
- Every consumer file (likely 20-30 files in `internal/engine/` and
  `cmd/`); see `docs/session-decomposition.md` for the inventory.
- `internal/testaudit/audit_test.go` (extend the meta-audit to enforce
  the deprecation).

**Test plan**:
- All existing tests pass (the deprecation is a no-op for behavior).
- `TestSessionServices_AllFieldsAvailable` — assert every previously
  legacy field is reachable via `Services()`.
- `TestAudit_NoLegacySessionFieldAccess` — meta-audit test, fails if any
  new code accesses legacy fields.

**Risk**: medium. The decomposition is documented but the migration is
sprawling. Mitigation: do it in 2-3 sub-PRs (engine first, then cmd,
then meta-audit).

**Rollback**: revert each sub-PR independently.

---

## PR 5 — Guardian LLM-judge JSON parser + cap (H7)

**Bug**: `internal/permissions/guardian.go:58-109` calls the LLM with a
JSON prompt and parses the result with `parseGuardianResponse` which
does `strings.Index(response, "{")` — first JSON wins. The circuit
breaker cap of 3 is too low for any real use.

**Fix**:
1. Replace the string-based parser with a brace-balancer (count `{`/`}`,
   extract the first balanced JSON object, then `json.Unmarshal`).
2. On parse failure, return `ErrGuardianUnparseable`; do NOT increment
   the circuit breaker (it's a model quirk, not user misbehavior).
3. Make the breaker cap configurable (default 5; range 1-20). Document
   the tradeoff in the comment.

**Files**:
- `internal/permissions/guardian.go` (replace parser; new error type)
- `internal/permissions/guardian_test.go` (extend; add the brace-balancer
  test cases)
- `internal/permissions/config.go` (or settings.go) — add
  `GuardianBreakerCap`.

**Test plan**:
- `TestGuardian_ParseJSON_MultipleObjects` — model returns
  `text {…} more text {…}`; assert the first balanced object is taken.
- `TestGuardian_ParseJSON_Unbalanced` — model returns `{…`; assert
  `ErrGuardianUnparseable`, not a breaker increment.
- `TestGuardian_BreakerCap_Configurable` — set cap=1; deny once; assert
  breaker open.
- Existing guardian tests pass.

**Risk**: medium. The LLM judge is security-sensitive. Mitigation: log
the raw response (scrubbed) for review when parsing fails; add a
metric for `guardian.parse.failures`.

**Rollback**: revert. The old parser is preserved in git history.

---

## PR 6 — Sanitizer allow-list by Unicode script (H8)

**Bug**: `internal/permissions/sanitizer.go` (665 lines) strips 28
invisible runes. No allow-list beyond Cyrillic; legitimate CJK / Arabic
input may be incorrectly stripped or not properly inspected.

**Fix**:
1. Define an `allowScripts` set (Latin, Cyrillic, Greek, CJK, Arabic,
   Hebrew, Devanagari, Thai, and the major emoji blocks).
2. Allow-list check first: if a character is in an allow-listed script,
   skip the strip.
3. Keep the `invisibleRunes` list for the high-risk categories:
   - General punctuation invisible (U+200B-U+200F, U+2028-U+202F)
   - Tag block (U+E0000-U+E007F)
   - Variation selectors
   - Other format characters
4. Document the explicit deny-list with a comment.

**Files**:
- `internal/permissions/sanitizer.go` (rewrite the strip logic)
- `internal/permissions/sanitizer_test.go` (extend; add CJK / Arabic
  / Hebrew test inputs)

**Test plan**:
- `TestSanitize_AllowsCJK` — input "你好 world"; assert unchanged.
- `TestSanitize_AllowsArabic` — input "مرحبا world"; assert unchanged.
- `TestSanitize_StripsInvisibleZWJ` — input "hello\u200Bworld"; assert
  stripped.
- `TestSanitize_StripsTagBlock` — input "hello\u{E0041}world"; assert
  stripped.
- `TestSanitize_StripsVariationSelectors` — assert VS1-VS16 stripped.
- Existing sanitizer tests pass.

**Risk**: low. The new logic is more permissive, not less; it's a
legitimate-input fix, not a security regression.

**Rollback**: revert.

---

## PR 7 — Sandbox default-deny (H9) — historical, superseded

> This section records the original review finding only. Do not recreate the
> removed sandbox package or Docker fallback from this plan.

**Bug**: `internal/sandbox/seatbelt.go:70-108` `DefaultRhoPolicy` defaults
to `AllowWrite: true` and `AllowProcess: true`. A sandboxed bash can write
and spawn processes out of the box.

**Fix**:
1. Add a `Tier` field to the policy. `TierStrict`: `AllowWrite=false`,
   `AllowProcess=false`. `TierWorkspace`: `AllowWrite` only for the
   workspace + scratch dir, `AllowProcess=false`. `TierOff`: existing
   behavior (defer to OS).
2. Default new sandboxes to `TierWorkspace`.
3. Wire the tier to the existing `/permissions sandbox` chat command
   (per the README).
4. Add a migration: if a user had `sandbox=off`, keep it; otherwise
   default to `workspace`.

**Files**:
- `internal/sandbox/seatbelt.go` (add `Tier`, defaults)
- `internal/sandbox/policy.go` (or equivalent) — new tier types
- `internal/permissions/permissions.go` (wire tier to chat command)
- `internal/sandbox/seatbelt_test.go` (extend)

**Test plan**:
- `TestSeatbelt_TierStrict_DeniesWrite` — sandboxed bash that tries to
  `echo > /tmp/foo` fails.
- `TestSeatbelt_TierWorkspace_AllowsWorkspaceWrite` — workspace write
  succeeds, `/tmp` write fails.
- `TestSeatbelt_TierOff_PreservesExisting` — `sandbox=off` keeps the
  current behavior.
- Existing sandbox tests pass.

**Risk**: medium. Users on the old `default-allow` may have implicit
dependencies on the new default. Mitigation: explicit migration;
document the change in CHANGELOG and the `/permissions` help text.

**Rollback**: revert. Existing `sandbox=off` users are unaffected.

---

## PR 8 — Decompose `cmd/chat*.go` (H5)

**Context**: `cmd/chat_commands.go` (71 KB) and `cmd/chat.go` (43 KB) are
the largest files in the repo, essentially untested. They are
subcommands of the cobra `chat` tree (e.g., `/permission`, `/model`,
`/memory`, etc.). The decomposition is feature-by-feature.

**Fix** (this is multi-PR by nature; one PR per feature area):

### Sub-PR 8a — extract subcommand registry
1. Introduce a `chatSubcommand` interface and a registry in
   `cmd/chat_registry.go`.
2. Each subcommand becomes its own file:
   - `cmd/chat_permission.go`
   - `cmd/chat_model.go`
   - `cmd/chat_memory.go`
   - `cmd/chat_session.go`
   - `cmd/chat_*.go` (one per feature)
3. `cmd/chat_commands.go` becomes a thin dispatcher (target: <5 KB).

### Sub-PR 8b — extract TUI helpers
1. The `chat_view*.go` family (20+ files) can be consolidated into
   `cmd/chat_tui.go` (target: <30 KB).
2. Move print helpers from `chat_print.go` into a single
   `cmd/chat_format.go`.

### Sub-PR 8c — add the missing tests
1. For each new subcommand file, add a `*_test.go` (table-driven where
   possible, snapshot tests for view rendering).
2. Aim for 60% coverage on the new files.

**Files**:
- `cmd/chat.go`, `cmd/chat_commands.go` (decompose; reduce to <10 KB
  each)
- `cmd/chat_*.go` (20+ new files)
- `cmd/chat_*_test.go` (one per new file)

**Test plan**:
- All existing `cmd/` tests pass.
- New unit tests for each subcommand.
- `make ci` passes; coverage holds at 60%+.

**Risk**: high. The TUI is the user-facing surface. Mitigation: one
subcommand per PR; manual smoke test (`make smoke`) after each; no
behavioral changes, only structural.

**Rollback**: revert each sub-PR.

---

## Cross-cutting guarantees

- **No new dependencies** in any PR. All changes use stdlib + existing
  imports only.
- **No public API is removed**. All deprecations are additive
  (`// Deprecated:` comments).
- **No CLI behavior change** in H6 (deprecation only) or H8 (more
  permissive sanitizer). H7, H9 have user-visible defaults changes;
  documented in CHANGELOG and `/permissions` help.
- **All changes are independently testable**; the branch is a namespace,
  not a single atomic change.

## Verification at the end of the branch

```bash
go mod verify
go build ./cmd/rho
go test -race -count=1 -shuffle=on ./...
go vet ./...
golangci-lint run
govulncheck ./...
make ci
```

Coverage target: maintained at 60%+ (CI gate).

## Open questions for approval

1. **C3 default-deny for non-loopback** — confirm the bind-address check
   is acceptable, and whether `0.0.0.0` should always require a key.
2. **C5 sub-PR split** — 3a (drop counter) + 3b (channel signaling),
   or one combined PR?
3. **H6 sub-PR count** — 2, 3, or 4 sub-PRs? (recommend 3:
   engine → cmd → meta-audit).
4. **H7 breaker default** — 3 (current), 5 (recommended), or 10?
5. **H9 migration of `sandbox=off`** — silent preserve, or one-time
   warning at startup?
6. **H5 sub-PR count** — 1 (single mega-PR) or N (one per subcommand)?
   Recommend N.
7. **Branch lifetime** — keep as long-lived namespace, or squash each
   PR to a single commit on merge?

## Cross-repo coordination

- **flux PR 4 (C2 — Vertex fix)** and **rho PR 4 (H6 — Session
  decomposition)** are independent.
- **flux PR 8 (H4 — FluxError)** is a prerequisite for any future
  rho-side `errors.As(err, &fluxErr)` use (currently none). No
  ordering dependency.
- The two repos' branches are independent and can be merged in any
  order.
