package daemon

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func newIPv4GatewayServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("restricted test environment does not allow local listeners: %v", err)
		}
		t.Fatalf("listen tcp4: %v", err)
	}
	srv := httptest.NewUnstartedServer(h)
	srv.Listener = ln
	srv.Start()
	return srv
}

// Compile-time assertions that all three gateways satisfy Gateway.
var (
	_ Gateway = (*TelegramGateway)(nil)
	_ Gateway = (*DiscordGateway)(nil)
	_ Gateway = (*SlackGateway)(nil)
)

func TestGatewayManager_ConfigGating(t *testing.T) {
	tests := []struct {
		name     string
		cfg      GatewaysConfig
		wantSet  map[string]bool // gateway name -> expected present
		wantSize int
	}{
		{
			name:     "all disabled",
			cfg:      GatewaysConfig{},
			wantSize: 0,
		},
		{
			name: "telegram enabled but no token is skipped",
			cfg: GatewaysConfig{
				Telegram: TelegramConfig{Enabled: true},
			},
			wantSize: 0,
		},
		{
			name: "telegram enabled with token",
			cfg: GatewaysConfig{
				Telegram: TelegramConfig{Enabled: true, Token: "t"},
			},
			wantSet:  map[string]bool{"telegram": true},
			wantSize: 1,
		},
		{
			name: "discord disabled even with token",
			cfg: GatewaysConfig{
				Discord: DiscordConfig{Enabled: false, Token: "d"},
			},
			wantSize: 0,
		},
		{
			name: "all three enabled and credentialed",
			cfg: GatewaysConfig{
				Telegram: TelegramConfig{Enabled: true, Token: "t"},
				Discord:  DiscordConfig{Enabled: true, Token: "d"},
				Slack:    SlackConfig{Enabled: true, SigningSecret: "s"},
			},
			wantSet:  map[string]bool{"telegram": true, "discord": true, "slack": true},
			wantSize: 3,
		},
		{
			name: "slack enabled without signing secret is skipped",
			cfg: GatewaysConfig{
				Slack: SlackConfig{Enabled: true, BotToken: "xoxb"},
			},
			wantSize: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// A real Server is required so Slack can register its route.
			srv := New(Config{Port: 0}, nil)
			m := newGatewayManager(tc.cfg, "http://127.0.0.1:1", "", srv)
			got := m.Gateways()
			if len(got) != tc.wantSize {
				t.Fatalf("got %d gateways, want %d", len(got), tc.wantSize)
			}
			present := map[string]bool{}
			for _, g := range got {
				present[g.Name()] = true
			}
			for name, want := range tc.wantSet {
				if present[name] != want {
					t.Errorf("gateway %q present=%v want=%v", name, present[name], want)
				}
			}
		})
	}
}

func TestGatewayManager_StartStopNoGateways(t *testing.T) {
	m := newGatewayManager(GatewaysConfig{}, "http://127.0.0.1:1", "", nil)
	// Should be a no-op and never block.
	m.Start(context.Background())
	m.Stop()
}

func TestAuthorizer_PairingAndAllowlist(t *testing.T) {
	tests := []struct {
		name        string
		pairingCode string
		seed        []string
		sender      string
		text        string
		wantIsPair  bool
		wantPairOK  bool
		wantAllowed bool // allowed() result AFTER tryPair
	}{
		{
			name:        "seeded allowlist is authorized",
			seed:        []string{"alice"},
			sender:      "alice",
			text:        "hello",
			wantAllowed: true,
		},
		{
			name:        "unknown sender rejected",
			pairingCode: "secret",
			sender:      "mallory",
			text:        "hello",
			wantAllowed: false,
		},
		{
			name:        "correct pairing code authorizes sender",
			pairingCode: "secret",
			sender:      "bob",
			text:        "/pair secret",
			wantIsPair:  true,
			wantPairOK:  true,
			wantAllowed: true,
		},
		{
			name:        "wrong pairing code rejected",
			pairingCode: "secret",
			sender:      "eve",
			text:        "/pair wrong",
			wantIsPair:  true,
			wantPairOK:  false,
			wantAllowed: false,
		},
		{
			name:        "pair command with empty configured code fails",
			pairingCode: "",
			sender:      "carol",
			text:        "/pair anything",
			wantIsPair:  true,
			wantPairOK:  false,
			wantAllowed: false,
		},
		{
			name:        "non-pair message is not a pair attempt",
			pairingCode: "secret",
			sender:      "dan",
			text:        "what is the weather",
			wantIsPair:  false,
			wantAllowed: false,
		},
		{
			name:        "empty sender never allowed",
			seed:        []string{""},
			sender:      "",
			text:        "hi",
			wantAllowed: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newAuthorizer(tc.pairingCode, tc.seed)
			isPair, pairOK := a.tryPair(tc.sender, tc.text)
			if isPair != tc.wantIsPair {
				t.Errorf("isPair=%v want=%v", isPair, tc.wantIsPair)
			}
			if pairOK != tc.wantPairOK {
				t.Errorf("pairOK=%v want=%v", pairOK, tc.wantPairOK)
			}
			if got := a.allowed(tc.sender); got != tc.wantAllowed {
				t.Errorf("allowed=%v want=%v", got, tc.wantAllowed)
			}
		})
	}
}

