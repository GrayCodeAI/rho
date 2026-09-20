# Pi Adoption Plan

Status: Proposed

Source: `https://github.com/earendil-works/pi` (MIT, TypeScript/Bun monorepo)

This plan records the features identified as genuinely missing from rho while
auditing the Pi agent harness against Rho and its independent ecosystem
repositories. It is
an adoption plan, not a code-porting plan: rho reimplements compatible behavior
in Go and preserves its existing provider, session, permission, observability,
and protocol boundaries. Rho executes tools directly on the host; it does not
ship a product sandbox runtime.

## Executive Decision

Rho should adopt the following Pi features:

1. A Go telemetry conformance suite that verifies emitted OpenTelemetry spans
   and attributes match the documented schema.
2. An agent-runtime end-to-end eval harness that drives the real session and
   tool loop and snapshots session data as eval artifacts.
3. A differential-rendering terminal engine for the TUI (line-granular diff
   with synchronized output).
4. Durable session writer fencing and leases.
5. Kitty graphics protocol support for terminal images.
6. Session lease/ownership semantics in the daemon protocol.

Rho should not copy Pi's TypeScript code, replace its Go runtime, or adopt Pi's
custom CBOR RPC protocol. Its host-native permission and path/trust policy is
the local safety boundary.

## Existing Rho Capabilities

The audit found that rho already provides the foundation for most Pi features:

| Pi package | Rho implementation | Current decision |
|---|---|---|
| `pi-ai` (multi-provider) | sibling `flux` client + engine facade + catalog + router + credentials | Keep rho (broader) |
| `pi-agent-core` (agent loop) | `internal/engine` | Keep rho |
| `pi-coding-agent` (CLI) | `cmd` TUI + CLI + daemon | Keep rho |
| `pi-session-backends` (storage) | `internal/session` JSONL + zstd + WAL + SQLite index, sibling `swift` (Swift) | Keep rho |
| `pi-server` (RPC) | `internal/daemon` + `internal/acp` + `internal/mcp` | Keep rho (broader) |
| Permissions and trust | `internal/engine/safety`, path guards, `internal/trust`, `internal/env` | Keep rho's host-native boundary |
| `pi-tui` differential rendering | Bubble Tea v2 full-frame redraw | Adopt line-diff engine |
| `pi-telemetry` conformance | `docs/OTEL-CONVENTIONS.md`, flux `genai_semconv` pinning | Adopt conformance suite |
| `pi-evals` agent-level eval | `internal/features/eval` (model benchmark only) | Adopt agent-runtime eval |

## Priority Model

- **P0:** Required for correctness, security, or supportability.
- **P1:** High-value product improvements that follow P0.
- **P2:** Optional enhancements or larger architecture bets.
- **Defer:** Deliberately out of scope or blocked on a design RFC.

## P0: Telemetry Conformance Suite

### Goal

Guarantee the emitted OpenTelemetry spans and attributes always match the
documented `gen_ai.*` / `cost.usd` / `tool.name` / `session.id` / `agent.id`
contract, so the schema cannot silently drift across Rho and its independent
ecosystem repositories.

### Scope and ownership

- Primary implementation: `internal/observability` and
  `flux/internal/observability` (the canonical `genai_semconv`
  constants and their pinning test).
- Shared schema constants: sibling `eagle` if a cross-repo
  contract is needed, otherwise keep them in flux as today.
- No changes to `internal/mcp` or `internal/daemon` for telemetry adoption.

### Required behavior

1. Define a typed schema object for every emitted span
   (`agent_loop`, `tool.<name>`, `compact.<strategy>`, `api.chat`, `session`).
2. Enumerate required and optional attributes per span, with types, cardinality,
   sensitive flags, and example values.
3. Provide a conformance harness that:
   - records spans from the in-memory OTel adapter,
   - asserts every span name and attribute key exists in the schema,
   - asserts required attributes are present,
   - asserts sensitive attributes never carry raw prompt/response text,
   - asserts parent/child span relationships and settlement semantics,
   - is runner-independent (usable from any Go test harness).
4. Wire the conformance harness into the rho and flux test suites so CI
   enforces the contract.
5. Keep the conformance layer passive and non-throwing: malformed or unreadable
   telemetry payloads must not break agent execution.

### Acceptance criteria

- Every span rho emits is covered by the schema.
- A deliberate schema drift (adding a span or attribute) fails the conformance
  test until the schema is updated.
