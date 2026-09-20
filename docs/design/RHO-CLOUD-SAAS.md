# Rho Cloud — Hosted Execution Plane (Design Doc)

**Status:** Draft / Proposal
**Author:** Platform team
**Last updated:** 2026-06-06
**Scope:** Multi-month product effort. This is a design a team executes against, not a code-session deliverable.

---

## 1. Overview & Competitive Context

Rho today is a terminal-native, single-user, privacy-first Go binary. The daemon
(`rho/internal/daemon/daemon.go`) exposes a small HTTP API — `POST /v1/chat`
with optional SSE streaming, plus session CRUD — bound to loopback
(`netutil.LoopbackHost`, port `4590` by default) and protected by a single
optional shared API key (`Server.apiKey`, compared in `daemon.go:185-205`). There
is no notion of a user, an org, a tenant, a credit balance, or remote isolated
execution. Rho does not provide a container or OS sandbox runtime; commands
run on the host behind the permission engine and tool-level path guards.

This doc designs **Rho Cloud**: a hosted, multi-tenant execution plane that runs
the rho agent as a managed service, authenticated by OAuth and API keys, metered
and billed by credits, organized into team workspaces with org RBAC, and backed by
cloud sandboxed execution (E2B-/Daytona-style microVMs) instead of the user's
local Docker. It also covers the two surfaces that depend on this plane: a
browser/web chat-editing UI served from the daemon, and IDE extensions
(VS Code / JetBrains) speaking the Agent Client Protocol (ACP).

### Which Top-20 repos ship this

From `TOP20_COMPARISON.md`, the rho P0 table (lines 27-37) and P2 table
(lines 70-86) name these directly:

| Capability | Top-20 repos that ship it | Comparison ref |
|---|---|---|
| Cloud-hosted SaaS / managed execution plane | OpenHands Cloud, E2B, Claude Code (managed Routines), Daytona | `TOP20_COMPARISON.md:33` |
| Multi-user / team workspaces + conversation sharing | OpenHands Enterprise, Cursor, Windsurf, Daytona | `TOP20_COMPARISON.md:34` |
| Cloud sandboxed execution (E2B Firecracker / Daytona) | E2B (Firecracker microVMs), Daytona, OpenHands | `TOP20_COMPARISON.md:35` |
| Browser/web UI for chat-driven editing | Aider (`--browser`/Gradio), OpenHands (React SPA), Continue | `TOP20_COMPARISON.md:31` |
| VS Code / JetBrains extensions, ACP protocol | Continue, Claude Code, Cursor, Windsurf, Aider, Gemini CLI (ACP) | `TOP20_COMPARISON.md:32,36` |
| Org RBAC (Member/Admin/Owner), shared credit pools | OpenHands Cloud | `TOP20_COMPARISON.md:72` |

The comparison doc is blunt that this is a **fundamental product-tier gap**
(`TOP20_COMPARISON.md:33`) — the single largest item in the report — and that org
RBAC "requires a multi-tenant SaaS backend ... beyond the current single-user
token store at `/rho/internal/auth/auth.go`" (`TOP20_COMPARISON.md:72`).

The flux side of the house has the parallel gaps: a LiteLLM-compatible proxy
endpoint and "multi-tenant team/project management with SSO/RBAC" with per-key
budgets (`TOP20_COMPARISON.md:95-96`). Rho Cloud and flux multi-tenancy share
the same identity, billing, and RBAC substrate; this doc designs that substrate
once and shows how both consume it.

---

## 2. Goals / Non-Goals

### Goals

1. **Managed daemon-as-a-service.** Run the existing engine (`internal/engine`)
   behind a hosted, internet-facing, horizontally scalable control plane that
   anyone can reach with a token — without the agent ever touching the operator's
   infrastructure beyond its own sandbox.
2. **OAuth + API-key auth** layered on top of, not replacing, the existing
   single-user token store and device flow (`internal/auth/`).
3. **Multi-tenant session routing.** Every session is owned by a `(user, workspace,
   org)` tuple; the daemon today keys sessions only by string ID in a process-local
   `sync.Map` (`daemon.go:35`). Net-new: a routing layer that maps tenant → session
   → execution worker.
4. **Billing / credit system.** Meter token spend and sandbox-minutes per tenant,
   price normalized usage with Rho Cloud's versioned server catalog, enforce
   hard limits, and bill from the authoritative cloud ledger.
