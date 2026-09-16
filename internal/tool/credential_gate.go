package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CredentialGateFn is the host-side callback that prompts the user to approve
// or deny credential access. It blocks until the user responds or the context
// expires. The containerID is provided so the caller can flip the symlink.
type CredentialGateFn func(req CredentialRequest) CredentialResponse

// CredentialRequest describes a credential the AI wants to access.
type CredentialRequest struct {
	Credential  string `json:"credential"`  // credential ID (e.g. "kube")
	Reason      string `json:"reason"`      // why the AI needs it
	Name        string `json:"name"`        // human-readable name
	Description string `json:"description"` // what it's for
	ContainerID string `json:"container_id,omitempty"`
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
		"available inside the sandbox. Use this when a command fails due to missing credentials."
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

	return fmt.Sprintf("Access to %q granted. The credential is now available inside the sandbox.", p.Credential), nil
}

// FlipCredentialSymlink flips the symlink for an approved credential inside the
// container. Called by the host after the user approves.
func FlipCredentialSymlink(containerID, credentialID, stagingPath, containerPath string) error {
	if containerID == "" {
		return fmt.Errorf("no container ID")
	}
	// Remove existing symlink and create a new one pointing to staging.
	containerDir := containerPath[:strings.LastIndex(containerPath, "/")]
	cmdArgs := fmt.Sprintf("rm -f %q && mkdir -p %q && ln -sfn %q %q",
		containerPath, containerDir, stagingPath, containerPath)
	cmd := exec.Command("docker", "exec", containerID, "sh", "-c", cmdArgs) // #nosec G204 -- cmdArgs is safely quoted with %q
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to flip symlink for %q: %s", credentialID, strings.TrimSpace(string(out)))
	}
	return nil
}

// RequestCredentialTimeout is how long the AI waits for the user to respond.
const RequestCredentialTimeout = 5 * time.Minute
