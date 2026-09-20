package evalloop

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine"
	"github.com/GrayCodeAI/rho/internal/tool"
)

// SessionRuntime drives the real engine.Session agent loop with an injected
// ChatClient. It is the production Runtime for end-to-end agent evaluation.
type SessionRuntime struct {
	// Client is the LLM backend. Use a provider-bound client for real evals or
	// a deterministic mock for CI smoke runs.
	Client engine.ChatClient
	// Provider and Model identify the backend for reporting.
	Provider string
	Model    string
	// Registry supplies the tool surface the loop can call.
	Registry *tool.Registry
	// Config carries loop limits and the system prompt.
	Config Config
}

// NewSessionRuntime builds a SessionRuntime.
func NewSessionRuntime(client engine.ChatClient, provider, model string, registry *tool.Registry, cfg Config) *SessionRuntime {
	return &SessionRuntime{Client: client, Provider: provider, Model: model, Registry: registry, Config: cfg}
}

// Run drives the real agent loop for prompt inside workDir. It creates a
// session bound to workDir, streams the loop to completion, and snapshots the
// transcript. The caller is responsible for running in an isolated directory.
func (r *SessionRuntime) Run(ctx context.Context, workDir, prompt string) (Result, error) {
	if r == nil || r.Client == nil {
		return Result{}, fmt.Errorf("evalloop: client is required")
	}
	systemPrompt := r.Config.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = DefaultConfig().SystemPrompt
	}
	registry := r.Registry
	if registry == nil {
		registry = tool.NewRegistry()
	}

	start := time.Now()
	sess := engine.NewSessionWithClient(r.Client, r.Provider, r.Model, systemPrompt, registry, false)
	if r.Config.MaxTurns > 0 {
		_ = sess.SetMaxTurns(r.Config.MaxTurns)
	}
	sess.AddUser(prompt)

	ch, err := sess.Stream(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("evalloop: start stream: %w", err)
	}

	var result Result
	for ev := range ch {
		event := Event{Type: ev.Type, Content: ev.Content, Timestamp: time.Now()}
		result.Events = append(result.Events, event)
		switch ev.Type {
		case "content":
			result.Output += ev.Content
		case "error":
			result.Events = append(result.Events, event)
		}
	}
	result.Duration = time.Since(start)
	result.Model = r.Model
	result.Provider = r.Provider

	// Snapshot the transcript for offline replay of failing runs.
	if msgs := sess.Persistence().RawMessages(); msgs != nil {
		if data, err := json.MarshalIndent(msgs, "", "  "); err == nil {
			result.Transcript = data
		}
	}

	// Reproducibility hash over the fixed inputs and the transcript.
	result.ReproHash = ReproHashOf(Inputs{
		Model: r.Model, Provider: r.Provider, Prompt: prompt,
		ConfigVersion: evalConfigVersion,
	}, result.Transcript)

	// Report usage/cost when the backend exposes it via the session cost model.
	cost := sess.CostValue()
	if cost != nil {
		usage := cost.Snapshot()
		result.CostUSD = usage.TotalCostUSD
	}
	return result, nil
}

// evalConfigVersion is bumped whenever loop limits or system-prompt semantics
// change, so reproducibility hashes are invalidated across versions.
const evalConfigVersion = 1
