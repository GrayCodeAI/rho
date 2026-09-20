# Security Policy — rho

## Supported versions

We support the latest minor version on each `0.x` line, and the latest two
minor versions once `1.x` ships. Older versions receive critical-severity
fixes only on a best-effort basis.

The current canonical version is the contents of the [`VERSION`](./VERSION)
file at the repo root. See [`docs/versioning.md`](https://github.com/GrayCodeAI/rho/blob/main/docs/versioning.md)
for the eco-wide versioning scheme.

## Reporting a vulnerability

**Do not open a public GitHub issue for security vulnerabilities.** Instead:

1. Open a private [GitHub Security Advisory](https://github.com/GrayCodeAI/rho/security/advisories/new), **or**
2. Email `security@rho.ai` with the details below.

Include in your report:

- A description of the vulnerability and the affected component.
- Steps to reproduce, ideally with a minimal proof-of-concept.
- The version (`VERSION` file or git SHA) you tested against.
- The potential impact and any suggested mitigation.

**Response targets:**

- Initial acknowledgement: within **48 hours**.
- Triage and severity assessment: within **5 business days**.
- Coordinated fix and disclosure: within **30 days** for high/critical, **90
  days** for medium/low (per industry-standard responsible disclosure).

## Disclosure policy

We follow [coordinated vulnerability disclosure](https://en.wikipedia.org/wiki/Coordinated_vulnerability_disclosure):

- Reporters receive credit in the advisory and CHANGELOG (unless they opt
  out).
- We request that reporters refrain from public disclosure until a fix has
  been released or the disclosure deadline above has elapsed.
- We will not pursue legal action against good-faith researchers acting
  within this policy.

## Security practices in this repo

- **Dependency monitoring:** vulnerable dependencies are detected by
  `govulncheck`, which runs on every CI build (see "Vulnerability scanning").
- **Static analysis:** `golangci-lint` (including `gosec` rules) and `go vet`
  are enforced in CI.
- **Vulnerability scanning:** `govulncheck` runs on every CI build.
- **Dependency pinning:** `go.sum` is pinned and committed; independent
  ecosystem modules are checked for reachable published versions by
  `make release-parity`.
- **Reproducible builds:** release artefacts ship with SHA-256 checksums via
  goreleaser.
- **No secrets in source:** API keys are configuration, not constants. Pre-
  commit hooks block accidental secret commits.

## Scope

This policy covers the code in this repository and the release artefacts
published from it. It does not cover:

- Third-party dependencies (report to upstream).
- LLM provider services that rho integrates with (report to the
  provider).
- Local filesystem misuse where an attacker already has shell access (out of
  threat model).

For rho-specific threat-model notes, see the README and any docs in
this repo.

## Config security model

### Clone-and-load attack defense

Project-level `.rho/settings.json` can be committed to a git repository.
An attacker who controls a repository could define MCP servers that execute
arbitrary commands when a developer clones and runs rho in that directory.

**Mitigation:** Project-level MCP servers are stripped from project config
(`projectSafeSettings` in `internal/config/settings.go`) and project
hooks/MCP/plugins/LSP additionally require folder trust: the project root
must be trusted via `rho trust add` (`AllowProjectAutomation` in
`internal/trust/store.go`). There is no `--allow-project-mcp` flag.
Global MCP servers (from `~/.rho/settings.json`) are always loaded.

### Security-sensitive fields

The following settings **cannot** be set by project-level config (stripped by
`projectSafeSettings`):
- `model`, `provider` (selection stays in global config)
- `auto_allow`, `allowed_tools`, `disallowed_tools`, `never_allow` (permissions)
- MCP servers, custom providers, `deployment_routing`, thinking flags
- API keys (never stored in settings.json; use OS secret store via `/config`)

The following settings **can** be set by project config (anything not stripped):
- `theme`, `autonomy`, `max_budget_usd`, and other
  repository-local behavior

### Config merge precedence

Highest priority first (`LoadSettings` / `LoadSettingsWithOverride` in
`internal/config/settings.go`, per-command flag resolution in `cmd/options.go`):

1. CLI `--settings` JSON override
2. Per-command CLI flags (e.g., `--model`, `--provider`)
3. Environment variables (only where explicitly read; there is no global env layer)
4. Project `.rho/settings.json` (repository-safe subset only — see above)
5. Global `~/.rho/settings.json`
6. Built-in defaults (lowest priority)

Project-level config CANNOT escalate permissions beyond what global config
allows. The `MergeSettings` function in `internal/config/settings.go`
implements field-by-field merging with explicit precedence rules.

### Credential storage

API keys and secrets are stored in the OS secret store (macOS Keychain,
Linux secret service, Windows Credential Manager) via the `flux/credentials`
package. They are never written to `settings.json`, `.env`, or any file
in the repository. The `/config` command manages credential storage
interactively.