func TestForwardToRho(t *testing.T) {
	var gotAuth, gotPrompt string
	ts := newIPv4GatewayServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var body struct {
			Prompt string `json:"prompt"`
		}
		_ = decodeForTest(r, &body)
		gotPrompt = body.Prompt
		writeJSON(w, http.StatusOK, ChatResponse{Response: "pong"})
	}))
	defer ts.Close()

	reply, err := forwardToRho(context.Background(), ts.Client(), ts.URL, "key123", "ping")
	if err != nil {
		t.Fatalf("forwardToRho: %v", err)
	}
	if reply != "pong" {
		t.Errorf("reply=%q want pong", reply)
	}
	if gotAuth != "Bearer key123" {
		t.Errorf("auth header=%q want Bearer key123", gotAuth)
	}
	if gotPrompt != "ping" {
		t.Errorf("prompt=%q want ping", gotPrompt)
	}
}

// --- Slack signature verification ---

func slackSignature(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + ts + ":" + string(body)))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func TestSlackGateway_VerifySignature(t *testing.T) {
	const secret = "8f742231b10e8888abcd99yyyzzz85a5"
	body := []byte(`{"type":"event_callback"}`)
	fixedNow := time.Unix(1_600_000_000, 0)

	tests := []struct {
		name      string
		secret    string
		ts        string
		sig       string
		now       time.Time
		wantValid bool
	}{
		{
			name:      "valid signature",
			secret:    secret,
			ts:        strconv.FormatInt(fixedNow.Unix(), 10),
			sig:       slackSignature(secret, strconv.FormatInt(fixedNow.Unix(), 10), body),
			now:       fixedNow,
			wantValid: true,
		},
		{
			name:      "wrong secret fails",
			secret:    secret,
			ts:        strconv.FormatInt(fixedNow.Unix(), 10),
			sig:       slackSignature("other-secret", strconv.FormatInt(fixedNow.Unix(), 10), body),
			now:       fixedNow,
			wantValid: false,
		},
		{
			name:      "stale timestamp rejected",
			secret:    secret,
			ts:        strconv.FormatInt(fixedNow.Add(-10*time.Minute).Unix(), 10),
			sig:       slackSignature(secret, strconv.FormatInt(fixedNow.Add(-10*time.Minute).Unix(), 10), body),
			now:       fixedNow,
			wantValid: false,
		},
		{
			name:      "missing signature header",
			secret:    secret,
			ts:        strconv.FormatInt(fixedNow.Unix(), 10),
			sig:       "",
			now:       fixedNow,
			wantValid: false,
		},
		{
			name:      "empty signing secret fails closed",
			secret:    "",
			ts:        strconv.FormatInt(fixedNow.Unix(), 10),
			sig:       slackSignature(secret, strconv.FormatInt(fixedNow.Unix(), 10), body),
			now:       fixedNow,
			wantValid: false,
		},
		{
			name:      "non-numeric timestamp",
			secret:    secret,
			ts:        "not-a-number",
			sig:       slackSignature(secret, "not-a-number", body),
			now:       fixedNow,
			wantValid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := newSlackGateway(SlackConfig{SigningSecret: tc.secret}, "http://x", "", nil)
			g.now = func() time.Time { return tc.now }
			h := http.Header{}
			if tc.ts != "" {
				h.Set("X-Slack-Request-Timestamp", tc.ts)
			}
			if tc.sig != "" {
				h.Set("X-Slack-Signature", tc.sig)
			}
			if got := g.verifySignature(h, body); got != tc.wantValid {
				t.Errorf("verifySignature=%v want=%v", got, tc.wantValid)
			}
		})
	}
}

