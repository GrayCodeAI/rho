package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/GrayCodeAI/rho/internal/toolset"
)

// ToolsetTool lists and resolves named, composable tool groups (research,
// dev, ops, full_stack). It lets a user/agent scope the tool surface instead
// of always advertising every tool — adopted from Hermes Agent's toolset
// system.
type ToolsetTool struct{}

// ToolsetInput is the typed input for ToolsetTool.
type ToolsetInput struct {
	Action string `json:"action"`
	Name   string `json:"name"`
}

func (ToolsetTool) Name() string      { return "Toolset" }
func (ToolsetTool) RiskLevel() string { return "low" }
func (ToolsetTool) Aliases() []string { return []string{"toolset"} }
func (ToolsetTool) Description() string {
	return "List available toolsets or resolve one to its concrete tool list. Toolsets are named, composable groups (research, dev, ops, full_stack); resolving expands required toolsets transitively."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (ToolsetTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action": {Type: "string", Enum: []interface{}{"list", "resolve"}, Description: "list: show available toolsets; resolve: expand a toolset to its tools."},
			"name":   {Type: "string", Description: "Toolset name to resolve (action=resolve)."},
		},
		Required: []string{"action"},
	}
}

func (ToolsetTool) Parameters() map[string]interface{} {
	return toolsetSchema.ToJSONSchema()
}

// toolsetSchema is the single source of truth for Toolset's input schema.
var toolsetSchema = ToolsetTool{}.Schema()

func (ToolsetTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[ToolsetInput]("Toolset", input)
	if err != nil {
		return "", err
	}
	reg, err := toolset.NewRegistry(toolset.Defaults())
	if err != nil {
		return "", err
	}
	switch strings.ToLower(strings.TrimSpace(p.Action)) {
	case "list":
		return "Available toolsets: " + strings.Join(reg.Names(), ", "), nil
	case "resolve":
		tools, err := reg.Resolve(strings.TrimSpace(p.Name))
		if err != nil {
			return "", err
		}
		payload := map[string]interface{}{
			"toolset": strings.TrimSpace(p.Name),
			"tools":   tools,
			"count":   len(tools),
		}
		out, _ := json.MarshalIndent(payload, "", "  ")
		return string(out), nil
	default:
		return "", fmt.Errorf("unsupported action %q (use list or resolve)", p.Action)
	}
}
