package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SmartCreator generates boilerplate content when creating new files based on
// project conventions and file type.
type SmartCreator struct {
	ProjectDir  string
	Conventions map[string]string
}

// FileTemplate describes the boilerplate template for a given file type.
type FileTemplate struct {
	Extension           string
	Language            string
	Template            string
	RequiresPackageName bool
}

// NewSmartCreator creates a SmartCreator rooted at the given project directory.
func NewSmartCreator(projectDir string) *SmartCreator {
	return &SmartCreator{
		ProjectDir:  projectDir,
		Conventions: make(map[string]string),
	}
}

// GenerateBoilerplate produces starter content for a new file based on its
// extension and path within the project.
func (sc *SmartCreator) GenerateBoilerplate(path string) string {
	if path == "" {
		return ""
	}

	ext := filepath.Ext(path)
	base := filepath.Base(path)

	// Go test files get special treatment.
	if strings.HasSuffix(base, "_test.go") {
		return sc.generateGoTestBoilerplate(path)
	}

	switch ext {
	case ".go":
		return sc.generateGoBoilerplate(path)
	case ".py":
		return sc.generatePythonBoilerplate(path)
	case ".ts":
		return sc.generateTypeScriptBoilerplate(path, false)
	case ".tsx":
		return sc.generateTypeScriptBoilerplate(path, true)
	case ".rs":
		return sc.generateRustBoilerplate(path)
	case ".yaml", ".yml":
		return sc.generateYAMLBoilerplate(path)
	}

	// Match by full filename.
	switch base {
	case "Dockerfile":
		return sc.generateDockerfileBoilerplate()
	case "Makefile":
		return sc.generateMakefileBoilerplate()
	}

	return ""
}

// InferPackageName determines the Go package name from a file path.
func (sc *SmartCreator) InferPackageName(filePath string) string {
	if filePath == "" {
		return "main"
	}

	dir := filepath.Dir(filePath)
	dirName := filepath.Base(dir)

	// Special cases for Go conventions.
	if dirName == "cmd" || dirName == "." || dirName == "/" {
		return "main"
	}

	// If path contains cmd/ as a parent, use main.
	parts := strings.Split(filepath.ToSlash(filePath), "/")
	for i, p := range parts {
		if p == "cmd" && i < len(parts)-1 {
			return "main"
		}
	}

	// Clean the directory name to be a valid Go identifier.
	pkg := strings.ReplaceAll(dirName, "-", "")
	pkg = strings.ReplaceAll(pkg, ".", "")
	if pkg == "" {
		return "main"
	}

	return pkg
}

// DetectCopyright scans existing source files in the project for a copyright
// header and returns it if found.
func (sc *SmartCreator) DetectCopyright(projectDir string) string {
	if projectDir == "" {
		return ""
	}

	// Look for Go files first, then any source files.
	extensions := []string{"*.go", "*.py", "*.ts", "*.rs"}
	for _, pattern := range extensions {
		matches, err := filepath.Glob(filepath.Join(projectDir, pattern))
		if err != nil || len(matches) == 0 {
			// Try one level deeper.
			matches, err = filepath.Glob(filepath.Join(projectDir, "*", pattern))
			if err != nil || len(matches) == 0 {
				continue
			}
		}

		for _, match := range matches {
			header := extractCopyrightHeader(match)
			if header != "" {
				return header
			}
		}
	}

	return ""
}

// DetectImportStyle looks at existing files to determine import conventions
// for the given language.
func (sc *SmartCreator) DetectImportStyle(projectDir string, language string) string {
	if projectDir == "" {
		return ""
	}

	var pattern string
	switch language {
	case "go":
		pattern = "*.go"
	case "python":
		pattern = "*.py"
	case "typescript":
		pattern = "*.ts"
	default:
		return ""
	}

	matches, err := filepath.Glob(filepath.Join(projectDir, pattern))
	if err != nil || len(matches) == 0 {
		matches, _ = filepath.Glob(filepath.Join(projectDir, "*", pattern))
	}
	if len(matches) == 0 {
		return ""
	}

	// Read the first matching file and extract import section.
	for _, match := range matches {
		style := extractImportStyle(match, language)
		if style != "" {
			return style
		}
	}

	return ""
}

// GenerateTestFile creates a test file corresponding to the given source file.
func (sc *SmartCreator) GenerateTestFile(sourcePath string) string {
	if sourcePath == "" {
		return ""
	}

	ext := filepath.Ext(sourcePath)
	switch ext {
	case ".go":
		return sc.generateGoTestFromSource(sourcePath)
	case ".py":
		return sc.generatePythonTestFromSource(sourcePath)
	case ".ts", ".tsx":
		return sc.generateTypeScriptTestFromSource(sourcePath)
	default:
		return ""
	}
}

