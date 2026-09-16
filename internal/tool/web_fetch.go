package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type WebFetchTool struct{}

func (WebFetchTool) Name() string      { return "WebFetch" }
func (WebFetchTool) Aliases() []string { return []string{"web_fetch"} }
func (WebFetchTool) Description() string {
	return "Fetch a URL and return its content as text. HTML is converted to plain text."
}

// WebFetchInput is the typed input for WebFetchTool.
type WebFetchInput struct {
	URL string `json:"url"`
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (WebFetchTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"url": {Type: "string", Description: "URL to fetch"},
		},
		Required: []string{"url"},
	}
}

func (WebFetchTool) Parameters() map[string]interface{} {
	return webFetchSchema.ToJSONSchema()
}

// webFetchSchema is the single source of truth for WebFetch's input schema.
var webFetchSchema = WebFetchTool{}.Schema()

// Timeout declares WebFetch's execution budget so the engine's dispatch
// deadline matches the tool's own 30s HTTP deadline. Declared budgets win
// over the name-based fallback (DSH tool-declared timeout policy).
func (WebFetchTool) Timeout() time.Duration { return 30 * time.Second }

var (
	htmlTagRe    = regexp.MustCompile(`<[^>]*>`)
	multiSpaceRe = regexp.MustCompile(`\s{3,}`)
)

func (WebFetchTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[WebFetchInput]("WebFetch", input)
	if err != nil {
		return "", err
	}
	if p.URL == "" {
		return "", fmt.Errorf("url is required")
	}
	pinnedURL, origHost, err := ValidateURLPublic(ctx, p.URL)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", pinnedURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "rho/0.1.0")
	// Preserve the original Host header so virtual-host routing works
	// correctly. validateURLPublic pins the connection to the validated
	// IP (preventing DNS rebinding), but replaces the URL host with the IP.
	// Setting req.Host restores the original hostname for the Host header.
	if origHost != "" {
		req.Host = origHost
	}

	client := SSRFSafeClient(ctx, 30*time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 500_000))
	if err != nil {
		return "", err
	}

	text := string(body)
	// Strip HTML tags for a rough text extraction
	if strings.Contains(resp.Header.Get("Content-Type"), "html") {
		text = htmlTagRe.ReplaceAllString(text, " ")
		text = multiSpaceRe.ReplaceAllString(text, "\n")
		text = strings.TrimSpace(text)
	}

	if len(text) > 50000 {
		text = text[:50000] + "\n... (truncated)"
	}
	return fmt.Sprintf("[%d] %s\n\n%s", resp.StatusCode, p.URL, text), nil
}
