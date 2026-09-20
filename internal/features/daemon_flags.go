package feature

// Daemon-specific feature flags. These are registered once at package init
// time and used throughout the daemon codebase to gate experimental or
// operational capabilities.
//
// Override at runtime via environment variables:
//
//	RHO_FEATURE_TELEMETRY_OTEL=0     — disable OTel SDK (use in-memory tracer only)
//	RHO_FEATURE_METRICS_ENDPOINT=0   — disable the /v1/metrics endpoint
//	RHO_FEATURE_SECURITY_HEADERS=1   — enable security headers middleware
//	RHO_FEATURE_CORS=0               — enable CORS support on daemon API
//	RHO_FEATURE_AUDIT_LOG=1          — enable tamper-evident audit logging

var (
	// TelemetryOTel controls whether the full OpenTelemetry SDK is
	// initialized (with OTLP export). Enabled by default since the SDK is
	// always compiled in; set RHO_ENABLE_TELEMETRY=1 to actually
	// activate the OTLP exporter. This flag gates whether the SDK code path
	// is exercised at all.
	TelemetryOTel = Register("telemetry-otel", true,
		"Enable OpenTelemetry SDK for distributed tracing and metrics")

	// MetricsEndpoint controls whether the GET /v1/metrics endpoint
	// is registered on the daemon. Enabled by default.
	MetricsEndpoint = Register("metrics-endpoint", true,
		"Expose GET /v1/metrics Prometheus endpoint on the daemon")

	// SecurityHeaders controls whether the security headers middleware
	// (X-Content-Type-Options, X-Frame-Options, CSP, HSTS, Referrer-Policy)
	// is applied to all daemon responses. Enabled by default.
	SecurityHeaders = Register("security-headers", true,
		"Apply security headers (CSP, HSTS, X-Frame-Options, etc.) to all responses")

	// CORS controls whether CORS support is enabled on the daemon API.
	// Disabled by default for security; enable only when serving
	// browser-based clients.
	CORS = Register("cors", false,
		"Enable CORS support on the daemon API")

	// AuditLog controls whether the tamper-evident security event log
	// is initialized and used for auth-denied and tool-execution events.
	// Enabled by default.
	AuditLog = Register("audit-log", true,
		"Enable tamper-evident security event logging")
)
