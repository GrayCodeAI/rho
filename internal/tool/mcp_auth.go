package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/GrayCodeAI/rho/internal/mcp"
)

// MCPAuthState tracks OAuth state for an MCP server.
type MCPAuthState struct {
	ServerName string `json:"serverName"`
	AuthURL    string `json:"authUrl,omitempty"`
	Status     string `json:"status"` // "pending", "authenticated", "error"
	Error      string `json:"error,omitempty"`
}

// MCPAuthManager tracks in-flight and completed OAuth flows for MCP
// servers, keyed by server name.
type MCPAuthManager struct {
	mu     sync.RWMutex
	states map[string]*MCPAuthState
}

var globalMCPAuthManager = &MCPAuthManager{states: make(map[string]*MCPAuthState)}

func GetMCPAuthManager() *MCPAuthManager { return globalMCPAuthManager }

func (m *MCPAuthManager) GetState(serverName string) (*MCPAuthState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.states[serverName]
	return s, ok
}

func (m *MCPAuthManager) setState(serverName string, state *MCPAuthState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[serverName] = state
}

// mcpAuthCallbackTimeout bounds how long the loopback listener waits for
// the user to complete authorization in their browser before giving up.
const mcpAuthCallbackTimeout = 5 * time.Minute

// McpAuthTool starts (or reports on) an OAuth authorization-code+PKCE flow
// for a remote MCP server. It returns immediately with the URL to visit;
// completion happens asynchronously once the user authorizes in their
// browser and the loopback callback fires — call this tool again with the
// same server_name later to check progress.
type McpAuthTool struct{}

// McpAuthInput is the typed input for McpAuthTool.
type McpAuthInput struct {
	ServerName string `json:"server_name"`
	ServerURL  string `json:"server_url"`
	ClientID   string `json:"client_id"`
}

func (McpAuthTool) Name() string      { return "McpAuth" }
func (McpAuthTool) Aliases() []string { return []string{"mcp_auth"} }
func (McpAuthTool) Description() string {
	return "Start OAuth authentication for an MCP server that requires authorization. " +
		"Call again with the same server_name to check progress once the user has visited the URL."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (McpAuthTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"server_name": {Type: "string", Description: "Name of the MCP server to authenticate"},
			"server_url":  {Type: "string", Description: "URL of the MCP server"},
			"client_id":   {Type: "string", Description: "Optional pre-registered OAuth client_id. If omitted, rho attempts dynamic client registration (RFC 7591) against the server's advertised registration endpoint."},
		},
		Required: []string{"server_name", "server_url"},
	}
}

func (McpAuthTool) Parameters() map[string]interface{} {
	return mcpAuthSchema.ToJSONSchema()
}

// mcpAuthSchema is the single source of truth for McpAuth's input schema.
var mcpAuthSchema = McpAuthTool{}.Schema()

func (McpAuthTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[McpAuthInput]("McpAuth", input)
	if err != nil {
		return "", err
	}
	if p.ServerName == "" {
		return "", fmt.Errorf("server_name is required")
	}
	if p.ServerURL == "" {
		return "", fmt.Errorf("server_url is required")
	}

	// Already authenticated, or a flow is already in flight: report status
	// rather than starting a duplicate flow (and a duplicate loopback
	// listener) for the same server.
	if existing, ok := globalMCPAuthManager.GetState(p.ServerName); ok &&
		(existing.Status == "pending" || existing.Status == "authenticated") {
		return authStatusJSON(existing), nil
	}

	meta, err := mcp.DiscoverOAuthMetadata(ctx, p.ServerURL)
	if err != nil {
		return authStatusJSON(failState(p.ServerName, err)), nil
	}

	redirectURI, resultCh, shutdown, err := mcp.StartLoopbackCallback(context.Background(), mcpAuthCallbackTimeout)
	if err != nil {
		return authStatusJSON(failState(p.ServerName, err)), nil
	}

	clientID := p.ClientID
	if clientID == "" {
		if meta.RegistrationEndpoint == "" {
			shutdown()
			return authStatusJSON(failState(p.ServerName, fmt.Errorf(
				"no client_id was provided and the server doesn't advertise a registration_endpoint "+
					"for dynamic client registration — pass client_id explicitly",
			))), nil
		}
		clientID, err = mcp.RegisterClient(ctx, meta.RegistrationEndpoint, redirectURI)
		if err != nil {
			shutdown()
			return authStatusJSON(failState(p.ServerName, fmt.Errorf("dynamic client registration failed: %w", err))), nil
		}
	}

	verifier, challenge, err := mcp.GeneratePKCE()
	if err != nil {
		shutdown()
		return authStatusJSON(failState(p.ServerName, err)), nil
	}
	reqState, err := mcp.GenerateState()
	if err != nil {
		shutdown()
		return authStatusJSON(failState(p.ServerName, err)), nil
	}

	authURL := mcp.BuildAuthorizationURL(meta, clientID, redirectURI, reqState, challenge, p.ServerURL)
	state := &MCPAuthState{ServerName: p.ServerName, AuthURL: authURL, Status: "pending"}
	globalMCPAuthManager.setState(p.ServerName, state)

	go completeMCPAuth(context.WithoutCancel(ctx), p.ServerName, meta, clientID, verifier, reqState, redirectURI, resultCh, shutdown)

	return authStatusJSON(state), nil
}

