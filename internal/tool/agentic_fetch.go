package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	agentcontracts "github.com/GrayCodeAI/rho/internal/contracts/agent"
)

func agentSpawnExplore(prompt string) agentcontracts.SpawnRequest {
	return agentcontracts.SpawnRequest{
		Prompt:       prompt,
		SubagentType: string(agentcontracts.TypeExplore),
	}
}

func spawnOutput(res agentcontracts.SpawnResult) string {
	if res.Output != "" {
		return res.Output
	}
	return res.Summary
}

// maxAgenticFetchConcurrency bounds how many pages are fetched and summarized
// in parallel when a batch of URLs is supplied, matching the WebSearch cap so
// the SSRF-aware fetch client does not face a connection storm.
const maxAgenticFetchConcurrency = 4

// AgenticFetchTool spawns a sub-agent to fetch, process, and summarize web content.
type AgenticFetchTool struct{}

func (AgenticFetchTool) Name() string      { return "AgenticFetch" }
func (AgenticFetchTool) RiskLevel() string { return "low" }
func (AgenticFetchTool) Aliases() []string { return []string{"agentic_fetch", "research"} }
func (AgenticFetchTool) Description() string {
	return "Fetch and intelligently summarize web content using a sub-agent. Better than raw WebFetch for research — the sub-agent extracts only the relevant information."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (AgenticFetchTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"url":   {Type: "string", Description: "URL to fetch and analyze. Provide this OR urls (not both)."},
			"urls":  {Type: "array", Items: &SchemaProperty{Type: "string"}, Description: "Multiple URLs to fetch and summarize concurrently against the same query, in a single call."},
			"query": {Type: "string", Description: "What to look for or extract from the page(s)"},
		},
	}
}

func (AgenticFetchTool) Parameters() map[string]interface{} {
	return agenticFetchSchema.ToJSONSchema()
}

// agenticFetchSchema is the single source of truth for AgenticFetch's input schema.
var agenticFetchSchema = AgenticFetchTool{}.Schema()

// AgenticFetchInput is the typed input for AgenticFetchTool.
type AgenticFetchInput struct {
	URL   string   `json:"url"`
	URLs  []string `json:"urls"`
	Query string   `json:"query"`
}

func (t AgenticFetchTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[AgenticFetchInput]("AgenticFetch", input)
	if err != nil {
		return "", err
	}

	urls := p.URLs
	if p.URL != "" {
		urls = append(urls, p.URL)
	}
	if len(urls) == 0 {
		return "", fmt.Errorf("url or urls is required")
	}
	if p.Query == "" {
		p.Query = "Extract the key information from this page."
	}

	tc := GetToolContext(ctx)
	if tc == nil || tc.AgentSpawnFn == nil {
		// Fallback: do a direct fetch if no sub-agent available. Only the
		// single-URL case maps onto WebFetch's input shape.
		if len(urls) == 1 {
			fetchInput, _ := json.Marshal(map[string]string{"url": urls[0]})
			return (WebFetchTool{}).Execute(ctx, fetchInput)
		}
		return "", fmt.Errorf("batch agentic fetch requires a sub-agent, which is unavailable in this context")
	}

	if len(urls) == 1 {
		res, err := tc.AgentSpawnFn(ctx, agentSpawnExplore(t.researchPrompt(urls[0], p.Query)))
		if err != nil {
			return "", err
		}
		return spawnOutput(res), nil
	}

	// Batch: summarize each URL concurrently, bounded, with labeled sections.
	outputs := make([]string, len(urls))
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxAgenticFetchConcurrency)
	for i, u := range urls {
		wg.Add(1)
		go func(idx int, u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res, err := tc.AgentSpawnFn(ctx, agentSpawnExplore(t.researchPrompt(u, p.Query)))
			if err != nil {
				outputs[idx] = fmt.Sprintf("## %s\n\nError: %v", u, err)
				return
			}
			outputs[idx] = fmt.Sprintf("## %s\n\n%s", u, spawnOutput(res))
		}(i, u)
	}
	wg.Wait()

	return strings.Join(outputs, "\n\n---\n\n"), nil
}

// researchPrompt builds the sub-agent instruction for one URL. It includes an
// explicit relevance-refusal contract: if the page does not address the query,
// the sub-agent returns a short "NO_RELEVANT_INFORMATION" signal instead of
// padding a vague summary, keeping the caller's context clean.
func (AgenticFetchTool) researchPrompt(url, query string) string {
	return fmt.Sprintf(`You are a web research agent. Your task:
1. Fetch the URL: %s
2. Extract information relevant to: %s
3. If the page genuinely does not contain information relevant to the query,
   respond with exactly "NO_RELEVANT_INFORMATION" and one short sentence on why
   — do not invent or pad a summary.
4. Otherwise, return a concise, well-structured summary of the relevant content,
   including specific details, code examples, or data points — not vague descriptions.

Use the WebFetch tool to retrieve the page content, then analyze and summarize it.`, url, query)
}
