package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/GrayCodeAI/rho/internal/mcp"
)

// mcpClient is the minimal surface MCPTool needs from a connected MCP
// server, regardless of transport. CallTool and Close already have
// identical signatures across mcp.Server (stdio), mcp.HTTPServer, and
// mcp.WSServer — this interface lets MCPTool wrap any of them without
// changing any of those concrete types. ListTools deliberately isn't part
// of this interface: it's only ever called once per connect, directly on
// the concrete type, inside the transport-specific loader function below.
type mcpClient interface {
	CallTool(ctx context.Context, name string, args map[string]interface{}) (string, error)
	Close() error
}

var connectedMCPServers = struct {
	sync.RWMutex
	servers map[string]mcpClient
}{servers: make(map[string]mcpClient)}

// MCPTool wraps an MCP server tool as a rho tool.
type MCPTool struct {
	server      mcpClient
	serverName  string
	toolName    string
	aliases     []string
	remoteName  string
	description string
	schema      map[string]interface{}
}

func NewMCPTool(serverName string, server mcpClient, t mcp.Tool) *MCPTool {
	tsName := fmt.Sprintf("mcp__%s__%s", normalizeNameForMCP(serverName), normalizeNameForMCP(t.Name))
	legacyName := fmt.Sprintf("mcp_%s_%s", serverName, t.Name)
	return &MCPTool{
		server:      server,
		serverName:  serverName,
		toolName:    tsName,
		aliases:     []string{legacyName},
		remoteName:  t.Name,
		description: fmt.Sprintf("[MCP:%s] %s", serverName, t.Description),
		schema:      t.InputSchema,
	}
}

func (m *MCPTool) Name() string                       { return m.toolName }
func (m *MCPTool) Aliases() []string                  { return m.aliases }
func (m *MCPTool) Description() string                { return m.description }
func (m *MCPTool) Parameters() map[string]interface{} { return m.schema }
func (m *MCPTool) MCPServerName() string              { return m.serverName }

// Untrusted identifies MCP tools as external implementations. Their schema
// describes inputs, not side effects, so registration is never a capability
// declaration for permission purposes.
func (m *MCPTool) Untrusted() bool { return true }

// RiskLevel reports MCP tools as high risk. A remote MCP server is untrusted
// third-party code whose declared schema does not reveal whether the tool
// writes files, executes commands, or performs network calls, so it must not
// default to the "medium" bucket that skips the strongest permission prompts.
func (m *MCPTool) RiskLevel() string { return "high" }

func (m *MCPTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var args map[string]interface{}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}
	if err := m.validateRequired(args); err != nil {
		return "", err
	}
	return m.server.CallTool(ctx, m.remoteName, args)
}

// validateRequired enforces the tool's declared JSON-Schema "required" list
// before forwarding LLM-supplied arguments to the remote server, so a
// malformed or partial tool call is rejected locally instead of producing an
// opaque remote failure.
func (m *MCPTool) validateRequired(args map[string]interface{}) error {
	required, ok := m.schema["required"].([]interface{})
	if !ok {
		return nil
	}
	for _, r := range required {
		name, ok := r.(string)
		if !ok {
			continue
		}
		if _, present := args[name]; !present {
			return fmt.Errorf("mcp tool %s: missing required argument %q", m.toolName, name)
		}
	}
	return nil
}

func normalizeNameForMCP(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r)
		case r >= '0' && r <= '9':
			out = append(out, r)
		case r == '_' || r == '-':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

// LoadMCPTools connects to an MCP server over stdio and returns rho tools
// for all its tools.
func LoadMCPTools(ctx context.Context, name, command string, args ...string) ([]Tool, error) {
	server, err := mcp.Connect(ctx, name, command, args...)
	if err != nil {
		return nil, err
	}
	registerMCPServer(name, server)
	mcpTools, err := server.ListTools()
	if err != nil {
		_ = server.Close()
		return nil, err
	}
	var tools []Tool
	for _, t := range mcpTools {
		tools = append(tools, NewMCPTool(name, server, t))
	}
	return tools, nil
}

// LoadRemoteMCPTools connects to an MCP server over http, sse, or websocket
// and returns rho tools for all its tools. headers is merged onto every
// outgoing request/handshake by the transport (e.g. a static API key, or an
// auto-injected OAuth bearer token — see internal/mcp/oauth.go).
func LoadRemoteMCPTools(
	ctx context.Context,
	name, serverType, url string,
	headers map[string]string,
) ([]Tool, error) {
	var (
		server   mcpClient
		mcpTools []mcp.Tool
		listErr  error
		connErr  error
	)
	switch serverType {
	case "sse":
		s, err := mcp.ConnectSSE(ctx, name, url, headers)
		connErr = err
		if err == nil {
			server = s
			mcpTools, listErr = s.ListTools(ctx)
		}
	case "websocket":
		s, err := mcp.ConnectWS(ctx, name, url, headers)
		connErr = err
		if err == nil {
			server = s
			mcpTools, listErr = s.ListTools(ctx)
		}
	default: // "http"
		s, err := mcp.ConnectHTTP(ctx, name, url, headers)
		connErr = err
		if err == nil {
			server = s
			mcpTools, listErr = s.ListTools(ctx)
		}
	}
	if connErr != nil {
		return nil, connErr
	}
	registerMCPServer(name, server)
	if listErr != nil {
		_ = server.Close()
		return nil, listErr
	}
	var tools []Tool
	for _, t := range mcpTools {
		tools = append(tools, NewMCPTool(name, server, t))
	}
	return tools, nil
}

func registerMCPServer(name string, server mcpClient) {
	connectedMCPServers.Lock()
	defer connectedMCPServers.Unlock()
	connectedMCPServers.servers[name] = server
}

// listMCPServers returns every connected MCP server, keyed by the name it
// was registered under.
func listMCPServers() map[string]mcpClient {
	connectedMCPServers.RLock()
	defer connectedMCPServers.RUnlock()
	out := make(map[string]mcpClient, len(connectedMCPServers.servers))
	for name, server := range connectedMCPServers.servers {
		out[name] = server
	}
	return out
}

func getMCPServer(name string) (mcpClient, bool) {
	connectedMCPServers.RLock()
	defer connectedMCPServers.RUnlock()
	server, ok := connectedMCPServers.servers[name]
	return server, ok
}
