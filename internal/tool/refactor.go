package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Refactorer applies common refactoring patterns without requiring LLM calls.
//
// NOTE (M9): the current implementation is regex-based, not AST-based. That makes
// it fast and dependency-free but imprecise: RenameSymbol can rename a symbol
// inside a string literal, a comment, or a different scope that happens to share
// the name, and ExtractFunction's parameter detection is heuristic. A correct
// implementation needs go/ast + go/types (scope-aware resolution). Keep the
// caller-facing contract small and verify results with a build/test before trusting
// output on code where those edge cases matter.
type Refactorer struct {
	mu sync.Mutex
}

// RefactoringResult describes the outcome of a single refactoring operation.
type RefactoringResult struct {
	File        string
	Changes     int
	Before      string
	After       string
	Type        string
	Description string
}

// NewRefactorer creates a new Refactorer.
func NewRefactorer() *Refactorer {
	return &Refactorer{}
}

// ExtractFunction extracts lines startLine..endLine (1-based, inclusive) from file
// into a new function named newName, replacing the original lines with a call.
// It detects needed parameters from variables used but not declared in the extracted block.
func (r *Refactorer) ExtractFunction(file string, startLine, endLine int, newName string) (*RefactoringResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := readPinnedFile(file)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	content := string(data)
	lines := strings.Split(content, "\n")

	if startLine < 1 || endLine < startLine || endLine > len(lines) {
		return nil, fmt.Errorf("invalid line range %d-%d (file has %d lines)", startLine, endLine, len(lines))
	}

	// Extract the block.
	extracted := lines[startLine-1 : endLine]
	extractedBlock := strings.Join(extracted, "\n")

	// Detect parameters: variables that are used in the extracted block but declared before it.
	params := detectParameters(lines[:startLine-1], extracted)

	// Build the new function.
	paramList := ""
	argList := ""
	if len(params) > 0 {
		var paramParts []string
		var argParts []string
		for _, p := range params {
			paramParts = append(paramParts, p+" string")
			argParts = append(argParts, p)
		}
		paramList = strings.Join(paramParts, ", ")
		argList = strings.Join(argParts, ", ")
	}

	newFunc := fmt.Sprintf("\nfunc %s(%s) {\n", newName, paramList)
	for _, line := range extracted {
		newFunc += "\t" + line + "\n"
	}
	newFunc += "}\n"

	// Build the call.
	call := fmt.Sprintf("\t%s(%s)", newName, argList)

	// Replace the extracted lines with the call.
	var newLines []string
	newLines = append(newLines, lines[:startLine-1]...)
	newLines = append(newLines, call)
	newLines = append(newLines, lines[endLine:]...)

	// Append the new function at end.
	newContent := strings.Join(newLines, "\n") + newFunc

	if err := writePinnedFile(file, []byte(newContent), 0o600); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	return &RefactoringResult{
		File:        file,
		Changes:     1,
		Before:      extractedBlock,
		After:       call,
		Type:        "extract_function",
		Description: fmt.Sprintf("Extracted lines %d-%d into function %s", startLine, endLine, newName),
	}, nil
}

// detectParameters finds identifiers used in the extracted block that appear as declarations
// before the block.
func detectParameters(beforeLines, extractedLines []string) []string {
	// Look for short variable declarations (:=) and var declarations in preceding lines.
	declaredRe := regexp.MustCompile(`(?:(\w+)\s*:=|var\s+(\w+))`)
	declared := make(map[string]bool)
	for _, line := range beforeLines {
		matches := declaredRe.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			if m[1] != "" {
				declared[m[1]] = true
			}
			if m[2] != "" {
				declared[m[2]] = true
			}
		}
	}

	// Find identifiers used in extracted block.
	identRe := regexp.MustCompile(`\b([a-zA-Z_]\w*)\b`)
	used := make(map[string]bool)
	for _, line := range extractedLines {
		matches := identRe.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			used[m[1]] = true
		}
	}

	// Intersection: used in block and declared before it.
	var params []string
	for v := range used {
		if declared[v] {
			params = append(params, v)
		}
	}
	sort.Strings(params)
	return params
}

