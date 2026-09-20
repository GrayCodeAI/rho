package cmd

import (
	"testing"

	"github.com/GrayCodeAI/rho/internal/engine/safety"
)

// Every built-in tool must have explicit capability metadata. Without this
// guard, a tool can be executable in the registry while the permission UI
// renders it as an unknown/high-risk action and policy inspection reports a
// different result from the live engine.
func TestBuiltInToolsHaveCapabilityPolicies(t *testing.T) {
	for _, candidate := range allTools() {
		policy := safety.ToolPolicyFor(candidate.Name())
		if len(policy.Capabilities) == 1 && policy.Capabilities[0] == safety.CapabilityUnknown {
			t.Errorf("built-in tool %q has no capability policy", candidate.Name())
		}
	}
}