5. **Team workspaces & conversation sharing.** Shared, permissioned session state;
   read-only and continue-able shares.
6. **Org RBAC** with Member / Admin / Owner tiers, invitations, and org-scoped
   credit pools and provider config.
7. **Cloud-isolated execution.** Keep local rho host-native and, if this
   proposal is implemented, run cloud workers in on-demand isolated microVMs
   (E2B/Daytona/Firecracker) behind a new cloud-only boundary.
8. **Browser/web UI** for chat-driven editing, served from the plane, consuming
   the existing SSE stream.
9. **IDE extensions** (VS Code, JetBrains) over ACP with IDE-native diff review.

### Non-Goals

- **Not** replacing the local/offline single-binary mode. Local rho remains
  fully functional with zero cloud dependency; cloud is strictly additive. The
  privacy-first posture (`TOP20_COMPARISON.md:20`) is preserved — cloud is opt-in.
- **Not** building our own microVM hypervisor. We integrate E2B/Daytona (build-vs-buy
  in §6), not write Firecracker orchestration from scratch.
- **Not** an LLM gateway rewrite. Flux remains the provider engine behind
  Rho's `flux/engine` integration; the plane calls a Rho agent worker, which
  composes Flux. The plane does not re-implement routing, caching, or provider
  adapters.
- **Not** SSO/SAML in P0 (deferred to P2; OIDC social login + API keys first).
- **Not** on-prem/self-hosted enterprise distribution in P0 (P2).
- **Not** mobile-native apps (the browser UI is responsive; that suffices initially).

---

## 3. Architecture

### 3.1 Components

```
                         ┌────────────────────────────────────────┐
   Browser UI ──┐        │              Rho Cloud                 │
   IDE (ACP)  ──┼──TLS──▶│                                         │
   CLI (token)──┘        │  ┌──────────────┐   ┌────────────────┐ │
                         │  │  Edge / API  │   │  Identity svc  │ │
                         │  │  Gateway     │──▶│ OAuth, APIkeys │ │
                         │  │ (auth, rate, │   │ orgs/users/RBAC│ │
                         │  │  routing)    │   └────────────────┘ │
                         │  └──────┬───────┘   ┌────────────────┐ │
                         │         │           │  Billing svc   │ │
                         │         │           │ credits/meter  │ │
                         │         ▼           └────────────────┘ │
                         │  ┌──────────────┐   ┌────────────────┐ │
                         │  │ Session      │   │ Workspace svc  │ │
                         │  │ Router       │◀─▶│ shares/state   │ │
                         │  └──────┬───────┘   └────────────────┘ │
                         │         │                              │
                         │         ▼                              │
                         │  ┌──────────────┐      ┌─────────────┐ │
                         │  │ Agent Worker │─────▶│  flux       │ │
                         │  │ (engine.     │      │  (model      │ │
                         │  │  Session)    │      │   runtime)   │ │
                         │  └──────┬───────┘      └─────────────┘ │
                         └─────────┼──────────────────────────────┘
                                   ▼
                         ┌──────────────────────┐
                         │ Sandbox provider      │
                         │ (E2B / Daytona microVM)│  ◀── CloudSandbox
                         └──────────────────────┘     uses a new cloud-only
                                                       execution boundary
```

**Edge / API Gateway** — terminates TLS, authenticates every request (OAuth bearer
or API key), enforces rate limits and per-tenant credit checks, then routes to the
Session Router. This is the cloud-hosted descendant of the daemon's `auth`
middleware (`daemon.go:185`) and `routes()` table (`daemon.go:174-183`).

**Identity service** — orgs, users, memberships, roles, API keys, OAuth tokens.
Net-new. Wraps/extends `internal/auth` (see §4).

**Billing service** — credit ledger, metering ingestion, server-side pricing,
hard-limit enforcement, and invoicing. Rho Cloud owns this authority; Flux
supplies normalized model and token usage through Rho's Engine boundary.

**Workspace service** — workspace membership, shared session state, share links,
permission checks for view/continue.

**Session Router** — maps an authenticated `(org, workspace, user, session_id)` to
a live Agent Worker (or schedules a new one). This is the multi-tenant generalization
of the daemon's `sessions sync.Map` (`daemon.go:35`).