// RenameSymbol renames all occurrences of oldName to newName within the file,
// respecting word boundaries.
func (r *Refactorer) RenameSymbol(file, oldName, newName string) (*RefactoringResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := readPinnedFile(file)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	content := string(data)
	before := content

	pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(oldName) + `\b`)
	matches := pattern.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("symbol %q not found in %s", oldName, file)
	}

	result := pattern.ReplaceAllString(content, newName)

	if err := writePinnedFile(file, []byte(result), 0o600); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	return &RefactoringResult{
		File:        file,
		Changes:     len(matches),
		Before:      before,
		After:       result,
		Type:        "rename_symbol",
		Description: fmt.Sprintf("Renamed %q to %q (%d occurrences)", oldName, newName, len(matches)),
	}, nil
}

// InlineVariable replaces a variable declaration on the given line with its value
// at all use sites within the file.
func (r *Refactorer) InlineVariable(file string, line int) (*RefactoringResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := readPinnedFile(file)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	content := string(data)
	lines := strings.Split(content, "\n")

	if line < 1 || line > len(lines) {
		return nil, fmt.Errorf("line %d out of range (file has %d lines)", line, len(lines))
	}

	targetLine := lines[line-1]

	// Match patterns: x := expr or var x = expr
	shortDeclRe := regexp.MustCompile(`^\s*(\w+)\s*:=\s*(.+)$`)
	varDeclRe := regexp.MustCompile(`^\s*var\s+(\w+)\s*=\s*(.+)$`)

	var varName, value string
	if m := shortDeclRe.FindStringSubmatch(targetLine); m != nil {
		varName = m[1]
		value = strings.TrimSpace(m[2])
	} else if m := varDeclRe.FindStringSubmatch(targetLine); m != nil {
		varName = m[1]
		value = strings.TrimSpace(m[2])
	} else {
		return nil, fmt.Errorf("line %d does not contain a variable declaration", line)
	}

	// Remove the declaration line.
	newLines := make([]string, 0, len(lines)-1)
	newLines = append(newLines, lines[:line-1]...)
	newLines = append(newLines, lines[line:]...)

	// Replace all occurrences of varName with value in remaining lines.
	pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(varName) + `\b`)
	changes := 0
	for i, l := range newLines {
		if pattern.MatchString(l) {
			newLines[i] = pattern.ReplaceAllString(l, value)
			changes++
		}
	}

	result := strings.Join(newLines, "\n")
	if err := writePinnedFile(file, []byte(result), 0o600); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	return &RefactoringResult{
		File:        file,
		Changes:     changes + 1, // +1 for removing the declaration
		Before:      targetLine,
		After:       fmt.Sprintf("(inlined %q = %s at %d sites)", varName, value, changes),
		Type:        "inline_variable",
		Description: fmt.Sprintf("Inlined variable %q with value %q at %d use sites", varName, value, changes),
	}, nil
}

