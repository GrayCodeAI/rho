package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/GrayCodeAI/rho/internal/features"
)

// requestIDKey is the context key for the request ID.
type requestIDKey struct{}

// RequestIDFromContext extracts the request ID from the context.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}

// generateRequestID generates a random 16-byte hex request ID.
func generateRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// requestIDMiddleware assigns a unique request ID to every incoming request,
// stores it in the context, and adds it to the response as X-Request-ID.
func (s *Server) requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = generateRequestID()
		}
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// loggingMiddleware logs each HTTP request with method, path, remote IP,
// status code, and duration. It wraps the response writer to capture the
// status code.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			duration := time.Since(start)
			reqID := RequestIDFromContext(r.Context())
			slog.Info(
				"http_request",
				"method", r.Method,
				// Redact credential-like segments (e.g. bot tokens embedded in URL
				// paths such as Telegram's /bot<TOKEN>/...) so secrets never land
				// in logs even if an inbound proxy folds an outbound URL into a path.
				"path", redactURLPath(r.URL.Path),
				"remote", clientIP(r),
				"status", ww.status,
				"duration_ms", duration.Milliseconds(),
				"request_id", reqID,
				"user_agent", r.Header.Get("User-Agent"),
			)
			s.metrics.Timer("http.request_duration_ms").Record(duration)
		}()
		next.ServeHTTP(ww, r)
	})
}

// securityHeadersMiddleware adds standard security headers to every response.
func (s *Server) securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Content-Security-Policy: allow nothing inline by default.
		// Daemon serves JSON/JSONP/SSE — no inline scripts or styles needed.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")

		// HSTS: only set when bound to non-loopback or TLS.
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		// Prevent caching of sensitive responses.
		if r.Method == http.MethodPost || r.Method == http.MethodDelete {
			w.Header().Set("Cache-Control", "no-store")
		}

		next.ServeHTTP(w, r)
	})
}

// corsMiddleware adds CORS headers for cross-origin requests.
// When CORSOrigins includes "*", all origins are allowed (useful for
// development). In production, specify exact origins.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.corsOrigins) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}

		matched, wildcard := s.matchCORSOrigin(origin)
		if wildcard {
			// Wildcard: reflect "*" and omit credentials — browsers reject the
			// "*" + Allow-Credentials combination, and reflecting an arbitrary
			// origin with credentials would let any site authenticate against us.
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else if matched {
			// Explicit origin: echo back only the matched origin (never the raw
			// request Origin) and allow credentials.
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}

		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, X-Request-ID")
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.status = code
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// Unwrap preserves optional net/http capabilities (notably
// ResponseController's deadline methods) through the logging middleware.
func (rw *responseWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }

// Flush keeps SSE and other streaming handlers working when the response
// writer is wrapped for logging.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// isCORSSettingAllowed reports whether the given origin is permitted
// by the configured CORS origins.
// matchCORSOrigin reports whether origin is allowed and whether the match came
// from the wildcard entry "*". Callers use the wildcard flag to decide whether
// it is safe to echo credentials (it is not — see corsMiddleware).
func (s *Server) matchCORSOrigin(origin string) (matched bool, wildcard bool) {
	for _, o := range s.corsOrigins {
		if o == "*" {
			return true, true
		}
		if o == origin {
			return true, false
		}
	}
	return false, false
}

// credentialPathSegments are URL path prefixes that commonly embed a secret
// token directly in the path (e.g. Telegram's /bot<TOKEN>/...). When a logged
// path starts with one of these, the remainder of that segment is replaced
// with "<REDACTED>" so the token never appears in logs.
var credentialPathSegments = []string{"/bot"}

// redactURLPath masks credential-like segments in a URL path before logging.
// It only alters paths beginning with a known credential segment; everything
// else is returned unchanged.
func redactURLPath(path string) string {
	for _, seg := range credentialPathSegments {
		if strings.HasPrefix(path, seg) {
			return seg + "<REDACTED>" + strings.TrimPrefix(path, seg)
		}
	}
	return path
}

// installMiddleware wraps the server's mux with the standard middleware
// stack: request IDs → security headers (if enabled) → CORS (if enabled) →
// request logging. This is called after routes() to ensure all routes are
// registered.
func (s *Server) installMiddleware() {
	handler := http.Handler(s.mux)
	handler = s.requestIDMiddleware(handler)
	if feature.Enabled(feature.SecurityHeaders) {
		handler = s.securityHeadersMiddleware(handler)
	}
	// CORS is active when the feature flag is on AND origins are configured.
	if feature.Enabled(feature.CORS) && len(s.corsOrigins) > 0 {
		handler = s.corsMiddleware(handler)
	}
	handler = s.loggingMiddleware(handler)
	s.server.Handler = handler
}
