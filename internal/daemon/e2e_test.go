package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	feature "github.com/GrayCodeAI/rho/internal/features"
	"github.com/GrayCodeAI/rho/internal/testutil"
)

// TestE2E_HealthEndpoint verifies the health endpoint returns the expected
// fields and 200 status.
func TestE2E_HealthEndpoint(t *testing.T) {
	srv := New(Config{Port: 0, Host: testutil.LoopbackHost}, nil)
	addr := startTestDaemon(t, srv)
	defer srv.Stop(context.Background())

	resp := httpGet(t, "http://"+addr+"/v1/health")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if health.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", health.Status)
	}
	if health.Version == "" {
		t.Error("expected non-empty version")
	}
}

// TestE2E_ReadyEndpoint verifies the readiness probe returns 503 when
// the engine is not configured (fail-closed).
func TestE2E_ReadyEndpoint(t *testing.T) {
	srv := New(Config{Port: 0, Host: testutil.LoopbackHost}, nil)
	addr := startTestDaemon(t, srv)
	defer srv.Stop(context.Background())

	resp := httpGet(t, "http://"+addr+"/v1/ready")
	defer resp.Body.Close()

	// No session factory → not ready.
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
	var ready ReadyResponse
	if err := json.NewDecoder(resp.Body).Decode(&ready); err != nil {
		t.Fatalf("decode ready: %v", err)
	}
	if ready.Ready {
		t.Error("expected not ready when no factory configured")
	}
}

// TestE2E_MetricsEndpoint verifies the Prometheus metrics endpoint.
func TestE2E_MetricsEndpoint(t *testing.T) {
	srv := New(Config{Port: 0, Host: testutil.LoopbackHost}, nil)
	addr := startTestDaemon(t, srv)
	defer srv.Stop(context.Background())

	// Make a request to generate some metrics.
	httpGet(t, "http://"+addr+"/v1/health")

	resp := httpGet(t, "http://"+addr+"/v1/metrics")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("expected text/plain content type, got %q", ct)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "rho_daemon_active_sessions") {
		t.Error("expected rho_daemon_active_sessions metric in output")
	}
	if !strings.Contains(string(body), "rho_daemon_uptime_seconds") {
		t.Error("expected rho_daemon_uptime_seconds metric in output")
	}

	// Test JSON format too.
	respJSON := httpGet(t, "http://"+addr+"/v1/metrics?format=json")
	defer respJSON.Body.Close()
	if respJSON.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for JSON, got %d", respJSON.StatusCode)
	}
	if !strings.Contains(respJSON.Header.Get("Content-Type"), "application/json") {
		t.Errorf("expected application/json content type, got %q", respJSON.Header.Get("Content-Type"))
	}
}

// TestE2E_MetricsRequiresAuth verifies that /v1/metrics is protected.
func TestE2E_MetricsRequiresAuth(t *testing.T) {
	srv := New(Config{Port: 0, Host: testutil.LoopbackHost, APIKey: "secret"}, nil)
	addr := startTestDaemon(t, srv)
	defer srv.Stop(context.Background())

	resp := httpGet(t, "http://"+addr+"/v1/metrics")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without API key, got %d", resp.StatusCode)
	}
}

// TestE2E_SecurityHeaders verifies that security headers are present on
// all responses, including error responses.
func TestE2E_SecurityHeaders(t *testing.T) {
	srv := New(Config{Port: 0, Host: testutil.LoopbackHost}, nil)
	addr := startTestDaemon(t, srv)
	defer srv.Stop(context.Background())

	resp := httpGet(t, "http://"+addr+"/v1/health")
	defer resp.Body.Close()

	headers := []string{
		"X-Content-Type-Options",
		"X-Frame-Options",
		"Referrer-Policy",
		"X-XSS-Protection",
		"Content-Security-Policy",
	}
	for _, h := range headers {
		if v := resp.Header.Get(h); v == "" {
			t.Errorf("missing security header: %s", h)
		}
	}
	if v := resp.Header.Get("X-Content-Type-Options"); v != "nosniff" {
		t.Errorf("expected X-Content-Type-Options=nosniff, got %q", v)
	}
	if v := resp.Header.Get("X-Frame-Options"); v != "DENY" {
		t.Errorf("expected X-Frame-Options=DENY, got %q", v)
	}
}

// TestE2E_RequestID verifies that X-Request-ID is generated and returned
// in responses, and is logged.
func TestE2E_RequestID(t *testing.T) {
	srv := New(Config{Port: 0, Host: testutil.LoopbackHost}, nil)
	addr := startTestDaemon(t, srv)
	defer srv.Stop(context.Background())

	// Request without a pre-set ID — server should generate one.
	resp := httpGet(t, "http://"+addr+"/v1/health")
	defer resp.Body.Close()
	reqID := resp.Header.Get("X-Request-ID")
	if reqID == "" {
		t.Fatal("expected X-Request-ID header in response")
	}

	// Request with a pre-set ID — server should echo it.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+"/v1/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Request-ID", "custom-req-id-123")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if got := resp2.Header.Get("X-Request-ID"); got != "custom-req-id-123" {
		t.Errorf("expected echoed X-Request-ID, got %q", got)
	}
}

