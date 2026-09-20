# Vercel fx Adoption Plan

Status: Implemented in Rho's native Go architecture

Source: `https://github.com/vercel-labs/fx`

This plan records the useful ideas identified while comparing Vercel Labs'
`fx` Unix-like coding agent with Rho and its independent ecosystem repositories. It is an
adoption plan, not a code-porting plan. Rho should reimplement compatible
behavior in Go and preserve its existing provider, memory, review, audit,
session, and security boundaries.

## Executive Decision

Rho should adopt the following `fx` ideas:

1. Stable identities and operational commands for persisted permission rules.
2. A unified machine-readable runtime status snapshot.
3. Deterministic terminal recording and replay for TUI debugging.
4. A documented separation between repository-safe configuration and private
   user authority.
5. Compatibility aliases for common permission modes in CLI and ACP surfaces.
6. Stronger subagent lifecycle and permission observability.
7. A narrow embeddable host API over Rho's existing daemon and ACP surfaces.

Rho should not copy `fx` source code, replace its Go runtime with Zig, create a
second permission system, or reduce its code-intelligence tool surface merely to
match `fx`'s smaller binary.

## Existing Rho Capabilities

The comparison found that Rho already provides the foundation for most `fx`
features:

| `fx` capability | Rho implementation | Current decision |
|---|---|---|
| Native single binary | Go static binary and cross-platform release builds | Keep Rho implementation |
| Agent loop | `internal/engine` | Keep Rho implementation |
| Permission and host-policy split | `internal/engine/safety`, `internal/permissions`, `internal/trust` | Harden and document |
| `ask`/automatic approval behavior | Autonomy profiles, governance, grants, hooks | Add compatibility aliases only |
| Child agents | `internal/multiagent`, continuable children, cold resume | Add observability and configuration polish |
| Sessions | `internal/session`, WAL, JSONL, recovery, fork, replay | Add unified status integration |
| MCP | `internal/mcp`, sibling `falcon` | Keep architecture; audit trust defaults |
| Skills | Community registry, validation, provenance, scopes | Keep architecture; improve status output |
| ACP | `internal/acp` | Extend status/config parity where useful |
| Swift and replay | sibling `swift` (Swift), `internal/session/replay` | Add terminal-level tape capability |
| Persistent memory | sibling `harrier` (Harrier) | Do not replace with flat JSON state |
| Provider runtime | sibling `flux` | Do not add provider logic to Rho |

## Priority Model

- **P0:** Required for secure, supportable production behavior.
- **P1:** High-value product improvements that should follow P0.
- **P2:** Optional integration or debugging improvements.
- **Reject:** Deliberately out of scope or harmful duplication.

## P0: Stable Permission Rule Management

### Goal

Make persisted permission grants independently addressable and revocable, even
when their original command, path, or workspace has changed.

### Scope and ownership

- Primary implementation: Rho `internal/permissions` and
  `internal/engine/safety`.
- Shared contract changes, if needed: sibling `eagle/policy`.
- User-facing commands: `cmd` and the existing slash-command surface.
- No changes to sibling `flux` or `falcon`.

### Required behavior

1. Assign every persisted grant a stable, non-secret rule ID.
2. Preserve the existing precedence model: governance and hard safety denies
   must remain stronger than user grants; deny rules must not be bypassable by
   a broad allow rule.
3. Support listing effective rules in text and JSON.
4. Support revoking by rule ID.
5. Reject malformed, ambiguous, or unknown IDs without changing state.
6. Keep session-only approvals separate from persisted grants.
7. Include scope, source, action, pattern, creation time, and expiry metadata in
   the structured representation, without exposing secrets.
8. Make revocation atomic and crash-safe.
9. Emit an audit/event-log record for create, revoke, reset, and failed revoke.
10. Apply the same behavior to parent and delegated child policy snapshots.

### Proposed interfaces

```text
rho permissions list [--json]
rho permissions revoke <rule-id>
rho permissions reset [--scope user|project]
```

The exact command names may follow existing Rho conventions, but the typed
operation should be shared by CLI, TUI, daemon, and ACP.

### Acceptance criteria

- A rule remains revocable after its source path no longer exists.
- A revoked rule is not used by a new evaluation or child snapshot.
- Existing policy precedence and destructive-command hard blocks are unchanged.
- Concurrent list/revoke operations do not corrupt persisted state.
- JSON output contains stable IDs and no command credentials or MCP secrets.
- Unit, race, restart/recovery, and ACP/daemon tests pass.

