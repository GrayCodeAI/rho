# Rho Architecture Baseline

> **Historical.** This document describes the pre-2026-09 multi-engine
> ecosystem. Rho now depends only on `flux` and embeds its own token engine;
> the harrier, kestrel, merlin, shrike, and swift integrations have been
> removed. Kept for record.

**Status:** Phase 0 baseline
**Date:** 2026-08-04
**Baseline commit:** `69ce83f55f9098623e5a891e8c52be636db89c7c`

This document records the architecture that exists in the Rho repository at
the beginning of the architecture improvement program. It separates current
implementation facts from the intended ecosystem design. It is not a product
quality score and does not claim that the migration is complete.

## Authority and terminology

Use the documents in this order when statements conflict:

1. This document for the dated implementation baseline and migration status.
2. `rho-current-vs-proposed.md` for the ecosystem repository map.
3. `rho-product-architecture.md` for ownership and runtime responsibilities.
4. `rho-dependency-rules.md` for allowed and forbidden dependency edges.
5. `spec.md` for behavioral requirements and agent-loop semantics.

Rho is the main Go CLI/product and workspace entry point in a
multi-repository ecosystem. `graycode-eco` is only the plain parent folder;
the sibling repositories retain independent Git histories and release
cadences. Local integration uses the parent `go.work`; standalone builds use
published module pins. There is no current `rho/external/` vendor tree.

## Target product graph

```text
users / SDKs / skills / daemon clients
                |
                v
              rho
       /        |        \
    flux     harrier       shrike
    swift     kestrel    merlin
                |
                v
       eagle
```

The graph is intentionally directional:

- Rho owns user-facing orchestration, sessions, tools, permissions,
  composition, and public product surfaces.
- Flux owns provider protocols, routing, credentials, catalogs, and provider
  execution behind `flux/engine`.
- Harrier, Shrike, Swift, Kestrel, and Merlin are support engines and must not import
  Rho internals or one another.
- Core contracts contain stable cross-repository vocabulary and DTOs, not
  runtime orchestration.
- SDKs and skills consume Rho surfaces rather than support-engine internals.

## Current implementation state

### Complete or enforced

- Rho production code uses Flux through the `flux/engine` facade.
- Kestrel and Merlin are integrated through Rho bridge packages.
- Support-engine sibling imports and imports of Rho internals are guarded.
- The AST/package-graph guard reports production boundary violations with
  file/line diagnostics across Rho and available support repositories.
- Persisted tool, review, verification, event, and policy contracts use the
  implemented portions of `eagle`.
- Native-compaction capability contracts use `eagle/llm`; Flux
  request translation remains inside `internal/provider/gateway`, keeping the
  engine layer independent of the provider adapter package for this path.
- Agent commands execute directly on the host. There is no container executor
  or product sandbox runtime; safety is provided by the permission engine,
  path guard, trust policy, and destructive-command hard blocks.
- `GraphAwareBudget` reads Harrier through `HarrierBridge`; its graph-budget path no
  longer imports Harrier engine or storage implementation types directly.
- `CodeMemoryLinker` also routes node search, edge creation, and file-anchor
  persistence through `HarrierBridge`; remaining direct Harrier users are isolated
  to the other memory workflow slices awaiting migration.
- The local boundary suite, full Go tests, and `go vet` pass at this baseline.

### Transitional

- `internal/engine.Session` still contains service fields and remaining legacy
  state. The service graph is authoritative in the migrated paths, but the
  decomposition is not complete.
- `Session.Cost` remains a public compatibility field because existing callers
  assign it directly. New code must use `CostValue()`; removing the field is a
  versioned API change, not a safe internal extraction.
- `internal/engine` remains a large compatibility and orchestration package.
  At this baseline its top-level production files contain approximately
  19,253 lines, its top-level tests approximately 11,855 lines, and the
  subtree contains compatibility alias/re-export files.
