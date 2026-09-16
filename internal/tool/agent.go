package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	agentcontracts "github.com/GrayCodeAI/rho/internal/contracts/agent"
)

const (
	// maxAgentPromptBytes caps a sub-agent prompt so a single LLM-emitted
	// tool call cannot balloon memory with an enormous prompt.
	maxAgentPromptBytes = 256 * 1024 // 256KB
	// maxParallelAgentTasks caps how many sub-agents a single MultiAgent call
	// will fan out to, bounding goroutine and memory growth from an
	// LLM-supplied tasks array.
	maxParallelAgentTasks = 32
	// maxConcurrentAgentTasks bounds how many sub-agents run at once in the
	// synchronous MultiAgent path so we don't fire dozens of LLM calls
	// simultaneously.
	maxConcurrentAgentTasks = 8
)

type AgentTool struct{}

// AgentInput is the typed input for AgentTool.
type AgentInput struct {
	Prompt          string `json:"prompt"`
	Description     string `json:"description"`
	SubagentType    string `json:"subagent_type"`
	CapabilityMode  string `json:"capability_mode"`
	Isolation       string `json:"isolation"`
	Thoroughness    string `json:"thoroughness"`
	CWD             string `json:"cwd"`
	Model           string `json:"model"`
	RunInBackground bool   `json:"run_in_background"`
	AgentID         string `json:"agent_id"`
	ResumeFrom      string `json:"resume_from"`
	RetryOf         string `json:"retry_of"`
}

func (AgentTool) Name() string      { return "Agent" }
func (AgentTool) RiskLevel() string { return "medium" }
func (AgentTool) Aliases() []string { return []string{"agent", "Task"} }
func (AgentTool) Description() string {
	return "Spawn a sub-agent to handle a complex task independently. " +
		"Choose subagent_type: explore (read-only research), plan (read-only planning), " +
		"or general-purpose (full tools). Optional capability_mode, isolation, thoroughness, " +
		"cwd, model, description, and run_in_background control spawn behavior."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (AgentTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"prompt":            {Type: "string", Description: "Task description for the sub-agent"},
			"description":       {Type: "string", Description: "Short human-readable label for the spawn (3–5 words)."},
			"subagent_type":     {Type: "string", Enum: []interface{}{"explore", "plan", "general-purpose", "general"}, Description: "explore | plan | general-purpose (alias: general). Default: explore."},
			"capability_mode":   {Type: "string", Enum: []interface{}{"read-only", "read-write", "execute", "all"}, Description: "read-only | read-write | execute | all. Defaults from subagent_type when omitted."},
			"isolation":         {Type: "string", Enum: []interface{}{"none", "worktree"}, Description: "none | worktree. Mutually exclusive with cwd when worktree."},
			"thoroughness":      {Type: "string", Enum: []interface{}{"quick", "medium", "very-thorough"}, Description: "Explore only: quick | medium | very-thorough."},
			"cwd":               {Type: "string", Description: "Working directory for the sub-agent. Mutually exclusive with isolation=worktree."},
			"model":             {Type: "string", Description: "Optional model override for the sub-agent."},
			"run_in_background": {Type: "boolean", Description: "If true, spawn asynchronously — results are collected when the main turn ends."},
			"agent_id":          {Type: "string", Description: "ID of a previous sub-agent to query status/result (legacy resume lookup)."},
			"resume_from":       {Type: "string", Description: "Subagent ID to resume with full transcript (typed spawn)."},
			"retry_of":          {Type: "string", Description: "ID of a failed sub-agent to retry. Spawns a new agent with the same request."},
		},
		Required: []string{"prompt"},
	}
}

func (AgentTool) Parameters() map[string]interface{} {
	return agentSchema.ToJSONSchema()
}

// agentSchema is the single source of truth for Agent's input schema.
var agentSchema = AgentTool{}.Schema()