## P0: Configuration Authority Boundaries

### Goal

Ensure repository-local configuration can provide safe project defaults but
cannot silently grant private authority or execute untrusted integrations.

### Required audit

Review every setting source and classify it as:

- **Project-safe:** sandbox defaults, context loading, bounded tool-result
  limits, repository-local display or workflow defaults.
- **User-private:** credentials, model/provider authentication, persistent
  permission rules, private MCP headers and environment values, global hooks,
  notification preferences.
- **Runtime-only:** process environment overrides, ACP client options, active
  session grants, temporary approvals.

### Required behavior

1. Project settings cannot contain or override credentials.
2. Project settings cannot create user-global permission grants.
3. Private MCP configuration remains outside the repository by default.
4. Project MCP or hook activation requires explicit trust and clear status.
5. Invalid trusted profiles fail closed for sensitive operations while preserving
   the last valid runtime where safe.
6. Environment overrides affect only the current process and are never written
   back to settings.
7. Status and diagnostics redact tokens, headers, environment values, and raw
   MCP URLs where disclosure would be unsafe.

### Acceptance criteria

- A hostile repository fixture cannot grant itself write, network, or MCP
  authority merely by being opened.
- Configuration precedence is documented and covered by table-driven tests.
- Existing trusted-project workflows continue to work.
- Security tests cover symlinks, malformed JSON, path traversal, and stale
  configuration.

## P0: Unified Runtime Status Snapshot

### Goal

Provide one stable, machine-readable status model for support tools, scripts,
the daemon, ACP clients, and the interactive UI.

### Ownership

- Snapshot contract: Rho root or `eagle` if consumed cross-repo.
- Assembly: `internal/engine`, `internal/session`, `internal/mcp`,
  `internal/permissions`, `internal/multiagent`.
- Rendering: CLI/TUI and daemon adapters.

### Required fields

```text
schema_version
rho_version
session_id
workspace
git_branch
provider
model
permission_mode
autonomy_tier
sandbox_mode
effective_permission_summary
active_subagents
turns_used / turns_limit
tool_calls_used / tool_calls_limit
tokens_used / tokens_limit
cost_usd / cost_limit_usd
active_goal
session_recovery_state
mcp_summary
hook_summary
trace_or_recording_state
warnings
```

All sensitive values must be represented by redacted booleans, counts, or
identifiers rather than raw secrets.

### Required interfaces

```text
rho status
rho status --json
```

The daemon and ACP should expose the same snapshot schema, with transport
metadata kept outside the product snapshot.

### Acceptance criteria

- Text and JSON are rendered from the same typed snapshot.
- Snapshot generation does not start providers, MCP servers, or network calls.
- Status works when a session is partially initialized or recovering.
- Output is deterministic enough for scripts and golden tests.
- Schema versioning is documented and additive changes are backward-compatible.

## P1: Deterministic Terminal Recording and Replay

### Goal

Add `fx`-style terminal byte and resize recording to complement Rho's existing
agent-session swift and replay features.

### Ownership

- Preferred home: sibling `swift` (Swift) if the capability is intended for reuse by
  other agents.
- Rho integration: `internal/swift` or the TUI composition layer.
- Do not put terminal tape parsing in the agent engine.

### Required behavior

1. Record output bytes written by the owned terminal surface.
2. Record resize events with timestamps or deterministic frame ordering.
3. Optionally record raw input only when explicitly enabled.
4. Record interrupts and terminal ownership transitions.
5. Use a versioned, bounded, append-only format.
6. Support redaction or opt-out for sensitive terminal content.
7. Replay without starting an LLM, shell, provider, MCP server, or network.
8. Produce final-grid, frame, and JSON metadata output.
9. Detect truncated or corrupt tapes without panicking.

### Proposed interfaces

```text
rho swift record --output <path>
rho swift replay <path>
rho swift replay <path> --frames
rho swift replay <path> --json
```

Existing Swift session capture remains the source of prompts, tool calls, git
events, and cost data. Terminal tape is a separate artifact linked by session
ID.

### Acceptance criteria

- Resize and rendering regressions can be reproduced without credentials.
- Replay output is stable on macOS and Linux for the same tape.
- Tape files have restrictive permissions and never contain provider secrets by
  default.
- Golden tests cover wrapping, colors, alternate-screen transitions, resize,
  interrupt, truncation, and malformed input.

## P1: Permission Mode Compatibility