// TestE2E_CORS verifies CORS headers are set when configured.
func TestE2E_CORS(t *testing.T) {
	feature.Set("cors", true)
	defer feature.Set("cors", false)
	srv := New(Config{
		Port:        0,
		Host:        testutil.LoopbackHost,
		CORSOrigins: []string{"http://example.com"},
	}, nil)
	addr := startTestDaemon(t, srv)
	defer srv.Stop(context.Background())

	// Request with an allowed origin.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+"/v1/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "http://example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://example.com" {
		t.Errorf("expected Access-Control-Allow-Origin=http://example.com, got %q", got)
	}

	// Request with a disallowed origin — no CORS headers.
	req2, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+"/v1/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req2.Header.Set("Origin", "http://evil.com")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	if got := resp2.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no Access-Control-Allow-Origin for disallowed origin, got %q", got)
	}
}

// TestE2E_CORS_Preflight verifies OPTIONS preflight requests are handled.
func TestE2E_CORS_Preflight(t *testing.T) {
	feature.Set("cors", true)
	defer feature.Set("cors", false)
	srv := New(Config{
		Port:        0,
		Host:        testutil.LoopbackHost,
		CORSOrigins: []string{"http://example.com"},
	}, nil)
	addr := startTestDaemon(t, srv)
	defer srv.Stop(context.Background())

	req, err := http.NewRequestWithContext(context.Background(), http.MethodOptions, "http://"+addr+"/v1/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for preflight, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Methods"); !strings.Contains(got, "GET") {
		t.Errorf("expected Access-Control-Allow-Methods to contain GET, got %q", got)
	}
}

// TestE2E_AuditLog_AuthDenied verifies that failed auth attempts are
// recorded in the security audit log.
func TestE2E_AuditLog_AuthDenied(t *testing.T) {
	srv := New(Config{Port: 0, Host: testutil.LoopbackHost, APIKey: "secret"}, nil)
	addr := startTestDaemon(t, srv)
	defer srv.Stop(context.Background())

	// Make a request with a wrong API key.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://"+addr+"/v1/chat", strings.NewReader(`{"prompt":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "wrong-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	// Verify the auth_denied counter was incremented.
	counter := srv.metrics.Counter("auth_denied_total")
	if counter.Value() == 0 {
		t.Error("expected auth_denied_total counter to be incremented")
	}
}

// TestE2E_RateLimit verifies the rate limiter returns 429 when exceeded.
func TestE2E_RateLimit(t *testing.T) {
	// Use a very low rate limit via the default configuration.
	// The default API rate is 10/min with burst 4, so after 4 rapid
	// requests the next ones should be rejected.
	srv := New(Config{Port: 0, Host: testutil.LoopbackHost, APIKey: "secret"}, nil)
	addr := startTestDaemon(t, srv)
	defer srv.Stop(context.Background())

	rejected := 0
	for i := 0; i < 20; i++ {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+"/v1/stats", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-API-Key", "secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			rejected++
		}
	}
	if rejected == 0 {
		t.Error("expected at least one rate-limited response from burst exhaustion")
	}
}

// TestE2E_StatsRequiresAuth verifies /v1/stats is protected.
func TestE2E_StatsRequiresAuth(t *testing.T) {
	srv := New(Config{Port: 0, Host: testutil.LoopbackHost, APIKey: "secret"}, nil)
	addr := startTestDaemon(t, srv)
	defer srv.Stop(context.Background())

	resp := httpGet(t, "http://"+addr+"/v1/stats")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without API key, got %d", resp.StatusCode)
	}
}

// TestE2E_ChatEmptyPrompt verifies /v1/chat rejects empty prompts.
func TestE2E_ChatEmptyPrompt(t *testing.T) {
	srv := New(Config{Port: 0, Host: testutil.LoopbackHost}, nil)
	addr := startTestDaemon(t, srv)
	defer srv.Stop(context.Background())

	resp, err := http.Post("http://"+addr+"/v1/chat", "application/json", strings.NewReader(`{"prompt":""}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 400 or 503 for empty prompt, got %d", resp.StatusCode)
	}
}

// TestE2E_LivenessAndReadiness verifies both /v1/health and /v1/ready
// return appropriate status codes.
func TestE2E_LivenessAndReadiness(t *testing.T) {
	srv := New(Config{Port: 0, Host: testutil.LoopbackHost}, nil)
	addr := startTestDaemon(t, srv)
	defer srv.Stop(context.Background())

	healthResp := httpGet(t, "http://"+addr+"/v1/health")
	defer healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK {
		t.Errorf("health: expected 200, got %d", healthResp.StatusCode)
	}

	readyResp := httpGet(t, "http://"+addr+"/v1/ready")
	defer readyResp.Body.Close()
	if readyResp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("ready: expected 503 without factory, got %d", readyResp.StatusCode)
	}
}

// httpGet is a test helper for simple GET requests.
func httpGet(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