// GenerateInterface produces a Go interface definition from function signatures.
func (sc *SmartCreator) GenerateInterface(functions []string) string {
	if len(functions) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("type Interface interface {\n")
	for _, fn := range functions {
		sb.WriteString("\t")
		sb.WriteString(fn)
		sb.WriteString("\n")
	}
	sb.WriteString("}\n")
	return sb.String()
}

// --- Go boilerplate ---

func (sc *SmartCreator) generateGoBoilerplate(path string) string {
	var sb strings.Builder

	// Add copyright header if project has one.
	copyright := sc.DetectCopyright(sc.ProjectDir)
	if copyright != "" {
		sb.WriteString(copyright)
		sb.WriteString("\n\n")
	}

	pkg := sc.InferPackageName(path)
	sb.WriteString("package ")
	sb.WriteString(pkg)
	sb.WriteString("\n")

	return sb.String()
}

func (sc *SmartCreator) generateGoTestBoilerplate(path string) string {
	var sb strings.Builder

	pkg := sc.InferPackageName(path)
	sb.WriteString("package ")
	sb.WriteString(pkg)
	sb.WriteString("\n\n")
	sb.WriteString("import (\n")
	sb.WriteString("\t\"testing\"\n")
	sb.WriteString(")\n\n")

	// Generate a test function name from file name.
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, "_test.go")
	funcName := "Test" + capitalize(name)

	sb.WriteString("func ")
	sb.WriteString(funcName)
	sb.WriteString("(t *testing.T) {\n")
	sb.WriteString("\t// TODO: implement test\n")
	sb.WriteString("}\n")

	return sb.String()
}

func (sc *SmartCreator) generateGoTestFromSource(sourcePath string) string {
	// Read source file to find exported functions.
	functions := extractGoExportedFunctions(sourcePath)

	pkg := sc.InferPackageName(sourcePath)

	var sb strings.Builder
	sb.WriteString("package ")
	sb.WriteString(pkg)
	sb.WriteString("\n\n")
	sb.WriteString("import (\n")
	sb.WriteString("\t\"testing\"\n")
	sb.WriteString(")\n")

	if len(functions) == 0 {
		base := filepath.Base(sourcePath)
		name := strings.TrimSuffix(base, ".go")
		sb.WriteString("\nfunc Test")
		sb.WriteString(capitalize(name))
		sb.WriteString("(t *testing.T) {\n")
		sb.WriteString("\t// TODO: implement test\n")
		sb.WriteString("}\n")
	} else {
		for _, fn := range functions {
			sb.WriteString("\nfunc Test")
			sb.WriteString(fn)
			sb.WriteString("(t *testing.T) {\n")
			sb.WriteString("\t// TODO: implement test\n")
			sb.WriteString("}\n")
		}
	}

	return sb.String()
}

// --- Python boilerplate ---

func (sc *SmartCreator) generatePythonBoilerplate(path string) string {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, ".py")

	var sb strings.Builder
	sb.WriteString("\"\"\"")
	sb.WriteString(name)
	sb.WriteString(" module.\"\"\"\n\n\n")
	sb.WriteString("def main():\n")
	sb.WriteString("    \"\"\"Main entry point.\"\"\"\n")
	sb.WriteString("    pass\n\n\n")
	sb.WriteString("if __name__ == \"__main__\":\n")
	sb.WriteString("    main()\n")

	return sb.String()
}

func (sc *SmartCreator) generatePythonTestFromSource(sourcePath string) string {
	base := filepath.Base(sourcePath)
	module := strings.TrimSuffix(base, ".py")

	var sb strings.Builder
	sb.WriteString("\"\"\"Tests for ")
	sb.WriteString(module)
	sb.WriteString(" module.\"\"\"\n\n")
	sb.WriteString("import unittest\n\n")
	sb.WriteString("from ")
	sb.WriteString(module)
	sb.WriteString(" import *\n\n\n")
	sb.WriteString("class Test")
	sb.WriteString(capitalize(module))
	sb.WriteString("(unittest.TestCase):\n")
	sb.WriteString("    \"\"\"Test cases for ")
	sb.WriteString(module)
	sb.WriteString(".\"\"\"\n\n")
	sb.WriteString("    def test_placeholder(self):\n")
	sb.WriteString("        \"\"\"TODO: implement test.\"\"\"\n")
	sb.WriteString("        pass\n\n\n")
	sb.WriteString("if __name__ == \"__main__\":\n")
	sb.WriteString("    unittest.main()\n")

	return sb.String()
}

// --- TypeScript boilerplate ---