### Goal

Expose familiar `fx` permission names without weakening Rho's richer policy
engine.

### Mapping

| Compatibility name | Rho behavior |
|---|---|
| `ask` | Supervised or equivalent prompt-required policy |
| `auto` | Automatic review/approval behavior where configured |
| `yolo` | Explicit high-autonomy mode, still subject to governance, sandbox, and destructive-command hard blocks |

These are aliases or presentation-layer modes, not a replacement for Rho's
autonomy tiers, governance ceiling, spec gates, and sandbox policy.

### Required behavior

1. Accept names in configuration, CLI, and ACP only where the surface supports
   them.
2. Display the effective Rho tier and hard safety constraints.
3. Never let `yolo` bypass governance, hard-deny rules, destructive-command
   blocks, or mandatory spec approval.
4. Persist the canonical Rho representation, not an ambiguous alias.
5. Add migration and invalid-value diagnostics.

## P1: Subagent Lifecycle and Authority Observability

### Goal

Adopt `fx`'s useful child-agent visibility while retaining Rho's continuable
child sessions, worktree isolation, model routing, and delegated policy
inheritance.

### Required behavior

1. Status shows child ID, parent ID, state, model, mode, workspace, and budget
   summary.
2. Child creation records the effective permission and sandbox snapshot.
3. Child model and permission changes are explicit and auditable.
4. Parent cancellation, child cancellation, resume, close, and reparenting have
   deterministic lifecycle events.
5. Child operations use idempotent request IDs where they can create durable
   state.
6. Child output and transcripts remain isolated from parent context unless the
   parent explicitly requests synthesis.
7. Explore and plan children remain read-only by default.

### Ownership

- `internal/multiagent`: lifecycle and relationship model.
- `internal/session`: durable child state.
- `internal/eventlog`: lifecycle facts.
- `internal/engine/safety`: authority snapshots.
- `internal/engine/agent_session_tool.go`: existing orchestration adapter.

## P2: MCP and Skills Trust UX

### Goal

Improve discoverability and trust reporting using `fx`'s clear status model,
without changing Rho's existing MCP and skills architecture.

### Required behavior

1. Display whether an MCP server or skill is user, project, managed, or plugin
   supplied.
2. Display trust state before enabling project-local integrations.
3. Keep credentials and environment values out of status output.
4. Validate and stage MCP configuration before publishing a replacement.
5. Keep lazy tool discovery and bounded descriptions/schema payloads.
6. Show disabled, degraded, unavailable, and authenticated states distinctly.
7. Preserve skill provenance, source revision, validation results, and license
   metadata.

### Ownership

- MCP runtime: `internal/mcp` and sibling `falcon`.
- Skill registry/validation: `internal/plugin` and the community registry.
- Trust decisions: `internal/trust`.

## P2: Embedding and Host API

### Goal

Offer a supported embedding boundary inspired by `fx`'s ACP and WASM surfaces,
without committing Rho to a WASM rewrite.

### First implementation

1. Treat ACP as the stable external integration protocol.
2. Treat the daemon API as the local programmatic host API.
3. Publish typed status, session, prompt, cancel, permission, and MCP-health
   operations.
4. Define ownership for authentication, session storage, terminal I/O, and
   network transport.
5. Add a small Go SDK only after the daemon/ACP contracts stabilize.

### Deferred

- Go-to-WASM agent embedding.
- Browser-hosted TUI embedding.
- A second in-process SDK runtime with different lifecycle semantics.

## Cross-Cutting Security Requirements

Every implementation phase must preserve these invariants:

1. Permission checks happen immediately before execution.
2. Permission and sandbox decisions remain separate.
3. Governance and personal hard ceilings cannot be overridden by autonomy mode.
4. Destructive commands remain hard-blocked.
5. Child agents cannot elevate parent authority at delegation time.
6. Project-local integrations require trust and are bounded by workspace policy.
7. Secrets never enter prompts, transcripts, status snapshots, traces, or tapes
   unless the user explicitly opts into an audited diagnostic flow.
8. Recovery and replay paths never execute tools or contact providers.
9. All persisted state writes are atomic, permission-restricted, and resilient to
   interruption.

## Test and Verification Matrix

### Unit tests

- Permission ID generation, lookup, revoke, precedence, expiry, and migration.
- Configuration source precedence and authority classification.
- Status snapshot completeness, redaction, versioning, and partial state.
- Terminal tape encoding, decoding, replay, corruption, and bounds.
- Compatibility mode parsing and canonical persistence.
- Child lifecycle transitions and idempotent operations.
- MCP/skill trust and staged reload behavior.

