package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/GrayCodeAI/rho/internal/sessionquery"
)

// SessionQueryTool enables the agent to search past and current conversation sessions via FTS5.
type SessionQueryTool struct {
	Service *sessionquery.Service
}

func (SessionQueryTool) Name() string { return "SessionQuery" }

// SessionQueryInput is the typed input for SessionQueryTool.
type SessionQueryInput struct {
	Query     string `json:"query"`
	SessionID string `json:"session_id"`
	Workspace string `json:"workspace"`
	Limit     int    `json:"limit"`
	Offset    int    `json:"offset"`
}

func (SessionQueryTool) Aliases() []string { return []string{"session_query", "session-query"} }
func (SessionQueryTool) Description() string {
	return "Search conversation history across past and current sessions using SQLite full-text search."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SessionQueryTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"query":      {Type: "string", Description: "Full-text search query to find relevant past conversations"},
			"session_id": {Type: "string", Description: "Optional specific session ID to filter search within"},
			"workspace":  {Type: "string", Description: "Optional workspace directory path to filter sessions within"},
			"limit":      {Type: "integer", Description: "Maximum number of search results to return (default 10, max 50)"},
			"offset":     {Type: "integer", Description: "Pagination offset for scrolling through results (default 0)"},
		},
		Required: []string{"query"},
	}
}

func (SessionQueryTool) Parameters() map[string]interface{} {
	return sessionQuerySchema.ToJSONSchema()
}

// sessionQuerySchema is the single source of truth for SessionQuery's input schema.
var sessionQuerySchema = SessionQueryTool{}.Schema()

func (t SessionQueryTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var p SessionQueryInput
	if len(input) > 0 && string(input) != "null" {
		decoded, err := DecodeInput[SessionQueryInput]("SessionQuery", input)
		if err != nil {
			return "", err
		}
		p = decoded
	}

	if strings.TrimSpace(p.Query) == "" {
		return "", fmt.Errorf("query parameter is required")
	}

	svc := t.Service
	if svc == nil {
		var err error
		svc, err = sessionquery.DefaultService()
		if err != nil {
			return "", fmt.Errorf("failed to initialize session query service: %w", err)
		}
	}

	cwd, _ := os.Getwd()
	callerWs := cwd
	if p.Workspace != "" {
		callerWs = p.Workspace
	}

	res, err := svc.Search(ctx, sessionquery.SearchParams{
		CallerWorkspace: callerWs,
		Workspace:       p.Workspace,
		SessionID:       p.SessionID,
		Query:           p.Query,
		Limit:           p.Limit,
		Offset:          p.Offset,
	})
	if err != nil {
		return "", err
	}

	if len(res.Matches) == 0 {
		return fmt.Sprintf("No matching messages found for query: %q", p.Query), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d match(es) for query %q (showing %d–%d):\n\n",
		res.TotalCount, p.Query, res.Offset+1, res.Offset+len(res.Matches)))

	for _, m := range res.Matches {
		sb.WriteString(fmt.Sprintf("### Session `%s` (Message #%d, Role: %s)\n", m.SessionID, m.MsgIndex, m.Role))
		if m.Workspace != "" {
			sb.WriteString(fmt.Sprintf("**Workspace:** `%s`\n", m.Workspace))
		}
		if m.Snippet != "" {
			sb.WriteString(fmt.Sprintf("**Match:** %s\n\n", m.Snippet))
		} else {
			sb.WriteString(fmt.Sprintf("**Content:** %s\n\n", m.Content))
		}
	}

	if res.HasMore {
		sb.WriteString(fmt.Sprintf("*(More results available. Use offset=%d to fetch the next page)*\n", res.Offset+len(res.Matches)))
	}

	return strings.TrimRight(sb.String(), "\n"), nil
}