- No raw prompt/response text appears in recorded attributes.
- The conformance harness runs green in rho and flux CI.

## P0: Agent-Runtime Eval Harness

### Goal

Evaluate the full rho agent end-to-end (real session, tool loop, planning,
host policy) against tasks, and snapshot session data as artifacts — not just a
model-level benchmark.

### Scope and ownership

- Primary implementation: a new `internal/features/evalloop` package or an
  extension of `internal/features/eval`.
- Reuse: `internal/engine` session runtime, `internal/tool` registry,
  host path/trust policy, `internal/session` persistence.
- CLI: extend `cmd/eval.go` with a loop mode.

### Required behavior

1. Drive the real `Session` and tool loop for a task, not a direct LLM call.
2. Run each evaluation in an isolated temporary directory with explicit host
   path guards; do not imply that the temporary directory is a security
   sandbox.
3. Capture normalized events: user prompt, assistant turns, tool calls/results,
   final output, usage, and cost.
4. Snapshot the underlying session JSONL as an eval artifact (per-run), so
   failures can be replayed offline.
5. Support comparative runs across models or configurations.
6. Report pass/fail, token/latency/cost deltas, and reproducibility hashes.
7. Keep model-level benchmarks (`internal/features/eval`) intact.

### Acceptance criteria

- A task that requires tool use (e.g. "edit file and run tests") is executed
  through the real loop and verified.
- Session JSONL artifacts are produced and reproducible.
- Runs are isolated and do not mutate the user's working directory.
- CI can run a small smoke eval without external credentials when gated.

## P1: Differential-Rendering Terminal Engine

### Goal

Reduce render cost and flicker for fast, focused agent sessions by re-emitting
only changed terminal lines, with synchronized output.

### Scope and ownership

- Primary implementation: a new package (e.g. `internal/tui/diff`) or a custom
  Bubble Tea renderer in `cmd`.
- Reuse: existing `internal/terminal` PTY store and `internal/terminal/tape`
  recording.
- Do not couple the diff engine to the agent loop or session logic.

### Required behavior

1. Render the full component tree to lines, then diff against the previous
   frame.
2. Re-emit only the changed line range (first-changed to last-changed), moving
   the cursor and clearing changed lines.
3. Wrap updates in synchronized output sequences to avoid tearing.
4. Handle full render on first paint and on terminal width changes.
5. Manage scrollback/viewport and alt-screen transitions correctly.
6. Keep the existing fxtape recording and replay working.

### Acceptance criteria

- A single-line update (e.g. spinner) re-emits only that line, measurable in
  tests.
- Resize and scrollback behavior matches current Bubble Tea output.
- Recording/replay golden tests still pass.
- No flicker regression on fast streaming updates.

## P1: Session Writer Fencing and Leases

### Goal

Prevent stale writers from corrupting a session after a takeover, mirroring
Pi's fencing-token model.

### Scope and ownership

- Primary implementation: `internal/session` write path and WAL.
- Consume: `internal/daemon` remote-session leases.

### Required behavior

1. Assign a monotonically increasing fence token to each session writer.
2. Reject writes from a writer whose fence token is older than the current one.
3. Add an expiration window for lease ownership.
4. Surface session-lease acquisition and ownership errors through the daemon.
5. Keep existing fork/rewind/checkpoint behavior intact.

### Acceptance criteria

- A stale writer's append is rejected without corrupting the chain.
- Lease expiry prevents an abandoned owner from writing after takeover.
- Concurrent writes from the same owner remain serialized.
- Existing fork/rewind/checkpoint tests pass.

## P2: Kitty Graphics Protocol

### Goal

Render terminal images via the Kitty graphics protocol in the TUI.

### Scope and ownership

- Primary implementation: `cmd` TUI render path, adjacent to the diff engine.
- Reuse: existing image/attachment handling in `internal/attachment`.

### Required behavior

1. Detect Kitty graphics support in the terminal.
2. Encode and transmit images via the Kitty protocol with a reserved row block.
3. Force repaint of the image block on any line change inside it.
4. Disable gracefully when the terminal lacks support.

### Acceptance criteria

- Images render in supported terminals and degrade to placeholders otherwise.
- The diff engine repaints the full image block on partial change.
- No regressions to the text-only render path.

## P2: Daemon Session Leases

### Goal

Add client-side session lease/ownership semantics to the daemon protocol so
remote sessions are single-owner.

### Scope and ownership

