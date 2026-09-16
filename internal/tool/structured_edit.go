package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// StructuredEditTool applies search-and-replace blocks (structured diffs) to files.
// This is more precise and token-efficient than whole-file writes.
// Inspired by Aider's search/replace edit format.
type StructuredEditTool struct{}

func (StructuredEditTool) Name() string      { return "StructuredEdit" }
func (StructuredEditTool) RiskLevel() string { return "medium" }
func (StructuredEditTool) Aliases() []string { return []string{"sed", "search_replace"} }

func (StructuredEditTool) Description() string {
	return "Apply search-and-replace edits to a file. Provide one or more SEARCH/REPLACE blocks. Each block finds exact text and replaces it. The SEARCH text must match the file contents exactly, including whitespace."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (StructuredEditTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"path": {Type: "string", Description: "Path to the file to edit"},
			"blocks": {
				Type:        "array",
				Description: "List of SEARCH/REPLACE blocks",
				Items: &SchemaProperty{
					Type: "object",
					Properties: map[string]SchemaProperty{
						"search":  {Type: "string", Description: "Exact text to find. Must match the file exactly, including whitespace."},
						"replace": {Type: "string", Description: "Text to replace the search text with."},
					},
					Required: []string{"search", "replace"},
				},
			},
			"auto_format": {Type: "boolean", Description: "If true, ignore insignificant whitespace differences when matching (default: false)"},
		},
		Required: []string{"path", "blocks"},
	}
}

func (StructuredEditTool) Parameters() map[string]interface{} {
	return structuredEditSchema.ToJSONSchema()
}

// structuredEditSchema is the single source of truth for StructuredEdit's input schema.
var structuredEditSchema = StructuredEditTool{}.Schema()

// StructuredEditInput is the typed input for StructuredEditTool.
// Aliased from the legacy unexported name.
type StructuredEditInput = structuredEditInput

type structuredEditInput struct {
	Path       string               `json:"path"`
	Blocks     []searchReplaceBlock `json:"blocks"`
	AutoFormat bool                 `json:"auto_format"`
}

type searchReplaceBlock struct {
	Search  string `json:"search"`
	Replace string `json:"replace"`
}

func (s StructuredEditTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[structuredEditInput]("StructuredEdit", input)
	if err != nil {
		return "", err
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if err := validatePathAllowed(ctx, p.Path); err != nil {
		return "", err
	}
	if len(p.Blocks) == 0 {
		return "", fmt.Errorf("at least one SEARCH/REPLACE block is required")
	}
	if err := validatePathAllowed(ctx, p.Path); err != nil {
		return "", err
	}

	// Read the file.
	data, err := readGuardedFile(ctx, p.Path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p.Path, err)
	}
	content := string(data)

	// Apply each block in order.
	applied := 0
	skipped := 0
	for _, block := range p.Blocks {
		search := block.Search
		replace := block.Replace

		if p.AutoFormat {
			search = normalizeWhitespace(search)
		}

		if !strings.Contains(content, search) {
			skipped++
			continue
		}

		if strings.Count(content, search) > 1 {
			return "", fmt.Errorf("search text found %d times in %s — please provide more context to make the match unique", strings.Count(content, search), p.Path)
		}

		content = strings.Replace(content, search, replace, 1)
		applied++
	}

	if applied == 0 {
		return "", fmt.Errorf("no SEARCH/REPLACE blocks matched in %s (%d blocks skipped)", p.Path, skipped)
	}

	// Write the result.
	if err := writeGuardedFile(ctx, p.Path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", p.Path, err)
	}

	msg := fmt.Sprintf("Applied %d block(s) to %s", applied, p.Path)
	if skipped > 0 {
		msg += fmt.Sprintf(" (%d block(s) skipped — no match found)", skipped)
	}
	if autoCommitEnabled(ctx) {
		if err := AutoCommit(ctx, p.Path, "StructuredEdit", msg); err != nil {
			slog.Warn("auto-commit failed", "path", p.Path, "error", err)
		}
	}
	return msg, nil
}
