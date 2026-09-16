package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// MultiEditTool applies multiple edits to a single file in one call.
type MultiEditTool struct{}

func (MultiEditTool) Name() string      { return "MultiEdit" }
func (MultiEditTool) RiskLevel() string { return "medium" }
func (MultiEditTool) Aliases() []string { return []string{"multi_edit", "multi_file_edit"} }
func (MultiEditTool) Description() string {
	return "Apply multiple edits to a single file in one call. Each edit replaces an exact string match. Edits are applied sequentially."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (MultiEditTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"file_path": {Type: "string", Description: "File path to edit"},
			"edits": {
				Type:        "array",
				Description: "Array of edit operations",
				Items: &SchemaProperty{
					Type: "object",
					Properties: map[string]SchemaProperty{
						"old_string":  {Type: "string", Description: "Exact string to find"},
						"new_string":  {Type: "string", Description: "Replacement string"},
						"replace_all": {Type: "boolean", Description: "Replace all occurrences (default: first only)"},
					},
				},
			},
		},
	}
}

func (MultiEditTool) Parameters() map[string]interface{} {
	return multiEditSchema.ToJSONSchema()
}

// multiEditSchema is the single source of truth for MultiEdit's input schema.
var multiEditSchema = MultiEditTool{}.Schema()

// MultiEditInput is the typed input for MultiEditTool.
type MultiEditInput struct {
	FilePath string `json:"file_path"`
	Edits    []struct {
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all,omitempty"`
	} `json:"edits"`
}

func (MultiEditTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[MultiEditInput]("MultiEdit", input)
	if err != nil {
		return "", err
	}
	if p.FilePath == "" {
		return "", fmt.Errorf("file_path is required")
	}
	if len(p.Edits) == 0 {
		return "", fmt.Errorf("at least one edit is required")
	}
	if err := validatePathAllowed(ctx, p.FilePath); err != nil {
		return "", err
	}
	if reason := IsSensitivePath(p.FilePath); reason != "" {
		return "", fmt.Errorf("blocked: %s", reason)
	}
	if tc := GetToolContext(ctx); tc != nil && tc.Protected != nil && tc.Protected.IsProtected(p.FilePath) {
		return "", fmt.Errorf("path %s is protected (read-only)", p.FilePath)
	}

	data, err := readGuardedFile(ctx, p.FilePath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	_, _ = BackupFile(p.FilePath)
	content := string(data)

	applied, failed := 0, 0
	for i, edit := range p.Edits {
		if edit.OldString == "" {
			failed++
			continue
		}
		if !strings.Contains(content, edit.OldString) {
			failed++
			continue
		}
		if edit.ReplaceAll {
			content = strings.ReplaceAll(content, edit.OldString, edit.NewString)
		} else {
			content = strings.Replace(content, edit.OldString, edit.NewString, 1)
		}
		applied++
		_ = i
	}

	if applied == 0 {
		return fmt.Sprintf("No edits applied (%d failed — old_string not found in file).", failed), nil
	}

	if err := writeGuardedFile(ctx, p.FilePath, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return fmt.Sprintf("Applied %d/%d edit(s) to %s.", applied, applied+failed, p.FilePath), nil
}
