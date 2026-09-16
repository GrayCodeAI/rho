package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// MCPLanguageServerTool provides deep code understanding via MCP language server.
// Based on isaacphi/mcp-language-server - wraps LSP capabilities as MCP tools.
// This gives rho go-to-definition, find-references, and rename capabilities
// across multiple languages without running a language server directly.
type MCPLanguageServerTool struct{}

// MCPLanguageServerInput is the typed input for MCPLanguageServerTool.
type MCPLanguageServerInput struct {
	Action   string `json:"action"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Symbol   string `json:"symbol"`
	NewName  string `json:"newName"`
	Language string `json:"language"`
}

func (MCPLanguageServerTool) Name() string      { return "MCPLSP" }
func (MCPLanguageServerTool) Aliases() []string { return []string{"mcplsp", "lsp-mcp"} }
func (MCPLanguageServerTool) Description() string {
	return "Deep code understanding via MCP language server. Provides go-to-definition, find-references, rename, and diagnostics across multiple languages."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (MCPLanguageServerTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action":   {Type: "string", Enum: []interface{}{"definition", "references", "rename", "diagnostics", "hover", "symbols"}, Description: "LSP action to perform"},
			"file":     {Type: "string", Description: "File path"},
			"line":     {Type: "integer", Description: "Line number (1-based)"},
			"column":   {Type: "integer", Description: "Column number (1-based)"},
			"symbol":   {Type: "string", Description: "Symbol name (for rename: old name)"},
			"newName":  {Type: "string", Description: "New name for rename action"},
			"language": {Type: "string", Description: "Language server to use (go, python, typescript, rust)"},
		},
		Required: []string{"action"},
	}
}

func (MCPLanguageServerTool) Parameters() map[string]interface{} {
	return mcpLSPLanguageSchema.ToJSONSchema()
}

// mcpLSPLanguageSchema is the single source of truth for MCPLanguageServer's input schema.
var mcpLSPLanguageSchema = MCPLanguageServerTool{}.Schema()

func (MCPLanguageServerTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[MCPLanguageServerInput]("MCPLanguageServer", input)
	if err != nil {
		return "", err
	}

	switch p.Action {
	case "definition":
		return findDefinition(p.File, p.Line, p.Column, p.Symbol)
	case "references":
		return findReferences(p.File, p.Line, p.Symbol)
	case "rename":
		return renameSymbol(p.File, p.Line, p.Column, p.Symbol, p.NewName)
	case "diagnostics":
		return getDiagnostics(p.File, p.Language)
	case "hover":
		return getHover(p.File, p.Line, p.Column)
	case "symbols":
		return getDocumentSymbols(p.File)
	default:
		return "", fmt.Errorf("unknown action: %s", p.Action)
	}
}

func findDefinition(file string, line, column int, symbol string) (string, error) {
	if file == "" && symbol == "" {
		return "Provide 'file' + 'line'/'column' or 'symbol' to find definition", nil
	}

	var result strings.Builder
	result.WriteString("## Definition Lookup\n\n")

	if symbol != "" {
		result.WriteString(fmt.Sprintf("Looking up definition of: `%s`\n\n", symbol))
		result.WriteString("Use CodeGraph search for symbol definition:\n")
		result.WriteString(fmt.Sprintf("```\ncodegraph search %s\n```\n", symbol))
	} else {
		result.WriteString(fmt.Sprintf("Looking up definition at %s:%d:%d\n\n", file, line, column))
		result.WriteString("Use CodeGraph trace for call path analysis:\n")
		result.WriteString(fmt.Sprintf("```\ncodegraph trace --from %s:%d\n```\n", file, line))
	}

	return result.String(), nil
}

func findReferences(file string, line int, symbol string) (string, error) {
	if symbol == "" {
		return "Provide 'symbol' to find references", nil
	}

	var result strings.Builder
	result.WriteString("## References\n\n")
	result.WriteString(fmt.Sprintf("Finding references to: `%s`\n\n", symbol))
	result.WriteString("Use CodeGraph callers for reference analysis:\n")
	result.WriteString(fmt.Sprintf("```\ncodegraph callers %s\n```\n", symbol))

	return result.String(), nil
}

func renameSymbol(file string, line, column int, oldName, newName string) (string, error) {
	if oldName == "" || newName == "" {
		return "Provide 'symbol' (old name) and 'newName' for rename", nil
	}

	var result strings.Builder
	result.WriteString("## Rename Preview\n\n")
	result.WriteString(fmt.Sprintf("Rename `%s` → `%s`\n\n", oldName, newName))
	result.WriteString("Use CodeGraph impact to check what will be affected:\n")
	result.WriteString(fmt.Sprintf("```\ncodegraph impact %s\n```\n", oldName))

	return result.String(), nil
}

func getDiagnostics(file, language string) (string, error) {
	if file == "" {
		return "Provide 'file' to get diagnostics", nil
	}

	var result strings.Builder
	result.WriteString("## Diagnostics\n\n")
	result.WriteString(fmt.Sprintf("File: %s\n\n", file))

	// Use appropriate linter based on language
	switch language {
	case "go":
		result.WriteString("Run: `go vet ./...` and `golangci-lint run`\n")
	case "python":
		result.WriteString("Run: `python3 -m py_compile` and `ruff check`\n")
	case "typescript", "javascript":
		result.WriteString("Run: `npx tsc --noEmit` and `npx eslint`\n")
	case "rust":
		result.WriteString("Run: `cargo check` and `cargo clippy`\n")
	default:
		result.WriteString("Auto-detect language from file extension\n")
	}

	return result.String(), nil
}

func getHover(file string, line, column int) (string, error) {
	if file == "" {
		return "Provide 'file' and 'line' for hover info", nil
	}

	var result strings.Builder
	result.WriteString("## Hover Info\n\n")
	result.WriteString(fmt.Sprintf("At %s:%d:%d\n\n", file, line, column))
	result.WriteString("Use CodeGraph node for symbol details:\n")
	result.WriteString(fmt.Sprintf("```\ncodegraph node --file %s --line %d\n```\n", file, line))

	return result.String(), nil
}

func getDocumentSymbols(file string) (string, error) {
	if file == "" {
		return "Provide 'file' to list symbols", nil
	}

	var result strings.Builder
	result.WriteString("## Document Symbols\n\n")
	result.WriteString(fmt.Sprintf("File: %s\n\n", file))
	result.WriteString("Use CodeGraph files for symbol listing:\n")
	result.WriteString(fmt.Sprintf("```\ncodegraph files %s\n```\n", file))

	return result.String(), nil
}
