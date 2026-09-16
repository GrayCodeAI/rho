package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// CodeSearchTool searches the codebase semantically.
type CodeSearchTool struct{}

// CodeSearchInput is the typed input for CodeSearchTool.
type CodeSearchInput struct {
	Query    string `json:"query"`
	Limit    int    `json:"limit"`
	Language string `json:"language"`
	Refresh  bool   `json:"refresh"`
}

func (CodeSearchTool) Name() string      { return "CodeSearch" }
func (CodeSearchTool) RiskLevel() string { return "low" }

func (CodeSearchTool) Aliases() []string { return []string{"code_search", "search_code"} }

func (CodeSearchTool) Description() string {
	return `Semantic code search over the local code index. Use this instead of Grep when you need to find implementations by meaning, not exact text. Start with limit=5; if results look relevant, use offset to paginate. Set refresh=true to update the index first.`
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (CodeSearchTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"query":    {Type: "string", Description: "The semantic search query describing what you are looking for."},
			"limit":    {Type: "integer", Description: "Maximum number of results to return (default 5)."},
			"language": {Type: "string", Description: "Optional language filter (e.g. go, python, typescript)."},
			"refresh":  {Type: "boolean", Description: "If true, refresh the code index before searching."},
		},
		Required: []string{"query"},
	}
}

func (CodeSearchTool) Parameters() map[string]interface{} {
	return codeSearchSchema.ToJSONSchema()
}

// codeSearchSchema is the single source of truth for CodeSearch's input schema.
var codeSearchSchema = CodeSearchTool{}.Schema()

func (CodeSearchTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	params, err := DecodeInput[CodeSearchInput]("CodeSearch", input)
	if err != nil {
		return "", err
	}

	if params.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if params.Limit <= 0 {
		params.Limit = 5
	}

	tc := GetToolContext(ctx)
	if tc == nil || tc.CodeSearchFn == nil {
		return "Code search is not available in this session.", nil
	}

	if params.Refresh && tc.RefreshCodeIndexFn != nil {
		_ = tc.RefreshCodeIndexFn(ctx)
	}

	results, err := tc.CodeSearchFn(ctx, params.Query, params.Limit)
	if err != nil {
		return "", fmt.Errorf("code search failed: %w", err)
	}

	if len(results) == 0 {
		return "No results found.", nil
	}

	// Filter by language if specified
	if params.Language != "" {
		lang := strings.ToLower(params.Language)
		var filtered []CodeSearchResult
		for _, r := range results {
			if strings.ToLower(r.Language) == lang {
				filtered = append(filtered, r)
			}
		}
		results = filtered
		if len(results) == 0 {
			return fmt.Sprintf("No results found for language %q.", params.Language), nil
		}
	}

	// Format results
	var sb strings.Builder
	for i, r := range results {
		if i > 0 {
			sb.WriteString("\n---\n")
		}
		lang := r.Language
		if lang == "" {
			lang = "text"
		}
		sb.WriteString(fmt.Sprintf("%s:%d-%d\n```%s\n%s\n```\n", r.Path, r.StartLine, r.EndLine, lang, r.Content))
	}

	return sb.String(), nil
}