func (sc *SmartCreator) generateTypeScriptBoilerplate(path string, isTSX bool) string {
	base := filepath.Base(path)
	var name string
	if isTSX {
		name = strings.TrimSuffix(base, ".tsx")
	} else {
		name = strings.TrimSuffix(base, ".ts")
	}

	var sb strings.Builder

	if isTSX {
		sb.WriteString("import React from 'react';\n\n")
		sb.WriteString("interface ")
		sb.WriteString(capitalize(name))
		sb.WriteString("Props {\n")
		sb.WriteString("  // TODO: define props\n")
		sb.WriteString("}\n\n")
		sb.WriteString("export default function ")
		sb.WriteString(capitalize(name))
		sb.WriteString("(props: ")
		sb.WriteString(capitalize(name))
		sb.WriteString("Props) {\n")
		sb.WriteString("  return (\n")
		sb.WriteString("    <div>\n")
		sb.WriteString("      {/* TODO: implement component */}\n")
		sb.WriteString("    </div>\n")
		sb.WriteString("  );\n")
		sb.WriteString("}\n")
	} else {
		sb.WriteString("export default function ")
		sb.WriteString(name)
		sb.WriteString("() {\n")
		sb.WriteString("  // TODO: implement\n")
		sb.WriteString("}\n")
	}

	return sb.String()
}

func (sc *SmartCreator) generateTypeScriptTestFromSource(sourcePath string) string {
	ext := filepath.Ext(sourcePath)
	base := filepath.Base(sourcePath)
	name := strings.TrimSuffix(base, ext)

	var sb strings.Builder
	sb.WriteString("import { ")
	sb.WriteString(name)
	sb.WriteString(" } from './")
	sb.WriteString(name)
	sb.WriteString("';\n\n")
	sb.WriteString("describe('")
	sb.WriteString(name)
	sb.WriteString("', () => {\n")
	sb.WriteString("  it('should work', () => {\n")
	sb.WriteString("    // TODO: implement test\n")
	sb.WriteString("  });\n")
	sb.WriteString("});\n")

	return sb.String()
}

// --- Rust boilerplate ---

func (sc *SmartCreator) generateRustBoilerplate(path string) string {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, ".rs")

	var sb strings.Builder
	if name != "main" && name != "lib" {
		sb.WriteString("//! ")
		sb.WriteString(name)
		sb.WriteString(" module.\n\n")
		sb.WriteString("pub mod ")
		sb.WriteString(name)
		sb.WriteString(" {\n")
		sb.WriteString("    // TODO: implement module\n")
		sb.WriteString("}\n")
	} else if name == "main" {
		sb.WriteString("fn main() {\n")
		sb.WriteString("    // TODO: implement\n")
		sb.WriteString("}\n")
	} else {
		sb.WriteString("//! Library crate root.\n")
	}

	return sb.String()
}

// --- Dockerfile boilerplate ---

func (sc *SmartCreator) generateDockerfileBoilerplate() string {
	var sb strings.Builder
	sb.WriteString("# Build stage\n")
	sb.WriteString("FROM golang:1.22-alpine AS builder\n\n")
	sb.WriteString("WORKDIR /app\n\n")
	sb.WriteString("COPY go.mod go.sum ./\n")
	sb.WriteString("RUN go mod download\n\n")
	sb.WriteString("COPY . .\n")
	sb.WriteString("RUN CGO_ENABLED=0 go build -o /app/bin/server .\n\n")
	sb.WriteString("# Runtime stage\n")
	sb.WriteString("FROM alpine:3.19\n\n")
	sb.WriteString("RUN apk --no-cache add ca-certificates\n\n")
	sb.WriteString("WORKDIR /app\n")
	sb.WriteString("COPY --from=builder /app/bin/server .\n\n")
	sb.WriteString("EXPOSE 8080\n\n")
	sb.WriteString("ENTRYPOINT [\"./server\"]\n")

	return sb.String()
}

// --- Makefile boilerplate ---

func (sc *SmartCreator) generateMakefileBoilerplate() string {
	var sb strings.Builder
	sb.WriteString(".PHONY: build test lint clean\n\n")
	sb.WriteString("build:\n")
	sb.WriteString("\tgo build -o bin/ ./...\n\n")
	sb.WriteString("test:\n")
	sb.WriteString("\tgo test -race ./...\n\n")
	sb.WriteString("lint:\n")
	sb.WriteString("\tgolangci-lint run ./...\n\n")
	sb.WriteString("clean:\n")
	sb.WriteString("\trm -rf bin/\n")

	return sb.String()
}

// --- YAML boilerplate ---

func (sc *SmartCreator) generateYAMLBoilerplate(path string) string {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	var sb strings.Builder
	sb.WriteString("# ")
	sb.WriteString(name)
	sb.WriteString(" configuration\n")
	sb.WriteString("#\n")
	sb.WriteString("# Purpose: TODO describe the purpose of this file\n")
	sb.WriteString("---\n")

	return sb.String()
}

// --- Helpers ---

