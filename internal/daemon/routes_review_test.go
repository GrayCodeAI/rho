package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/GrayCodeAI/rho/internal/testutil"
)

// newReviewTestServer starts a daemon. RegisterReviewRoutes() is already
// called by routes() inside New(), so review endpoints are available on any
// daemon instance — this helper just documents the intent at call sites.
func newReviewTestServer(t *testing.T) string {
	t.Helper()
	srv := New(Config{Port: 0, Host: testutil.LoopbackHost}, nil)
	addr := startTestDaemon(t, srv)
	t.Cleanup(func() { srv.Stop(context.Background()) })
	return addr
}

func TestDaemon_Review_MissingSHA(t *testing.T) {
	addr := newReviewTestServer(t)

	body, _ := json.Marshal(ReviewRequest{})
	resp, err := http.Post("http://"+addr+"/v1/review", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /v1/review failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing sha, got %d", resp.StatusCode)
	}
}

func TestDaemon_Review_InvalidSHA(t *testing.T) {
	addr := newReviewTestServer(t)

	tests := []string{"not-hex!", "abc", strings.Repeat("a", 41)}
	for _, sha := range tests {
		body, _ := json.Marshal(ReviewRequest{SHA: sha})
		resp, err := http.Post("http://"+addr+"/v1/review", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /v1/review failed: %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("sha=%q: expected 400, got %d", sha, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestDaemon_Review_ConcernsFlagInjectionGuard(t *testing.T) {
	addr := newReviewTestServer(t)

	body, _ := json.Marshal(ReviewRequest{SHA: "abc1234", Concerns: "--dangerous-flag"})
	resp, err := http.Post("http://"+addr+"/v1/review", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /v1/review failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for concerns starting with '--', got %d", resp.StatusCode)
	}
}

func TestDaemon_Review_ModelFlagInjectionGuard(t *testing.T) {
	addr := newReviewTestServer(t)

	body, _ := json.Marshal(ReviewRequest{SHA: "abc1234", Model: "--evil"})
	resp, err := http.Post("http://"+addr+"/v1/review", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /v1/review failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for model starting with '--', got %d", resp.StatusCode)
	}
}

func TestDaemon_Review_Accepted(t *testing.T) {
	addr := newReviewTestServer(t)

	body, _ := json.Marshal(ReviewRequest{SHA: "abc1234"})
	resp, err := http.Post("http://"+addr+"/v1/review", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /v1/review failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}

	var got ReviewResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SHA != "abc1234" {
		t.Errorf("SHA = %q, want %q", got.SHA, "abc1234")
	}
	if got.Status != "queued" {
		t.Errorf("Status = %q, want %q", got.Status, "queued")
	}
}

func TestDaemon_ReviewStatus(t *testing.T) {
	addr := newReviewTestServer(t)

	resp, err := http.Get("http://" + addr + "/v1/review/status")
	if err != nil {
		t.Fatalf("GET /v1/review/status failed: %v", err)
	}
	defer resp.Body.Close()

	// `rho review status` shells out to the rho binary; in a restricted
	// test environment that command may not resolve, so the handler's own
	// 500 branch is just as valid an outcome as a real 200 — both are
	// well-defined, deterministic behavior we can assert on.
	switch resp.StatusCode {
	case http.StatusOK:
		var body map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode 200 body: %v", err)
		}
		if _, ok := body["status"]; !ok {
			t.Error("200 response missing 'status' field")
		}
	case http.StatusInternalServerError:
		var body map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode 500 body: %v", err)
		}
		if _, ok := body["error"]; !ok {
			t.Error("500 response missing 'error' field")
		}
	default:
		t.Errorf("unexpected status %d", resp.StatusCode)
	}
}
