# CommandCodeAI Adoption Plan

Status: Implemented in the Rho working tree where the existing architecture
supports a safe, native implementation.

## Source Review

The reviewed CommandCodeAI organization contains four relevant categories:

| Repository | Evidence | Decision |
|---|---|---|
| `command-code` | Current public tree is documentation/assets; historical `gpt3-agent` is a small unsafe prototype | Do not copy code; adopt UX and workflow documentation ideas |
| `cmd-old-public` | Archived placeholder/documentation repository | No implementation to adopt |
| `BaseAI` | Archived TypeScript pipe SDK/local provider server; licensing metadata is inconsistent | Reimplement narrow ideas only; do not add as a dependency |
| `agent-skills` | MIT skill collection with progressive-disclosure guidance | Adopt authoring/process ideas; preserve individual asset licenses |
| `awesome-agents` | Apache-2.0 example applications | Reference only; do not merge into Rho skills |

`starling` remains the canonical public skill registry. Its
validator and registry tooling are more complete than the CommandCodeAI
repositories and should remain authoritative.

## Adopted Work

### 5. Kimi Code workflow parity

The Kimi Code comparison confirmed that Rho already provides native equivalents
for most of its useful workflow ideas. The remaining gaps were addressed without
adding a second agent runtime:

- Subagent model selection now has distinct `planner` and `explorer` roles.
  Explorers default to the economical model, while explicit per-spawn model
  overrides remain authoritative.
- Hook configuration accepts Kimi-compatible lifecycle names including
  `UserPromptQueued`, `TurnStarted`, `PostToolFailure`, `PermissionResult`,
  `SessionHeartbeat`, `TaskStarted`, `StopFailure`, `Interrupt`, and
  `Notification`.
- Rho's existing permission engine already supports ordered allow/deny rules
  such as `Bash(git status*)` and `Write(*.env)`, pre-tool denial hooks, scoped
  policy snapshots, and destructive-command hard blocks.
- Rho's existing goal tracker already provides durable objective state,
  dependencies, progress, token budgets, continuation prompts, and lifecycle
  events. A second `GOAL.md` state machine would duplicate this implementation.

The comparison also found no reason to adopt Kimi Code's two-engine split or
replace Rho's stronger Harrier memory, Shrike token controls, Swift replay, Kestrel
review, Merlin auditing, or Flux provider runtime.

### 1. Skill metadata interoperability

Rho's smart-skill parser accepts both hyphenated and community-schema
snake_case keys:

- `auto-invoke` / `auto_invoke`
- `allowed-tools` / `allowed_tools`
- `source-repo` / `source_repo`
- `source-ref` / `source_ref`
- `source-installed-at` / `source_installed_at`
- `chain-after` / `chain_after`
- `chain-before` / `chain_before`
- `chain-conflicts` / `chain_conflicts`
- `chain-enhances` / `chain_enhances`

Tests cover all aliases. This prevents a community skill from installing
successfully while silently losing invocation, tool, provenance, or chain
metadata.

### 2. Local skill validation

Rho's existing Unicode audit remains the runtime security scanner. A new
structural validator complements it by checking:

- required `name` and `description`
- lowercase kebab-case names
- name/directory agreement
- description length
- semantic version format
- `SKILL.md` size
- `@ref(...)` path containment

`rho skills audit` now reports both Unicode and structural findings. This is a
small Go-native subset of the community repository's broader validation model;
it does not duplicate the registry's Python implementation.

### 3. Transparent preference model

Rho already has `internal/features/taste` with confidence, sample count,
decay, project identity, merge, reset, prompt projection, and accept/edit
signals. No second preference database was created. The user-facing model and
policy are documented in `docs/user-guide/26-learned-preferences.md`:

- explicit instructions remain authoritative
- skills are reusable capabilities
- preferences are inferred tendencies
- Harrier stores durable facts and decisions
- objective review findings cannot be suppressed by preferences

### 4. Workflow and harness documentation

CommandCodeAI's strongest product contribution is discoverability. Rho now
documents its existing capabilities in:

- `docs/user-guide/26-learned-preferences.md`
- `docs/user-guide/27-workflows.md`
- `docs/architecture/rho-harness.md`
- `docs/user-guide/28-workflow-budgets.md`

These cover slash/shell/file-context input, headless review, MCP-backed
analysis, session recovery, recording/replay, skills, preference boundaries,
tool-call repair, permission ordering, stream recovery, and the distinct turn,
tool, depth, time, token, and cost budgets.

## Deliberately Not Adopted

- CommandCodeAI provider adapters: Flux owns provider protocols and routing.
- BaseAI remote pipes: incompatible with Rho's local authority and durable
  event model.
- BaseAI `lowdb` JSON memory: weaker than Harrier and Rho persistence.
- Unconditional parallel tool execution: unsafe for mutations and approvals.
- Historical `gpt3-agent`: no permissions, sandbox, path guard, audit, or tests.
- Media/UI/status repositories: outside Rho's code-intelligence boundary.
- CommandCodeAI branding, proprietary model claims, and undocumented services.

## Verification Plan

1. Run formatting and static checks on all changed Rho Go files.
2. Run focused parser, validator, taste, engine, and command tests.
3. Run the full Rho test suite and vet.
4. Repeat the focused and full checks independently.
5. Merlin the final diff, worktree, and sibling-repository status.

The Flux repository remains a separate repository change and must be published
through its own feature branch and PR before updating Rho's module pin.
