# Production Operations Checklist

Use this checklist when deploying or upgrading rho in a production
environment. Each item links to the relevant configuration option or
documentation section.

## Pre-Deployment

- [ ] **API key is set** — `RHO_DAEMON_API_KEY` environment variable is
  configured to a cryptographically random value (≥ 32 bytes). Do **not**
  rely on the auto-generated key for production.
  ```bash
  export RHO_DAEMON_API_KEY=$(openssl rand -base64 32)
  ```
- [ ] **Bind address** — Daemon binds to `0.0.0.0` (not just loopback) only
  when remote access is needed. A non-loopback bind requires an API key and
  native TLS; otherwise startup fails closed.
- [ ] **TLS configured** — Enable native TLS with `--tls-cert` / `--tls-key`
  flags. If a reverse proxy terminates TLS, keep Rho bound to loopback or
  an internal-only interface and restrict that network path at the firewall;
  the daemon does not treat `X-Forwarded-Proto` as transport encryption.
- [ ] **CORS configured** — If serving browser-based clients, set
  `--cors https://app.example.com` to allow cross-origin requests from
  trusted origins only. Use `--cors '*'` only for development.
- [ ] **Rate limits reviewed** — Default: 10 req/min for general API, 30 req/min
  for chat, 4 concurrent chat sessions. Tune via
  `RHO_DAEMON_MAX_CONCURRENT`.
- [ ] **Resource limits set** — Configure CPU/memory limits in systemd
  (`MemoryMax`, `CPUQuota`) or Kubernetes. Defaults in the systemd unit file:
  `MemoryMax=4G`, `CPUQuota=200%`.
- [ ] **Log retention** — Daemon logs at `~/.rho/state/daemon.log`.
  Configure log rotation (logrotate, journald retention) to prevent disk
  exhaustion.
- [ ] **State directory backed up** — The `~/.rho/state/` directory contains
  the PID file, API key pin file, audit log, and session state. Back up the
  audit log key (`securitylog/sel.key`) — **losing it makes all historical
  audit entries unverifiable**.
- [ ] **Firewall rules** — Only expose port 4590 to trusted networks or
  behind a reverse proxy. Do not expose the daemon directly to the internet.

## Observability

- [ ] **Telemetry enabled (optional)** — Set `RHO_ENABLE_TELEMETRY=1`
  and configure `OTEL_EXPORTER_OTLP_ENDPOINT` to send traces to your OTLP
  collector. Telemetry is opt-in by default.
- [ ] **Prometheus scraping** — If using Prometheus, configure a scrape
  target for `http://<daemon>:4590/v1/metrics` with authentication:
  ```yaml
  scrape_configs:
    - job_name: 'rho-daemon'
      bearer_token: '<RHO_DAEMON_API_KEY>'
      static_configs:
        - targets: ['rho-daemon:4590']
      metrics_path: '/v1/metrics'
  ```
- [ ] **Health/readiness probes** — Configure in your orchestrator:
  - Liveness: `GET /v1/health` (no auth required)
  - Readiness: `GET /v1/ready` (no auth required)
- [ ] **Alerting rules** — Import the alerting recommendations from
  [docs/monitoring-guide.md](monitoring-guide.md) §6.

## Security Hardening

- [ ] **API key rotated** — The API key is written to a pinned file at
  `~/.rho/state/daemon.key` for convenience. **Remove this file in
  production** or ensure it has `0600` permissions and is not world-readable.
- [ ] **Audit log verification** — Periodically verify the audit log
  integrity:
  ```bash
  # The Verify function checks the HMAC chain
  go run ./cmd/rho securitylog verify
  ```
- [ ] **Security headers** — Verify `X-Content-Type-Options`,
  `X-Frame-Options`, and `Content-Security-Policy` headers are present
  (enabled by default via the `security-headers` feature flag).
- [x] **Execution model** — Rho runs tools directly on the host under its
  permission, path-safety, trust, and worktree controls. Docker/Podman is not
  required and is not an execution backend for the CLI.
- [ ] **No CGO** — The binary is built with `CGO_ENABLED=0` for a static binary.
  Verify the deployed binary has no dynamic library dependencies:
  ```bash
  ldd /usr/local/bin/rho  # should say "not a dynamic executable"
  ```

## Post-Deployment

- [ ] **Smoke test** — Verify the daemon responds:
  ```bash
  curl http://localhost:4590/v1/health
  curl http://localhost:4590/v1/ready
  ```
- [ ] **Auth test** — Verify protected endpoints reject unauthenticated
  requests:
  ```bash
  curl -o /dev/null -w "%{http_code}" http://localhost:4590/v1/stats
  # Should be 401
  ```
- [ ] **Metrics test** — Verify the metrics endpoint:
  ```bash
  curl -H "X-API-Key: $RHO_DAEMON_API_KEY" http://localhost:4590/v1/metrics
  ```
- [ ] **Version check** — Verify the deployed version matches expectations:
  ```bash
  curl http://localhost:4590/v1/health | jq .version
  ```

## Upgrade Procedure

1. **Back up state** — Copy `~/.rho/state/` to a safe location.
2. **Drain traffic** — Remove the daemon from load balancer rotation or
   stop sending new requests.
3. **Install new binary** — Replace the binary and run `rho daemon start`
   with the same configuration.
4. **Verify health** — Check `GET /v1/health` and `GET /v1/ready`.
5. **Verify metrics** — Check `GET /v1/metrics` for expected counters.
6. **Audit log** — Verify the audit log continues from the previous
   sequence (no gaps in the HMAC chain).
7. **Resume traffic** — Re-enable the daemon in your load balancer.