func (AgentTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[AgentInput]("Agent", input)
	if err != nil {
		return "", err
	}
	if len(p.Prompt) > maxAgentPromptBytes {
		return "", fmt.Errorf("agent prompt too large: %d bytes (max %d)", len(p.Prompt), maxAgentPromptBytes)
	}
	tc := GetToolContext(ctx)
	if tc == nil || tc.AgentSpawnFn == nil {
		return "", fmt.Errorf("agent spawning not configured")
	}

	// Resume/status lookup for a previous agent by ID.
	if p.AgentID != "" {
		if tc.BackgroundManager == nil {
			return "", fmt.Errorf("background agent manager not configured")
		}
		result, ok := tc.BackgroundManager.GetResult(p.AgentID)
		if ok {
			return agentEnvelopeWithID(p.AgentID, "completed", result.Output), nil
		}
		if tc.BackgroundManager.IsRunning(p.AgentID) {
			elapsed := tc.BackgroundManager.Elapsed(p.AgentID)
			return fmt.Sprintf(`{"agent":"%s","status":"running","elapsed":"%s"}`, p.AgentID, elapsed), nil
		}
		return "", fmt.Errorf("agent_id %q not found", p.AgentID)
	}

	// Retry a failed agent with its original request.
	if p.RetryOf != "" {
		if tc.BackgroundManager == nil {
			return "", fmt.Errorf("background agent manager not configured")
		}
		result, ok := tc.BackgroundManager.GetResult(p.RetryOf)
		if !ok {
			return "", fmt.Errorf("retry_of %q not found or still running", p.RetryOf)
		}
		req := result.Request
		if req.Prompt == "" {
			req.Prompt = result.Prompt
		}
		id := fmt.Sprintf("retry-%d", time.Now().UnixNano())
		req.Background = true
		tc.BackgroundManager.Spawn(ctx, id, req, tc.AgentSpawnFn)
		return fmt.Sprintf(`{"agent":"%s","retry_of":"%s","status":"running","message":"Retrying failed agent."}`, id, p.RetryOf), nil
	}

	req := agentcontracts.SpawnRequest{
		Prompt:         p.Prompt,
		Description:    p.Description,
		SubagentType:   p.SubagentType,
		CapabilityMode: p.CapabilityMode,
		Isolation:      p.Isolation,
		ResumeFrom:     p.ResumeFrom,
		CWD:            p.CWD,
		Model:          p.Model,
		Background:     p.RunInBackground,
		Thoroughness:   p.Thoroughness,
	}
	if _, err := req.Normalize(); err != nil {
		return "", err
	}

	if p.RunInBackground {
		if tc.BackgroundManager == nil {
			return "", fmt.Errorf("background agent manager not configured")
		}
		id := fmt.Sprintf("bg-%d", time.Now().UnixNano())
		tc.BackgroundManager.Spawn(ctx, id, req, tc.AgentSpawnFn)
		return fmt.Sprintf(`{"agent":"%s","status":"running","message":"Sub-agent spawned in background. Results will be injected when the main turn ends."}`, id), nil
	}

	res, err := tc.AgentSpawnFn(ctx, req)
	if err != nil {
		return "", err
	}
	out := res.Output
	if out == "" {
		out = res.Summary
	}
	id := res.SubagentID
	if id == "" {
		id = "sub-agent"
	}
	status := res.Status
	if status == "" {
		status = "success"
	}
	return agentEnvelopeWithID(id, status, out), nil
}

func agentEnvelope(status, output string) string {
	return agentEnvelopeWithID("sub-agent", status, output)
}

func agentEnvelopeWithID(id, status, output string) string {
	summary := output
	if len(summary) > 200 {
		summary = summary[:200]
	}
	env := struct {
		Agent      string `json:"agent"`
		Status     string `json:"status"`
		Summary    string `json:"summary"`
		TokensUsed int    `json:"tokens_used"`
		FullOutput string `json:"full_output"`
	}{
		Agent:      id,
		Status:     status,
		Summary:    summary,
		TokensUsed: 0,
		FullOutput: output,
	}
	b, _ := json.Marshal(env)
	return string(b)
}

// MultiAgentTool spawns multiple sub-agents in parallel.
type MultiAgentTool struct{}

func (MultiAgentTool) Name() string      { return "MultiAgent" }
func (MultiAgentTool) Aliases() []string { return []string{"multi_agent"} }
func (MultiAgentTool) Description() string {
	return "Spawn multiple sub-agents in parallel for independent tasks. " +
		"Each task is a prompt string (explore by default) or an object with typed spawn fields."
}

