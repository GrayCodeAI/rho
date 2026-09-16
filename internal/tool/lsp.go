package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/GrayCodeAI/rho/internal/codegraph"
	"github.com/GrayCodeAI/rho/internal/lsp"
)

type LSPTool struct {
	Manager *lsp.LSPManager
}

// LSPInput is the typed input for LSPTool.
type LSPInput struct {
	Action string `json:"action"`
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Symbol string `json:"symbol"`
	Root   string `json:"root"`
}

func (LSPTool) Name() string      { return "LSP" }
func (LSPTool) Aliases() []string { return []string{"lsp"} }
func (LSPTool) Description() string {
	return "Get code intelligence through configured language servers, with codegraph and local-tool fallbacks."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (LSPTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action": {Type: "string", Enum: []interface{}{"diagnostics", "definition", "references", "implementations"}, Description: "LSP action"},
			"path":   {Type: "string", Description: "File path"},
			"line":   {Type: "integer", Description: "Line number (1-based)"},
			"column": {Type: "integer", Description: "Column number (1-based)"},
			"symbol": {Type: "string", Description: "Symbol name to look up (alternative to line/column)"},
			"root":   {Type: "string", Description: "Project root directory (default: current dir)"},
		},
		Required: []string{"action", "path"},
	}
}

func (LSPTool) Parameters() map[string]interface{} {
	return lspSchema.ToJSONSchema()
}

// lspSchema is the single source of truth for LSP's input schema.
var lspSchema = LSPTool{}.Schema()

func (t LSPTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[LSPInput]("LSP", input)
	if err != nil {
		return "", err
	}

	root := p.Root
	if root == "" {
		root = "."
	}
	if t.Manager != nil {
		if result, ok := t.executeLanguageServer(ctx, p.Action, p.Path, p.Line, p.Column); ok {
			return result, nil
		}
	}

	switch p.Action {
	case "diagnostics":
		return lspDiagnostics(ctx, p.Path)
	case "definition":
		return lspDefinition(root, p.Path, p.Line, p.Symbol)
	case "references":
		return lspReferences(root, p.Path, p.Line, p.Symbol)
	case "implementations":
		return lspImplementations(root, p.Symbol)
	default:
		return "", fmt.Errorf("unknown LSP action: %s", p.Action)
	}
}

func (t LSPTool) executeLanguageServer(ctx context.Context, action, path string, line, column int) (string, bool) {
	lang, _, ok := t.Manager.Config().ServerForFile(path)
	if !ok || path == "" {
		return "", false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	text, err := os.ReadFile(absPath) // #nosec G304 -- workspace path supplied to the LSP tool
	if err != nil {
		return "", false
	}
	if line < 1 {
		line = 1
	}
	if column < 1 {
		column = 1
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absPath)}).String()
	var result string
	err = t.Manager.Execute(ctx, lang, action != "rename", func(client *lsp.LSPClient) error {
		if err := client.DidOpen(ctx, uri, lang, 1, string(text)); err != nil {
			return err
		}
		defer func() { _ = client.DidClose(context.Background(), uri) }()
		var err error
		switch action {
		case "diagnostics":
			var items []lsp.Diagnostic
			items, err = client.Diagnostics(ctx, uri)
			result = formatServerDiagnostics(items)
		case "definition":
			var locations []lsp.Location
			locations, err = client.GotoDefinition(ctx, uri, line-1, column-1)
			result = formatServerLocations("Definitions", locations)
		case "references":
			var locations []lsp.Location
			locations, err = client.FindReferences(ctx, uri, line-1, column-1)
			result = formatServerLocations("References", locations)
		case "symbols":
			var symbols []lsp.SymbolInformation
			symbols, err = client.DocumentSymbol(ctx, uri)
			result = formatServerSymbols(symbols)
		default:
			return fmt.Errorf("unsupported language-server action %q", action)
		}
		return err
	})
	if err != nil {
		return "", false
	}
	return result, true
}