**Agent Worker** — a process (or pod) running `engine.NewSessionWithClient(...)`
(`internal/engine/session.go:137`) and driving `Session.Stream(ctx)`
(`internal/engine/stream.go:20`). This is the existing rho agent loop, unchanged,
running server-side. One worker handles one active session at a time (worktree-per-
session isolation), matching the existing single-user model — we scale by running
many workers, not by making one worker multi-tenant.

**Cloud execution provider** — a future `CloudSandbox` boundary backed by
E2B/Daytona. This is intentionally net-new cloud code; local rho has no
container executor or sandbox backend to reuse.

### 3.2 Data Model

New persistent store (Postgres). Local mode keeps its file/SQLite stores untouched.

```
orgs(id, name, created_at, billing_customer_id, default_provider_config jsonb)

users(id, email, name, created_at, oauth_subject, oauth_provider)

memberships(org_id, user_id, role)            -- role ∈ {owner, admin, member}

workspaces(id, org_id, name, created_at, settings jsonb)

workspace_members(workspace_id, user_id, role) -- workspace-scoped role

api_keys(id, org_id, user_id, prefix, hash, scopes, last_used_at, revoked_at)
        -- hash only; never store the raw key (mirrors daemon constant-time
        --  compare in daemon.go:207, but stored hashed)

sessions(id, workspace_id, owner_user_id, created_at, updated_at,
         model, provider, cwd, name, state, worker_id, sandbox_id)
        -- supersedes the in-memory daemon Session struct (daemon.go:81-88)

session_shares(session_id, shared_by, mode, token, expires_at)
        -- mode ∈ {view, continue}

credit_ledger(id, org_id, ts, delta_credits, reason, ref_session_id, ref_meter_id)
        -- append-only; balance = SUM(delta_credits)

meter_events(id, org_id, workspace_id, session_id, ts, kind,
             tokens_in, tokens_out, model, sandbox_ms, cost_usd, credits,
             pricing_catalog_version)
        -- kind ∈ {llm, sandbox}; cost_usd is priced by Rho Cloud
```

`meter_events.cost_usd` is computed from normalized model/token dimensions by
Rho Cloud's versioned pricing catalog. Client-side estimates may be displayed
or audited, but never affect credits or invoices.

### 3.3 API Surface

The cloud API is a **superset** of the existing daemon routes. Existing daemon
paths are preserved verbatim so the CLI and gateways keep working; tenant-scoped
paths are added. (Existing routes: `daemon.go:174-183`.)

```
# Auth & identity
POST   /v1/auth/oauth/authorize         # OAuth code grant start (PKCE)
POST   /v1/auth/oauth/token             # code → access/refresh
POST   /v1/auth/device                  # device grant (reuses DeviceFlow, §4)
POST   /v1/orgs/{org}/api-keys          # mint API key (admin+)
DELETE /v1/orgs/{org}/api-keys/{id}     # revoke

# Orgs / RBAC
POST   /v1/orgs                         # create org (creator = owner)
POST   /v1/orgs/{org}/invitations       # invite user (admin+)
PUT    /v1/orgs/{org}/members/{user}    # change role (owner)
GET    /v1/orgs/{org}/credits           # balance + recent ledger

# Workspaces
POST   /v1/workspaces                   # create in org
POST   /v1/workspaces/{ws}/members      # add member

# Chat (TENANT-SCOPED; same body/SSE as today)
POST   /v1/workspaces/{ws}/chat         # == existing POST /v1/chat + tenant ctx
GET    /v1/workspaces/{ws}/sessions     # list (tenant-filtered)
GET    /v1/sessions/{id}                # detail (existing: routes_sessions.go:13)
GET    /v1/sessions/{id}/messages       # paginated (existing: routes_sessions.go:53)
POST   /v1/sessions/{id}/share          # create share link
GET    /v1/shared/{token}               # view/continue a shared session

# Compatibility (unchanged)
POST   /v1/chat                         # legacy single-tenant (daemon.go:176)
GET    /v1/health                       # (daemon.go:175)
```

The `ChatRequest`/`ChatResponse`/SSE event shapes (`daemon.go:60-79`,
`daemon.go:315-337`) are reused unchanged. The web UI and IDE extensions consume
the **exact same SSE `content`/`done` event stream** the CLI already gets.

### 3.4 Key Flows (sequences)

**A. Authenticated cloud chat (SSE)**

