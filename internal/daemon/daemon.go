package daemon

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine/safety"

	"github.com/GrayCodeAI/rho/internal/engine"
	"github.com/GrayCodeAI/rho/internal/netutil"
	"github.com/GrayCodeAI/rho/internal/observability/metrics"
	"github.com/GrayCodeAI/rho/internal/securitylog"
	rhosession "github.com/GrayCodeAI/rho/internal/session"
	"github.com/GrayCodeAI/rho/internal/storage"
)

const maxRequestBodyBytes = 1 << 20

// sseWriteTimeout is the per-frame write deadline applied to SSE responses.
// It is far shorter than the server's absolute WriteTimeout so a client that
// stops reading releases the handler (and its concurrency slot) quickly,
// while still being generous enough for slow-but-alive clients of agentic
// streams. See writeSSE.
const sseWriteTimeout = 90 * time.Second

// Defaults for the daemon's global request throttling (H9). The chat limit is
// deliberately stricter than the general API limit because each generation is
// long-running and expensive. Both are per-IP token buckets.
const (
	defaultMaxConcurrentChat = 4
	defaultChatRatePerMin    = 30.0 // tokens per minute for /v1/chat
	defaultChatBurst         = 6
	defaultAPIRatePerMin     = 10.0 // tokens per minute for other authed routes
	defaultAPIBurst          = 4
)

// SessionFactory creates a configured engine session for a given request.
// The caller (cmd package) provides this, wiring system prompts, tools, keys.
type SessionFactory func(req ChatRequest) (*engine.Session, error)

// InvalidChatRequestError lets a SessionFactory reject a request without
// exposing internal construction errors as part of the public API. Factories
// should use it for user-controlled values such as an unknown agent persona.
type InvalidChatRequestError struct {
	Message string
	Err     error
}

func (e *InvalidChatRequestError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Message
}

func (e *InvalidChatRequestError) Unwrap() error { return e.Err }

// version is set via SetVersion from main.go at startup.
var version = "0.0.0"

// SetVersion propagates the canonical rho version into the daemon.
func SetVersion(v string) { version = v }

// Server is the rho daemon HTTP server for programmatic/CI access.
type Server struct {
	addr      string
	mux       *http.ServeMux
	server    *http.Server
	startedAt time.Time
	sessions  sync.Map // sessionID -> *Session
	// A fixed stripe set serializes continuation and deletion for the same
	// durable ID without allowing arbitrary client-supplied IDs to grow a lock
	// map for the lifetime of the daemon.
	sessionLocks [64]sync.Mutex
	newSession   SessionFactory
	apiKey       string
	gateways     *gatewayManager

	// readyFn reports whether Flux's preflight/readiness dependencies are
	// initialized and the server can serve real traffic. It is consulted by
	// GET /v1/ready. A nil probe fails closed: a session factory alone does not
	// prove that Flux's catalog, credentials, and model selection are ready.
	readyMu sync.RWMutex
	readyFn func() (bool, string)

	// graphFactory projects a persisted Rho session into the portable,
	// privacy-safe execution graph. The composition root supplies the builder;
	// the daemon owns only HTTP validation, authentication, and encoding.
	graphMu      sync.RWMutex
	graphFactory GraphFactory

	// graphLedger retains accepted POST /v1/graph/sync payloads for
	// idempotency and durable retention. Defaults to an in-memory ledger;
	// the composition root injects a SQLite-backed ledger for persistence
	// across daemon restarts.
	graphLedger GraphLedger

	// routePatterns records every "METHOD /path" pattern registered on the
	// mux so tests can verify the HTTP surface matches api/openapi.yaml.
	routePatterns []string

	// concurrencySem bounds the number of in-flight /v1/chat generations
	// server-wide. Per-session stripe locks already serialize the *same*
	// session; this caps total load across sessions (H9).
	concurrencySem chan struct{}
	// metrics registry tracks daemon-level request and resource metrics.
	metrics *metrics.Registry
	// cancelMu guards cancels, the sessionID -> cancel mapping used by
	// POST /v1/cancel to abort an in-flight generation (H10).
	cancelMu sync.Mutex
	cancels  map[string]*cancelEntry
	// reviewWG tracks in-flight `rho review run` subprocesses spawned by
	// POST /v1/review so Stop can wait for them.
	reviewWG sync.WaitGroup
	// General per-IP token bucket for non-chat API routes.
	apiLimiter *ipLimiter
	// Per-IP token bucket for /v1/chat generations (heavier, so lower rate).
	chatLimiter *ipLimiter
	// corsOrigins is the list of allowed CORS origins. Empty disables CORS.
	corsOrigins []string
	// securityLog is the tamper-evident audit log for security events.
	securityLog *securitylog.Log
	// tlsCertFile and tlsKeyFile enable HTTPS when both are set.
	tlsCertFile string
	tlsKeyFile  string
	// maxAutonomy caps the autonomy tier clients may request (server-side
	// policy; zero means DefaultMaxAutonomy). The daemon has no human to
	// approve permission prompts, so full/YOLO must be operator-opted-in.
	maxAutonomy safety.AutonomyLevel
}