// ExtractVariable replaces an expression on the given line with a named variable,
// inserting the variable declaration before that line.
func (r *Refactorer) ExtractVariable(file string, line int, expr, varName string) (*RefactoringResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := readPinnedFile(file)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	content := string(data)
	lines := strings.Split(content, "\n")

	if line < 1 || line > len(lines) {
		return nil, fmt.Errorf("line %d out of range (file has %d lines)", line, len(lines))
	}

	targetLine := lines[line-1]
	if !strings.Contains(targetLine, expr) {
		return nil, fmt.Errorf("expression %q not found on line %d", expr, line)
	}

	// Determine indentation of the target line.
	indent := ""
	for _, ch := range targetLine {
		if ch == '\t' || ch == ' ' {
			indent += string(ch)
		} else {
			break
		}
	}

	// Create the variable declaration.
	declLine := fmt.Sprintf("%s%s := %s", indent, varName, expr)

	// Replace expression on target line.
	newTargetLine := strings.Replace(targetLine, expr, varName, 1)

	// Build new content: insert decl before the target line, then the modified line.
	var newLines []string
	newLines = append(newLines, lines[:line-1]...)
	newLines = append(newLines, declLine)
	newLines = append(newLines, newTargetLine)
	newLines = append(newLines, lines[line:]...)

	result := strings.Join(newLines, "\n")
	if err := writePinnedFile(file, []byte(result), 0o600); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	return &RefactoringResult{
		File:        file,
		Changes:     1,
		Before:      targetLine,
		After:       declLine + "\n" + newTargetLine,
		Type:        "extract_variable",
		Description: fmt.Sprintf("Extracted %q into variable %q", expr, varName),
	}, nil
}

// AddErrorCheck inserts an `if err != nil { return err }` block after the given line,
// which should be a function call that returns an error.
func (r *Refactorer) AddErrorCheck(file string, line int) (*RefactoringResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := readPinnedFile(file)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	content := string(data)
	lines := strings.Split(content, "\n")

	if line < 1 || line > len(lines) {
		return nil, fmt.Errorf("line %d out of range (file has %d lines)", line, len(lines))
	}

	targetLine := lines[line-1]

	// Determine indentation.
	indent := ""
	for _, ch := range targetLine {
		if ch == '\t' || ch == ' ' {
			indent += string(ch)
		} else {
			break
		}
	}

	// Check if the line already has err assignment or if we need to add one.
	// Patterns: _, err := foo() or err = foo() or result, err := foo()
	hasErrAssign := regexp.MustCompile(`\berr\s*[:=]`).MatchString(targetLine)
	if !hasErrAssign {
		// Wrap the call to capture err.
		// Replace "funcCall(...)" with "err := funcCall(...)"
		callRe := regexp.MustCompile(`^(\s*)(.+)$`)
		if m := callRe.FindStringSubmatch(targetLine); m != nil {
			targetLine = m[1] + "err := " + strings.TrimSpace(m[2])
			lines[line-1] = targetLine
		}
	}

	// Build the error check block.
	errCheck := []string{
		indent + "if err != nil {",
		indent + "\treturn err",
		indent + "}",
	}

	// Insert after target line.
	var newLines []string
	newLines = append(newLines, lines[:line]...)
	newLines = append(newLines, errCheck...)
	newLines = append(newLines, lines[line:]...)

	result := strings.Join(newLines, "\n")
	if err := writePinnedFile(file, []byte(result), 0o600); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	return &RefactoringResult{
		File:        file,
		Changes:     1,
		Before:      strings.Join(lines[line-1:line], "\n"),
		After:       strings.Join(append([]string{targetLine}, errCheck...), "\n"),
		Type:        "add_error_check",
		Description: fmt.Sprintf("Added error check after line %d", line),
	}, nil
}