```
Client → Edge:    POST /v1/workspaces/{ws}/chat  (Bearer token, Accept: text/event-stream)
Edge:             validate token  → (org,user)        [Identity svc]
Edge:             check membership(user, ws) ≥ member  [Workspace svc]
Edge:             check credit balance(org) > floor    [Billing svc]   ← reject 402 if empty
Edge → Router:    route(org, ws, user, session_id?)
Router → Worker:  acquire/lease Agent Worker (+ Sandbox)
Worker:           engine.Session.Stream(ctx)           [stream.go:20]
Worker → flux:   Chat/StreamChat with ctx carrying virtual key
                  (WithVirtualKey, budget_provider.go:23)
Worker → Edge:    SSE content events  (daemon.go:321-337, reused verbatim)
Edge → Client:    SSE relay
Worker → Billing: emit meter_event(llm)  + meter_event(sandbox) on turn/close
Billing:          append credit_ledger debit; if balance ≤ 0 → signal Worker to halt
```

**B. OAuth login (browser UI / IDE)**

PKCE authorization-code grant for the web UI; **device grant for the CLI/IDE** —
rho already implements RFC 8628 device flow end-to-end
(`internal/auth/device_flow.go`: `RequestCode`, `PollForToken`,
`exchangeCode`). Rho Cloud stands up the *server* side of these grants; the
client side is largely present.

**C. Share a conversation**

```
Owner → Edge:  POST /v1/sessions/{id}/share {mode: view|continue, expires}
Edge:          authorize owner is session owner or workspace admin
Workspace svc: insert session_shares row, return token
Viewer → Edge: GET /v1/shared/{token}
Edge:          resolve token → session; if mode=view, serve read-only message
               history (reuses handleGetMessages, routes_sessions.go:53);
               if mode=continue, fork session state for the viewer
```

**D. Cloud sandbox lifecycle**

```
Router:   need sandbox for session
Worker:   CloudSandbox.Start(ctx)         → E2B/Daytona create microVM
Worker:   CloudSandbox.Exec(ctx, cmd, t)  → run in microVM (same signature as
                                            ContainerSandbox.Exec, container.go:105)
Worker:   on idle/timeout → CloudSandbox.Stop()  → destroy microVM
Billing:  sandbox_ms metered from Start→Stop
```

---

## 4. Integration With Existing graycode-eco Code

This is the crux: **what is reusable today vs. net-new.**

### Reusable today (high leverage)

| Existing asset | File | How Rho Cloud reuses it |
|---|---|---|
| Engine session + agent loop | `internal/engine/session.go:42,132,137`, `internal/engine/stream.go:20` | The Agent Worker *is* `engine.Session.Stream`. No change to the agent loop — it already runs headless with a `SessionFactory` (`daemon.go:27`). |
| HTTP daemon + routes + SSE | `internal/daemon/daemon.go:174-183, 315-337` | The Edge/API gateway is the daemon's `routes()`/`handleChat` generalized. SSE framing (per-line `data:` escaping, `daemon.go:326-329`) is correct and reused verbatim. |
| `SessionFactory` indirection | `daemon.go:27,99-114`; wired in `cmd/daemon.go:99` | The factory boundary is exactly the seam to inject tenant context, per-tenant provider config, and the cloud sandbox. Cloud passes a tenant-aware factory. |
| Autonomy / permission gating | `daemon.go:286-298` (`PresetConfig`, `NeedsPermission`) | Server-side auto-approval policy already exists for non-interactive runs; cloud reuses it per-workspace policy. |
| Auth primitives | `internal/auth/auth.go` (`TokenStore`, `SecureStorage`), `device_flow.go` (full RFC 8628) | Device-grant **client** is done. `SecureStorage` (macOS keychain + file fallback, `auth.go:54-121`) stays for local credential caching of cloud tokens. `GenerateNonce` (`auth.go:124`) for OAuth state/PKCE. |
| Constant-time key compare | `daemon.go:207-224` | API-key verification logic carries over (but keys move to hashed storage; see net-new). |
| Flux normalized usage | `flux/engine` response and stream usage DTOs through Rho's adapter | Reuse model/token dimensions as metering input. Do not import lower Flux packages or trust client-computed prices for billing. |
| Remote execution interface | Planned cloud execution boundary | A future `CloudSandbox` would be isolated from the local host runtime behind an explicit provider interface. |
| Sandbox lifecycle manager | Net-new cloud component | E2B/Daytona pause/resume/snapshot semantics should be designed at the cloud boundary; local rho has no sandbox lifecycle manager. |
| Messaging gateways | `internal/daemon/gateway.go`, `telegram.go`, `discord.go`, `slack.go` | Already forward to `/v1/chat` via `forwardToRho` (`gateway.go:17`) with bearer auth. In cloud they forward to the tenant-scoped chat endpoint with the org's key — minimal change. |
| Cron engine | (not yet implemented) | Cloud scheduled runs (Routines) are planned, per `TOP20_COMPARISON.md:48`. Out of scope here but shares the worker plane. |