- Primary implementation: `internal/daemon` (new lease endpoint and ownership
  checks).
- Protocol: extend `api/openapi.yaml` and the parity test.

### Required behavior

1. Clients acquire a lease to a session before writing.
2. Ownership/disconnection errors are returned distinctly.
3. Leases expire and can be released.
4. Existing /v1/chat and /v1/sessions behavior is preserved for single-owner
   callers.

### Acceptance criteria

- Two clients cannot write the same session concurrently.
- Lease expiry and takeover are handled without corruption.
- OpenAPI parity test passes.

## Cross-Cutting Security Requirements

Every implementation phase must preserve:

1. Permission checks run immediately before execution.
2. Native sandbox enforcement is never bypassed by the diff engine or eval loop.
3. Telemetry never carries raw prompt/response text.
4. Session fencing prevents corruption, not privilege changes.
5. Eval runs are isolated and never mutate user workspaces.
6. Remote-session leases enforce ownership without leaking credentials.
7. Secrets never enter transcripts, traces, tapes, or artifacts unless explicitly
   opted into an audited diagnostic flow.

## Test and Verification Matrix

### Unit tests

- Telemetry schema conformance for every span and attribute.
- Differential renderer diff range, full-render, resize, and scrollback.
- Session fence-token rejection and lease expiry.
- Kitty image block repaint and graceful degradation.
- Daemon lease acquisition, release, takeover, and error mapping.
- Agent-runtime eval snapshotting and reproducibility hashes.

### Integration tests

- Eval runs the real tool loop end-to-end in an isolated dir.
- Daemon and ACP parity for leased sessions.
- fxtape recording/replay across the new renderer.
- Cross-repo telemetry conformance in rho and flux CI.

### Security tests

- Telemetry redaction of raw prompt/response text.
- Eval isolation (no writes outside the temp workspace).
- Session fence-token corruption resistance.
- Lease ownership with credential redaction.

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
updating the Rho pointer.

## Delivery Sequence

### Milestone 0: Contract and threat-model review

- Approve the telemetry schema object and conformance harness shape.
- Approve the differential-renderer API and its interaction with fxtape.
- Approve the session fence-token and daemon-lease model.
- Add redaction and isolation test fixtures.

### Milestone 1: Telemetry conformance (P0)

- [x] Define the typed span/attribute schema.
- [x] Implement the conformance harness.
- [x] Wire rho and flux CI.
- [x] Add schema-drift regression tests.

### Milestone 2: Agent-runtime eval (P0)

- [x] Implement the loop runner over the real Session/tool loop.
- [x] Add isolated execution and session-JSONL artifacts.
- [ ] Add comparative and reproducibility reporting.
- [x] Extend `rho eval` with a loop mode.

### Milestone 3: Differential renderer (P1)

- [x] Implement the line-diff engine (`internal/tui/diff`).
- [x] Add synchronized-output emission and range tests.
- [ ] Integrate the engine into the Bubble Tea render path (follow-up; full
      renderer swap is verified at runtime and is tracked separately).
- [x] Preserve fxtape recording/replay.

### Milestone 4: Session fencing + daemon leases (P1)

- [x] Add fence tokens to the session write path.
- [x] Add daemon lease endpoint and ownership checks.
- [x] Update OpenAPI parity.

### Milestone 5: Kitty graphics (P2)

- [x] Add the Kitty graphics library (`internal/tui/kitty`): capability-response
      parsing and chunked frame encoding with round-trip tests.
- [ ] Render-loop integration (wiring encoded frames into the terminal
      renderer) remains a follow-up.

## Deliberately Deferred

- Copying Pi's TypeScript code or adopting its custom CBOR RPC protocol in place
  of rho's ACP/MCP/daemon stack.
- Replacing rho's native permission/sandbox model with Pi's container-only
  approach.
- Porting Pi's extension system verbatim; rho's plugin/hook/skills model already
  covers it.
- Adding a full agent-swarm/graph model beyond current subagent support.

## Success Criteria

The adoption is successful when rho closes each confirmed gap while retaining
its stronger architecture:

- Telemetry spans always conform to the documented schema across rho and its
  sibling repositories.
- The agent can be evaluated end-to-end through its real tool loop.
- The TUI re-renders only changed lines with synchronized output.
- Session writes are fenced and remote sessions are single-owner.
- Terminal images render in supported terminals.
- Existing host-native permission, path/trust, session, and protocol behavior
  remains intact; no sandbox runtime is introduced.
