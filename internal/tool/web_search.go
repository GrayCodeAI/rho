package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// maxWebSearchConcurrency bounds the number of search backends contacted in
// parallel when a batch of queries is issued, so a large queries[] array does
// not open a burst of outbound connections (and trip provider rate limits).
const (
	maxWebSearchConcurrency = 4
	maxWebSearchQueries     = 20
	maxWebSearchQueryLength = 2000
)

// searchResult is the shared result type for all search backends.
type searchResult struct {
	Title       string
	URL         string
	Description string
}

type WebSearchTool struct{}

func (WebSearchTool) Name() string { return "WebSearch" }

// WebSearchInput is the typed input for WebSearchTool.
type WebSearchInput struct {
	Query      string   `json:"query"`
	Queries    []string `json:"queries"`
	NumResults int      `json:"numResults"`
	SearchType string   `json:"searchType"`
}

func (WebSearchTool) RiskLevel() string { return "low" }
func (WebSearchTool) Aliases() []string { return []string{"web_search"} }
func (WebSearchTool) Description() string {
	return "Search the web and return structured results. Supports Brave Search, SearXNG, DeepSeek, Exa, Perplexity, and DuckDuckGo backends."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (WebSearchTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"query":      {Type: "string", Description: "Search query. Provide this OR queries (not both).", MaxLength: maxWebSearchQueryLength},
			"queries":    {Type: "array", Items: &SchemaProperty{Type: "string", MaxLength: maxWebSearchQueryLength}, MaxItems: maxWebSearchQueries, Description: "Multiple search queries to run concurrently in a single call. Use this to research several things at once instead of issuing one WebSearch per query."},
			"numResults": {Type: "integer", Description: "Number of results to return (1-20)", Minimum: 1, Maximum: 20, Default: 5},
			"searchType": {Type: "string", Description: "Type of search to perform", Enum: []interface{}{"web", "news"}, Default: "web"},
		},
	}
}

func (WebSearchTool) Parameters() map[string]interface{} {
	return webSearchSchema.ToJSONSchema()
}

// webSearchSchema is the single source of truth for WebSearch's input schema.
var webSearchSchema = WebSearchTool{}.Schema()

func (t WebSearchTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[WebSearchInput]("WebSearch", input)
	if err != nil {
		return "", err
	}

	// Normalize the single/batch surfaces into one list of queries.
	queries := p.Queries
	if p.Query != "" {
		queries = append(queries, p.Query)
	}
	if len(queries) == 0 {
		return "", fmt.Errorf("query or queries is required")
	}
	if len(queries) > maxWebSearchQueries {
		return "", fmt.Errorf("at most %d search queries are allowed", maxWebSearchQueries)
	}
	for _, query := range queries {
		if strings.TrimSpace(query) == "" {
			return "", fmt.Errorf("search queries must not be empty")
		}
		if len(query) > maxWebSearchQueryLength {
			return "", fmt.Errorf("search query exceeds %d characters", maxWebSearchQueryLength)
		}
	}

	if p.NumResults <= 0 {
		p.NumResults = 5
	}
	if p.NumResults > 20 {
		p.NumResults = 20
	}
	if p.SearchType == "" {
		p.SearchType = "web"
	}

	// Single query: keep the original output shape (no batch header).
	if len(queries) == 1 {
		out, err := t.searchOne(ctx, queries[0], p.NumResults)
		if err != nil {
			return "", err
		}
		return out, nil
	}

	// Batch: fan out concurrently, bounded, and label each result section so
	// the agent can attribute results to their query in one tool call.
	outputs := make([]string, len(queries))
	var wg sync.WaitGroup
	jobs := make(chan int)
	workerCount := min(maxWebSearchConcurrency, len(queries))
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				query := queries[idx]
				out, err := t.searchOne(ctx, query, p.NumResults)
				if err != nil {
					outputs[idx] = fmt.Sprintf("Search results for: %s\n\nError: %v", query, err)
					continue
				}
				outputs[idx] = out
			}
		}()
	}
	for i := range queries {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	var sb strings.Builder
	for i, out := range outputs {
		if i > 0 {
			sb.WriteString("\n\n---\n\n")
		}
		sb.WriteString(out)
	}
	return sb.String(), nil
}

// searchOne runs a single query through the provider cascade
// (Brave → SearXNG → DeepSeek → Exa → Perplexity → DuckDuckGo) and returns
// formatted results. Structured providers are preferred; DuckDuckGo is the
// final scrape-based fallback.
func (WebSearchTool) searchOne(ctx context.Context, query string, numResults int) (string, error) {
	var results []searchResult
	var err error

	// 1. Brave Search
	brave := newBraveClient()
	if brave.available() {
		results, err = brave.search(ctx, query, numResults)
		if err == nil {
			return formatSearchResults(query, results), nil
		}
		// Fall through to next provider on error
	}

	// 2. SearXNG
	searxng := newSearxngClient()
	if searxng.available() {
		results, err = searxng.search(ctx, query, numResults)
		if err == nil {
			return formatSearchResults(query, results), nil
		}
		// Fall through to the next provider on error
	}

	// 3. DeepSeek native web search (Anthropic-compatible Messages API)
	deepseek := newDeepseekSearchClient()
	if deepseek.available() {
		results, err = deepseek.search(ctx, query, numResults)
		if err == nil {
			return formatSearchResults(query, results), nil
		}
	}

	// 4. Exa Search
	exa := newExaSearchClient()
	if exa.available() {
		results, err = exa.search(ctx, query, numResults)
		if err == nil {
			return formatSearchResults(query, results), nil
		}
	}

	// 5. Perplexity Search
	perplexity := newPerplexitySearchClient()
	if perplexity.available() {
		results, err = perplexity.search(ctx, query, numResults)
		if err == nil {
			return formatSearchResults(query, results), nil
		}
	}

	// 6. DuckDuckGo (final fallback)
	results, err = duckDuckGoSearch(ctx, query, numResults)
	if err != nil {
		return "", fmt.Errorf("all search providers failed: %w", err)
	}
	return formatSearchResults(query, results), nil
}