### Net-new (must build)

1. **Multi-tenant identity store** — orgs/users/memberships/roles/api-keys/oauth
   tokens in Postgres. `internal/auth` today is a **single-user, single-process
   token map** (`auth.go:17-52`, an in-memory `map[string]string` of provider→token);
   it has no concept of users, orgs, or roles. This is the largest net-new chunk
   and is explicitly called out in `TOP20_COMPARISON.md:72`.
2. **OAuth server side** — the daemon only does the device-grant *client*
   (`device_flow.go`). The authorization server (issue codes, PKCE verification,
   refresh, social IdP federation) is net-new.
3. **API keys as first-class records** — today there is exactly one process-wide
   shared key (`Server.apiKey`, `daemon.go:37`, compared in `daemon.go:199`).
   Cloud needs minted, hashed, scoped, revocable per-org keys with `last_used_at`.
4. **Session Router + persistence** — the daemon's `sessions sync.Map`
   (`daemon.go:35`) is process-local and unauthenticated-by-tenant; session IDs are
   `fmt.Sprintf("daemon-%d", …)` timestamps (`daemon.go:357`). Net-new: durable,
   tenant-scoped session records and a router that leases workers.
5. **Worker orchestration** — running, scaling, and isolating Agent Workers
   (pods/processes) with worktree-per-session. Today there is exactly one
   in-process engine. Net-new control loop.
6. **`CloudSandbox`** — E2B/Daytona-backed cloud-only execution provider.
   The interface and call sites exist; the cloud backend does not.
7. **Billing/credit ledger + invoicing + Stripe integration** — flux gives
   enforcement and cost math; the ledger, top-ups, plans, and payment processor are
   net-new.
8. **Workspace sharing** — `session_shares` and the share/continue flows.
9. **Web UI** — `go:embed`ed SPA served from the plane (Aider/OpenHands pattern,
   `TOP20_COMPARISON.md:31`).
10. **IDE extensions + ACP server** — VS Code/JetBrains clients and the daemon-side
    ACP endpoint (`TOP20_COMPARISON.md:32,36`).

---

## 5. Phased Rollout

### P0 — "It runs in the cloud for one team" (foundational)

**Milestones**
- **M0.1** Postgres data model (orgs/users/memberships/api_keys/sessions/credit_ledger/meter_events) + migrations.
- **M0.2** Identity service: OAuth code grant (PKCE) + social IdP (Google/GitHub), API-key mint/verify (hashed), reusing `GenerateNonce` (`auth.go:124`) and the device-grant client (`device_flow.go`).
- **M0.3** Edge gateway wrapping the existing `routes()`/`handleChat` with tenant auth middleware (generalizing `daemon.go:185`).
- **M0.4** Session Router + durable session store replacing the in-memory `sync.Map`; tenant-scoped `POST /v1/workspaces/{ws}/chat` (SSE reused verbatim).
- **M0.5** Single-region Agent Worker pool (1 session/worker, worktree isolation).
- **M0.6** `CloudSandbox` (E2B *or* Daytona — pick one) implementing `containerExecutor` (`container.go:18`).
- **M0.7** Metering: ingest normalized model/token dimensions, price them with
  the versioned Rho Cloud catalog, persist the Postgres credit ledger, and
  return HTTP 402 at the hard credit floor.
- **M0.8** Minimal web UI (single `go:embed` page) for chat + diff view.

**Exit criteria:** a paying single team can log in via OAuth, run rho against a
cloud sandbox, see streamed output and diffs in the browser, and get cut off when
credits hit zero.

### P1 — "Teams, sharing, billing UX, IDE"