func extractCopyrightHeader(filePath string) string {
	f, err := os.Open(filePath) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	var lines []string
	inHeader := false

	for scanner.Scan() {
		line := scanner.Text()

		if !inHeader {
			// Look for copyright in comment block at top of file.
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "/*") {
				lower := strings.ToLower(line)
				if strings.Contains(lower, "copyright") || strings.Contains(lower, "license") {
					inHeader = true
					lines = append(lines, line)
				}
			} else if trimmed != "" {
				// Non-comment, non-empty line — no header.
				break
			}
		} else {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, " *") || strings.HasPrefix(trimmed, "*/") {
				lines = append(lines, line)
				if strings.HasPrefix(trimmed, "*/") {
					break
				}
			} else {
				break
			}
		}
	}

	if len(lines) == 0 {
		return ""
	}

	return strings.Join(lines, "\n")
}

func extractImportStyle(filePath string, language string) string {
	f, err := os.Open(filePath) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	var importLines []string
	inImport := false

	for scanner.Scan() {
		line := scanner.Text()

		switch language {
		case "go":
			if strings.HasPrefix(strings.TrimSpace(line), "import") {
				inImport = true
				importLines = append(importLines, line)
				if !strings.Contains(line, "(") {
					// Single-line import.
					return strings.Join(importLines, "\n")
				}
			} else if inImport {
				importLines = append(importLines, line)
				if strings.Contains(line, ")") {
					return strings.Join(importLines, "\n")
				}
			}
		case "python":
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "from ") {
				importLines = append(importLines, line)
			} else if len(importLines) > 0 && trimmed == "" {
				return strings.Join(importLines, "\n")
			}
		case "typescript":
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "import ") {
				importLines = append(importLines, line)
			} else if len(importLines) > 0 && trimmed == "" {
				return strings.Join(importLines, "\n")
			}
		}
	}

	if len(importLines) > 0 {
		return strings.Join(importLines, "\n")
	}
	return ""
}

func extractGoExportedFunctions(filePath string) []string {
	f, err := os.Open(filePath) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	var functions []string
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "func ") {
			continue
		}

		rest := strings.TrimPrefix(line, "func ")

		// Skip method receivers: func (r *Receiver) Name(...)
		if strings.HasPrefix(rest, "(") {
			closeIdx := strings.Index(rest, ")")
			if closeIdx < 0 || closeIdx+1 >= len(rest) {
				continue
			}
			rest = strings.TrimSpace(rest[closeIdx+1:])
		}

		// Check if the function name starts with uppercase (exported).
		if len(rest) > 0 && rest[0] >= 'A' && rest[0] <= 'Z' {
			// Extract just the function name.
			nameEnd := strings.IndexAny(rest, "([ ")
			if nameEnd < 0 {
				continue
			}
			name := rest[:nameEnd]
			functions = append(functions, name)
		}
	}

	return functions
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	// Capitalize first letter.
	first := strings.ToUpper(s[:1])
	if len(s) == 1 {
		return first
	}
	return first + s[1:]
}

// --- SmartCreateTool ---

// SmartCreateTool implements the Tool interface for smart file creation.
type SmartCreateTool struct {
	Creator *SmartCreator
}

// smartCreateInput is the JSON input for the SmartCreate tool.
// SmartCreateInput is the typed input for SmartCreateTool.
type SmartCreateInput struct {
	Path string `json:"path"`
}

func (t *SmartCreateTool) Name() string {
	return "SmartCreate"
}

func (t *SmartCreateTool) Description() string {
	return "Creates a new file with appropriate boilerplate based on project conventions and file type."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (t *SmartCreateTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"path": {Type: "string", Description: "Path of the file to create"},
		},
		Required: []string{"path"},
	}
}

func (t *SmartCreateTool) Parameters() map[string]interface{} {
	return smartCreateSchema.ToJSONSchema()
}

// smartCreateSchema is the single source of truth for SmartCreate's input schema.
var smartCreateSchema = (&SmartCreateTool{}).Schema()

func (t *SmartCreateTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	params, err := DecodeInput[SmartCreateInput]("SmartCreate", input)
	if err != nil {
		return "", err
	}

	if params.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if err := validatePathAllowed(ctx, params.Path); err != nil {
		return "", err
	}

	// Generate boilerplate.
	content := t.Creator.GenerateBoilerplate(params.Path)

	// Ensure parent directory exists.
	dir := filepath.Dir(params.Path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Check if file already exists.
	if _, err := os.Stat(params.Path); err == nil {
		return "", fmt.Errorf("file already exists: %s", params.Path)
	}

	// Write the file.
	if err := os.WriteFile(params.Path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	if content == "" {
		return fmt.Sprintf("Created empty file: %s", params.Path), nil
	}
	return fmt.Sprintf("Created file with boilerplate: %s", params.Path), nil
}