// WrapWithContext wraps a `return err` statement on the given line with
// `return fmt.Errorf("%s: %%w", context, err)`.
func (r *Refactorer) WrapWithContext(file string, line int, context string) (*RefactoringResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := readPinnedFile(file)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	content := string(data)
	lines := strings.Split(content, "\n")

	if line < 1 || line > len(lines) {
		return nil, fmt.Errorf("line %d out of range (file has %d lines)", line, len(lines))
	}

	targetLine := lines[line-1]

	// Match "return err" or "return someErr"
	returnErrRe := regexp.MustCompile(`^(\s*)return\s+(\w+)\s*$`)
	m := returnErrRe.FindStringSubmatch(targetLine)
	if m == nil {
		// Also try: return nil, err patterns
		returnMultiErrRe := regexp.MustCompile(`^(\s*)return\s+(.*,\s*)(\w+)\s*$`)
		m2 := returnMultiErrRe.FindStringSubmatch(targetLine)
		if m2 == nil {
			return nil, fmt.Errorf("line %d does not contain a return error statement", line)
		}
		indent := m2[1]
		prefix := m2[2]
		errVar := m2[3]
		newLine := fmt.Sprintf(`%sreturn %sfmt.Errorf("%s: %%w", %s)`, indent, prefix, context, errVar)
		before := targetLine
		lines[line-1] = newLine

		result := strings.Join(lines, "\n")
		if err := writePinnedFile(file, []byte(result), 0o600); err != nil {
			return nil, fmt.Errorf("write file: %w", err)
		}

		return &RefactoringResult{
			File:        file,
			Changes:     1,
			Before:      before,
			After:       newLine,
			Type:        "wrap_with_context",
			Description: fmt.Sprintf("Wrapped error return with context %q", context),
		}, nil
	}

	indent := m[1]
	errVar := m[2]
	newLine := fmt.Sprintf(`%sreturn fmt.Errorf("%s: %%w", %s)`, indent, context, errVar)
	before := targetLine
	lines[line-1] = newLine

	result := strings.Join(lines, "\n")
	if err := writePinnedFile(file, []byte(result), 0o600); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	return &RefactoringResult{
		File:        file,
		Changes:     1,
		Before:      before,
		After:       newLine,
		Type:        "wrap_with_context",
		Description: fmt.Sprintf("Wrapped error return with context %q", context),
	}, nil
}

// ConvertToTableTest converts a simple test function into table-driven test format.
func (r *Refactorer) ConvertToTableTest(file, testFunc string) (*RefactoringResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := readPinnedFile(file)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	content := string(data)

	// Find the test function.
	funcRe := regexp.MustCompile(`(?ms)^func\s+` + regexp.QuoteMeta(testFunc) + `\s*\(\s*t\s+\*testing\.T\s*\)\s*\{(.+?)^\}`)
	loc := funcRe.FindStringSubmatchIndex(content)
	if loc == nil {
		return nil, fmt.Errorf("test function %q not found in %s", testFunc, file)
	}

	funcBody := content[loc[2]:loc[3]]
	before := content[loc[0]:loc[1]]

	// Determine the base test name (strip "Test" prefix for naming).
	baseName := strings.TrimPrefix(testFunc, "Test")

	// Build table-driven version.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("func %s(t *testing.T) {\n", testFunc))
	b.WriteString("\ttests := []struct {\n")
	b.WriteString("\t\tname string\n")
	b.WriteString("\t}{\n")
	b.WriteString(fmt.Sprintf("\t\t{name: \"%s\"},\n", baseName))
	b.WriteString("\t}\n\n")
	b.WriteString("\tfor _, tt := range tests {\n")
	b.WriteString("\t\tt.Run(tt.name, func(t *testing.T) {\n")

	// Indent the existing function body as the test case body.
	bodyLines := strings.Split(strings.TrimSpace(funcBody), "\n")
	for _, bl := range bodyLines {
		trimmed := strings.TrimSpace(bl)
		if trimmed == "" {
			continue
		}
		b.WriteString("\t\t\t" + trimmed + "\n")
	}

	b.WriteString("\t\t})\n")
	b.WriteString("\t}\n")
	b.WriteString("}")

	after := b.String()
	newContent := content[:loc[0]] + after + content[loc[1]:]

	if err := writePinnedFile(file, []byte(newContent), 0o600); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	return &RefactoringResult{
		File:        file,
		Changes:     1,
		Before:      before,
		After:       after,
		Type:        "convert_table_test",
		Description: fmt.Sprintf("Converted %s to table-driven test", testFunc),
	}, nil
}

