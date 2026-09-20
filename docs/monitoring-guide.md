# Monitoring Guide

This guide covers how to monitor rho's daemon and CLI for production health,
performance, and security.

## 1. Daemon Health & Readiness

### `GET /v1/health` — Liveness probe

Returns 200 with `{"status":"ok","version":"...","uptime":"...","active_sessions":N}`.

Use this for liveness probes in Kubernetes or systemd:

```bash
curl -sf http://localhost:4590/v1/health
```

### `GET /v1/ready` — Readiness probe

Returns 200 when the daemon is fully ready to serve traffic (session factory
configured, Flux preflight checks pass). Returns 503 with the failed
dependency during startup or when dependencies are unavailable.

```bash
curl -sf http://localhost:4590/v1/ready
```

### `GET /v1/stats` — Usage statistics

Returns aggregated usage statistics (sessions, messages, tool calls, cost)
for the last N days (default 30, `?days=30`).

```bash
curl -H "X-API-Key: $RHO_DAEMON_API_KEY" \
  http://localhost:4590/v1/stats
```

## 2. Metrics Endpoint

### `GET /v1/metrics` — Prometheus format

The daemon exposes metrics in Prometheus text exposition format at
`GET /v1/metrics`. This endpoint requires authentication.

```bash
curl -H "X-API-Key: $RHO_DAEMON_API_KEY" \
  http://localhost:4590/v1/metrics
```

Available metrics:

| Metric | Type | Description |
|--------|------|-------------|
| `rho_daemon_active_sessions` | gauge | Number of active daemon sessions |
| `rho_daemon_chat_concurrency_used` | gauge | Number of in-use chat concurrency slots |
| `rho_daemon_uptime_seconds` | gauge | Daemon uptime in seconds |
| `http_requests_total` | counter | Total HTTP requests received |
| `http_request_duration_ms` | histogram | HTTP request duration in milliseconds |
| `http_rate_limited_total` | counter | Number of requests rejected by rate limiter |
| `auth_denied_total` | counter | Number of denied authentication attempts |
| `tool_exec_total` | counter | Number of tool executions |

### JSON format

Pass `?format=json` to get metrics as a JSON object instead of Prometheus
text format.

## 3. OpenTelemetry Tracing

Telemetry is **opt-in**. Enable it by setting:

```bash
export RHO_ENABLE_TELEMETRY=1
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
rho daemon start
```

The daemon will automatically:

- Initialize the OTel SDK with a batch span processor (5s batch interval).
- Export traces to the OTLP endpoint (`OTEL_EXPORTER_OTLP_ENDPOINT`).
- Send trace headers from `OTEL_EXPORTER_OTLP_HEADERS`.
- Set the service name (default: `rho`) and version.

### Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `RHO_ENABLE_TELEMETRY` | `0` | Set to `1` to enable OTP telemetry |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | _(empty)_ | OTLP collector endpoint |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` | OTLP transport protocol |
| `OTEL_EXPORTER_OTLP_HEADERS` | _(empty)_ | Comma-separated `key=value` headers |
| `RHO_OTEL_SHUTDOWN_TIMEOUT_MS` | `2000` | Shutdown timeout in milliseconds |

### OTel Conventions

rho follows the OpenTelemetry semantic conventions for traces. See
[docs/OTEL-CONVENTIONS.md](OTEL-CONVENTIONS.md) for span naming and attribute
details.

## 4. Structured Logging

The daemon writes structured SLOG logs to `~/.rho/state/daemon.log` by
default. Control the log level via:

```bash
rho daemon start --log-level DEBUG
```

Or via environment variable:

```bash
export OTEL_LOG_LEVEL=DEBUG
```

Log levels: `DEBUG`, `INFO` (default), `WARN`, `ERROR`.

### Log fields

All daemon log entries include:

- `time` — RFC3339 timestamp
- `level` — log level
- `msg` — log message
- Contextual fields (e.g., `method`, `path`, `status`, `request_id`, `remote`)

### Audit log

Security-relevant events (auth failures, tool executions) are written to a
tamper-evident log at `~/.rho/state/securitylog/security_events.jsonl`.

Verify the audit log integrity:

```bash
# The securitylog package provides a verify command
go run ./cmd/rho securitylog verify
```

## 5. Prometheus Scraping

### Kubernetes

```yaml
apiVersion: v1
kind: Service
metadata:
  name: rho-daemon
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "4590"
    prometheus.io/path: "/v1/metrics"
spec:
  selector:
    app: rho-daemon
  ports:
    - port: 4590
      targetPort: 4590
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rho-daemon
spec:
  template:
    spec:
      containers:
        - name: rho
          image: ghcr.io/graycodeai/rho-daemon:latest
          env:
            - name: RHO_DAEMON_API_KEY
              valueFrom:
                secretKeyRef:
                  name: rho-secret
                  key: api-key
          ports:
            - containerPort: 4590
          livenessProbe:
            httpGet:
              path: /v1/health
              port: 4590
            initialDelaySeconds: 10
            periodSeconds: 30
          readinessProbe:
            httpGet:
              path: /v1/ready
              port: 4590
            initialDelaySeconds: 5
            periodSeconds: 10
```

## 6. Alerting Recommendations

| Alert | Condition | Severity |
|-------|-----------|----------|
| Daemon down | `rho_daemon_uptime_seconds` does not increase for 2+ minutes | critical |
| High request latency | `histogram_quantile(0.95, http_request_duration_ms)` > 10000ms for 5 minutes | warning |
| Rate limit saturation | `rate(http_rate_limited_total[5m])` > 10/s | warning |
| Auth failures | `rate(auth_denied_total[5m])` > 5/s | critical (possible brute force) |
| High concurrency | `rho_daemon_chat_concurrency_used` sustained at max for 5+ minutes | warning |
| No active sessions | `rho_daemon_active_sessions` = 0 during business hours | warning |

## 7. Systemd Logging

When running under systemd, logs from stderr/stdout are captured by journald:

```bash
journalctl -u rho-daemon -f
```

The daemon also writes its own structured log to
`~/.rho/state/daemon.log`:

```bash
tail -f ~/.rho/state/daemon.log
```

## 8. Feature Flags

Feature flags allow runtime configuration without restarts. They are
controlled via environment variables:

```bash
export RHO_FEATURE_<FLAG_NAME>=1
```

| Flag | Default | Description |
|------|---------|-------------|
| `telemetry-otel` | `1` | Enable OpenTelemetry SDK |
| `metrics-endpoint` | `1` | Expose GET /v1/metrics |
| `security-headers` | `1` | Apply security headers middleware |
| `cors` | `0` | Enable CORS support |
| `audit-log` | `1` | Enable tamper-evident audit logging |

List all registered flags:

```bash
rho features
```