// formatSearchResults formats results as numbered structured text.
func formatSearchResults(query string, results []searchResult) string {
	if len(results) == 0 {
		return fmt.Sprintf("Search results for: %s\n\nNo results found.", query)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for: %s\n\n", query))

	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. Title: %s\n", i+1, r.Title))
		sb.WriteString(fmt.Sprintf("   URL: %s\n", r.URL))
		sb.WriteString(fmt.Sprintf("   Description: %s\n", r.Description))
		if i < len(results)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// duckDuckGoSearch scrapes DuckDuckGo HTML results as a fallback.
func duckDuckGoSearch(ctx context.Context, query string, count int) ([]searchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	u := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "rho/0.1.0")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 200_000))
	if err != nil {
		return nil, err
	}

	return parseDuckDuckGoHTML(string(body), count), nil
}

// parseDuckDuckGoHTML extracts search results from DuckDuckGo HTML response.
func parseDuckDuckGoHTML(html string, count int) []searchResult {
	var results []searchResult

	// Split on result links - DDG uses class="result__a" for result titles
	parts := strings.Split(html, "class=\"result__a\"")
	if len(parts) <= 1 {
		// Fallback: try to extract plain text lines
		return parseDuckDuckGoPlainText(html, count)
	}

	for i := 1; i < len(parts) && len(results) < count; i++ {
		var r searchResult

		// Extract href (URL)
		hrefIdx := strings.Index(parts[i], "href=\"")
		if hrefIdx == -1 {
			// Look backward in the tag opening
			prefix := parts[i-1]
			hrefIdx = strings.LastIndex(prefix, "href=\"")
			if hrefIdx != -1 {
				hrefEnd := strings.Index(prefix[hrefIdx+6:], "\"")
				if hrefEnd != -1 {
					rawURL := prefix[hrefIdx+6 : hrefIdx+6+hrefEnd]
					r.URL = extractDDGURL(rawURL)
				}
			}
		} else {
			hrefEnd := strings.Index(parts[i][hrefIdx+6:], "\"")
			if hrefEnd != -1 {
				rawURL := parts[i][hrefIdx+6 : hrefIdx+6+hrefEnd]
				r.URL = extractDDGURL(rawURL)
			}
		}

		// Extract title (text between > and </a>)
		closeTag := strings.Index(parts[i], ">")
		endAnchor := strings.Index(parts[i], "</a>")
		if closeTag != -1 && endAnchor != -1 && closeTag < endAnchor {
			title := parts[i][closeTag+1 : endAnchor]
			title = htmlTagRe.ReplaceAllString(title, "")
			r.Title = strings.TrimSpace(title)
		}

		// Extract description/snippet - DDG uses class="result__snippet"
		snippetIdx := strings.Index(parts[i], "class=\"result__snippet\"")
		if snippetIdx != -1 {
			after := parts[i][snippetIdx:]
			closeSnip := strings.Index(after, ">")
			endSnip := strings.Index(after, "</a>")
			if endSnip == -1 {
				endSnip = strings.Index(after, "</td>")
			}
			if endSnip == -1 {
				endSnip = strings.Index(after, "</span>")
			}
			if closeSnip != -1 && endSnip != -1 && closeSnip < endSnip {
				desc := after[closeSnip+1 : endSnip]
				desc = htmlTagRe.ReplaceAllString(desc, "")
				r.Description = strings.TrimSpace(desc)
			}
		}

		if r.Title != "" || r.URL != "" {
			results = append(results, r)
		}
	}

	return results
}

// extractDDGURL extracts the actual URL from DuckDuckGo's redirect URL.
func extractDDGURL(rawURL string) string {
	// DDG often wraps URLs like //duckduckgo.com/l/?uddg=<encoded_url>&...
	if strings.Contains(rawURL, "uddg=") {
		parts := strings.SplitN(rawURL, "uddg=", 2)
		if len(parts) == 2 {
			ampIdx := strings.Index(parts[1], "&")
			encoded := parts[1]
			if ampIdx != -1 {
				encoded = parts[1][:ampIdx]
			}
			decoded, err := url.QueryUnescape(encoded)
			if err == nil {
				return decoded
			}
		}
	}
	// Clean up protocol-relative URLs
	if strings.HasPrefix(rawURL, "//") {
		return "https:" + rawURL
	}
	return rawURL
}

// parseDuckDuckGoPlainText is a last-resort parser that strips HTML and returns lines.
func parseDuckDuckGoPlainText(html string, count int) []searchResult {
	text := htmlTagRe.ReplaceAllString(html, " ")
	text = multiSpaceRe.ReplaceAllString(text, "\n")

	var lines []string
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && len(l) > 20 {
			lines = append(lines, l)
			if len(lines) >= count {
				break
			}
		}
	}

	var results []searchResult
	for _, l := range lines {
		results = append(results, searchResult{
			Title:       l,
			URL:         "",
			Description: "",
		})
	}
	return results
}