- **M1.1** Org RBAC enforced everywhere: Member/Admin/Owner, invitations, role changes (`TOP20_COMPARISON.md:72`).
- **M1.2** Workspaces + workspace-scoped membership.
- **M1.3** Conversation sharing (view + continue) via `session_shares` (`TOP20_COMPARISON.md:34`).
- **M1.4** Stripe billing: plans, top-ups, shared org credit pools, usage dashboard.
- **M1.5** Full web UI (session list, message history via `handleGetMessages`, side-by-side diff, file tree).
- **M1.6** VS Code extension over ACP with IDE-native diff review (`TOP20_COMPARISON.md:32,36`).
- **M1.7** Sandbox pause/resume/snapshot mapped to provider APIs (mirrors `snapshot_sandbox.go` semantics) for fast session resume + cost savings.

### P2 — "Enterprise & scale"

- **M2.1** SSO/SAML + SCIM provisioning.
- **M2.2** Multi-region worker pools + autoscaling; second sandbox provider for redundancy.
- **M2.3** JetBrains extension (ACP).
- **M2.4** Org-scoped provider config (BYO keys / BYO model endpoints), aligning with flux multi-tenant proxy (`TOP20_COMPARISON.md:96`).
- **M2.5** Audit logging, IT-managed policy tiers, self-hosted/on-prem distribution.
- **M2.6** Cloud Routines (scheduled/triggered runs) on the worker plane (`TOP20_COMPARISON.md:48`).

---

## 6. Build vs. Buy & Dependencies

| Concern | Decision | Rationale / Licensing |
|---|---|---|
| MicroVM sandboxing | **Buy/integrate** E2B (P0) + Daytona (P2 redundancy) | Building Firecracker orchestration is a multi-quarter effort orthogonal to product value. E2B SDK is open source (Apache-2.0/MIT family); Daytona is Apache-2.0. Both have hosted offerings; we can also self-host E2B later. The `containerExecutor` seam (`container.go:18`) keeps us provider-agnostic. |
| Identity / OAuth server | **Buy** (Ory Hydra/Kratos, Auth0, or WorkOS) for P0; revisit P2 | Writing a compliant OAuth2/OIDC AS is risky. Ory is Apache-2.0 (self-hostable, privacy-aligned). WorkOS/Auth0 are SaaS — faster but introduce a third party in the auth path (weigh against privacy posture). Prefer **Ory self-hosted** to keep the privacy-first promise. |
| Billing / payments | **Buy** Stripe for payment + invoicing; **build** the credit ledger | Never build a card processor. The ledger and metering are ours (and partly exist in flux). Stripe SDK is permissive. |
| Database | **Buy** managed Postgres | Standard. |
| Metering enforcement | **Build in Rho Cloud** on normalized Engine usage | Billing authority must be server-owned and versioned; lower Flux packages remain engine implementation details. |
| Web UI framework | **Build** small React SPA, `go:embed` served (Aider/OpenHands pattern) | Keeps deployment a single binary; matches `TOP20_COMPARISON.md:31`. |
| IDE protocol | **Adopt** ACP (Agent Client Protocol) | Industry trajectory (Gemini CLI, Zed, JetBrains), `TOP20_COMPARISON.md:32,36`. Avoid bespoke per-IDE protocols. |

**Licensing watch-outs:** keep all new server dependencies permissive
(Apache-2.0/MIT/BSD). Flag any AGPL transitive deps (the cross-cutting license
scan in `TOP20_COMPARISON.md:241` should gate this). The hosted plane is a network
service, so AGPL deps would create source-disclosure obligations — avoid them.

---

## 7. Security & Privacy

These repos are privacy-first (`TOP20_COMPARISON.md:20`); the cloud plane must not
erode that.

1. **Cloud is opt-in and isolated from local mode.** Local rho never phones home;
   nothing in `internal/engine` gains a cloud dependency. The
   plane is a separate deployable.
