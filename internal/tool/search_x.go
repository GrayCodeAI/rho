package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// SearchXTool performs live X/Twitter search by forwarding a query to an
// xAI-compatible chat-completions endpoint with server-side X search enabled,
// returning the model's summarized answer. Adopted from grok-cli's search_x
// (server-side search tooling rather than a bespoke crawler). Inline HTTP,
// boundary-compliant (no flux/client import), httptest-testable.
type SearchXTool struct{}

// SearchXInput is the typed input for SearchXTool.
type SearchXInput struct {
	Query string `json:"query"`
	Model string `json:"model"`
}

func (SearchXTool) Name() string      { return "SearchX" }
func (SearchXTool) RiskLevel() string { return "medium" }
func (SearchXTool) Aliases() []string { return []string{"search_x", "x_search", "xsearch"} }
func (SearchXTool) Description() string {
	return "Search X/Twitter for live posts matching a query. Returns a summarized answer incorporating current X results. Requires XAI_API_KEY."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SearchXTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"query": {Type: "string", Description: "The X/Twitter search query (topic, hashtag, account, etc.)."},
			"model": {Type: "string", Description: "Model to use (default grok-4.20-non-reasoning)."},
		},
		Required: []string{"query"},
	}
}

func (SearchXTool) Parameters() map[string]interface{} {
	return searchXSchema.ToJSONSchema()
}

// searchXSchema is the single source of truth for SearchX's input schema.
var searchXSchema = SearchXTool{}.Schema()

// xAISearchSystemPrompt directs the model to use its built-in X search and
// return a concise, cited summary.
const xAISearchSystemPrompt = "You are a live X/Twitter search assistant. Use your built-in X search tool to find current posts matching the user's query, then summarize the most relevant results with attribution. If X search is unavailable, say so clearly."

// xAISearchBaseURL is the xAI API host; a var so tests can redirect.
var xAISearchBaseURL = "https://api.x.ai"

// xAISearchKey reads the API key (XAI_API_KEY, with GROK_API_KEY fallback).
func xAISearchKey() string {
	if k := os.Getenv("XAI_API_KEY"); k != "" {
		return k
	}
	return os.Getenv("GROK_API_KEY")
}

// xAISearchModel is the default model.
func xAISearchModel() string { return "grok-4.20-non-reasoning" }

func (SearchXTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[SearchXInput]("SearchX", input)
	if err != nil {
		return "", err
	}
	query := strings.TrimSpace(p.Query)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	model := strings.TrimSpace(p.Model)
	if model == "" {
		model = xAISearchModel()
	}
	apiKey := xAISearchKey()
	if apiKey == "" {
		return "", fmt.Errorf("XAI_API_KEY not set — required for X search")
	}

	body, err := json.Marshal(map[string]interface{}{
		"model":      model,
		"max_tokens": 1000,
		"messages": []map[string]string{
			{"role": "system", "content": xAISearchSystemPrompt},
			{"role": "user", "content": query},
		},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		xAISearchBaseURL+"/v1/chat/completions", bytes.NewReader(body)) // #nosec G107 -- fixed API host
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("x search: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("x search API %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("x search decode: %w", err)
	}
	if len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "X search returned no results.", nil
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}
