package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/GrayCodeAI/rho/internal/mcp"
)

// PluginToolAdapter wraps a PluginTool as a tool.Tool interface
// so it can be registered in the main tool registry.
type PluginToolAdapter struct {
	plugin  *DynamicPlugin
	tool    PluginTool
	manager *DynamicPluginManager
}

// Name returns the fully qualified tool name (plugin__pluginName__toolName).
func (a *PluginToolAdapter) Name() string {
	return fmt.Sprintf("plugin__%s__%s", a.plugin.Plugin.Name, a.tool.Name)
}

// Description returns the tool description.
func (a *PluginToolAdapter) Description() string {
	if a.tool.Description != "" {
		return a.tool.Description
	}
	return fmt.Sprintf("Tool %q from plugin %q", a.tool.Name, a.plugin.Plugin.Name)
}

// Parameters returns the JSON schema for tool input, pruned to the subset that
// strict OpenAI-compatible function-calling endpoints accept. MCP servers may
// publish full JSON Schema documents; mcp.SanitizeToolSchema keeps only
// {type, properties, required} (and a per-property keyword subset) so a verbose
// upstream schema does not get a tool rejected by the provider. A nil schema
// yields the canonical empty-object schema.
func (a *PluginToolAdapter) Parameters() map[string]interface{} {
	return mcp.SanitizeToolSchema(a.tool.InputSchema)
}

// Execute runs the tool via the plugin manager.
func (a *PluginToolAdapter) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	if a.plugin.State != StateActive {
		return "", fmt.Errorf("plugin %q is not active (state: %s)", a.plugin.Plugin.Name, a.plugin.State)
	}

	return a.manager.ExecuteTool(ctx, a.plugin.Plugin.Name, a.tool.Name, input)
}

// Aliases returns alternative names for this tool.
func (a *PluginToolAdapter) Aliases() []string {
	return []string{
		fmt.Sprintf("plugin__%s__%s", a.plugin.Plugin.Name, a.tool.Name),
	}
}

// RiskLevel returns the risk classification for this plugin tool.
// Daemon plugins are considered lower risk since they run in a controlled process.
// Subprocess plugins default to medium risk.
func (a *PluginToolAdapter) RiskLevel() string {
	if a.plugin.ManifestV2 != nil && a.plugin.ManifestV2.Mode == "daemon" {
		return "low"
	}
	return "medium"
}

// Untrusted marks plugin tools as external implementations. A plugin's
// registry entry and risk label do not describe its actual side effects, so
// the permission engine requires explicit approval before execution.
func (a *PluginToolAdapter) Untrusted() bool { return true }

// PluginName returns the name of the owning plugin.
func (a *PluginToolAdapter) PluginName() string {
	return a.plugin.Plugin.Name
}

// ToolName returns the unqualified tool name.
func (a *PluginToolAdapter) ToolName() string {
	return a.tool.Name
}