func failState(serverName string, err error) *MCPAuthState {
	state := &MCPAuthState{ServerName: serverName, Status: "error", Error: err.Error()}
	globalMCPAuthManager.setState(serverName, state)
	return state
}

// completeMCPAuth waits for the loopback callback, exchanges the code for
// tokens, and persists the result. It runs independently of the tool call
// that started it, since the user completing authorization in their
// browser is an open-ended, asynchronous step from rho's perspective.
func completeMCPAuth(
	parentCtx context.Context,
	serverName string,
	meta *mcp.AuthServerMetadata,
	clientID, verifier, wantState, redirectURI string,
	resultCh <-chan mcp.CallbackResult,
	shutdown func(),
) {
	defer shutdown()
	var result mcp.CallbackResult
	select {
	case result = <-resultCh:
	case <-parentCtx.Done():
		failState(serverName, parentCtx.Err())
		return
	}
	if result.Err != nil {
		failState(serverName, result.Err)
		return
	}
	if result.State != wantState {
		failState(serverName, fmt.Errorf("callback state did not match the authorization request (possible CSRF)"))
		return
	}

	ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
	defer cancel()
	tokens, err := mcp.ExchangeCode(ctx, meta, clientID, result.Code, verifier, redirectURI)
	if err != nil {
		failState(serverName, fmt.Errorf("token exchange failed: %w", err))
		return
	}

	stored := &mcp.StoredToken{
		AccessToken:   tokens.AccessToken,
		RefreshToken:  tokens.RefreshToken,
		ExpiresAt:     tokens.ExpiresAt,
		ClientID:      clientID,
		TokenEndpoint: meta.TokenEndpoint,
	}
	if err := mcp.SaveToken(serverName, stored); err != nil {
		failState(serverName, fmt.Errorf("failed to store token: %w", err))
		return
	}

	globalMCPAuthManager.setState(serverName, &MCPAuthState{ServerName: serverName, Status: "authenticated"})
}

func authStatusJSON(state *MCPAuthState) string {
	out, _ := json.Marshal(map[string]any{
		"status":  state.Status,
		"message": formatAuthMessage(state),
		"authUrl": state.AuthURL,
	})
	return string(out)
}

func formatAuthMessage(state *MCPAuthState) string {
	switch state.Status {
	case "pending":
		return fmt.Sprintf(
			"Please visit the following URL to authorize: %s\nOnce you approve it, this MCP server "+
				"will be usable the next time it connects (e.g. restart rho, or reconfigure it).",
			state.AuthURL,
		)
	case "authenticated":
		return fmt.Sprintf("Server %q is authenticated.", state.ServerName)
	case "error":
		return fmt.Sprintf("Authentication for %q failed: %s", state.ServerName, state.Error)
	default:
		return state.Error
	}
}

// AuthHeaderForMCPServer returns the "Bearer <token>" value to auto-inject
// as the Authorization header when connecting to a remote MCP server, if a
// valid (or refreshable) OAuth token is stored for it. If the stored token
// is close to expiring, it's refreshed and re-persisted first; if that
// fails (or there's no refresh token), it reports no token rather than
// connecting with a stale one.
func AuthHeaderForMCPServer(serverName string) (string, bool) {
	tok, err := mcp.LoadToken(serverName)
	if err != nil {
		return "", false
	}
	if tok.NeedsRefresh() {
		if tok.RefreshToken == "" || tok.TokenEndpoint == "" {
			return "", false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		refreshed, err := mcp.RefreshOAuthToken(ctx, &mcp.AuthServerMetadata{TokenEndpoint: tok.TokenEndpoint}, tok.ClientID, tok.RefreshToken)
		if err != nil {
			return "", false
		}
		tok.AccessToken = refreshed.AccessToken
		if refreshed.RefreshToken != "" {
			tok.RefreshToken = refreshed.RefreshToken
		}
		tok.ExpiresAt = refreshed.ExpiresAt
		_ = mcp.SaveToken(serverName, tok)
	}
	return "Bearer " + tok.AccessToken, true
}
