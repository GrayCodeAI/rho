# Rho permission model

Rho executes tools directly on the host. There is no Docker-backed execution
path and no product sandbox runtime. Safety comes from a layered, fail-closed
policy pipeline, path guards, trust checks, tool-level hard stops, and explicit
human approval.

## Decision pipeline

Every tool call is evaluated by `internal/engine/safety.PermissionEngine` in
this order:

1. Dry-run kill switch — deny every tool call.
2. Governance ceiling — an administrator policy can only narrow access.
3. Personal `never` rules — the user's hard deny ceiling.
4. Pre-tool hooks — project or user hooks may deny.
5. Spec-stage gate — restrict tools during specification workflows.
6. Destructive-command hard stop — destructive shell commands cannot be
   granted by autonomy, remembered rules, or bypass.
7. Unified grants — explicit deny wins over allow; narrower rules win over
   broad rules.
8. Autonomy profile — safe calls may proceed without a prompt.
9. Scoped break-glass bypass — optional, time-bound, category-limited, and
   audited.
10. Safe-command classifier.
11. Human approval when no earlier decision applies.

The engine returns structured `allow`, `ask`, or `deny` decisions with a stable
reason, risk level, capabilities, and policy revision. Human-readable text is
an adapter concern; policy code does not depend on the TUI.

## Approval semantics

Interactive approvals have deliberately different scopes:

| Key | Meaning | Remembered? |
|---|---|---|
| `y` | Allow this request once | No |
| `n` | Deny this request once | No |
| `s` | Allow matching calls for this session | Yes, memory only |
| `d` | Deny matching calls for this session | Yes, memory only |

The old `a` key remains an undocumented compatibility alias for `s`. Session
rules disappear when the process exits. A one-shot answer never trains the
auto-mode learner and never silently becomes a rule.

Durable project rules require an explicit command:

```bash
rho permissions list
rho permissions add allow command 'git status'
rho permissions add deny file '.env'
rho permissions revoke <rule-id>
rho permissions reset
```

The reset command asks for confirmation before mutating the rule store.

## Autonomy tiers

| Tier | Intended behavior |
|---|---|
| `supervised` | Ask for every action that is not read-only |
| `scout` | Automatically permit clearly safe inspection |
| `builder` | Permit normal edits; ask for higher-impact execution |
| `operator` | Broad trusted operation with hard stops intact |
| `autonomous` | No normal prompts; governance, hooks, spec gates, and hard stops remain |

Use `/autonomy` in the TUI to inspect or change the tier. Use
`/autonomy rules` to see active grants and their provenance. Use
`/autonomy audit` and `/autonomy metrics` to inspect recent decisions.

## Rules and precedence

Rules use stable tool/pattern identities such as:

```text
Bash(git status)
Bash(go test *)
Write(*.go)
Edit(src/**)
```

The unified grant view keeps sources visible (`memory`, `auto`, `hook`, or
`governance`). Deny always beats allow at the same match, and a more specific
match beats a broad match. Governance is evaluated before user grants, so a
local rule cannot widen an administrator ceiling.

## Host-native boundaries

Rho does not pretend that a host permission prompt is an operating-system
containment boundary. The following are separate defenses:

- Path tools enforce project and protected-path rules.
- Shell execution blocks destructive commands and suspicious escapes.
- Credential paths are protected and credential requests are approval-gated.
- Folder trust controls project hooks, MCP servers, and plugins.
- Worktrees provide repository-level isolation for delegated agents.
- Dry-run provides a deterministic preview mode for CI and review.

If a task requires stronger process or filesystem containment than these host
controls provide, run Rho inside an environment chosen by the operator. That
deployment decision is intentionally outside Rho's product runtime.

## Verification

Permission changes must have focused unit tests and full repository checks:

```bash
go test ./cmd ./internal/engine/safety ./internal/permissions -count=1
make lint
go test ./... -count=1
make boundaries
```

The actual binary should also be smoke-tested:

```bash
make build
bin/rho --help
bin/rho permissions list --json
bin/rho completion json
```
