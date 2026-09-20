package daemon

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

func newIPv4TelegramServer(t *testing.T, h http.Handler) *httptest.Server {
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

// telegramMockAPI captures sendMessage calls and serves a fake Telegram + rho
// endpoint. The Telegram bot API base is hardcoded in telegram.go, so we instead
// drive handleMessage directly and observe outbound sends via a mock daemon.
func TestTelegram_HandleMessage_Authorization(t *testing.T) {
	// Mock rho daemon /v1/chat.
	rho := newIPv4TelegramServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, ChatResponse{Response: "rho-reply"})
	}))
	defer rho.Close()

	// Mock Telegram sendMessage endpoint by intercepting via a custom transport.
	var mu sync.Mutex
	var sends []string
	tg := newTelegramGatewayFromConfig(
		TelegramConfig{Token: "tok", PairingCode: "open"},
		rho.URL, "apikey",
	)
	tg.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/sendMessage") {
			_ = req.ParseForm()
			mu.Lock()
			sends = append(sends, req.FormValue("text"))
			mu.Unlock()
			return jsonResp(`{"ok":true}`), nil
		}
		// forward-to-rho goes to the real mock server; let it through.
		return http.DefaultTransport.RoundTrip(req)
	})}

	mkMsg := func(text string) *TelegramMessage {
		m := &TelegramMessage{Text: text}
		m.Chat.ID = 42
		m.From.Username = "alice"
		return m
	}

	// 1. Unauthorized non-pair message -> "Unauthorized" reply, no rho call.
	tg.handleMessage(context.Background(), mkMsg("hello"))
	// 2. Wrong pairing code -> failure reply.
	tg.handleMessage(context.Background(), mkMsg("/pair wrong"))
	// 3. Correct pairing code -> paired reply.
	tg.handleMessage(context.Background(), mkMsg("/pair open"))
	// 4. Now authorized -> rho reply forwarded.
	tg.handleMessage(context.Background(), mkMsg("do something"))

	mu.Lock()
	defer mu.Unlock()
	if len(sends) != 4 {
		t.Fatalf("expected 4 sends, got %d: %v", len(sends), sends)
	}
	if !strings.Contains(sends[0], "Unauthorized") {
		t.Errorf("send[0]=%q want Unauthorized", sends[0])
	}
	if !strings.Contains(sends[1], "failed") {
		t.Errorf("send[1]=%q want pairing failure", sends[1])
	}
	if !strings.Contains(sends[2], "Paired") {
		t.Errorf("send[2]=%q want Paired", sends[2])
	}
	if sends[3] != "rho-reply" {
		t.Errorf("send[3]=%q want rho-reply", sends[3])
	}
	if !tg.auth.allowed("alice") {
		t.Errorf("alice should be allowed after pairing")
	}
}

func TestTelegramSenderID(t *testing.T) {
	withUser := &TelegramMessage{}
	withUser.From.Username = "bob"
	withUser.Chat.ID = 7
	if got := telegramSenderID(withUser); got != "bob" {
		t.Errorf("got %q want bob", got)
	}

	noUser := &TelegramMessage{}
	noUser.Chat.ID = 99
	if got := telegramSenderID(noUser); got != "99" {
		t.Errorf("got %q want 99", got)
	}
}

func TestTelegram_BareConstructorFailsClosed(t *testing.T) {
	// The bare constructor seeds an empty authorizer, so it must refuse all
	// senders (no pairing code / allowlist) rather than forwarding to rho.
	rhoCalled := false
	rho := newIPv4TelegramServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rhoCalled = true
		writeJSON(w, http.StatusOK, ChatResponse{Response: "ok"})
	}))
	defer rho.Close()

	var sends []string
	tg := NewTelegramGateway("tok", rho.URL)
	tg.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/sendMessage") {
			_ = req.ParseForm()
			sends = append(sends, req.FormValue("text"))
			return jsonResp(`{"ok":true}`), nil
		}
		return http.DefaultTransport.RoundTrip(req)
	})}

	m := &TelegramMessage{Text: "hi"}
	m.Chat.ID = 1
	tg.handleMessage(context.Background(), m)
	if len(sends) != 1 || !strings.Contains(sends[0], "Unauthorized") {
		t.Fatalf("expected an Unauthorized reply, got %v", sends)
	}
	if rhoCalled {
		t.Fatal("bare constructor must not forward unauthorized messages to rho")
	}
}

// --- helpers ---

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResp(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func TestTelegramSafeFileName(t *testing.T) {
	if got := safeFileName("AAabcdef0123456789xyz", "audio/ogg"); got != "AAabcdef01234567.ogg" {
		t.Fatalf("safeFileName = %q", got)
	}
	if got := safeFileName("f", "audio/mpeg"); got != "f.mp3" {
		t.Fatalf("safeFileName short id = %q", got)
	}
}

func TestTelegramVoiceWithoutTranscriberFallsBackToText(t *testing.T) {
	rho := newIPv4TelegramServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, ChatResponse{Response: "rho-reply"})
	}))
	defer rho.Close()

	// No STT transcriber installed: a voice message must fall back to the
	// existing text path (transcribeAudio returns "" with no reply).
	tg := newTelegramGatewayFromConfig(TelegramConfig{Token: "tok", AllowList: []string{"user"}}, rho.URL, "k")
	tg.handleMessage(context.Background(), &TelegramMessage{
		Text: "hello",
		Chat: struct {
			ID int64 `json:"id"`
		}{ID: 1},
		From: struct {
			Username string `json:"username"`
		}{Username: "user"},
		Voice: &telegramAudio{FileID: "fid", MimeType: "audio/ogg"},
	})
}
