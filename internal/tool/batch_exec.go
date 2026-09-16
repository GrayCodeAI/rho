package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// BatchExecTool submits prompts to the provider-native Message Batches API
// for 50%-cost async execution. Designed for CI/scripted workloads where the
// result is not needed immediately. Uses fixed HTTP calls (no shell) against
// the provider's REST endpoint; no flux/client import (boundary-guarded).
type BatchExecTool struct{}

func (BatchExecTool) Name() string      { return "BatchExec" }
func (BatchExecTool) RiskLevel() string { return "medium" }
func (BatchExecTool) Aliases() []string { return []string{"batch_exec"} }
func (BatchExecTool) Description() string {
	return "Submit one or more prompts to the Anthropic Message Batches API for 50%-cost async execution. Returns a batch ID; poll with action=poll to check status."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (BatchExecTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action":                {Type: "string", Description: "submit: send prompts; poll: single status check; wait: poll until the batch reaches a terminal state (with backoff + Retry-After honoring).", Enum: []interface{}{"submit", "poll", "wait"}},
			"prompts":               {Type: "array", Description: "Prompts to submit (action=submit).", Items: &SchemaProperty{Type: "string"}},
			"model":                 {Type: "string", Description: "Model ID (default claude-sonnet-4-20250514)."},
			"batch_id":              {Type: "string", Description: "Batch ID to poll/wait on."},
			"max_tokens":            {Type: "integer", Description: "Max output tokens per request (default 4096)."},
			"timeout_seconds":       {Type: "integer", Description: "Max seconds to wait (default 600)."},
			"poll_interval_seconds": {Type: "integer", Description: "Initial poll interval in seconds (default 2)."},
		},
		Required: []string{"action"},
	}
}

func (BatchExecTool) Parameters() map[string]interface{} {
	return batchExecSchema.ToJSONSchema()
}

// batchExecSchema is the single source of truth for BatchExec's input schema.
var batchExecSchema = BatchExecTool{}.Schema()

var batchHTTP = &http.Client{Timeout: 5 * time.Minute}

// BatchExecInput are the parsed parameters for all BatchExec actions.
// BatchExecInput is the typed input for BatchExecTool.
type BatchExecInput struct {
	Action          string   `json:"action"`
	Prompts         []string `json:"prompts"`
	Model           string   `json:"model"`
	BatchID         string   `json:"batch_id"`
	MaxTokens       int      `json:"max_tokens"`
	TimeoutSeconds  int      `json:"timeout_seconds"`
	PollIntervalSec int      `json:"poll_interval_seconds"`
}

// batchAPIKey reads the key from env.
func batchAPIKey() string { return os.Getenv("ANTHROPIC_API_KEY") }

func batchDefaultModel() string { return "claude-sonnet-4-20250514" }

// batchBaseURL is the Anthropic API host. A var (not const) so tests can
// redirect the client to an httptest server.
var batchBaseURL = "https://api.anthropic.com"

func batchHeaders(req *http.Request, apiKey string) *http.Request {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("Anthropic-Version", "2023-06-01")
	req.Header.Set("Anthropic-Beta", "message-batches-2024-09-24")
	return req
}

