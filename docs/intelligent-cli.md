# Intelligent CLI capabilities

Rho keeps the startup tool schema small while making the full capability
surface discoverable on demand. The registry currently contains core tools,
lazy tools, and MCP tools; intent routing promotes only the tools relevant to
the current request.

## Capability bundles

The router recognizes these request families:

- `web`: URL fetching, search, browser navigation, and screenshots
- `code-understanding`: compact reads, outlines, code search, graph, LSP, and impact
- `verification`: project detection, build/test/lint/format checks, diagnostics, and static analysis
- `git`: structured Git, GitHub inspection, history, worktrees, conflicts, and PR summaries
- `editing`: patches, atomic edits, imports, and deterministic refactors
- `data`: SQL and notebook workflows
- `security`: dependency, static-analysis, secret, and history review
- `tool-health`: runtime prerequisite and tool-surface diagnostics

Promotion changes only which schemas are sent to the model. It does not execute
anything, grant approval, or bypass the permission engine.

## Useful tools

```text
ToolHealth       list registered tools and git/go/node/python/gh/Chrome availability
ProjectVerify    detect and run bounded build/test/lint/format checks without a shell
DependencyAudit  check dependency integrity or outdated packages without installing anything
GitHub           list repositories, PRs, issues, checks, and workflow runs through gh
```

All verification commands use fixed executable/argument lists, per-command
timeouts, project-root validation, and structured status/exit-code output.
Dependency and GitHub operations are network-gated and read-only by default.

## Inspecting the registry

```bash
rho tools
rho tools --json
```

The JSON form includes risk level, read-only status, aliases, and intent
categories. Use `ToolSearch` with `select:<ToolName>` when a lazy tool was not
promoted automatically.
