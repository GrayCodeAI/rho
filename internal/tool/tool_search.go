package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type ToolSearchTool struct{}

// ToolSearchInput is the typed input for ToolSearchTool.
type ToolSearchInput struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
}

func (ToolSearchTool) Name() string      { return "ToolSearch" }
func (ToolSearchTool) RiskLevel() string { return "low" }
func (ToolSearchTool) Aliases() []string { return []string{"tool_search"} }
func (ToolSearchTool) Description() string {
	return `Search available tools by name or description. Use query "select:<tool_name>" for direct selection.`
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (ToolSearchTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"query":       {Type: "string", Description: `Search terms, or "select:<tool_name>"`},
			"max_results": {Type: "integer", Description: "Maximum results to return (default 5)"},
		},
		Required: []string{"query"},
	}
}

func (ToolSearchTool) Parameters() map[string]interface{} {
	return toolSearchSchema.ToJSONSchema()
}

// toolSearchSchema is the single source of truth for ToolSearch's input schema.
var toolSearchSchema = ToolSearchTool{}.Schema()

func (ToolSearchTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[ToolSearchInput]("ToolSearch", input)
	if err != nil {
		return "", err
	}
	p.Query = strings.TrimSpace(p.Query)
	if p.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if p.MaxResults <= 0 {
		p.MaxResults = 5
	}
	tc := GetToolContext(ctx)
	if tc == nil || len(tc.AvailableTools) == 0 {
		return `{"matches":[],"query":` + quoteJSONString(p.Query) + `,"total_tools":0}`, nil
	}

	matches := searchAvailableTools(tc.AvailableTools, p.Query, p.MaxResults)
	// select:<name> promotes hidden optional tools onto the model surface.
	if strings.HasPrefix(strings.ToLower(p.Query), "select:") && tc.Registry != nil {
		for _, name := range matches {
			_ = tc.Registry.PromoteModelTool(name)
		}
	}
	out := map[string]interface{}{
		"matches":     matches,
		"query":       p.Query,
		"total_tools": len(tc.AvailableTools),
		"promoted":    strings.HasPrefix(strings.ToLower(p.Query), "select:"),
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	return string(data), nil
}

func searchAvailableTools(tools []Tool, query string, maxResults int) []string {
	if selectArg, ok := strings.CutPrefix(strings.ToLower(query), "select:"); ok {
		requested := splitSelectTools(selectArg)
		var matches []string
		for _, want := range requested {
			for _, t := range tools {
				if toolMatchesName(t, want) {
					matches = appendUnique(matches, t.Name())
					break
				}
			}
		}
		return matches
	}

	type scored struct {
		name  string
		score int
	}
	terms := strings.Fields(strings.ToLower(query))
	var scoredMatches []scored
	for _, t := range tools {
		haystack := strings.ToLower(t.Name() + " " + strings.Join(toolAliases(t), " ") + " " + t.Description())
		score := 0
		for _, term := range terms {
			if strings.Contains(strings.ToLower(t.Name()), term) {
				score += 4
			}
			if strings.Contains(haystack, term) {
				score++
			}
		}
		if score > 0 {
			scoredMatches = append(scoredMatches, scored{name: t.Name(), score: score})
		}
	}
	sort.Slice(scoredMatches, func(i, j int) bool {
		if scoredMatches[i].score == scoredMatches[j].score {
			return scoredMatches[i].name < scoredMatches[j].name
		}
		return scoredMatches[i].score > scoredMatches[j].score
	})
	if len(scoredMatches) > maxResults {
		scoredMatches = scoredMatches[:maxResults]
	}
	matches := make([]string, 0, len(scoredMatches))
	for _, m := range scoredMatches {
		matches = append(matches, m.name)
	}
	return matches
}

func splitSelectTools(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func toolMatchesName(t Tool, name string) bool {
	if strings.EqualFold(t.Name(), name) {
		return true
	}
	for _, alias := range toolAliases(t) {
		if strings.EqualFold(alias, name) {
			return true
		}
	}
	return false
}

func toolAliases(t Tool) []string {
	aliased, ok := t.(AliasedTool)
	if !ok {
		return nil
	}
	return aliased.Aliases()
}

func appendUnique(values []string, next string) []string {
	for _, value := range values {
		if value == next {
			return values
		}
	}
	return append(values, next)
}

func quoteJSONString(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}
