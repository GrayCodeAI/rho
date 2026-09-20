package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// CredentialGateFn is the host-side callback that prompts the user to approve
// or deny credential access. It blocks until the user responds or the context
// expires.
type CredentialGateFn func(req CredentialRequest) CredentialResponse

// CredentialRequest describes a credential the AI wants to access.
type CredentialRequest struct {
	Credential  string `json:"credential"`  // credential ID (e.g. "kube")
	Reason      string `json:"reason"`      // why the AI needs it
	Name        string `json:"name"`        // human-readable name
	Description string `json:"description"` // what it's for
}

// CredentialResponse is the user's decision.
type CredentialResponse struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}

// RequestCredentialTool lets the AI request access to a host credential.
type RequestCredentialTool struct {
	Gateway func() CredentialGateFn // returns the current gate callback (host-side)
}

func (RequestCredentialTool) Name() string      { return "RequestCredential" }
func (RequestCredentialTool) Aliases() []string { return []string{"request_credential"} }
func (RequestCredentialTool) Description() string {
	return "Request access to a host credential (e.g. kube config, AWS creds, git config). " +
		"The user will be prompted to approve or deny. Only approved credentials become " +
		"available to approved host operations. Use this when a command fails due to missing credentials."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (RequestCredentialTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"credential": {Type: "string", Description: "Credential ID to request. One of: gitconfig, kube, aws, gh, docker, gnupg, terraform."},
			"reason":     {Type: "string", Description: "Why this credential is needed (e.g. 'run kubectl get pods')."},
		},
		Required: []string{"credential", "reason"},
	}
}

func (RequestCredentialTool) Parameters() map[string]interface{} {
	return requestCredentialSchema.ToJSONSchema()
}

// requestCredentialSchema is the single source of truth for RequestCredential's input schema.
var requestCredentialSchema = RequestCredentialTool{}.Schema()

// RequestCredentialInput is the typed input for RequestCredentialTool.
type RequestCredentialInput struct {
	Credential string `json:"credential"`
	Reason     string `json:"reason"`
}

func (t RequestCredentialTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[RequestCredentialInput]("RequestCredential", input)
	if err != nil {
		return "", err
	}
	if p.Credential == "" {
		return "", fmt.Errorf("credential is required")
	}
	if p.Reason == "" {
		return "", fmt.Errorf("reason is required")
	}

	if t.Gateway == nil {
		return "", fmt.Errorf("credential gating is not configured — cannot request credentials")
	}
	gateFn := t.Gateway()
	if gateFn == nil {
		return "", fmt.Errorf("credential gate callback is nil")
	}

	req := CredentialRequest{
		Credential: p.Credential,
		Reason:     p.Reason,
	}

	// Block waiting for the user's decision (the callback handles the TUI prompt).
	resp := gateFn(req)
	if !resp.Approved {
		if resp.Reason != "" {
			return "", fmt.Errorf("credential %q denied: %s", p.Credential, resp.Reason)
		}
		return "", fmt.Errorf("credential %q denied by user", p.Credential)
	}

	return fmt.Sprintf("Access to %q granted for approved host operations.", p.Credential), nil
}

// RequestCredentialTimeout is how long the AI waits for the user to respond.
const RequestCredentialTimeout = 5 * time.Minute