// ReadyResponse is the JSON response from GET /v1/ready.
type ReadyResponse struct {
	// Ready is true only when every dependency is initialized.
	Ready bool `json:"ready"`
	// Reason describes why the daemon is not ready (empty when ready).
	Reason string `json:"reason,omitempty"`
	// Uptime is the time since the server started.
	Uptime string `json:"uptime"`
}

// Config holds daemon configuration.
type Config struct {
	Port    int    `json:"port"`
	Host    string `json:"host"`
	LogFile string `json:"log_file"`
	APIKey  string `json:"api_key,omitempty"`
	// Gateways configures optional messaging bridges (Telegram/Discord/Slack).
	// All are disabled by default; the daemon starts normally when none are set.
	Gateways GatewaysConfig `json:"gateways,omitempty"`
	// CORSOrigins configures allowed CORS origins for the daemon API.
	// Empty (default) disables CORS. Use ["*"] to allow all origins (dev only).
	CORSOrigins []string `json:"cors_origins,omitempty"`
	// TLSCertFile and TLSKeyFile enable HTTPS for the daemon. When both
	// are set, the server listens with TLS. Empty (default) uses plain HTTP.
	TLSCertFile string `json:"tls_cert_file,omitempty"`
	TLSKeyFile  string `json:"tls_key_file,omitempty"`
	// SecurityLog provides the audit log instance for tool execution
	// auditing. If nil, the server creates one from DefaultDir().
	SecurityLog *securitylog.Log `json:"-"`
	// MaxAutonomy caps the autonomy tier a client may request via
	// POST /v1/chat. The daemon is non-interactive: it has no human to
	// approve permission prompts, so remote callers must not be able to
	// escalate to full/YOLO autonomy on their own. Zero means the default
	// cap (AutonomySemi) applies; set explicitly (e.g. to AutonomyFull)
	// only for trusted, operator-owned deployments.
	MaxAutonomy safety.AutonomyLevel `json:"-"`
	// GraphLedger durably retains accepted POST /v1/graph/sync payloads for
	// idempotency across daemon restarts. If nil, the server uses an
	// in-memory ledger (survives only the process lifetime).
	GraphLedger GraphLedger `json:"-"`
}

// DefaultMaxAutonomy is the highest autonomy tier a daemon client may
// request when the operator has not configured a higher cap. Semi
// auto-approves reads and writes but still gates Bash behind permission,
// which is the most permissive setting that remains safe without a human
// in the loop.
const DefaultMaxAutonomy = safety.AutonomySemi

// DefaultConfig returns reasonable defaults.
func DefaultConfig() Config {
	return Config{
		Port: 4590,
		Host: netutil.LoopbackHost,
	}
}

