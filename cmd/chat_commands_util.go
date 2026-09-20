package cmd

import (
	"fmt"
	"strings"

	"github.com/GrayCodeAI/rho/internal/engine"
	"github.com/GrayCodeAI/rho/internal/features/taste"
	workspacefeature "github.com/GrayCodeAI/rho/internal/features/workspace"
	"github.com/GrayCodeAI/rho/internal/plugin"
	"github.com/GrayCodeAI/rho/internal/system/staleness"
)

func gitOutput(args ...string) (string, error) {
	return workspacefeature.GitOutput(args...)
}

func branchSummary() string {
	return workspacefeature.BranchSummary()
}

func filesSummary() string {
	return workspacefeature.FilesSummary()
}

func additionalDirContext(dir string) (string, string, error) {
	return workspacefeature.AdditionalDirContext(dir)
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (m *chatModel) mcpSummary() string {
	var b strings.Builder
	configured := len(m.settings.MCPServers) + len(mcpServers)
	if configured == 0 {
		b.WriteString("No MCP servers configured.")
	} else {
		b.WriteString(fmt.Sprintf("MCP servers configured: %d\n", configured))
		for _, cfg := range m.settings.MCPServers {
			name := cfg.Name
			if name == "" {
				name = cfg.Command
			}
			b.WriteString(fmt.Sprintf("  %s: %s %s\n", name, cfg.Command, strings.Join(cfg.Args, " ")))
		}
		for _, cmd := range mcpServers {
			b.WriteString("  cli: " + cmd + "\n")
		}
	}
	if m.registry != nil {
		var toolNames []string
		for _, t := range m.registry.FluxTools() {
			if strings.HasPrefix(t.Name, "mcp__") {
				toolNames = append(toolNames, t.Name)
			}
		}
		if len(toolNames) > 0 {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString("Connected MCP tools:\n  " + strings.Join(toolNames, "\n  "))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func sessionStats(sess *engine.Session, id string) string {
	return fmt.Sprintf("Session: %s\nMessages: %d\nModel: %s/%s\n%s",
		id, sess.MessageCount(), sess.Provider(), sess.Model(), sess.CostValue().Summary())
}

func hooksSummary() string {
	return "Hooks: pre_query, post_query, pre_tool, post_tool, session_start, session_end, permission_ask, error\nConfigure in Rho user settings"
}

func pluginsSummary(rt *plugin.Runtime) string {
	if rt == nil {
		return "No plugins loaded."
	}
	plugins := rt.ListPlugins()
	if len(plugins) == 0 {
		return "No plugins installed."
	}
	var b strings.Builder
	b.WriteString("Installed plugins:\n")
	for _, p := range plugins {
		b.WriteString(fmt.Sprintf("  %s (%s)\n", p.Name, p.Version))
	}
	return b.String()
}

// tasteStoreForSession returns a taste store using the default location.
func tasteStoreForSession() (*taste.Store, error) {
	return taste.NewStore("")
}

// stalenessFormatReport formats stale rules for display.
func stalenessFormatReport(rules []staleness.StaleRule) string {
	return staleness.FormatReport(rules)
}

// truncatePromptPreview truncates a prompt to a preview string.
func truncatePromptPreview(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