// MultiAgentInput is the typed input for MultiAgentTool.
type MultiAgentInput struct {
	Tasks           []json.RawMessage `json:"tasks"`
	RunInBackground bool              `json:"run_in_background"`
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (MultiAgentTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"tasks": {
				Type: "array",
				Items: &SchemaProperty{
					OneOf: []SchemaProperty{
						{Type: "string"},
						{
							Type: "object",
							Properties: map[string]SchemaProperty{
								"prompt":          {Type: "string"},
								"subagent_type":   {Type: "string"},
								"capability_mode": {Type: "string"},
								"isolation":       {Type: "string"},
								"thoroughness":    {Type: "string"},
								"description":     {Type: "string"},
								"model":           {Type: "string"},
								"cwd":             {Type: "string"},
							},
							Required: []string{"prompt"},
						},
					},
				},
			},
			"run_in_background": {Type: "boolean", Description: "If true, spawn all sub-agents in the background."},
		},
		Required: []string{"tasks"},
	}
}

func (MultiAgentTool) Parameters() map[string]interface{} {
	return multiAgentSchema.ToJSONSchema()
}

// multiAgentSchema is the single source of truth for MultiAgent's input schema.
var multiAgentSchema = MultiAgentTool{}.Schema()

func (MultiAgentTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[MultiAgentInput]("MultiAgent", input)
	if err != nil {
		return "", err
	}
	if len(p.Tasks) > maxParallelAgentTasks {
		return "", fmt.Errorf("too many tasks: %d (max %d)", len(p.Tasks), maxParallelAgentTasks)
	}

	reqs := make([]agentcontracts.SpawnRequest, 0, len(p.Tasks))
	for _, raw := range p.Tasks {
		req, err := parseMultiAgentTask(raw)
		if err != nil {
			return "", err
		}
		if len(req.Prompt) > maxAgentPromptBytes {
			return "", fmt.Errorf("task prompt too large: %d bytes (max %d)", len(req.Prompt), maxAgentPromptBytes)
		}
		if _, err := req.Normalize(); err != nil {
			return "", err
		}
		reqs = append(reqs, req)
	}

	tc := GetToolContext(ctx)
	if tc == nil || tc.AgentSpawnFn == nil {
		return "", fmt.Errorf("agent spawning not configured")
	}

	if p.RunInBackground {
		if tc.BackgroundManager == nil {
			return "", fmt.Errorf("background agent manager not configured")
		}
		ids := make([]string, len(reqs))
		for i, req := range reqs {
			id := fmt.Sprintf("bg-%d-%d", time.Now().UnixNano(), i)
			req.Background = true
			tc.BackgroundManager.Spawn(ctx, id, req, tc.AgentSpawnFn)
			ids[i] = id
		}
		b, _ := json.Marshal(ids)
		return fmt.Sprintf(`{"agents":%s,"status":"running","message":"%d sub-agents spawned in background."}`, string(b), len(ids)), nil
	}

	type result struct {
		idx    int
		output string
		err    error
	}
	results := make([]result, len(reqs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentAgentTasks)
	for i, req := range reqs {
		wg.Add(1)
		go func(idx int, r agentcontracts.SpawnRequest) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res, err := tc.AgentSpawnFn(ctx, r)
			out := res.Output
			if out == "" {
				out = res.Summary
			}
			results[idx] = result{idx: idx, output: out, err: err}
		}(i, req)
	}
	wg.Wait()
	envelopes := make([]json.RawMessage, len(results))
	for i, r := range results {
		var status, output string
		if r.err != nil {
			status = "error"
			output = r.err.Error()
		} else {
			status = "success"
			output = r.output
		}
		envelopes[i] = json.RawMessage(agentEnvelope(status, output))
	}
	b, _ := json.Marshal(envelopes)
	return string(b), nil
}

func parseMultiAgentTask(raw json.RawMessage) (agentcontracts.SpawnRequest, error) {
	// String form: treat as explore prompt.
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return agentcontracts.SpawnRequest{Prompt: asString}, nil
	}
	var obj struct {
		Prompt         string `json:"prompt"`
		Description    string `json:"description"`
		SubagentType   string `json:"subagent_type"`
		CapabilityMode string `json:"capability_mode"`
		Isolation      string `json:"isolation"`
		Thoroughness   string `json:"thoroughness"`
		CWD            string `json:"cwd"`
		Model          string `json:"model"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return agentcontracts.SpawnRequest{}, fmt.Errorf("invalid multi-agent task: %w", err)
	}
	return agentcontracts.SpawnRequest{
		Prompt:         obj.Prompt,
		Description:    obj.Description,
		SubagentType:   obj.SubagentType,
		CapabilityMode: obj.CapabilityMode,
		Isolation:      obj.Isolation,
		Thoroughness:   obj.Thoroughness,
		CWD:            obj.CWD,
		Model:          obj.Model,
	}, nil
}
