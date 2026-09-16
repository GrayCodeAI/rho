package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// NotebookEditTool edits Jupyter notebook cells.
type NotebookEditTool struct{}

func (NotebookEditTool) Name() string      { return "NotebookEdit" }
func (NotebookEditTool) Aliases() []string { return []string{"notebook_edit"} }
func (NotebookEditTool) Description() string {
	return "Edit a Jupyter notebook cell. Specify the notebook path, cell number, and new source."
}

// NotebookEditInput is the typed input for NotebookEditTool.
type NotebookEditInput struct {
	Path       string `json:"path"`
	CellNumber int    `json:"cell_number"`
	NewSource  string `json:"new_source"`
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (NotebookEditTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"path":        {Type: "string", Description: "Notebook file path (.ipynb)"},
			"cell_number": {Type: "integer", Description: "Cell number (0-based)"},
			"new_source":  {Type: "string", Description: "New cell source content"},
		},
		Required: []string{"path", "cell_number", "new_source"},
	}
}

func (NotebookEditTool) Parameters() map[string]interface{} {
	return notebookEditSchema.ToJSONSchema()
}

// notebookEditSchema is the single source of truth for NotebookEdit's input schema.
var notebookEditSchema = NotebookEditTool{}.Schema()

func (NotebookEditTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[NotebookEditInput]("NotebookEdit", input)
	if err != nil {
		return "", err
	}
	if err := validatePathAllowed(ctx, p.Path); err != nil {
		return "", err
	}
	if reason := IsSensitivePath(p.Path); reason != "" {
		return "", fmt.Errorf("blocked: %s", reason)
	}
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return "", err
	}
	var nb map[string]interface{}
	if err := json.Unmarshal(data, &nb); err != nil {
		return "", fmt.Errorf("invalid notebook: %w", err)
	}
	cells, ok := nb["cells"].([]interface{})
	if !ok || p.CellNumber >= len(cells) {
		return "", fmt.Errorf("cell %d not found (notebook has %d cells)", p.CellNumber, len(cells))
	}
	cell, ok := cells[p.CellNumber].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("cell %d is not a valid JSON object", p.CellNumber)
	}
	lines := strings.Split(p.NewSource, "\n")
	sourceLines := make([]interface{}, len(lines))
	for i, l := range lines {
		if i < len(lines)-1 {
			sourceLines[i] = l + "\n"
		} else {
			sourceLines[i] = l
		}
	}
	cell["source"] = sourceLines
	out, _ := json.MarshalIndent(nb, "", " ")
	if tc := GetToolContext(ctx); tc != nil && tc.Protected != nil && tc.Protected.IsProtected(p.Path) {
		return "", fmt.Errorf("path %s is protected (read-only)", p.Path)
	}
	if cred := DetectCredentials(p.NewSource); cred != "" {
		return "", fmt.Errorf("content contains a credential (%s) — refusing to write", cred)
	}
	if err := os.WriteFile(p.Path, out, 0o600); err != nil {
		return "", err
	}
	return fmt.Sprintf("Edited cell %d in %s", p.CellNumber, p.Path), nil
}

// ConfigTool reads/writes rho settings.
type ConfigTool struct{}

func (ConfigTool) Name() string      { return "Config" }
func (ConfigTool) Aliases() []string { return []string{"config"} }
func (ConfigTool) Description() string {
	return "Read or modify rho configuration settings."
}

// ConfigInput is the typed input for ConfigTool.
type ConfigInput struct {
	Action string `json:"action"`
	Key    string `json:"key"`
	Value  string `json:"value"`
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (ConfigTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action": {Type: "string", Description: "Action", Enum: []interface{}{"get", "set"}},
			"key":    {Type: "string", Description: "Setting key"},
			"value":  {Type: "string", Description: "Setting value (for set)"},
		},
		Required: []string{"action", "key"},
	}
}

func (ConfigTool) Parameters() map[string]interface{} {
	return configSchema.ToJSONSchema()
}

// configSchema is the single source of truth for Config's input schema.
var configSchema = ConfigTool{}.Schema()

func (ConfigTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[ConfigInput]("Config", input)
	if err != nil {
		return "", err
	}
	tc := GetToolContext(ctx)
	switch p.Action {
	case "get":
		if tc == nil || tc.SettingsGet == nil {
			return "", fmt.Errorf("settings access not available")
		}
		value, ok := tc.SettingsGet(p.Key)
		if !ok {
			return "", fmt.Errorf("unsupported setting key: %s", p.Key)
		}
		return fmt.Sprintf("%s=%s", p.Key, value), nil
	case "set":
		if tc == nil || tc.SettingsSet == nil {
			return "", fmt.Errorf("settings access not available")
		}
		if err := tc.SettingsSet(p.Key, p.Value); err != nil {
			return "", err
		}
		return fmt.Sprintf("Set %q in global settings", p.Key), nil
	default:
		return "", fmt.Errorf("unknown action: %s", p.Action)
	}
}

// BriefTool (SendUserMessage) sends a message the user will read.
// Text outside this tool is visible in the detail view; the answer lives here.
type BriefTool struct{}

func (BriefTool) Name() string      { return "SendUserMessage" }
func (BriefTool) Aliases() []string { return []string{"brief", "Brief"} }
func (BriefTool) Description() string {
	return "Send a message to the user. Supports markdown. Use 'proactive' status when surfacing something the user hasn't asked for."
}

func (BriefTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"message": map[string]interface{}{
				"type":        "string",
				"description": "The message for the user. Supports markdown formatting.",
			},
			"attachments": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Optional file paths to attach (images, diffs, logs)",
			},
			"status": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"normal", "proactive"},
				"description": "Use 'proactive' when surfacing something the user hasn't asked for",
			},
		},
		"required": []string{"message"},
	}
}

func (BriefTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	var p struct {
		Message     string   `json:"message"`
		Attachments []string `json:"attachments"`
		Status      string   `json:"status"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return "", err
	}
	if p.Message == "" {
		return "", fmt.Errorf("message is required")
	}
	return p.Message, nil
}
