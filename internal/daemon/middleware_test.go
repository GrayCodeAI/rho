package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	feature "github.com/GrayCodeAI/rho/internal/features"
	"github.com/GrayCodeAI/rho/internal/testutil"
)

// --- Pure helper unit tests ---

func TestClientIP(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		want   string
	}{
		{"ipv4 with port", "127.0.0.1:4590", "127.0.0.1"},
		{"ipv6 with port", "[::1]:4590", "::1"},
		{"no port", "10.0.0.1", "10.0.0.1"},
		{"empty remote", "", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
			req.RemoteAddr = tt.remote
			if got := clientIP(req); got != tt.want {
				t.Errorf("clientIP(%q) = %q, want %q", tt.remote, got, tt.want)
			}
		})
	}
}

func TestStripSlackMention(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"<@U12345> hello there", "hello there"},
		{"<@U12345>", ""},
		{"no mention here", "no mention here"},
		{"  <@U1>  spaced  ", "spaced"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := stripSlackMention(tt.in); got != tt.want {
			t.Errorf("stripSlackMention(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGenerateRequestID(t *testing.T) {
	// IDs are 32 hex chars (16 random bytes), and two consecutive calls differ.
	first := generateRequestID()
	second := generateRequestID()
	if len(first) != 32 {
		t.Errorf("generateRequestID() length = %d, want 32", len(first))
	}
	for _, r := range first {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			t.Fatalf("generateRequestID() = %q, want hex only", first)
		}
	}
	if first == second {
		t.Error("generateRequestID() returned identical values")
	}
}

func TestMatchCORSOrigin(t *testing.T) {
	s := &Server{corsOrigins: []string{"http://example.com", "http://other.com"}}
	for _, origin := range []string{"http://example.com", "http://other.com"} {
		matched, wildcard := s.matchCORSOrigin(origin)
		if !matched {
			t.Errorf("matchCORSOrigin(%q) = false, want true", origin)
		}
		if wildcard {
			t.Errorf("matchCORSOrigin(%q) = wildcard true, want false", origin)
		}
	}
	for _, origin := range []string{"http://evil.com", ""} {
		if matched, _ := s.matchCORSOrigin(origin); matched {
			t.Errorf("matchCORSOrigin(%q) = true, want false", origin)
		}
	}

	wildcard := &Server{corsOrigins: []string{"*"}}
	matched, isWildcard := wildcard.matchCORSOrigin("http://anything.com")
	if !matched {
		t.Error("wildcard origin should allow any origin")
	}
	if !isWildcard {
		t.Error("wildcard origin should report wildcard=true")
	}
}

// --- Middleware stack behavior ---

func TestRequestIDMiddleware_SetsAndPropagates(t *testing.T) {
	var gotID string
	handler := (&Server{}).requestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	// No incoming ID: middleware generates one and it appears in the response
	// header and the context.
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if gotID == "" {
		t.Fatal("request ID not propagated to context")
	}
	if rec.Header().Get("X-Request-ID") != gotID {
		t.Errorf("response header X-Request-ID = %q, context ID = %q", rec.Header().Get("X-Request-ID"), gotID)
	}

	// Incoming ID is preserved rather than replaced.
	req2 := httptest.NewRequest(http.MethodGet, "/x", nil)
	req2.Header.Set("X-Request-ID", "client-supplied-id")
	handler.ServeHTTP(httptest.NewRecorder(), req2)
	if gotID != "client-supplied-id" {
		t.Errorf("context ID = %q, want preserved client-supplied-id", gotID)
	}
}

func TestRequestIDFromContext_Empty(t *testing.T) {
	if got := RequestIDFromContext(context.Background()); got != "" {
		t.Errorf("RequestIDFromContext(empty ctx) = %q, want empty", got)
	}
}

func TestSecurityHeadersMiddleware_SetsHeaders(t *testing.T) {
	handler := (&Server{}).securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	for _, h := range []string{"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "X-XSS-Protection", "Content-Security-Policy"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("security header %q not set", h)
		}
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", rec.Header().Get("X-Content-Type-Options"))
	}
	// POST requests must not be cached.
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store for POST", rec.Header().Get("Cache-Control"))
	}
}

func TestSecurityHeadersMiddleware_HSTSOnlyOverTLS(t *testing.T) {
	handler := (&Server{}).securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Plain HTTP: no HSTS.
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Errorf("HSTS set on plain HTTP: %q", rec.Header().Get("Strict-Transport-Security"))
	}

	// X-Forwarded-Proto: https: HSTS set.
	req2 := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req2.Header.Set("X-Forwarded-Proto", "https")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Header().Get("Strict-Transport-Security") == "" {
		t.Error("HSTS not set when X-Forwarded-Proto is https")
	}
}

func TestCORSMiddleware_Preflight(t *testing.T) {
	feature.Set("cors", true)
	defer feature.Set("cors", false)
	s := &Server{corsOrigins: []string{"http://example.com"}}

	handler := s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/v1/chat", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("preflight status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://example.com" {
		t.Errorf("preflight Allow-Origin = %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("preflight Allow-Methods not set")
	}
}

func TestCORSMiddleware_DisallowedOriginGetsNoHeaders(t *testing.T) {
	feature.Set("cors", true)
	defer feature.Set("cors", false)
	s := &Server{corsOrigins: []string{"http://example.com"}}

	handler := s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("Origin", "http://evil.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("disallowed origin got Allow-Origin %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestResponseWriter_CapturesStatus(t *testing.T) {
	rw := &responseWriter{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}
	rw.WriteHeader(http.StatusTeapot)
	if rw.status != http.StatusTeapot {
		t.Errorf("responseWriter.status = %d, want 418", rw.status)
	}
}

// TestFullMiddlewareStack_RequestIDAndHeaders exercises the installed stack
// (request ID + security headers + logging) through an in-process server.
func TestFullMiddlewareStack_RequestIDAndHeaders(t *testing.T) {
	srv := New(Config{Port: 0, Host: testutil.LoopbackHost}, nil)
	addr := startTestDaemon(t, srv)
	defer srv.Stop(context.Background())

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+"/v1/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("X-Request-ID") == "" {
		t.Error("full stack did not set X-Request-ID")
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("full stack X-Content-Type-Options = %q, want nosniff", resp.Header.Get("X-Content-Type-Options"))
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		t.Errorf("full stack Content-Type = %q", resp.Header.Get("Content-Type"))
	}
}
