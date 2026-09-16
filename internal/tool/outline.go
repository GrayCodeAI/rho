package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// OutlineTool extracts function/type/class signatures from files without
// reading the full content. Returns a compact outline with line numbers.
type OutlineTool struct{}

// OutlineInput is the typed input for OutlineTool.
type OutlineInput struct {
	FilePath  string   `json:"file_path"`
	FilePaths []string `json:"file_paths,omitempty"`
}

func (OutlineTool) Name() string      { return "Outline" }
func (OutlineTool) RiskLevel() string { return "low" }
func (OutlineTool) Aliases() []string { return []string{"outline"} }
func (OutlineTool) Description() string {
	return "Extract function/type/class signatures from one or more files. Returns a compact outline with line numbers — much cheaper than reading the full file."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (OutlineTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"file_path":  {Type: "string", Description: "Path to a single file, relative to the project root (e.g. 'src/main.go')"},
			"file_paths": {Type: "array", Items: &SchemaProperty{Type: "string"}, Description: "Paths to multiple files. Use instead of file_path to outline several files in one call (max 20)."},
		},
	}
}

func (OutlineTool) Parameters() map[string]interface{} {
	return outlineSchema.ToJSONSchema()
}

// outlineSchema is the single source of truth for Outline's input schema.
var outlineSchema = OutlineTool{}.Schema()

const outlineMaxFiles = 20

func (OutlineTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := DecodeInput[OutlineInput]("Outline", input)
	if err != nil {
		return "", err
	}

	var paths []string
	if in.FilePath != "" {
		paths = append(paths, in.FilePath)
	}
	paths = append(paths, in.FilePaths...)

	if len(paths) == 0 {
		return "", fmt.Errorf("file_path or file_paths is required")
	}
	if len(paths) > outlineMaxFiles {
		return "", fmt.Errorf("too many files: %d (max %d)", len(paths), outlineMaxFiles)
	}

	if len(paths) == 1 {
		return outlineOne(paths[0])
	}

	var parts []string
	for _, p := range paths {
		result, err := outlineOne(p)
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("=== %s ===\n%s", p, result))
	}
	return strings.Join(parts, "\n\n"), nil
}

// outlinePatterns maps file extensions to regex patterns for signature extraction.
var outlinePatterns = map[string]*regexp.Regexp{
	".go":   regexp.MustCompile(`^(func |type |var |const |package )`),
	".py":   regexp.MustCompile(`^(class |def |async def |\s+def |\s+async def )`),
	".js":   regexp.MustCompile(`^(export |function |class |const |let |var |async function )`),
	".jsx":  regexp.MustCompile(`^(export |function |class |const |let |var |async function )`),
	".ts":   regexp.MustCompile(`^(export |function |class |const |let |var |interface |type |enum |async function )`),
	".tsx":  regexp.MustCompile(`^(export |function |class |const |let |var |interface |type |enum |async function )`),
	".rs":   regexp.MustCompile(`^(pub |fn |struct |enum |trait |impl |mod |type |use )`),
	".java": regexp.MustCompile(`^(public |private |protected |class |interface |enum |static )`),
	".rb":   regexp.MustCompile(`^(class |module |def |end|  def )`),
	".c":    regexp.MustCompile(`^(static |extern |struct |typedef |enum |void |int |char |float |double )`),
	".h":    regexp.MustCompile(`^(static |extern |struct |typedef |enum |void |int |char |float |double |#ifndef |#define )`),
	".cpp":  regexp.MustCompile(`^(class |struct |namespace |template |void |int |bool |auto |static |const )`),
	".hpp":  regexp.MustCompile(`^(class |struct |namespace |template |void |int |bool |auto |static |const |#ifndef |#define )`),
}

func outlineOne(filePath string) (string, error) {
	ext := filepath.Ext(filePath)

	pattern, ok := outlinePatterns[ext]
	if !ok {
		// Unknown language — return first 20 and last 20 lines.
		return outlineHeadTail(filePath)
	}

	f, err := os.Open(filePath) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if pattern.MatchString(line) {
			lines = append(lines, fmt.Sprintf("%d: %s", lineNum, line))
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Sprintf("error reading: %v", err), nil
	}

	if len(lines) == 0 {
		return "(no declarations found)", nil
	}

	// Cap at 100 lines to keep output compact.
	if len(lines) > 100 {
		lines = lines[:100]
		lines = append(lines, fmt.Sprintf("... (%d more declarations)", len(lines)-100))
	}

	return strings.Join(lines, "\n"), nil
}

func outlineHeadTail(filePath string) (string, error) {
	f, err := os.Open(filePath) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}
	defer func() { _ = f.Close() }()

	var head []string
	var all []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		all = append(all, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return fmt.Sprintf("error reading: %v", err), nil
	}

	if len(all) == 0 {
		return "(empty file)", nil
	}

	headLines := 20
	if len(all) < headLines {
		headLines = len(all)
	}
	for i := 0; i < headLines; i++ {
		head = append(head, fmt.Sprintf("%d: %s", i+1, all[i]))
	}

	if len(all) > 40 {
		head = append(head, "---")
		tailStart := len(all) - 20
		for i := tailStart; i < len(all); i++ {
			head = append(head, fmt.Sprintf("%d: %s", i+1, all[i]))
		}
	}

	return strings.Join(head, "\n"), nil
}