- Rho's Harrier and Shrike implementation imports are now concentrated in
  `HarrierBridge` and `internal/token` for the migrated production paths. The
  graph/projection packages used for capture remain explicit integration
  surfaces; replaceability is improved, but still not equivalent to the Flux
  boundary.
- `PersistenceService` is the in-memory runtime owner for transcript/context
  state and checkpoint metadata. The active durable session path remains
  `internal/session` JSONL plus the external file WAL used for crash recovery.
  `SQLiteStore` is implemented but dormant: it is not the active `Load`/`Save`
  backend. Workspace snapshots, conversation graphs, graph journals, and
  execution graphs are secondary records or projections, not the canonical
  durable transcript. An ADR is still required to define whether JSONL/WAL
  remains authoritative or SQLite becomes authoritative, including migration,
  retention, and recovery semantics.
- CLI, daemon, and other entry points share substantial construction and
  orchestration responsibilities instead of depending on one explicit
  application composition root.
- Non-interactive entry points now share `cmd.newConfiguredRhoSession`; the
  interactive TUI intentionally retains a lightweight startup path followed by
  deferred heavy configuration to protect first-frame latency.

## Architecture decisions for the improvement program

### ADR-B01 — Keep the multi-repository ecosystem

Do not collapse the support engines into Rho or force a shared release cycle.
The `graycode-eco` parent workspace and Rho's published module pins provide
local integration without removing independent ownership and release
boundaries.

### ADR-B02 — Preserve the Flux boundary

Provider implementation, catalog metadata, credential mapping, and protocol
adapters remain owned by Flux. Rho may own product policy and user-facing
selection, but production provider access remains through `flux/engine`.

### ADR-B03 — Complete internal consolidation before adding new seams

The next architecture work prioritizes Session decomposition, composition-root
centralization, and persistence ownership. New engines or broad contract
packages should not be added until the current seams are explicit.

### ADR-B04 — Add facades selectively

Harrier, Shrike, and Swift require a facade decision based on actual replacement and
release needs. A facade is justified when it isolates Rho from implementation
types or enables independent upgrades; it is not justified merely to increase
the number of packages.

### ADR-B05 — No subjective architecture score is a release criterion

Documents may compare capabilities and trade-offs, but unsupported scores such
as “9.2/10” or “10/10” are not architecture evidence. Architecture readiness
must be assessed using dependency checks, tests, migration completion, and
operational guarantees.

## Non-goals

This program does not aim to:

- rewrite the agent loop from scratch;
- merge all engines into one repository;
- move every runtime or persistence type into `eagle`;
- make every engine use identical integration depth;
- add IDE parity before the internal architecture is stable;
- treat passing tests as proof that migration work is complete.

## Baseline verification

The following checks passed against the baseline commit:

```text
make boundaries
go test ./internal/testaudit/... -count=1
go test ./... -count=1 -timeout=120s
go vet ./...
```

The GitNexus workspace index also reports the baseline commit as up to date.
Architecture work must still run impact analysis before changing code symbols
and change-scope detection before committing.

## Next phase

Phase 1 adds AST/package-graph dependency checks. Phase 2 continues the safe
Session migration using the boundaries documented here, with `Session.Cost`
explicitly retained as a compatibility exception. Lazy persistence
initialization, cost snapshots, and WAL recovery error reporting are now
synchronized and tested. Phase 3 has explicit non-interactive and interactive
startup composition boundaries, with heavy TUI configuration remaining
deferred for first-frame latency. Phase 4 has consolidated the migrated Harrier
and Shrike implementation imports behind narrow Rho-owned facades. The next
decision is the persistence ADR: document and enforce one durable authority,
then define the migration and recovery contract before introducing additional
storage backends.

## Current branch follow-up

The architecture is strong but transitional, not perfect. The highest-value
remaining risk is persistence authority: several storage and observability
mechanisms exist, but only JSONL plus the external WAL currently define durable
session recovery. No code should silently switch the active backend until the
persistence ADR is approved and covered by compatibility and recovery tests.