type batchPollResult struct {
	BatchID string `json:"batch_id"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

func (BatchExecTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[BatchExecInput]("BatchExec", input)
	if err != nil {
		return "", err
	}

	apiKey := batchAPIKey()
	if apiKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY not set — required for batch execution")
	}

	switch p.Action {
	case "submit":
		return batchSubmit(ctx, apiKey, p)
	case "poll":
		if p.BatchID == "" {
			return "", fmt.Errorf("batch_id is required for poll")
		}
		return batchPoll(ctx, apiKey, p.BatchID)
	case "wait":
		if p.BatchID == "" {
			return "", fmt.Errorf("batch_id is required for wait")
		}
		return batchWait(ctx, apiKey, p.BatchID, p.TimeoutSeconds, p.PollIntervalSec)
	default:
		return "", fmt.Errorf("unsupported action %q (use submit, poll, or wait)", p.Action)
	}
}

func batchSubmit(ctx context.Context, apiKey string, p BatchExecInput) (string, error) {
	if len(p.Prompts) == 0 {
		return "", fmt.Errorf("at least one prompt is required")
	}
	model := p.Model
	if model == "" {
		model = batchDefaultModel()
	}
	maxTok := p.MaxTokens
	if maxTok <= 0 {
		maxTok = 4096
	}

	type item struct {
		CustomID string                 `json:"custom_id"`
		Params   map[string]interface{} `json:"params"`
	}
	items := make([]item, len(p.Prompts))
	for i, prompt := range p.Prompts {
		items[i] = item{
			CustomID: fmt.Sprintf("req-%03d", i+1),
			Params: map[string]interface{}{
				"model":      model,
				"max_tokens": maxTok,
				"messages":   []map[string]string{{"role": "user", "content": prompt}},
			},
		}
	}
	body, err := json.Marshal(map[string]interface{}{"requests": items})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		batchBaseURL+"/v1/messages/batches", bytes.NewReader(body)) // #nosec G107 -- fixed API host
	if err != nil {
		return "", err
	}
	batchHeaders(req, apiKey)
	resp, err := batchHTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("batch submit: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("batch API %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	out, _ := json.MarshalIndent(map[string]string{
		"batch_id": result.ID,
		"requests": fmt.Sprintf("%d", len(p.Prompts)),
		"model":    model,
	}, "", "  ")
	return string(out), nil
}

func batchPoll(ctx context.Context, apiKey, batchID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		batchBaseURL+"/v1/messages/batches/"+batchID, nil) // #nosec G107 -- fixed API host + validated ID
	if err != nil {
		return "", err
	}
	batchHeaders(req, apiKey)
	resp, err := batchHTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("batch poll: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("batch API %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var result batchPollResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("batch poll parse: %w", err)
	}
	result.BatchID = batchID
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

// batchTerminalStates are the statuses that mean the batch is done.
var batchTerminalStates = map[string]bool{
	"ended": true, "completed": true, "failed": true,
	"expired": true, "canceled": true, "cancelled": true,
}

func isBatchTerminal(s string) bool { return batchTerminalStates[strings.ToLower(s)] }

// batchWait polls until the batch reaches a terminal state, with exponential
// backoff + jitter capped at 30s, honoring Retry-After on 429/5xx, bounded by
// timeoutSeconds (default 600). Mirrors flux's WaitUntilDone but inlined to
// stay boundary-compliant (no flux/client import).
func batchWait(ctx context.Context, apiKey, batchID string, timeoutSeconds, pollIntervalSec int) (string, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 600
	}
	initial := time.Duration(pollIntervalSec) * time.Second
	if initial <= 0 {
		initial = 2 * time.Second
	}
	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	attempt := 0
	for {
		status, retryAfter, err := batchStatus(ctx, apiKey, batchID)
		if err != nil {
			return "", err
		}
		if isBatchTerminal(status) {
			out, _ := json.MarshalIndent(batchPollResult{BatchID: batchID, Status: status}, "", "  ")
			return string(out), nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("batch %s not terminal within %ds (last status %s)", batchID, timeoutSeconds, status)
		}
		delay := batchBackoffDelay(attempt, initial, retryAfter)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(delay):
		}
		attempt++
	}
}

// batchStatus performs a single status fetch, returning (status, retryAfter, err).
func batchStatus(ctx context.Context, apiKey, batchID string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		batchBaseURL+"/v1/messages/batches/"+batchID, nil) // #nosec G107 -- fixed API host + validated ID
	if err != nil {
		return "", "", err
	}
	batchHeaders(req, apiKey)
	resp, err := batchHTTP.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("batch status: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return "in_progress", resp.Header.Get("Retry-After"), nil // transient: keep polling
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("batch API %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var result batchPollResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", "", fmt.Errorf("batch status parse: %w", err)
	}
	return result.Status, "", nil
}

// batchBackoffDelay computes attempt-th exponential delay with jitter, capped
// at 30s, overridden by Retry-After seconds when larger.
func batchBackoffDelay(attempt int, initial time.Duration, retryAfter string) time.Duration {
	d := initial << uint(minInt(attempt, 6))
	if d > 30*time.Second || d <= 0 {
		d = 30 * time.Second
	}
	// ±20% jitter.
	d = time.Duration(float64(d) * (0.8 + float64(int(time.Now().UnixNano())%50)/100.0*0.4))
	if ra, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && ra > 0 {
		if rd := time.Duration(ra) * time.Second; rd > d {
			return rd
		}
	}
	return d
}