2. **Tenant isolation by construction.** One session ↔ one Agent Worker ↔ one
   microVM; no shared filesystem or process between tenants. Worktree-per-session
   (already rho's isolation model) carries over. Cloud microVMs should default to
   no network egress unless a workspace explicitly grants it through a cloud-owned
   allowlist. Local host execution remains governed by rho's permission and path
   policies, not by this cloud design.
3. **Secrets never stored raw.** API keys stored as hashes only (the constant-time
   compare in `daemon.go:207` becomes hash-compare). OAuth refresh tokens encrypted
   at rest. Reuse `SecureStorage` (`auth.go:54`) for client-side caching only.
4. **Per-line SSE escaping is a real injection defense** already present
   (`daemon.go:324-329`) and must be preserved in the relay path — LLM output can
   contain newlines that would otherwise forge SSE events.
5. **Credit enforcement is a DoS control too.** Hard floors (HTTP 402) via flux
   `CheckBudget` (`budget_provider.go:181`) bound runaway agent spend per tenant.
6. **Code privacy.** Customer code lives only in the ephemeral microVM and the
   session store; offer per-org data-retention controls and a "no-retention" mode
   that discards transcripts after the session. Redaction (shrike secrets detection,
   `shrike/secrets.go`) should run before any transcript is persisted to the shared
   store.
7. **RBAC checks at the edge AND the service layer** (defense in depth) — never
   trust workspace/org IDs from the client without verifying membership.
8. **Audit log** (P2) of privileged actions (role changes, key mint/revoke, share
   creation), append-only, mirroring the credit ledger's append-only design.
9. **Sandbox egress.** The browser-UI and IDE surfaces must not let a shared
   "continue" session exfiltrate the original owner's secrets — `continue` shares
   fork state and re-scope credentials to the continuer's identity.

---

## 8. Open Questions

1. **Worker model:** one OS process per session (simple, heavy) vs. goroutine-per-
   session within a multi-tenant worker (efficient, but the engine is built
   single-user — `daemon.go` keys one `apiKey`, sessions share a process). Leaning
   process/pod-per-session for isolation; needs a cost model.
2. **Sandbox provider:** E2B vs. Daytona for P0. E2B = fastest microVM cold start;
   Daytona = richer dev-environment model. Which maps better onto rho's
   pause/resume/snapshot (`snapshot_sandbox.go`)?
3. **Credit unit:** bill in $-equivalent credits (1 credit = $0.01?) covering both
   server-priced LLM usage and sandbox-minutes — what's the blended margin?
4. **BYO keys vs. managed keys:** if an org brings its own provider keys, do their
   LLM tokens still consume credits (we only charge sandbox/orchestration), or run
   free? Affects flux provider-config plumbing (`TOP20_COMPARISON.md:96`).
5. **Identity build-vs-buy:** Ory self-hosted (privacy, ops burden) vs. WorkOS/Auth0
   (speed, third party in auth path). Privacy posture argues for Ory.
6. **Session store backend:** Postgres for everything vs. Postgres (metadata) +
   object store (transcripts/diffs). Transcripts can be large.
7. **ACP maturity:** is ACP stable enough across VS Code + JetBrains to be the sole
   IDE protocol, or do we need a VS Code-native fallback in P1?
8. **Data residency / regions:** does P0 single-region block any target customers?
9. **Local↔cloud session portability:** can a user start locally and "push" a
   session to the cloud (and back)? The session JSONL format (`routes_sessions.go`)
   is portable; the sandbox state is not.

---

## 9. Effort Estimate (rough, eng-weeks)

Assumes a small senior team (3-4 engineers). Estimates are for engineering only
(exclude design, GTM, SRE buildout).

| Workstream | P0 | P1 | P2 |
|---|---:|---:|---:|
| Data model + migrations | 2 | 1 | 1 |
| Identity (OAuth server, API keys, integrate Ory) | 6 | 2 | 4 (SSO/SCIM) |
| Edge gateway + tenant middleware | 3 | 1 | 2 (multi-region) |
| Session Router + durable sessions + worker orchestration | 6 | 3 | 5 (autoscale/regions) |
| `CloudSandbox` (E2B integration) | 4 | 2 (pause/resume) | 4 (2nd provider) |
| Billing/credit ledger + Stripe + metering | 4 | 4 (dashboard/pools) | 2 (audit) |
| Org RBAC + workspaces + sharing | 2 | 5 | — |
| Web UI | 3 | 5 | 2 |
| IDE extensions (ACP) | — | 5 (VS Code) | 5 (JetBrains) |
| Security hardening / privacy controls | 2 | 2 | 4 |
| **Subtotal** | **~32** | **~30** | **~33** |

**Total: ~95 eng-weeks (~22-24 calendar weeks for a 4-person team across P0→P2),
with P0 a hard ~8-10 calendar weeks for a usable single-team beta.**

Leverage note: the agent loop, SSE, sandbox interface, device-flow client, and
flux budget machinery are **already built** — they collapse what would otherwise
be the most expensive parts (the agent runtime and the metering enforcement engine)
into integration work. The genuinely greenfield cost is identity/multi-tenancy,
worker orchestration, and the cloud sandbox backend.
