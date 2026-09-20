# Rho feature-module architecture

## Honest baseline

Rho is a single Go module, not a feature-modular monorepo. The `graycode-eco`
parent is a workspace of independent repositories; it is not a nested module
inside Rho. Within this repository, `cmd` is still a large composition package
with extensive shared state. Moving those files into folders without changing
package ownership would not work: in Go, each directory is a separate package.

The target is therefore an incremental modular monolith, not a rewrite:

```text
apps/rho/                 binary entrypoint (later extraction from cmd/rho)
internal/features/
  chat/                   transcript policy, prompt history, and TUI adapter
  commands/               command parsing, catalog, completion, typo recovery, and input history
  config/                 settings and credential flows
  execution/              one-shot and autonomous execution
  explain/                source-line provenance and git history lookup
  parallel/               validated multi-agent execution requests
  review/                 review, fix, and verification workflows
  session/                session lifecycle and history
  workspace/              repository status and additional-directory context
  welcome/                startup presentation and mascot capability
internal/platform/        OS, terminal, network, and persistence adapters
internal/contracts/       stable neutral DTOs and events
cmd/                      compatibility composition root during migration
```

## Rules

1. A feature owns one folder, one package, and its tests.
2. Features expose small interfaces or application services; they do not
   reach into another feature's unexported state.
3. `cmd` may compose features. Features must never import `cmd`.
4. Bubble Tea, Bubbles, and Lipgloss orchestration stays in `cmd`; features
   may return data or side-effect APIs but not UI commands or models.
5. Provider protocol and catalog code stays in Flux; Rho owns product policy
   and presentation only.
6. Shared types go into `internal/contracts` only when at least two feature
   packages need the same stable vocabulary. It is not a dumping ground.
7. A migration moves one vertical slice at a time and must preserve CLI
   behavior, tests, and package-boundary checks.

`make feature-boundaries-guard` enforces rules 2 and 3 for production feature
code. The broader `make boundaries` target includes this guard.

## Migration order

The first slice was `welcome`, because it has a clean terminal capability
boundary and no product-domain coupling. The second slice is the
`internal/features/session` persistence and lifecycle seam: it owns
runtime-to-durable conversion, conversation-arc sidecars, session reports,
cleanup, integrity checks, resume, recovery, legacy message-index forks, and
transcript hydration/export projections. It also owns the role-aware exchange mutation
rule used by rewind, drop, and retry, session rename/tag mutations, plus stable
cancellation ordering for session-scoped background work. Saved-session
relevance ranking for the picker also lives there; the TUI owns only query
state, selection, and rendering.
The review pipeline, test-first execution workflow, configuration command
policy, transcript formatting, bounded prompt history, stream buffering,
stream-source execution policy, turn-state semantics, and transcript trimming
are now feature-owned. Command normalization and alias resolution are also
feature-owned, as are session-command argument policies (formats, counts,
days, fork indices, and search extraction). Interactive, one-shot, and REPL
stream consumers share the feature-owned stream runner. Command registry
metadata, aliasing, collision checks, and enumeration now live in the commands
feature; typed handler attachment remains in `cmd` deliberately:
its handlers still require the live Bubble Tea model, provider/session state,
and terminal services. Moving it now would only hide those dependencies behind
an interface. The registry should move only after those application services
exist. The chat model remains the largest coupling hub.

The completion criterion is not “more folders.” It is that `cmd` becomes a
thin adapter, feature packages can be tested without Bubble Tea, and the
dependency graph remains one-way.

## Druk review: adopted and rejected patterns

The Druk repository is a useful reference because its architecture document
names state owners, composition order, dependency direction, and the tests
that enforce those rules. Rho adopts the parts that fit a Go agent CLI:

- command metadata is feature-owned and deterministic; `ValidateBuiltIns` now
  rejects duplicate names, missing descriptions, and dangling aliases;
- the TUI remains a composition root, while reusable behavior moves into
  feature packages with package-level tests;
- boundary checks are executable (`make feature-boundaries-guard`), rather
  than relying on directory naming alone;
- state ownership is explicit: session persistence, chat transcript policy,
  stream lifecycle, and command policy do not live in the Bubble Tea model.

Rho deliberately does not copy Druk's Solid/OpenTUI controller model or its
extension-market design. Rho is a Go agent runtime with provider, tool,
permission, session, and daemon contracts; those are different seams. Rho
also runs tools directly on the host under its permission and path-safety
pipeline. There is no container backend, image build, or pending-change
sandbox in the product. File edits are ordinary tool operations and remain
visible through the normal diff and review surfaces.
