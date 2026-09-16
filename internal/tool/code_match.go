package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CodeMatchTool performs structural, AST-level code search using tree-sitter
// query patterns — the rho counterpart to cocoindex-code's by-example
// structural grep. Patterns are tree-sitter query S-expressions over the
// language grammar, e.g. for Go:
//
//	(function_declaration name: (identifier) @name) @fn
//
// Captures (@name) surface per match so callers extract identifiers without
// regex fragility. Unlike Grep (textual), matches are syntax-aware: comments
// and strings cannot produce false positives.
type CodeMatchTool struct{}

// CodeMatchInput is the typed input for CodeMatchTool.
type CodeMatchInput struct {
	Pattern  string `json:"pattern"`
	Path     string `json:"path"`
	Language string `json:"language"`
	Limit    int    `json:"limit"`
}

func (CodeMatchTool) Name() string      { return "CodeMatch" }
func (CodeMatchTool) RiskLevel() string { return "low" }
func (CodeMatchTool) Aliases() []string { return []string{"code_match", "match_code"} }

func (CodeMatchTool) Description() string {
	return `Structural code search with tree-sitter query patterns. Matches the AST, not text: comments/strings cannot false-positive. Pattern is a tree-sitter query S-expression; use @captures to extract parts. Examples - Go functions: "(function_declaration name: (identifier) @name) @fn" | Go calls of one function: "(call_expression function: (identifier) @callee) @call" | Python defs: "(function_definition name: (identifier) @name) @fn". Language auto-detects per file; restrict with language=go|python|typescript|tsx.`
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (CodeMatchTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"pattern":  {Type: "string", Description: "Tree-sitter query pattern (S-expression). Captures (@name) are returned per match."},
			"path":     {Type: "string", Description: "Project directory (default: session working directory)."},
			"language": {Type: "string", Enum: []interface{}{"go", "python", "typescript", "tsx"}, Description: "Restrict to one language (default: all supported)."},
			"limit":    {Type: "integer", Minimum: 1, Maximum: 200, Description: "Maximum matches total (default 30)."},
		},
		Required: []string{"pattern"},
	}
}

func (CodeMatchTool) Parameters() map[string]interface{} {
	return codeMatchSchema.ToJSONSchema()
}

// codeMatchSchema is the single source of truth for CodeMatch's input schema.
var codeMatchSchema = CodeMatchTool{}.Schema()

// codeMatchHit is one structural match.
type codeMatchHit struct {
	File      string   `json:"file"`
	StartLine int      `json:"start_line"`
	EndLine   int      `json:"end_line"`
	Captures  []string `json:"captures,omitempty"`
	Snippet   string   `json:"snippet"`
}

// codeMatchLanguages maps file extensions to supported grammar names.
var codeMatchExtLang = map[string]string{
	".go": "go", ".py": "python",
	".ts": "typescript", ".tsx": "tsx", ".mts": "typescript", ".cts": "typescript",
}

func (CodeMatchTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	params, err := DecodeInput[CodeMatchInput]("CodeMatch", input)
	if err != nil {
		return "", err
	}
	params.Pattern = strings.TrimSpace(params.Pattern)
	if params.Pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	params.Language = strings.ToLower(strings.TrimSpace(params.Language))

	root := params.Path
	if root == "" {
		if tc := GetToolContext(ctx); tc != nil && tc.WorkingDir != "" {
			root = tc.WorkingDir
		} else {
			var err error
			root, err = os.Getwd()
			if err != nil {
				return "", fmt.Errorf("resolve working directory: %w", err)
			}
		}
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve project path: %w", err)
	}
	if err := validatePathAllowed(ctx, absRoot); err != nil {
		return "", err
	}
	if params.Limit <= 0 {
		params.Limit = 30
	}
	if params.Limit > 200 {
		params.Limit = 200
	}

	return runCodeMatch(ctx, absRoot, params.Pattern, params.Language, params.Limit)
}