// SortImports groups and sorts imports in a Go file (stdlib, external, internal).
func (r *Refactorer) SortImports(file string) (*RefactoringResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := readPinnedFile(file)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	content := string(data)

	organizer := NewImportOrganizer("go")
	result, err := organizer.OrganizeGo(content)
	if err != nil {
		return nil, fmt.Errorf("organize imports: %w", err)
	}

	if result == content {
		return &RefactoringResult{
			File:        file,
			Changes:     0,
			Before:      content,
			After:       result,
			Type:        "sort_imports",
			Description: "Imports already sorted",
		}, nil
	}

	if err := writePinnedFile(file, []byte(result), 0o600); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	return &RefactoringResult{
		File:        file,
		Changes:     1,
		Before:      content,
		After:       result,
		Type:        "sort_imports",
		Description: "Grouped and sorted imports (stdlib, external, internal)",
	}, nil
}

// RemoveUnusedParams detects and removes parameters not used in the specified function body.
func (r *Refactorer) RemoveUnusedParams(file, funcName string) (*RefactoringResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := readPinnedFile(file)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	content := string(data)

	// Find the function signature and body.
	funcRe := regexp.MustCompile(`(?ms)^func\s+(?:\([^)]*\)\s*)?` + regexp.QuoteMeta(funcName) + `\s*\(([^)]*)\)([^{]*)\{(.+?)^\}`)
	loc := funcRe.FindStringSubmatchIndex(content)
	if loc == nil {
		return nil, fmt.Errorf("function %q not found in %s", funcName, file)
	}

	paramStr := content[loc[2]:loc[3]]
	funcBody := content[loc[6]:loc[7]]
	before := content[loc[0]:loc[1]]

	if strings.TrimSpace(paramStr) == "" {
		return &RefactoringResult{
			File:        file,
			Changes:     0,
			Before:      before,
			After:       before,
			Type:        "remove_unused_params",
			Description: fmt.Sprintf("Function %s has no parameters", funcName),
		}, nil
	}

	// Parse parameters.
	params := parseParamList(paramStr)
	if len(params) == 0 {
		return &RefactoringResult{
			File:        file,
			Changes:     0,
			Before:      before,
			After:       before,
			Type:        "remove_unused_params",
			Description: fmt.Sprintf("Function %s has no parameters to remove", funcName),
		}, nil
	}

	// Check which params are used in body.
	var usedParams []paramInfo
	var removedParams []string
	for _, p := range params {
		if p.name == "_" || p.name == "" {
			usedParams = append(usedParams, p)
			continue
		}
		paramRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(p.name) + `\b`)
		if paramRe.MatchString(funcBody) {
			usedParams = append(usedParams, p)
		} else {
			removedParams = append(removedParams, p.name)
		}
	}

	if len(removedParams) == 0 {
		return &RefactoringResult{
			File:        file,
			Changes:     0,
			Before:      before,
			After:       before,
			Type:        "remove_unused_params",
			Description: fmt.Sprintf("All parameters of %s are used", funcName),
		}, nil
	}

	// Build new parameter list.
	newParamStr := formatParamList(usedParams)

	// Replace the parameter list in the content.
	newContent := content[:loc[2]] + newParamStr + content[loc[3]:]

	if err := writePinnedFile(file, []byte(newContent), 0o600); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	after := strings.Replace(before, paramStr, newParamStr, 1)

	return &RefactoringResult{
		File:        file,
		Changes:     len(removedParams),
		Before:      before,
		After:       after,
		Type:        "remove_unused_params",
		Description: fmt.Sprintf("Removed unused parameters from %s: %s", funcName, strings.Join(removedParams, ", ")),
	}, nil
}

// paramInfo holds parsed parameter name and type.
type paramInfo struct {
	name    string
	typExpr string
}

