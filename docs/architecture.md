<div align="center">

# <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/bird.svg" width="16" height="16" alt="bird" /> rho Architecture

**AI Coding Agent for Your Terminal**

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev/)
[![Port](https://img.shields.io/badge/Port-4590-orange)](https://www.iana.org/assignments/service-names-port-numbers/service-names-port-numbers.xhtml)
[![Protocol](https://img.shields.io/badge/Protocol-REST-blue)](https://swagger.io/specification/)

</div>

---

## <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/target.svg" width="16" height="16" alt="target" /> Overview

rho is an AI-powered coding agent for the terminal. It reads codebases, writes and edits files, runs tests, and manages git — all through natural language. Zero CGO, single static binary for linux/darwin/windows on amd64/arm64.

Detailed planning docs for the Rho product architecture live in [`docs/architecture/`](architecture/README.md).

---

## <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/blocks.svg" width="16" height="16" alt="blocks" /> Layered Architecture

```
rho/
├── api/openapi.yaml           <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/file-text.svg" width="16" height="16" alt="file-text" /> Daemon REST API contract (OpenAPI 3.1)
├── cmd/                       <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/terminal.svg" width="16" height="16" alt="terminal" /> Cobra CLI commands (200+ files)
│   ├── rho/main.go           <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/zap.svg" width="16" height="16" alt="zap" /> Entry point — calls cmd.Execute()
│   ├── root.go                <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/settings.svg" width="16" height="16" alt="settings" /> Root command, flag definitions
│   ├── daemon.go              <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/server.svg" width="16" height="16" alt="server" /> Daemon start/stop/status
│   ├── chat.go                <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/message-square.svg" width="16" height="16" alt="message-square" /> Interactive TUI chat
│   └── ...
├── internal/
│   ├── api/                   <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/globe.svg" width="16" height="16" alt="globe" /> HTTP server (:4590) — 8 REST endpoints
│   ├── daemon/                <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/server.svg" width="16" height="16" alt="server" /> Daemon lifecycle (PID file, socket)
│   ├── engine/                <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/brain.svg" width="16" height="16" alt="brain" /> Agent execution loop
│   │   ├── session.go         <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/refresh-cw.svg" width="16" height="16" alt="refresh-cw" /> Core agent loop (Stream, agentLoop)
│   │   ├── ctxmgr/            <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/package.svg" width="16" height="16" alt="package" /> Context packing and visualization
│   │   ├── token/             <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/coins.svg" width="16" height="16" alt="coins" /> Budget allocation and prediction
│   │   ├── streaming/         <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/radio.svg" width="16" height="16" alt="radio" /> Response cache and stream optimizer
│   │   ├── planning/          <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/target.svg" width="16" height="16" alt="target" /> Goals and task decomposition
│   │   └── workflow/          <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/wrench.svg" width="16" height="16" alt="wrench" /> JSON-defined automation pipelines
│   ├── tool/                  <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/hammer.svg" width="16" height="16" alt="hammer" /> 40+ built-in tools
│   ├── config/                <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/settings.svg" width="16" height="16" alt="settings" /> Settings, env manager, migration
│   ├── session/               <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/database.svg" width="16" height="16" alt="database" /> SQLite persistence, search, export
│   ├── permissions/           <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/shield.svg" width="16" height="16" alt="shield" /> Guardian, rules DSL, boundary checker
│   ├── intelligence/          <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/git-branch.svg" width="16" height="16" alt="git-branch" /> Repo map, AST analysis, deps
│   ├── multiagent/            <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/users.svg" width="16" height="16" alt="users" /> Personas, inter-agent messaging
│   ├── mcp/                   <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/plug.svg" width="16" height="16" alt="plug" /> MCP client and server
│   ├── bridge/                <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/link.svg" width="16" height="16" alt="link" /> Bridges to ecosystem services
│   └── resilience/            <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/refresh-cw.svg" width="16" height="16" alt="refresh-cw" /> Circuit breaker, retry, rate limit
├── docs/                      <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/book-open.svg" width="16" height="16" alt="book-open" /> Architecture docs
└── (ecosystem siblings live at ../<repo> in the graycode-eco workspace; see docs/architecture/ecosystem-design.md)
```

---

## <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/globe.svg" width="16" height="16" alt="globe" /> Daemon HTTP API (:4590)

| | |
|---|---|
| **Contract** | [`api/openapi.yaml`](../api/openapi.yaml) |
| **Port** | `:4590` (default). Override: `RHO_DAEMON_PORT` |
| **Auth** | Bearer token or `X-API-Key`. Set via `RHO_DAEMON_API_KEY` |

<details>
<summary><b><img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/radio.svg" width="16" height="16" alt="radio" /> Endpoint Summary</b></summary>

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/health` | <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/heart.svg" width="16" height="16" alt="heart" /> Health check |
| `GET` | `/v1/version` | <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/tag.svg" width="16" height="16" alt="tag" /> Version info |
| `POST` | `/v1/chat` | <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/message-square.svg" width="16" height="16" alt="message-square" /> Send message (JSON or SSE) |
| `GET` | `/v1/sessions` | <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/list.svg" width="16" height="16" alt="list" /> List sessions |
| `GET` | `/v1/sessions/{id}` | <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/search.svg" width="16" height="16" alt="search" /> Get session |
| `GET` | `/v1/sessions/{id}/messages` | <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/message-square.svg" width="16" height="16" alt="message-square" /> Get messages |
| `DELETE` | `/v1/sessions/{id}` | <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/trash-2.svg" width="16" height="16" alt="trash-2" /> Delete session |
| `GET` | `/v1/stats` | <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/bar-chart.svg" width="16" height="16" alt="bar-chart" /> Usage statistics |

</details>

---

## <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/link.svg" width="16" height="16" alt="link" /> Ecosystem Integration

| Service | Role | Connection |
|---------|------|------------|
| <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/bird.svg" width="16" height="16" alt="bird" /> **flux** | LLM provider runtime | Library — all LLM calls routed through its engine facade |
| <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/scissors.svg" width="16" height="16" alt="scissors" /> **token** | Token optimization | Embedded — counting, compression, secrets, usage tracking |

### Client SDKs

| SDK | Language | Key Features |
|-----|----------|--------------|
| **sparrow** | Go | Cobra-style client, context-aware retries, SSE streaming |
| **robin** | Python | Pydantic models, httpx async, pytest suite |
| **wren** | TypeScript | Zero dependencies (global fetch), Agent + tools |

All three SDKs share types from **eagle** and consume the daemon REST API (:4590).

> <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/lightbulb.svg" width="16" height="16" alt="lightbulb" /> **rho never talks to LLM APIs directly** — all calls go through flux.

---

## <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/shield.svg" width="16" height="16" alt="shield" /> Tool Safety Layer

Every tool call passes through the permission system before execution:

```
Tool Call → <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/shield.svg" width="16" height="16" alt="shield" /> Permission Rules → <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/blocks.svg" width="16" height="16" alt="blocks" /> Path & Boundary Checks → <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/user.svg" width="16" height="16" alt="user" /> User Approval → <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/check-circle.svg" width="16" height="16" alt="check-circle" /> Host Execute
```

---

## <img src="https://cdn.jsdelivr.net/gh/lucide-icons/lucide@latest/icons/ruler.svg" width="16" height="16" alt="ruler" /> Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| `cmd/rho/main.go` entry point | Standard Go layout — goreleaser builds with `main: ./cmd/rho` producing `rho` binary |
| `cmd/` is CLI library | Not a binary sub-directory — holds 200+ cobra command files |
| Zero CGO | Pure Go, cross-compilable. Tree-sitter is optional |
| `internal/` is private | Other repos should not import `internal/*` |
| `go.work` | Resolves the ecosystem siblings (`../<repo>`) for local and CI workspace integration |
| `eagle` | Shared cross-repo severity, findings, review, verify, tools, events, and policy contracts — engines import this instead of `rho/internal` |