// ChatRequest is the JSON body for POST /v1/chat.
type ChatRequest struct {
	Prompt    string `json:"prompt"`
	SessionID string `json:"session_id,omitempty"`
	Model     string `json:"model,omitempty"`
	MaxTurns  int    `json:"max_turns,omitempty"`
	Autonomy  string `json:"autonomy,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	Agent     string `json:"agent,omitempty"`
}

// ChatResponse is the JSON response from POST /v1/chat.
type ChatResponse struct {
	SessionID  string `json:"session_id"`
	Response   string `json:"response"`
	TokensIn   int    `json:"tokens_in"`
	TokensOut  int    `json:"tokens_out"`
	TurnsTaken int    `json:"turns_taken"`
	Duration   string `json:"duration"`
}

// Session tracks an active daemon session.
type Session struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	LastUsed  time.Time `json:"last_used"`
	Turns     int       `json:"turns"`
	CWD       string    `json:"cwd"`
	Agent     string    `json:"agent,omitempty"`
}

// HealthResponse is the JSON response from GET /v1/health.
type HealthResponse struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	Uptime    string `json:"uptime"`
	Sessions  int    `json:"active_sessions"`
	StartedAt string `json:"started_at"`
}

// New creates a new daemon server. If factory is nil, chat endpoint returns an error.
func New(cfg Config, factory SessionFactory) *Server {
	if cfg.Port < 0 {
		cfg.Port = DefaultConfig().Port
	}
	if cfg.Host == "" {
		cfg.Host = DefaultConfig().Host
	}

	s := &Server{
		addr:           fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		mux:            http.NewServeMux(),
		startedAt:      time.Now(),
		newSession:     factory,
		apiKey:         cfg.APIKey,
		concurrencySem: make(chan struct{}, maxConcurrentFromEnv()),
		cancels:        make(map[string]*cancelEntry),
		apiLimiter:     newIPLimiter(defaultAPIRatePerMin/60, defaultAPIBurst),
		chatLimiter:    newIPLimiter(defaultChatRatePerMin/60, defaultChatBurst),
		metrics:        metrics.NewRegistry(),
		graphLedger:    cfg.GraphLedger,
	}
	if s.graphLedger == nil {
		s.graphLedger = newMemoryGraphLedger()
	}
	s.routes()
	// Build the messaging-bridge manager. The daemon URL is finalised in Start
	// (port 0 resolves to a real port there); we pass the configured addr now so
	// Slack can register its webhook route on the mux, and patch the forward URL
	// for poll-based gateways at Start time.
	s.gateways = newGatewayManager(cfg.Gateways, "http://"+s.addr, cfg.APIKey, s)
	s.corsOrigins = cfg.CORSOrigins
	s.tlsCertFile = cfg.TLSCertFile
	s.tlsKeyFile = cfg.TLSKeyFile
	s.maxAutonomy = cfg.MaxAutonomy

	// Initialize the tamper-evident security event log. If a log is provided
	// in the config, use it; otherwise create one from the default directory.
	if cfg.SecurityLog != nil {
		s.securityLog = cfg.SecurityLog
	} else if secLog, err := securitylog.New(securitylog.DefaultDir()); err != nil {
		slog.Warn("failed to initialize security audit log", "error", err)
	} else {
		s.securityLog = secLog
	}

	s.server = &http.Server{
		Addr:              s.addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      300 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}
	// Install middleware stack: request IDs → security headers → CORS → logging.
	s.installMiddleware()
	return s
}

// Start begins serving in the background. Returns the listening address.
func (s *Server) Start() (string, error) {
	if err := s.validateAuthConfig(); err != nil {
		return "", err
	}
	s.warnInsecureAuthConfig()

	ln, err := new(net.ListenConfig).Listen(context.Background(), "tcp", s.addr)
	if err != nil {
		return "", fmt.Errorf("daemon listen: %w", err)
	}
	actualAddr := ln.Addr().String()
	s.addr = actualAddr

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("daemon server goroutine panicked", "recover", r)
			}
		}()
		var serveErr error
		if s.tlsCertFile != "" && s.tlsKeyFile != "" {
			serveErr = s.server.ServeTLS(ln, s.tlsCertFile, s.tlsKeyFile)
		} else {
			serveErr = s.server.Serve(ln)
		}
		if serveErr != nil && serveErr != http.ErrServerClosed {
			slog.Error("daemon server error", "error", serveErr)
		}
	}()

	if err := s.writePIDFile(); err != nil {
		slog.Warn("failed to write PID file", "error", err)
	}

	// Point poll-based gateways at the now-resolved daemon URL and launch them.
	if s.gateways != nil {
		s.gateways.setDaemonURL("http://" + actualAddr)
		s.gateways.Start(context.Background())
	}

	slog.Info("rho daemon started", "addr", actualAddr)
	return actualAddr, nil
}

// validateAuthConfig refuses to start the daemon with no API key on a
// non-loopback bind. The auth middleware (see auth) silently allows every
// request when apiKey == "", so a misconfigured production daemon would
// be wide open. The only safe no-key mode is loopback bind.
func (s *Server) validateAuthConfig() error {
	if s.apiKey == "" {
		host, _, err := net.SplitHostPort(s.addr)
		if err != nil {
			return fmt.Errorf("daemon: invalid bind address %q: %w", s.addr, err)
		}
		if !isLoopbackHost(host) {
			return fmt.Errorf("daemon: apiKey is empty and bind address %q is not loopback; refusing to start. Set Config.APIKey or bind to %s", s.addr, netutil.LoopbackHost)
		}
	}
	// A non-loopback bind exposes the API key and full conversation
	// history on the wire. Refuse to serve plaintext in that case:
	// remote callers must use TLS.
	host, _, err := net.SplitHostPort(s.addr)
	if err != nil {
		return fmt.Errorf("daemon: invalid bind address %q: %w", s.addr, err)
	}
	if !isLoopbackHost(host) && (s.tlsCertFile == "" || s.tlsKeyFile == "") {
		return fmt.Errorf("daemon: bind address %q is not loopback but TLS is not configured; refusing to start. Configure TLSCertFile/TLSKeyFile or bind to %s", s.addr, netutil.LoopbackHost)
	}
	return nil
}

// warnInsecureAuthConfig logs a WARN line when the daemon is started
// without an API key, even on a loopback bind. The user may not have
// intended to run an unauthenticated daemon.
func (s *Server) warnInsecureAuthConfig() {
	if s.apiKey != "" {
		return
	}
	slog.Warn(
		"rho daemon started without API key authentication; only loopback access allowed",
		"addr", s.addr,
		"hint", "Set Config.APIKey to enable authentication, or keep the default loopback bind.",
	)
}

// isLoopbackHost reports whether host is a loopback address: an IP in
// 127.0.0.0/8 or ::1, the literal "localhost", or an empty string
// (which SplitHostPort returns when the address has no host part —
// treated as non-loopback to fail safe).
func isLoopbackHost(host string) bool {
	if host == "" || host == "localhost" {
		return host == "localhost" // "" is unsafe; "localhost" is loopback
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// SecurityLog returns the daemon's security audit log, or nil if unset.
func (s *Server) SecurityLog() *securitylog.Log {
	return s.securityLog
}

func (s *Server) Stop(ctx context.Context) error {
	if s.gateways != nil {
		s.gateways.Stop()
	}
	if s.securityLog != nil {
		_ = s.securityLog.Close()
	}
	// Wait for in-flight review subprocesses, bounded by the caller's context.
	reviewsDone := make(chan struct{})
	go func() {
		s.reviewWG.Wait()
		close(reviewsDone)
	}()
	select {
	case <-reviewsDone:
	case <-ctx.Done():
	}
	_ = s.removePIDFile()
	return s.server.Shutdown(ctx)
}

// Addr returns the listening address.
func (s *Server) Addr() string {
	return s.addr
}

// SetReadyFn installs a custom readiness probe. The probe should return
// (true, "") when all dependencies (session store, provider connectivity) are
// initialized, or (false, reason) otherwise. Passing nil restores the default
// probe. This is additive and safe to call concurrently with serving.
func (s *Server) SetReadyFn(fn func() (bool, string)) {
	s.readyMu.Lock()
	s.readyFn = fn
	s.readyMu.Unlock()
}

// SetGraphFactory installs the read-only execution-graph projection used by
// GET /v1/sessions/{id}/graph. Passing nil makes the endpoint unavailable.
func (s *Server) SetGraphFactory(factory GraphFactory) {
	s.graphMu.Lock()
	s.graphFactory = factory
	s.graphMu.Unlock()
}

// ready evaluates readiness using the installed Flux probe. It fails closed
// when the engine is absent or the composition root did not install a probe;
// merely having a factory does not prove provider readiness.
func (s *Server) ready() (bool, string) {
	s.readyMu.RLock()
	fn := s.readyFn
	s.readyMu.RUnlock()
	if fn != nil {
		return fn()
	}
	if s.newSession == nil {
		return false, "engine not configured"
	}
	return false, "Flux readiness probe not configured"
}

func (s *Server) routes() {
	s.handle("GET /v1/health", s.handleHealth)
	s.handle("GET /v1/status", s.auth(s.rate(s.handleStatus, s.apiLimiter)))
	s.handle("GET /v1/agent/status", s.auth(s.rate(s.handleAgentStatus, s.apiLimiter)))
	s.handle("GET /v1/ready", s.handleReady)
	s.handle("POST /v1/chat", s.auth(s.rate(s.handleChat, s.chatLimiter)))
	s.handle("POST /v1/cancel", s.auth(s.rate(s.handleCancel, s.apiLimiter)))
	s.handle("GET /v1/sessions", s.auth(s.rate(s.handleListSessions, s.apiLimiter)))
	s.handle("GET /v1/sessions/{id}", s.auth(s.rate(s.handleGetSession, s.apiLimiter)))
	s.handle("GET /v1/sessions/{id}/messages", s.auth(s.rate(s.handleGetMessages, s.apiLimiter)))
	s.handle("POST /v1/sessions/{id}/lease", s.auth(s.rate(s.handleAcquireLease, s.apiLimiter)))
	s.handle("DELETE /v1/sessions/{id}/lease", s.auth(s.rate(s.handleReleaseLease, s.apiLimiter)))
	s.handle("GET /v1/sessions/{id}/graph", s.auth(s.rate(s.handleGetSessionGraph, s.apiLimiter)))
	s.handle("POST /v1/graph/sync", s.auth(s.rate(s.handleGraphSync, s.apiLimiter)))
	s.handle("GET /v1/projects/{projectId}/graph", s.auth(s.rate(s.handleGraphRead, s.apiLimiter)))
	s.handle("DELETE /v1/sessions/{id}", s.auth(s.rate(s.handleDeleteSession, s.apiLimiter)))
	s.handle("GET /v1/stats", s.auth(s.rate(s.handleStats, s.apiLimiter)))
	s.handle("GET /v1/metrics", s.auth(s.rate(s.handleMetrics, s.apiLimiter)))
	s.RegisterReviewRoutes()
}

// handle registers a handler on the mux and records the pattern for
// spec-parity verification (see openapi_parity_test.go).
func (s *Server) handle(pattern string, h http.HandlerFunc) {
	s.routePatterns = append(s.routePatterns, pattern)
	s.mux.HandleFunc(pattern, h)
}

// rate wraps a handler with per-IP rate limiting (H9). Rejected requests get
// 429 with a Retry-After hint.
func (s *Server) rate(next http.HandlerFunc, lim *ipLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if lim != nil && !lim.Allow(clientIP(r)) {
			s.metrics.Counter("http.rate_limited_total").Inc()
			w.Header().Set("Retry-After", "2")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
			return
		}
		next(w, r)
	}
}

// cancelEntry pairs a generation's cancel func with an identity pointer so
// unregisterCancel can remove only the *current* generation's entry (a later
// session may have replaced it).
type cancelEntry struct {
	cancel context.CancelFunc
}

// registerCancel records the cancel func for a session's in-flight
// generation so POST /v1/cancel can abort it.
func (s *Server) registerCancel(sessionID string, cancel context.CancelFunc) {
	s.cancelMu.Lock()
	s.cancels[sessionID] = &cancelEntry{cancel: cancel}
	s.cancelMu.Unlock()
}

// unregisterCancel removes a session's cancel entry; it is a no-op if the
// entry no longer belongs to this cancel func.
func (s *Server) unregisterCancel(sessionID string, cancel context.CancelFunc) {
	s.cancelMu.Lock()
	if e := s.cancels[sessionID]; e != nil && e.cancel != nil {
		// Funcs are only comparable to nil, so compare code pointers.
		if reflect.ValueOf(e.cancel).Pointer() == reflect.ValueOf(cancel).Pointer() {
			delete(s.cancels, sessionID)
		}
	}
	s.cancelMu.Unlock()
}

// cancelSession aborts the in-flight generation for sessionID, if any.
// Returns true when a generation was active and was cancelled.
func (s *Server) cancelSession(sessionID string) bool {
	s.cancelMu.Lock()
	e := s.cancels[sessionID]
	s.cancelMu.Unlock()
	if e == nil || e.cancel == nil {
		return false
	}
	e.cancel()
	return true
}

// maxConcurrentFromEnv reads RHO_DAEMON_MAX_CONCURRENT (clamped to >= 1) so
// operators can tune the global chat concurrency cap without a rebuild.
func maxConcurrentFromEnv() int {
	n := defaultMaxConcurrentChat
	if raw := os.Getenv("RHO_DAEMON_MAX_CONCURRENT"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			n = parsed
		}
	}
	return n
}

// RoutePatterns returns a copy of every registered "METHOD /path" pattern.
func (s *Server) RoutePatterns() []string {
	out := make([]string, len(s.routePatterns))
	copy(out, s.routePatterns)
	return out
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		token = strings.TrimPrefix(token, "Bearer ")
		if token == "" {
			token = r.Header.Get("X-API-Key")
		}
		if s.apiKey == "" {
			// No API key configured — always allow, but still perform a constant-time
			// comparison against a dummy value to avoid leaking "no auth configured" via timing.
			_ = constantTimeEqual(token, "no-auth-configured-dummy")
			next(w, r)
			return
		}
		if !constantTimeEqual(token, s.apiKey) {
			// Audit: log failed authentication attempt.
			if s.securityLog != nil {
				reqID := RequestIDFromContext(r.Context())
				_, _ = s.securityLog.Append(
					securitylog.SeverityWarning,
					"auth_denied",
					fmt.Sprintf("auth failed: wrong token (ip=%s, path=%s, request_id=%s)", clientIP(r), r.URL.Path, reqID),
					"", reqID,
				)
			}
			s.metrics.Counter("auth_denied_total").Inc()
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func constantTimeEqual(a, b string) bool {
	// Length mismatch check short-circuits via early return, which
	// leaks the length difference via timing. This is an accepted
	// trade-off for bearer-token authentication — tokens are fixed
	// length and the comparison result is not secret.
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must contain a single JSON object"})
		return false
	}
	return true
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	sessionCount := 0
	s.sessions.Range(func(_, _ any) bool {
		sessionCount++
		return true
	})

	resp := HealthResponse{
		Status:    "ok",
		Version:   version,
		Uptime:    time.Since(s.startedAt).Round(time.Second).String(),
		Sessions:  sessionCount,
		StartedAt: s.startedAt.Format(time.RFC3339),
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleReady is the readiness probe. Unlike /v1/health (liveness, always 200
// while the process is up), /v1/ready returns 200 only when all dependencies
// are initialized and 503 otherwise, so orchestrators can gate traffic.
func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	ok, reason := s.ready()
	resp := ReadyResponse{
		Ready:  ok,
		Reason: reason,
		Uptime: time.Since(s.startedAt).Round(time.Second).String(),
	}
	status := http.StatusOK
	if !ok {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, resp)
}

func wantsSSE(r *http.Request) bool {
	return r.Header.Get("Accept") == "text/event-stream"
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.Prompt == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt is required"})
		return
	}

	if s.newSession == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "engine not configured"})
		return
	}

	// Global concurrency cap (H9): refuse rather than queue when too many
	// generations are in flight, so a burst cannot build an unbounded backlog.
	select {
	case s.concurrencySem <- struct{}{}:
		defer func() { <-s.concurrencySem }()
	default:
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "server busy: too many concurrent generations in flight"})
		return
	}

	start := time.Now()
	requestedID := strings.TrimSpace(req.SessionID)
	if requestedID != "" && !validSessionID(requestedID) {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid session id: must be 1-128 characters using letters, digits, dash, underscore, or dot (reserved: . and ..)",
			Code:  "invalid_session_id",
		})
		return
	}

	sessionID := requestedID
	if sessionID == "" {
		var idErr error
		sessionID, idErr = newSessionID()
		if idErr != nil {
			slog.Error("session id generation failed", "err", idErr)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "session creation failed"})
			return
		}
	}

	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()

	var saved *rhosession.Session
	if requestedID != "" {
		var loadErr error
		saved, loadErr = rhosession.Load(sessionID)
		if loadErr != nil {
			if !errors.Is(loadErr, rhosession.ErrNotFound) {
				slog.Error("load continuation session failed", "err", loadErr, "session_id", sessionID)
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{
					Error: "session load failed",
					Code:  "session_load_failed",
				})
				return
			}
			writeJSON(w, http.StatusNotFound, ErrorResponse{
				Error: "session not found",
				Code:  "session_not_found",
			})
			return
		}
		if strings.TrimSpace(req.Model) == "" {
			req.Model = saved.Model
		}
		if strings.TrimSpace(req.Agent) == "" {
			req.Agent = saved.Agent
		}
	}

	requestedCWD := strings.TrimSpace(req.CWD)
	switch {
	case requestedCWD != "":
		canonicalCWD, cwdErr := canonicalSessionCWD(requestedCWD)
		if cwdErr != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{
				Error:   "invalid cwd",
				Code:    "invalid_cwd",
				Details: cwdErr.Error(),
			})
			return
		}
		req.CWD = canonicalCWD
	case saved != nil && saved.CWD != "":
		// CWD is durable session metadata. Inherit it on continuation even
		// if that directory has since been removed.
		req.CWD = saved.CWD
	default:
		canonicalCWD, cwdErr := canonicalSessionCWD("")
		if cwdErr != nil {
			slog.Error("resolve daemon cwd failed", "err", cwdErr)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "session creation failed"})
			return
		}
		req.CWD = canonicalCWD
	}

	req.SessionID = sessionID
	req.Agent = strings.TrimSpace(req.Agent)

	sess, err := s.newSession(req)
	if err != nil {
		var requestErr *InvalidChatRequestError
		if errors.As(err, &requestErr) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{
				Error: requestErr.Message,
				Code:  "invalid_chat_request",
			})
			return
		}
		slog.Error("session create failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session creation failed"})
		return
	}
	if sess == nil {
		slog.Error("session factory returned nil session")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session creation failed"})
		return
	}
	if saved != nil {
		sess.LoadMessages(rhosession.ToRuntimeMessages(saved.Messages))
		if err := sess.ReplayJournal(saved.Events); err != nil {
			slog.Error("replay session event journal failed", "err", err, "session_id", sessionID)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session event journal replay failed"})
			return
		}
		// Mark the end of the seeded (resumed) event sequence. Events before
		// this marker came from the seed; events after are live (DSH
		// session-endseed seam).
		sess.Persistence().Journal().AppendSessionEndSeed()
	}

	// Set autonomy, capped by server-side policy. The daemon is
	// non-interactive — no human can approve permission prompts — so a
	// client must not be able to escalate to full/YOLO autonomy on its own.
	// Operators opt in to higher tiers explicitly via Config.MaxAutonomy.
	if req.Autonomy != "" {
		requested := safety.ParseAutonomyLevel(req.Autonomy)
		max := s.maxAutonomy
		if max == 0 {
			max = DefaultMaxAutonomy
		}
		if requested > max {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{
				Error: fmt.Sprintf("autonomy %q exceeds the daemon's configured maximum (%s); the daemon is non-interactive and cannot approve escalated permissions. Raise Config.MaxAutonomy (RHO_DAEMON_AUTONOMY) to allow it", requested.String(), max.String()),
				Code:  "autonomy_denied",
			})
			return
		}
		sess.PermSvc().SetAutonomy(requested)
	}

	// The central PermissionEngine auto-allows calls permitted by the active
	// profile. If it reaches this callback, the daemon has no human UI to answer
	// the prompt, so fail closed rather than duplicating the legacy tier logic.
	sess.SetPermissionFn(func(pr safety.PermissionRequest) {
		if pr.Response != nil {
			pr.Response <- false
		}
	})

	if req.MaxTurns > 0 {
		if setErr := sess.SetMaxTurns(req.MaxTurns); setErr != nil {
			slog.Error("invalid max turns", "err", setErr, "max_turns", req.MaxTurns)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid max_turns"})
			return
		}
	}

	sess.AddUser(req.Prompt)

	// Derive a cancellable generation context so POST /v1/cancel can abort an
	// in-flight generation for this session (H10). The entry lives for the
	// duration of the generation only.
	genCtx, genCancel := context.WithCancel(r.Context())
	s.registerCancel(sessionID, genCancel)
	defer s.unregisterCancel(sessionID, genCancel)

	events, err := sess.Stream(genCtx)
	if err != nil {
		slog.Error("stream failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "stream failed"})
		return
	}

	if wantsSSE(r) {
		// Publish the user turn before exposing the session ID in streaming
		// response headers. Even if the client disconnects mid-stream, the ID
		// it received remains retrievable and resumable.
		if saveErr := persistDaemonSession(sessionID, req, sess, saved, start); saveErr != nil {
			slog.Error("persist streaming session start failed", "err", saveErr, "session_id", sessionID)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "session persistence failed"})
			return
		}
		streamSSE(s, w, r, events, sessionID, req, sess, saved, start)
		return
	}

	writeJSONResponse(s, w, events, sessionID, req, sess, saved, start)
}

// streamSSE writes a streaming response as SSE events, observing client
// disconnect via r.Context().Done() so the handler does not keep pushing
// events to a dead connection.
func streamSSE(s *Server, w http.ResponseWriter, r *http.Request, events <-chan engine.StreamEvent, sessionID string, req ChatRequest, sess *engine.Session, saved *rhosession.Session, start time.Time) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Rho-Session-ID", sessionID)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	flusher, _ := w.(http.Flusher)
	rc := http.NewResponseController(w)
	var totalIn, totalOut, turns int

	for {
		select {
		case <-r.Context().Done():
			slog.Info("SSE client disconnected", "session_id", sessionID)
			return
		case ev, ok := <-events:
			if !ok {
				if saveErr := persistDaemonSession(sessionID, req, sess, saved, start); saveErr != nil {
					slog.Error("persist streaming session failed", "err", saveErr, "session_id", sessionID)
					if writeSSE(w, rc, "event: error\ndata: session persistence failed\n\n") {
						flushSSE(flusher)
					}
					return
				}
				s.trackSession(sessionID, req, saved, start, turns)
				doneData, _ := json.Marshal(map[string]interface{}{
					"session_id":  sessionID,
					"tokens_in":   totalIn,
					"tokens_out":  totalOut,
					"turns_taken": turns,
				})
				if writeSSE(w, rc, "event: done\ndata: %s\n\n", doneData) {
					flushSSE(flusher)
				}
				return
			}
			switch ev.Type {
			case "content":
				for _, line := range strings.Split(ev.Content, "\n") {
					if !writeSSE(w, rc, "data: %s\n", line) {
						s.abortStreamedSession(sessionID)
						return
					}
				}
				if !writeSSE(w, rc, "\n") {
					s.abortStreamedSession(sessionID)
					return
				}
			case "usage":
				if ev.Usage != nil {
					totalIn += ev.Usage.PromptTokens
					totalOut += ev.Usage.CompletionTokens
					turns++
				}
			}
			flushSSE(flusher)
		}
	}
}

// writeSSE writes one SSE frame, returning false when the write failed (client
// gone, or the server write deadline lapsed). A failed write means the handler
// MUST stop immediately: with an absolute http.Server.WriteTimeout, a stalled
// client would otherwise keep the handler (and the session stripe lock plus
// the global concurrency slot) pinned forever.
func writeSSE(w http.ResponseWriter, rc *http.ResponseController, format string, args ...interface{}) bool {
	if rc != nil {
		// Reset the write deadline before each frame so long agentic streams
		// are not cut off by the server's absolute WriteTimeout, while a
		// stalled client still fails the write and releases the handler.
		_ = rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
	}
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return false
	}
	return true
}

func flushSSE(flusher http.Flusher) {
	if flusher != nil {
		flusher.Flush()
	}
}

// abortStreamedSession cancels the in-flight generation for a streaming
// session whose client connection died mid-stream, so the agent loop does not
// keep running (and blocking on the unconsumed events channel) after the
// handler has returned and released the concurrency slot.
func (s *Server) abortStreamedSession(sessionID string) {
	slog.Info("SSE write failed, aborting session", "session_id", sessionID)
	s.cancelSession(sessionID)
}

// writeJSONResponse accumulates events and writes a single JSON response.
func writeJSONResponse(s *Server, w http.ResponseWriter, events <-chan engine.StreamEvent, sessionID string, req ChatRequest, sess *engine.Session, saved *rhosession.Session, start time.Time) {
	var response strings.Builder
	var totalIn, totalOut, turns int

	for ev := range events {
		switch ev.Type {
		case "content":
			response.WriteString(ev.Content)
		case "usage":
			if ev.Usage != nil {
				totalIn += ev.Usage.PromptTokens
				totalOut += ev.Usage.CompletionTokens
				turns++
			}
		}
	}

	if saveErr := persistDaemonSession(sessionID, req, sess, saved, start); saveErr != nil {
		slog.Error("persist session failed", "err", saveErr, "session_id", sessionID)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "session persistence failed"})
		return
	}
	s.trackSession(sessionID, req, saved, start, turns)
	w.Header().Set("X-Rho-Session-ID", sessionID)

	writeJSON(w, http.StatusOK, ChatResponse{
		SessionID:  sessionID,
		Response:   response.String(),
		TokensIn:   totalIn,
		TokensOut:  totalOut,
		TurnsTaken: turns,
		Duration:   time.Since(start).Round(time.Millisecond).String(),
	})
}

func validSessionID(id string) bool {
	return rhosession.ValidID(id)
}

// CancelRequest is the JSON body for POST /v1/cancel.
type CancelRequest struct {
	SessionID string `json:"session_id"`
}

// handleCancel aborts the in-flight generation for a session (H10). It must
// not take the per-session stripe lock: handleChat holds that lock for the
// whole generation, so taking it here would deadlock.
func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	var req CancelRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id is required"})
		return
	}
	if !validSessionID(sessionID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid session id"})
		return
	}
	if !s.cancelSession(sessionID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active generation for session"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
}

func newSessionID() (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	return "daemon-" + hex.EncodeToString(entropy[:]), nil
}

func canonicalSessionCWD(cwd string) (string, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", canonical)
	}
	return canonical, nil
}

func (s *Server) sessionLock(id string) *sync.Mutex {
	// FNV-1a is sufficient here: the hash only selects a synchronization
	// stripe; session IDs remain validated and are never trusted as paths.
	const offset64 = uint64(14695981039346656037)
	const prime64 = uint64(1099511628211)
	hash := offset64
	for i := 0; i < len(id); i++ {
		hash ^= uint64(id[i])
		hash *= prime64
	}
	return &s.sessionLocks[hash%uint64(len(s.sessionLocks))]
}

func persistDaemonSession(id string, req ChatRequest, sess *engine.Session, previous *rhosession.Session, startedAt time.Time) error {
	createdAt := startedAt
	name := ""
	if previous != nil {
		createdAt = previous.CreatedAt
		name = previous.Name
	}
	if name == "" {
		name = sess.JournalTitle()
	}
	// Emit the computed title as a journal event so the event spine
	// records how the session was named (DSH session.title seam, Phase 3).
	if j := sess.Persistence().Journal(); j != nil {
		j.AppendSessionTitle(name)
	}
	return rhosession.Save(&rhosession.Session{
		ID:        id,
		Model:     sess.Model(),
		Provider:  sess.Provider(),
		Agent:     req.Agent,
		CWD:       req.CWD,
		Name:      name,
		Messages:  rhosession.FromRuntimeMessages(sess.RawMessages()),
		Events:    sess.JournalWire(),
		CreatedAt: createdAt,
	})
}

func (s *Server) trackSession(id string, req ChatRequest, previous *rhosession.Session, startedAt time.Time, turns int) {
	createdAt := startedAt
	if previous != nil && !previous.CreatedAt.IsZero() {
		createdAt = previous.CreatedAt
	}
	if current, ok := s.sessions.Load(id); ok {
		if active, activeOK := current.(*Session); activeOK {
			turns += active.Turns
		}
	}
	s.sessions.Store(id, &Session{
		ID:        id,
		CreatedAt: createdAt,
		LastUsed:  time.Now(),
		Turns:     turns,
		CWD:       req.CWD,
		Agent:     req.Agent,
	})
	// The sessions map is a soft in-memory index (durable state lives in the
	// on-disk session store) — bounding it prevents unbounded growth across a
	// long-lived daemon lifetime (LOW finding). Drop the oldest entries once
	// the cap is exceeded; LastUsed is recomputed on each trackSession call.
	s.evictStaleSessions(maxTrackedSessions)
}

// maxTrackedSessions caps the in-memory session index (LOW finding: the map
// previously grew without bound over the daemon lifetime). Durability is
// unaffected — this only backs GET /v1/sessions and per-session turn counts.
const maxTrackedSessions = 1000

// evictStaleSessions trims the in-memory session index to at most `maxKeep`
// entries, dropping the ones with the oldest LastUsed (ties broken by ID for
// determinism). It is safe to call concurrently with ongoing Store/Load.
func (s *Server) evictStaleSessions(maxKeep int) {
	var entries []struct {
		id string
		s  *Session
	}
	s.sessions.Range(func(k, v any) bool {
		id, _ := k.(string)
		sess, _ := v.(*Session)
		if id == "" || sess == nil {
			return true
		}
		entries = append(entries, struct {
			id string
			s  *Session
		}{id: id, s: sess})
		return true
	})
	if len(entries) <= maxKeep {
		return
	}
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].s.LastUsed.Equal(entries[j].s.LastUsed) {
			return entries[i].s.LastUsed.Before(entries[j].s.LastUsed)
		}
		return entries[i].id < entries[j].id
	})
	for i := 0; i < len(entries)-maxKeep; i++ {
		s.sessions.Delete(entries[i].id)
	}
}

func (s *Server) handleListSessions(w http.ResponseWriter, _ *http.Request) {
	var sessions []*Session
	s.sessions.Range(func(_, v any) bool {
		if sess, ok := v.(*Session); ok {
			sessions = append(sessions, sess)
		}
		return true
	})
	if sessions == nil {
		sessions = []*Session{}
	}
	writeJSON(w, http.StatusOK, sessions)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) writePIDFile() error {
	dir := pidDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	data, err := json.Marshal(struct {
		PID          int    `json:"pid"`
		Addr         string `json:"addr"`
		StartedAt    string `json:"started_at"`
		AuthRequired bool   `json:"auth_required"`
	}{
		PID:          os.Getpid(),
		Addr:         s.addr,
		StartedAt:    s.startedAt.Format(time.RFC3339),
		AuthRequired: s.apiKey != "",
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "daemon.json"), append(data, '\n'), 0o600)
}

func (s *Server) removePIDFile() error {
	return os.Remove(filepath.Join(pidDir(), "daemon.json"))
}

func pidDir() string {
	return storage.DaemonRunDir()
}