// parseParamList parses "a int, b string, c, d int" into paramInfo slices.
func parseParamList(paramStr string) []paramInfo {
	paramStr = strings.TrimSpace(paramStr)
	if paramStr == "" {
		return nil
	}

	parts := splitParams(paramStr)
	var params []paramInfo

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// Split into tokens.
		tokens := strings.Fields(part)
		if len(tokens) == 0 {
			continue
		}
		if len(tokens) == 1 {
			// Could be just a type (unnamed param) or just a name (type comes later).
			params = append(params, paramInfo{name: tokens[0], typExpr: ""})
		} else {
			// Last token is the type, preceding are names.
			typExpr := tokens[len(tokens)-1]
			// Handle pointer/slice/map types that may contain spaces.
			// Simple heuristic: if the first token starts with *, [], map, etc. it's a type.
			if strings.HasPrefix(tokens[0], "*") || strings.HasPrefix(tokens[0], "[]") || strings.HasPrefix(tokens[0], "map[") {
				// Single param with complex type.
				params = append(params, paramInfo{name: "", typExpr: part})
			} else {
				for _, name := range tokens[:len(tokens)-1] {
					name = strings.TrimSuffix(name, ",")
					params = append(params, paramInfo{name: name, typExpr: typExpr})
				}
			}
		}
	}

	return params
}