### Integration tests

- CLI text and JSON output from the same state.
- Daemon and ACP status/session/permission parity.
- Parent-child policy inheritance and cancellation.
- Restart during permission persistence, session save, and swift recording.
- Project fixture attempting to inject credentials, grants, hooks, or MCP
  authority.

### Security tests

- Path traversal, symlink replacement, malformed configuration, and permission
  file tampering.
- Secret redaction in status, swift, replay, logs, and errors.
- Governance denial under `yolo`/high-autonomy modes.
- Destructive-command denial under every compatibility mode.
- Untrusted MCP server and skill installation behavior.

### Release checks

```text
make fmt
make test
make test-race
make vet
make lint
make security
rho verify
```

For sibling-repository changes, run that repository's own tests and boundary checks before
updating the Rho pointer. Do not modify provider protocols or adapters in Rho;
those belong in the Flux repository.

## Delivery Sequence

### Milestone 0: Contract and threat-model review

- Approve the status schema and permission-rule identity model.
- Inventory all configuration sources and classify authority.
- Record compatibility and migration decisions.
- Add threat-model and redaction test fixtures.

### Milestone 1: Permission operations and configuration boundaries

- [x] Implement stable grant IDs and revoke/list/reset operations.
- [x] Add atomic persistence for exact permission mutations.
- [x] Complete project/user/runtime configuration authority tests.
- [x] Preserve `ask`/`auto`/`yolo` presentation aliases.

### Milestone 2: Unified status

- [x] Implement the typed snapshot.
- [x] Wire CLI, daemon, and ACP output.
- [x] Add redaction, partial-startup handling, and golden JSON tests.

### Milestone 3: Child-agent observability

- [x] Add child lifecycle snapshot fields and audit events.
- [x] Expose child model, policy inheritance, sandbox, and workspace information.
- [x] Retain cancellation/resume lifecycle coverage in continuable-child tests.

### Milestone 4: Terminal tape

- Design and review the versioned swift artifact format in sibling `swift` (Swift).
- Implement recording, replay, redaction, and deterministic golden tests.
- Integrate recording controls into Rho without coupling tape parsing to the
  agent loop.

### Milestone 5: Host integration and trust UX

- [x] Stabilize daemon/ACP status and session contracts.
- [x] Add redacted MCP and skill status to the shared snapshot.
- [x] Preserve project trust warnings without starting project integrations.
- Evaluate a Go SDK only after real consumers require it.

## Deliberately Rejected

- Copying Zig implementation code from `vercel-labs/fx`.
- Replacing Rho's Go runtime or independent ecosystem repositories.
- Adding a second permission evaluator.
- Replacing Harrier with flat JSON memory.
- Replacing Swift session capture with terminal tapes.
- Removing Rho's code intelligence, review, audit, token, or provider features
  to target `fx`'s binary size.
- Automatically executing repository-local MCP servers or hooks.
- Adding WASM before daemon and ACP contracts are stable and proven.

## Success Criteria

The adoption is successful when Rho can provide the operational clarity of
`fx` while retaining its stronger ecosystem architecture:

- Every persistent permission rule can be listed and revoked by stable ID.
- A single redacted status snapshot works across CLI, daemon, and ACP.
- TUI failures can be reproduced from a credential-free terminal tape.
- Project configuration cannot silently grant private authority.
- Child-agent authority, lifecycle, model, and budget state are inspectable.
- Existing Rho memory, provider, review, audit, MCP, ACP, and session behavior
  remains intact.

## Implemented Scope

- Stable exact permission IDs, atomic mutation persistence, list, add, revoke,
  and reset commands.
- Redacted schema-versioned status snapshots for CLI, daemon, and ACP.
- Project settings authority filtering for provider, permission, MCP, custom
  provider, and model-thinking fields.
- Native `fxtape` recording and replay parity, including resize, input, signal,
  markers, frame export, and JSON replay.
- Existing subagent model routing, lifecycle, delegated policy inheritance,
  continuable children, and cold resume retained as the authoritative runtime.

The remaining items in this document are future refinements to the already
implemented surfaces, not missing baseline features. Rho's subprocess LSP
manager remains a separate integration task: the current main `LSP` tool uses
the codegraph path, while `internal/lsp` provides the richer language-server
client and is not yet attached to the primary tool registry.