func TestSlackGateway_URLVerificationHandshake(t *testing.T) {
	const secret = "test-secret"
	srv := New(Config{Port: 0}, nil)
	g := newSlackGateway(SlackConfig{SigningSecret: secret, Path: "/v1/slack/events"}, "http://x", "", srv)

	body := []byte(`{"type":"url_verification","challenge":"abc123"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req := httptest.NewRequest(http.MethodPost, "/v1/slack/events", bytesReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", slackSignature(secret, ts, body))

	rec := httptest.NewRecorder()
	g.handleEvents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", rec.Code)
	}
	var resp map[string]string
	_ = decodeJSON(rec.Body.Bytes(), &resp)
	if resp["challenge"] != "abc123" {
		t.Errorf("challenge=%q want abc123", resp["challenge"])
	}
}

func TestSlackGateway_RejectsBadSignature(t *testing.T) {
	g := newSlackGateway(SlackConfig{SigningSecret: "secret"}, "http://x", "", nil)
	body := []byte(`{"type":"event_callback"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/slack/events", bytesReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	req.Header.Set("X-Slack-Signature", "v0=deadbeef")

	rec := httptest.NewRecorder()
	g.handleEvents(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status=%d want 401", rec.Code)
	}
}

func TestDiscordGateway_HandleMessage_FlowsThroughAllowlist(t *testing.T) {
	rho := newIPv4GatewayServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, ChatResponse{Response: "rho-reply"})
	}))
	defer rho.Close()

	g := newDiscordGateway(DiscordConfig{Token: "bot", PairingCode: "code"}, rho.URL, "")
	var sent []string
	send := func(s string) error { sent = append(sent, s); return nil }

	// Unauthorized non-pair message -> "Unauthorized", no rho call.
	g.handleMessage(context.Background(), "u1", "C", "hello", send)
	// Wrong pairing code -> failure.
	g.handleMessage(context.Background(), "u1", "C", "/pair nope", send)
	// Correct pairing code -> paired.
	g.handleMessage(context.Background(), "u1", "C", "/pair code", send)
	// Authorized -> forwarded to rho.
	g.handleMessage(context.Background(), "u1", "C", "do it", send)

	if len(sent) != 4 {
		t.Fatalf("expected 4 sends, got %d: %v", len(sent), sent)
	}
	if !strings.Contains(sent[0], "Unauthorized") {
		t.Errorf("sent[0]=%q want Unauthorized", sent[0])
	}
	if !strings.Contains(sent[1], "failed") {
		t.Errorf("sent[1]=%q want pairing failure", sent[1])
	}
	if !strings.Contains(sent[2], "Paired") {
		t.Errorf("sent[2]=%q want Paired", sent[2])
	}
	if sent[3] != "rho-reply" {
		t.Errorf("sent[3]=%q want rho-reply", sent[3])
	}
	if !g.auth.allowed("u1") {
		t.Errorf("u1 should be allowed after pairing")
	}
}

func TestWantsDiscordMessage(t *testing.T) {
	const self = "BOT"
	mc := func(authorID string, bot bool, guildID string, mentionIDs ...string) *discordgo.MessageCreate {
		mentions := make([]*discordgo.User, 0, len(mentionIDs))
		for _, id := range mentionIDs {
			mentions = append(mentions, &discordgo.User{ID: id})
		}
		return &discordgo.MessageCreate{Message: &discordgo.Message{
			Author:   &discordgo.User{ID: authorID, Bot: bot},
			GuildID:  guildID,
			Mentions: mentions,
		}}
	}

	cases := []struct {
		name string
		msg  *discordgo.MessageCreate
		want bool
	}{
		{"dm accepted", mc("u1", false, ""), true},
		{"guild mention accepted", mc("u1", false, "G", self), true},
		{"guild no mention ignored", mc("u1", false, "G"), false},
		{"bot author ignored", mc("x", true, "G", self), false},
		{"self ignored", mc(self, false, ""), false},
	}
	for _, tc := range cases {
		if got := wantsDiscordMessage(tc.msg, self); got != tc.want {
			t.Errorf("%s: wantsDiscordMessage=%v want %v", tc.name, got, tc.want)
		}
	}
}

func TestStripDiscordMention(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"<@BOT> hello", "hello"},
		{"<@!BOT>  spaced  ", "spaced"},
		{"no mention here", "no mention here"},
	} {
		if got := stripDiscordMention(tc.in, "BOT"); got != tc.want {
			t.Errorf("stripDiscordMention(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

// --- small helpers ---

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

func decodeForTest(r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}

func decodeJSON(b []byte, dst any) error {
	return json.Unmarshal(b, dst)
}