// splitParams splits a parameter string by commas, respecting nested brackets.
func splitParams(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i, ch := range s {
		switch ch {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// formatParamList formats a slice of paramInfo back into a Go parameter string.
func formatParamList(params []paramInfo) string {
	if len(params) == 0 {
		return ""
	}
	var parts []string
	for _, p := range params {
		if p.typExpr == "" {
			parts = append(parts, p.name)
		} else if p.name == "" {
			parts = append(parts, p.typExpr)
		} else {
			parts = append(parts, p.name+" "+p.typExpr)
		}
	}
	return strings.Join(parts, ", ")
}

// FormatRefactoringResult renders a RefactoringResult as a human-readable string.
func FormatRefactoringResult(result *RefactoringResult) string {
	if result == nil {
		return "No result"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Refactoring: %s\n", result.Type))
	b.WriteString(fmt.Sprintf("File: %s\n", result.File))
	b.WriteString(fmt.Sprintf("Changes: %d\n", result.Changes))
	b.WriteString(fmt.Sprintf("Description: %s\n", result.Description))
	if result.Before != "" && result.After != "" {
		b.WriteString("---\n")
		b.WriteString(fmt.Sprintf("Before:\n%s\n", result.Before))
		b.WriteString(fmt.Sprintf("After:\n%s\n", result.After))
	}
	return b.String()
}

// --- RefactorTool implements the Tool interface ---

// RefactorTool exposes refactoring operations as a rho tool.
type RefactorTool struct {
	refactorer *Refactorer
}

// NewRefactorTool creates a RefactorTool with its own Refactorer instance.
func NewRefactorTool() *RefactorTool {
	return &RefactorTool{refactorer: NewRefactorer()}
}

func (RefactorTool) Name() string { return "Refactor" }

// RefactorInput is the typed input for RefactorTool.
type RefactorInput struct {
	Action    string `json:"action"`
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	NewName   string `json:"new_name"`
	OldName   string `json:"old_name"`
	Line      int    `json:"line"`
	Expr      string `json:"expr"`
	VarName   string `json:"var_name"`
	Context   string `json:"context"`
	TestFunc  string `json:"test_func"`
	FuncName  string `json:"func_name"`
}

func (RefactorTool) Description() string {
	return "Apply common refactoring patterns (extract function, rename symbol, inline variable, extract variable, add error check, wrap with context, convert to table test, sort imports, remove unused params) without LLM calls."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (RefactorTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action":     {Type: "string", Enum: []interface{}{"extract_function", "rename_symbol", "inline_variable", "extract_variable", "add_error_check", "wrap_with_context", "convert_table_test", "sort_imports", "remove_unused_params"}, Description: "Refactoring action: extract_function, rename_symbol, inline_variable, extract_variable, add_error_check, wrap_with_context, convert_table_test, sort_imports, remove_unused_params"},
			"file":       {Type: "string", Description: "Absolute path to the file to refactor"},
			"start_line": {Type: "integer", Description: "Start line (1-based) for extract_function"},
			"end_line":   {Type: "integer", Description: "End line (1-based) for extract_function"},
			"new_name":   {Type: "string", Description: "New name for extract_function or rename_symbol"},
			"old_name":   {Type: "string", Description: "Old symbol name for rename_symbol"},
			"line":       {Type: "integer", Description: "Line number for inline_variable, extract_variable, add_error_check, or wrap_with_context"},
			"expr":       {Type: "string", Description: "Expression to extract for extract_variable"},
			"var_name":   {Type: "string", Description: "Variable name for extract_variable"},
			"context":    {Type: "string", Description: "Context message for wrap_with_context"},
			"test_func":  {Type: "string", Description: "Test function name for convert_table_test"},
			"func_name":  {Type: "string", Description: "Function name for remove_unused_params"},
		},
		Required: []string{"action", "file"},
	}
}

func (RefactorTool) Parameters() map[string]interface{} {
	return refactorSchema.ToJSONSchema()
}

// refactorSchema is the single source of truth for Refactor's input schema.
var refactorSchema = RefactorTool{}.Schema()

func (rt RefactorTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[RefactorInput]("Refactor", input)
	if err != nil {
		return "", err
	}
	if p.Action == "" {
		return "", fmt.Errorf("action is required")
	}
	if p.File == "" {
		return "", fmt.Errorf("file is required")
	}
	if err := validatePathAllowed(ctx, p.File); err != nil {
		return "", err
	}

	ref := rt.refactorer
	var result *RefactoringResult

	switch p.Action {
	case "extract_function":
		if p.NewName == "" {
			return "", fmt.Errorf("new_name is required for extract_function")
		}
		result, err = ref.ExtractFunction(p.File, p.StartLine, p.EndLine, p.NewName)

	case "rename_symbol":
		if p.OldName == "" || p.NewName == "" {
			return "", fmt.Errorf("old_name and new_name are required for rename_symbol")
		}
		result, err = ref.RenameSymbol(p.File, p.OldName, p.NewName)

	case "inline_variable":
		if p.Line == 0 {
			return "", fmt.Errorf("line is required for inline_variable")
		}
		result, err = ref.InlineVariable(p.File, p.Line)

	case "extract_variable":
		if p.Line == 0 || p.Expr == "" || p.VarName == "" {
			return "", fmt.Errorf("line, expr, and var_name are required for extract_variable")
		}
		result, err = ref.ExtractVariable(p.File, p.Line, p.Expr, p.VarName)

	case "add_error_check":
		if p.Line == 0 {
			return "", fmt.Errorf("line is required for add_error_check")
		}
		result, err = ref.AddErrorCheck(p.File, p.Line)

	case "wrap_with_context":
		if p.Line == 0 || p.Context == "" {
			return "", fmt.Errorf("line and context are required for wrap_with_context")
		}
		result, err = ref.WrapWithContext(p.File, p.Line, p.Context)

	case "convert_table_test":
		if p.TestFunc == "" {
			return "", fmt.Errorf("test_func is required for convert_table_test")
		}
		result, err = ref.ConvertToTableTest(p.File, p.TestFunc)

	case "sort_imports":
		result, err = ref.SortImports(p.File)

	case "remove_unused_params":
		if p.FuncName == "" {
			return "", fmt.Errorf("func_name is required for remove_unused_params")
		}
		result, err = ref.RemoveUnusedParams(p.File, p.FuncName)

	default:
		return "", fmt.Errorf("unknown action: %s", p.Action)
	}

	if err != nil {
		return "", err
	}

	return FormatRefactoringResult(result), nil
}

// Ensure RefactorTool satisfies Tool interface at compile time.
var _ Tool = RefactorTool{}
