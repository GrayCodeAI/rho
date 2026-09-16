package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DownloadTool downloads a file from a URL to a local path.
type DownloadTool struct{}

func (DownloadTool) Name() string      { return "Download" }
func (DownloadTool) RiskLevel() string { return "medium" }
func (DownloadTool) Aliases() []string { return []string{"download"} }
func (DownloadTool) Description() string {
	return "Download a file from a URL and save it to a local path."
}

// DownloadInput is the typed input for DownloadTool.
type DownloadInput struct {
	URL         string `json:"url"`
	Destination string `json:"destination"`
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (DownloadTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"url":         {Type: "string", Description: "URL to download from"},
			"destination": {Type: "string", Description: "Local file path to save to"},
		},
	}
}

func (DownloadTool) Parameters() map[string]interface{} {
	return downloadSchema.ToJSONSchema()
}

// downloadSchema is the single source of truth for Download's input schema.
var downloadSchema = DownloadTool{}.Schema()

const maxDownloadSize = 50 * 1024 * 1024 // 50MB

func (DownloadTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[DownloadInput]("Download", input)
	if err != nil {
		return "", err
	}
	if p.URL == "" || p.Destination == "" {
		return "", fmt.Errorf("url and destination are required")
	}
	if err := validatePathAllowed(ctx, p.Destination); err != nil {
		return "", err
	}
	if reason := IsSensitivePath(p.Destination); reason != "" {
		return "", fmt.Errorf("write blocked: %s", reason)
	}
	pinnedURL, origHost, err := ValidateURLPublic(ctx, p.URL)
	if err != nil {
		return "", err
	}

	client := SSRFSafeClient(ctx, 2*time.Minute)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pinnedURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	// Preserve the original Host header for virtual-host routing.
	if origHost != "" {
		req.Host = origHost
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	if resp.ContentLength > maxDownloadSize {
		return "", fmt.Errorf("download exceeds %d byte limit", maxDownloadSize)
	}

	// Read content into memory first so we can scan for credentials before writing.
	body, err := readDownloadBody(resp.Body, maxDownloadSize)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	// Check for credentials in downloaded content (same as FileWriteTool).
	if warn := DetectCredentials(string(body)); warn != "" {
		return "", fmt.Errorf("downloaded content contains potential credentials: %s — write blocked", warn)
	}

	_ = os.MkdirAll(filepath.Dir(p.Destination), 0o750)
	if err := os.WriteFile(p.Destination, body, 0o600); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	ct := resp.Header.Get("Content-Type")
	return fmt.Sprintf("Downloaded %d bytes to %s (type: %s)", len(body), p.Destination, ct), nil
}

func readDownloadBody(r io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("download exceeds %d byte limit", limit)
	}
	return body, nil
}