func formatServerDiagnostics(items []lsp.Diagnostic) string {
	if len(items) == 0 {
		return "No diagnostics found."
	}
	var b strings.Builder
	b.WriteString("## Diagnostics\n\n")
	for _, item := range items {
		fmt.Fprintf(&b, "- %s:%d:%d [%d] %s\n", item.Source, item.Range.Start.Line+1, item.Range.Start.Character+1, item.Severity, item.Message)
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatServerLocations(title string, locations []lsp.Location) string {
	if len(locations) == 0 {
		return "No " + strings.ToLower(title) + " found."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n", title)
	for _, location := range locations {
		fmt.Fprintf(&b, "- %s:%d:%d\n", strings.TrimPrefix(location.URI, "file://"), location.Range.Start.Line+1, location.Range.Start.Character+1)
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatServerSymbols(symbols []lsp.SymbolInformation) string {
	if len(symbols) == 0 {
		return "No symbols found."
	}
	var b strings.Builder
	b.WriteString("## Symbols\n\n")
	for _, symbol := range symbols {
		fmt.Fprintf(&b, "- %s at %d:%d\n", symbol.Name, symbol.Location.Range.Start.Line+1, symbol.Location.Range.Start.Character+1)
	}
	return strings.TrimRight(b.String(), "\n")
}

// lspDefinition finds where a symbol is defined using codegraph.
func lspDefinition(root, filePath string, line int, symbol string) (string, error) {
	cg, err := codegraph.Open(root)
	if err != nil {
		return "", fmt.Errorf("codegraph not available: %w", err)
	}
	defer func() { _ = cg.Close() }()

	// If symbol is provided directly, search for it
	if symbol != "" {
		nodes, err := cg.Search(symbol, 10)
		if err != nil || len(nodes) == 0 {
			return fmt.Sprintf("No definition found for %q", symbol), nil
		}

		var result strings.Builder
		result.WriteString(fmt.Sprintf("## Definition: %s\n\n", symbol))
		for _, n := range nodes {
			sig := n.QualifiedName
			if n.Signature != "" {
				sig = n.Signature
			}
			result.WriteString(fmt.Sprintf("- **%s** `%s`\n  in %s:%d\n", n.Kind, sig, n.FilePath, n.StartLine))
		}
		return result.String(), nil
	}

	// If line is provided, try to extract symbol from that line
	if line > 0 {
		absPath, err := filepath.Abs(filePath)
		if err != nil {
			absPath = filePath
		}

		source, err := os.ReadFile(absPath) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
		if err != nil {
			return fmt.Sprintf("Cannot read file: %s", filePath), nil
		}

		lines := strings.Split(string(source), "\n")
		if line <= len(lines) {
			symbolFromLine := extractSymbolFromLine(lines[line-1])
			if symbolFromLine != "" {
				nodes, err := cg.Search(symbolFromLine, 10)
				if err != nil || len(nodes) == 0 {
					return fmt.Sprintf("No definition found for %q (from line %d)", symbolFromLine, line), nil
				}

				var result strings.Builder
				result.WriteString(fmt.Sprintf("## Definition: %s (from %s:%d)\n\n", symbolFromLine, filePath, line))
				for _, n := range nodes {
					sig := n.QualifiedName
					if n.Signature != "" {
						sig = n.Signature
					}
					result.WriteString(fmt.Sprintf("- **%s** `%s`\n  in %s:%d\n", n.Kind, sig, n.FilePath, n.StartLine))
				}
				return result.String(), nil
			}
		}
	}

	return "Provide 'symbol' or 'line' to find definition", nil
}

// lspReferences finds all references to a symbol using codegraph.
func lspReferences(root, filePath string, line int, symbol string) (string, error) {
	cg, err := codegraph.Open(root)
	if err != nil {
		return "", fmt.Errorf("codegraph not available: %w", err)
	}
	defer func() { _ = cg.Close() }()

	// Get symbol name
	sym := symbol
	if sym == "" && line > 0 {
		absPath, absErr := filepath.Abs(filePath)
		if absErr != nil {
			absPath = filePath
		}
		source, readErr := os.ReadFile(absPath) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
		if readErr == nil {
			lines := strings.Split(string(source), "\n")
			if line <= len(lines) {
				sym = extractSymbolFromLine(lines[line-1])
			}
		}
	}

	if sym == "" {
		return "Provide 'symbol' or 'line' to find references", nil
	}

	// Search for the symbol
	nodes, err := cg.Search(sym, 5)
	if err != nil || len(nodes) == 0 {
		return fmt.Sprintf("No references found for %q", sym), nil
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("## References: %s\n\n", sym))

	totalRefs := 0
	for _, n := range nodes {
		// Get callers of this symbol
		callers, err := cg.GetCallers(n.ID, 1)
		if err != nil {
			continue
		}

		if len(callers) > 0 {
			result.WriteString(fmt.Sprintf("### %s `%s` in %s:%d\n", n.Kind, n.Name, n.FilePath, n.StartLine))
			result.WriteString(fmt.Sprintf("Called by %d:\n", len(callers)))
			for _, c := range callers {
				result.WriteString(fmt.Sprintf("- %s `%s` in %s:%d\n", c.Kind, c.Name, c.FilePath, c.StartLine))
				totalRefs++
			}
			result.WriteString("\n")
		}
	}

	if totalRefs == 0 {
		return fmt.Sprintf("No references found for %q", sym), nil
	}

	return result.String(), nil
}

// lspImplementations finds types that implement an interface using codegraph.
func lspImplementations(root, symbol string) (string, error) {
	if symbol == "" {
		return "Provide 'symbol' (interface name) to find implementations", nil
	}

	cg, err := codegraph.Open(root)
	if err != nil {
		return "", fmt.Errorf("codegraph not available: %w", err)
	}
	defer func() { _ = cg.Close() }()

	// Search for the interface
	nodes, err := cg.Search(symbol, 5)
	if err != nil || len(nodes) == 0 {
		return fmt.Sprintf("No interface found for %q", symbol), nil
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("## Implementations: %s\n\n", symbol))

	for _, n := range nodes {
		if n.Kind != "interface" && n.Kind != "trait" {
			continue
		}

		result.WriteString(fmt.Sprintf("### Interface: `%s` in %s:%d\n\n", n.QualifiedName, n.FilePath, n.StartLine))

		// Find nodes that implement this interface (via 'implements' edges)
		impact, err := cg.GetImpactRadius(n.ID, 2)
		if err != nil {
			continue
		}

		implementors := 0
		for _, imp := range impact {
			if imp.Kind == "struct" || imp.Kind == "class" {
				result.WriteString(fmt.Sprintf("- **%s** `%s` in %s:%d\n", imp.Kind, imp.Name, imp.FilePath, imp.StartLine))
				implementors++
			}
		}

		if implementors == 0 {
			result.WriteString("No implementations found.\n")
		}
	}

	return result.String(), nil
}

// extractSymbolFromLine extracts a likely symbol name from a line of code.
func extractSymbolFromLine(line string) string {
	line = strings.TrimSpace(line)

	// Common patterns to extract symbol names
	patterns := []struct {
		prefix string
		suffix string
	}{
		{"func ", "("},
		{"def ", "("},
		{"function ", "("},
		{"class ", "{"},
		{"class ", ":"},
		{"interface ", "{"},
		{"type ", " struct"},
		{"type ", " interface"},
		{"import {", "}"},
		{"from ", " import"},
		{"new ", "("},
		{"", "("}, // generic function call
	}

	for _, p := range patterns {
		if p.prefix != "" && !strings.HasPrefix(line, p.prefix) {
			continue
		}

		text := line
		if p.prefix != "" {
			text = strings.TrimPrefix(text, p.prefix)
		}

		if idx := strings.Index(text, p.suffix); idx > 0 {
			symbol := strings.TrimSpace(text[:idx])
			// Clean up common noise
			symbol = strings.TrimLeft(symbol, "*&") // pointer/receiver
			if strings.Contains(symbol, ".") {
				parts := strings.Split(symbol, ".")
				symbol = parts[len(parts)-1] // last part
			}
			if len(symbol) > 2 && len(symbol) < 100 {
				return symbol
			}
		}
	}

	// Fallback: look for camelCase or snake_case identifiers
	words := strings.FieldsFunc(line, func(r rune) bool {
		return !isAlphaNum(r) && r != '_' && r != '.'
	})
	for _, w := range words {
		if len(w) > 2 && (isCamelCase(w) || isSnakeCase(w)) {
			// Skip common keywords
			lower := strings.ToLower(w)
			if lower != "func" && lower != "return" && lower != "if" && lower != "else" &&
				lower != "for" && lower != "range" && lower != "var" && lower != "const" &&
				lower != "type" && lower != "struct" && lower != "interface" && lower != "package" {
				return w
			}
		}
	}

	return ""
}

func isAlphaNum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func isCamelCase(s string) bool {
	hasLower := false
	hasUpper := false
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			hasLower = true
		}
		if r >= 'A' && r <= 'Z' {
			hasUpper = true
		}
	}
	return hasLower && hasUpper
}

func isSnakeCase(s string) bool {
	return strings.Contains(s, "_") && len(s) > 3
}

func lspDiagnostics(ctx context.Context, path string) (string, error) {
	ext := ""
	if i := strings.LastIndex(path, "."); i >= 0 {
		ext = path[i:]
	}

	var cmd *exec.Cmd
	switch ext {
	case ".go":
		cmd = exec.CommandContext(ctx, "go", "vet", "./...")
	case ".ts", ".tsx", ".js", ".jsx":
		cmd = exec.CommandContext(ctx, "npx", "tsc", "--noEmit", "--pretty")
	case ".py":
		cmd = exec.CommandContext(ctx, "python3", "-m", "py_compile", path) // #nosec G204 -- fixed interpreter and separate path argument
	case ".rs":
		cmd = exec.CommandContext(ctx, "cargo", "check", "--message-format=short")
	default:
		return fmt.Sprintf("No linter configured for %s files", ext), nil
	}

	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil && result == "" {
		result = err.Error()
	}
	if result == "" {
		return "No diagnostics found.", nil
	}
	if len(result) > 10000 {
		result = result[:10000] + "\n... (truncated)"
	}
	return result, nil
}
