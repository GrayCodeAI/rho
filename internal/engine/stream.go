package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine/safety"

	"github.com/GrayCodeAI/rho/internal/engine/cost"
	"github.com/GrayCodeAI/rho/internal/engine/token"

	"github.com/GrayCodeAI/rho/internal/engine/control"

	"github.com/GrayCodeAI/rho/internal/provider/gateway"
	"github.com/GrayCodeAI/rho/internal/smartrouting"
	"github.com/GrayCodeAI/rho/internal/types"

	"github.com/GrayCodeAI/rho/internal/engine/branching"
	"github.com/GrayCodeAI/rho/internal/eventlog"
	"github.com/GrayCodeAI/rho/internal/hooks"
	"github.com/GrayCodeAI/rho/internal/observability/oteltrace"
	"github.com/GrayCodeAI/rho/internal/plugin"
	"github.com/GrayCodeAI/rho/internal/prompt"
	"github.com/GrayCodeAI/rho/internal/tool"

	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

// turnContext carries the per-turn, pre-compute values that buildTurnOptions
// assembles into a ChatOptions. Extracting it keeps agentLoop readable and
// gives the prompt-assembly logic a single home (H3).
type turnContext struct {
	ctx         context.Context
	activeModel string
	maxTok      int
	smallTalk   bool
	lastUserMsg string
	taskType    string
}

// buildTurnOptions assembles the ChatOptions for a single agentic turn: base
// system prompt (tiered for small-talk), ephemeral injections (beliefs, memory
// nudge, smart skills, spec stage, work mode), promoted tools, and the active
// model. Kept side-effect free except for PromoteForIntent (a cache warm) so it
// can be reasoned about and tested in isolation from the LLM call + stream
// consumption that follow it in agentLoop.
func (s *Session) buildTurnOptions(tc turnContext) types.ChatOptions {
	activeModel := tc.activeModel
	maxTok := tc.maxTok
	smallTalk := tc.smallTalk
	lastUserMsg := tc.lastUserMsg

	baseSystem := s.Persistence().System()
	if smallTalk {
		// The identity preamble already coaches the model to answer
		// greetings without tools — the role/tool/practice sections
		// below it only add prefill cost on this turn.
		baseSystem = prompt.System()
	}
	baseOpts := s.ChatLLM().BuildOptions(baseSystem, activeModel, maxTok, nil)
	opts := baseOpts
	// Inject beliefs as ephemeral context (not persisted to s.Persistence().System())
	if s.LifecycleSvc().Beliefs() != nil && s.LifecycleSvc().Beliefs().Size() > 0 {
		if summary := s.LifecycleSvc().Beliefs().FormatForPrompt(); summary != "" {
			opts.System += "\n\n## Agent Beliefs\n" + summary
		}
	}
	// Conversation arc: durable goals/decisions/milestones/phase, injected
	// ephemerally. Byte-stable, so unchanged arcs don't churn the prompt cache.
	if s.Arc() != nil && !s.Arc().IsEmpty() {
		if sum := s.Arc().Summary(); sum != "" {
			opts.System += "\n\n## Conversation Arc\n" + sum
		}
	}
	// Activity nudge: remind agent to persist learnings if idle. Injected
	// ephemerally (not persisted) so it never accumulates across turns.
	if s.MemorySvc().Activity() != nil {
		if nudge := s.MemorySvc().Activity().NudgeMessage(); nudge != "" {
			opts.System += "\n\n" + nudge
		}
	}
	// Auto-skill: match smart skills against the last user message and
	// inject a compact listing. The LLM uses the Skill tool for full content.
	smartSkills := s.LifecycleSvc().SmartSkills()
	if len(smartSkills) > 0 {
		lum := ""
		for i := len(s.Persistence().RawMessages()) - 1; i >= 0; i-- {
			if s.Persistence().RawMessages()[i].Role == "user" && len(s.Persistence().RawMessages()[i].ToolResults) == 0 {
				lum = s.Persistence().RawMessages()[i].Content
				break
			}
		}
		if lum != "" {
			if matched := plugin.MatchSkillsByContext(smartSkills, lum); len(matched) > 0 {
				if skillsPrompt := plugin.FormatSkillsCompact(matched); skillsPrompt != "" {
					opts.System += "\n\n" + skillsPrompt
				}
			}
		}
	}
	// Spec stage: steer the model through Specify -> Plan -> Tasks and an
	// explicit approval handoff before any changes. Ephemeral (not
	// persisted to s.Persistence().System()) so it disappears once the
	// stage advances to Implementing.
	if stage := s.PermSvc().SpecStage(); stage != safety.SpecStageNone && stage != safety.SpecStageImplementing {
		opts.System += specStageSystemPrompt
		// Inject project constitution as governing principles
		if constitution := constitutionForPrompt(s.PermSvc().SpecSlug()); constitution != "" {
			opts.System += constitution
		}
		// Inject user's spec configuration (language, framework, etc.)
		// as context so the model writes specs that match preferences.
		if cfgPrompt := specConfigForPrompt(); cfgPrompt != "" {
			opts.System += cfgPrompt
		}
	}
	// Work mode (plan/act/review) — ephemeral product control plane.
	if addon := s.workModeSystemAddon(); addon != "" {
		opts.System += "\n\n" + addon
	}
	if s.Tools() != nil && s.Tools().Registry() != nil {
		// Promote only the small set of registered tools that match the
		// current request. This keeps the default schema compact while
		// making URL, verification, git, and code-intelligence requests
		// discoverable without requiring the model to guess ToolSearch.
		if lastUserMsg != "" {
			s.Tools().Registry().PromoteForIntent(lastUserMsg)
		}
		if !smallTalk {
			opts.Tools = s.Tools().Registry().FluxTools()
		}
	}
	return opts
}

// Stream runs the agentic loop: LLM → tool_use → execute → loop.
func (s *Session) Stream(ctx context.Context) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, 64)
	go s.agentLoop(ctx, ch)
	return ch, nil
}

func (s *Session) agentLoop(ctx context.Context, ch chan<- StreamEvent) {
	defer close(ch)
	// emit sends an event to the consumer, but abandons the send (instead of
	// blocking forever) if the context is already done — e.g. the consumer
	// exited early after an error. This bounds the agentLoop goroutine and
	// prevents leaks on the post-stream bare sends below.
	emit := func(ev StreamEvent) {
		select {
		case ch <- ev:
		case <-ctx.Done():
		}
	}
	sessionStart := time.Now()

	// Start session-level trace span
	var sessionSpan *oteltrace.Span
	if s.TracerValue() != nil {
		ctx, sessionSpan = oteltrace.StartSessionSpan(ctx, s.TracerValue(), fmt.Sprintf("%d", sessionStart.UnixNano()))
		defer oteltrace.EndSpanWithError(sessionSpan, nil)
	}

	// Lifecycle and memory bookkeeping consume immutable service snapshots,
	// keeping the agent loop independent of backend-specific state.
	defer func() {
		success := ctx.Err() == nil
		if !success {
			// Interrupted mid-turn (Esc/cancel): drop any dangling partial
			// assistant tool_use or orphaned tool_result so the transcript
			// stays valid for the next provider call.
			s.Persistence().TrimIncompleteTurn()
		}
		messages := s.Persistence().Messages()
		s.LifecycleSvc().Finalize(ctx, messages, success, time.Since(sessionStart), s.CostValue().TotalUSD())
		s.MemorySvc().Finalize(messages, success)
	}()

	// Session start hook
	if j := s.Persistence().Journal(); j != nil {
		j.AppendHookInvoked(string(hooks.EventSessionStart))
	}
	hooks.ExecuteAsync(ctx, hooks.EventSessionStart, map[string]interface{}{
		"provider": s.ChatLLM().Provider(),
		"model":    s.ChatLLM().Model(),
	})

	// Self-improvement: inject learned guidelines and skills from prior sessions
	if s.LifecycleSvc().Lifecycle() != nil && len(s.Persistence().RawMessages()) > 0 {
		lastMsg := s.Persistence().RawMessages()[len(s.Persistence().RawMessages())-1].Content
		if learnedCtx := s.LifecycleSvc().StartContext(ctx, lastMsg); learnedCtx != "" {
			s.AppendSystemContext(learnedCtx)
		}
	}

	// Inject remembered context through the memory service boundary.
	if len(s.Persistence().RawMessages()) > 0 {
		lastMsg := s.Persistence().RawMessages()[len(s.Persistence().RawMessages())-1].Content
		if remembered := s.MemorySvc().RecallContext(ctx, lastMsg, 2000); remembered != "" {
			s.AppendSystemContext(remembered)
		}
	}

	// Few-shot learning: inject relevant examples from past successful sessions
	if s.LifecycleSvc().FewShotStore() != nil && len(s.Persistence().RawMessages()) > 0 {
		lastMsg := s.Persistence().RawMessages()[len(s.Persistence().RawMessages())-1].Content
		if fewShotCtx := s.LifecycleSvc().FewShotStore().FormatForPrompt(lastMsg); fewShotCtx != "" {
			s.AppendSystemContext(fewShotCtx)
		}
	}

	// Adaptive prompt: inject user preferences learned from corrections
	if s.LifecycleSvc().AdaptivePrompt() != nil {
		if prefs := s.LifecycleSvc().AdaptivePrompt().FormatForPrompt(); prefs != "" {
			s.AppendSystemContext(prefs)
		}
	}

	// Agents accumulator: inject learnings from previous sessions
	if s.LifecycleSvc().AgentsAccum() != nil {
		if learnings := s.LifecycleSvc().AgentsAccum().ForPrompt(5); learnings != "" {
			s.AppendSystemContext(learnings)
		}
	}

	// Auto-skill: load smart skills once at session start for per-turn matching
	s.LifecycleSvc().LoadSmartSkills()

	recoveryCount := 0
	turnCount := 0
	toolTurns := 0 // turns that used tools (for skill distillation)
	var toolsUsedSet map[string]bool
	var filesModifiedSet map[string]bool
	snowball := branching.NewSnowballDetector(500000)                 // 500K token ceiling
	loopDet := control.NewLoopDetector(10, control.DoomLoopThreshold) // 10-step window, 3 repeats = doom loop

	for {
		// Close the boundary of the previous turn before evaluating whether to
		// start another. Doing this first keeps the guard-condition exit path
		// from leaving the prior turn unclosed.
		if j := s.Persistence().Journal(); j != nil && turnCount > 0 {
			j.AppendStepEnd(turnCount, 1)
			j.AppendTurnEnd(turnCount, "completed")
		}
		if !s.checkGuardConditions(ctx, ch, turnCount, snowball, loopDet) {
			return
		}
		s.EnsureSkillCatalogStatement()
		turnCount++

		if j := s.Persistence().Journal(); j != nil {
			j.AppendTurnStart(turnCount)
			j.AppendStepStart(turnCount, 1)
		}

		if s.LifecycleSvc().Limits() != nil {
			s.LifecycleSvc().Limits().RecordTurn()
		}

		// Belief maintenance: prune stale beliefs (injected at query time below)
		if s.LifecycleSvc().Beliefs() != nil && s.LifecycleSvc().Beliefs().Size() > 0 {
			s.LifecycleSvc().Beliefs().Prune(turnCount)
		}
		// Context governor: collapse → micro/smart/truncate (settings threshold %).
		tokensBefore := token.EstimateTokens(s.Persistence().RawMessages())
		if s.WillCompactBeforeTurn() {
			emit(StreamEvent{Type: "compact_start"})
		}
		if compactStrategy, didCompact := s.ManageContextBeforeTurn(ctx); didCompact {
			tokensAfter := token.EstimateTokens(s.Persistence().RawMessages())
			s.Logger().Info("context compacted", map[string]interface{}{
				"strategy": compactStrategy,
				"messages": len(s.Persistence().RawMessages()),
			})
			emit(StreamEvent{
				Type:         "compact",
				Content:      compactStrategy,
				TokensBefore: tokensBefore,
				TokensAfter:  tokensAfter,
			})
		}

		// Integration pipeline: pre-query (intent, tools, budget, injection scan, cache)
		if s.LifecycleSvc().Pipeline() != nil {
			lastUserMsg := ""
			for i := len(s.Persistence().RawMessages()) - 1; i >= 0; i-- {
				if s.Persistence().RawMessages()[i].Role == "user" && len(s.Persistence().RawMessages()[i].ToolResults) == 0 {
					lastUserMsg = s.Persistence().RawMessages()[i].Content
					break
				}
			}
			preResult := s.LifecycleSvc().Pipeline().PreQuery(s.Persistence().RawMessages(), lastUserMsg)
			if preResult != nil {
				// Cache hit: short-circuit the LLM call
				if preResult.CacheHit && preResult.CachedResponse != "" {
					emit(StreamEvent{Type: "content", Content: preResult.CachedResponse})
					s.Persistence().AppendAssistantJournaled(types.FluxMessage{Role: "assistant", Content: preResult.CachedResponse})
					emit(StreamEvent{Type: "done"})
					return
				}
				if preResult.InjectionRisk != nil && preResult.InjectionRisk.IsRisky {
					if preResult.InjectionRisk.RiskLevel == "high" {
						emit(StreamEvent{Type: "error", Content: "High-risk prompt injection detected. Message blocked."})
						return
					}
					s.Logger().Warn("injection risk detected", map[string]interface{}{
						"level": preResult.InjectionRisk.RiskLevel,
					})
				}
				// Apply adaptive system prompt if generated
				if preResult.SystemPrompt != "" {
					s.Persistence().SetSystem(preResult.SystemPrompt)
				}
			}
		}

		// Pre-query hook
		if j := s.Persistence().Journal(); j != nil {
			j.AppendHookInvoked(string(hooks.EventPreQuery))
		}
		preErr := hooks.Execute(ctx, hooks.EventPreQuery, map[string]interface{}{
			"provider": s.ChatLLM().Provider(),
			"model":    s.ChatLLM().Model(),
			"messages": len(s.Persistence().RawMessages()),
		})
		if j := s.Persistence().Journal(); j != nil {
			hookErr := ""
			if preErr != nil {
				hookErr = preErr.Error()
			}
			j.AppendHookResult(string(hooks.EventPreQuery), hookErr)
		}

		s.Logger().Info("stream query", map[string]interface{}{
			"provider": s.ChatLLM().Provider(),
			"model":    s.ChatLLM().Model(),
			"messages": len(s.Persistence().RawMessages()),
		})

		// Dynamic max_tokens based on task type and recent tool patterns
		taskType := classifyPromptForBudget(s.Persistence().RawMessages())
		contextSize := s.ContextWindowSize()
		maxTok := token.DynamicMaxTokens(s.Persistence().RawMessages(), contextSize, taskType)

		// Model cascade: select optimal model for this request
		activeModel := strings.TrimSpace(s.ChatLLM().Model())
		if activeModel == "" {
			activeModel = strings.TrimSpace(s.ChatLLM().Model())
		}
		userMsg := ""
		for i := len(s.Persistence().RawMessages()) - 1; i >= 0; i-- {
			if s.Persistence().RawMessages()[i].Role == "user" {
				userMsg = s.Persistence().RawMessages()[i].Content
				break
			}
		}
		if s.LifecycleSvc().Cascade() != nil && s.LifecycleSvc().Cascade().Enabled {
			activeModel = s.LifecycleSvc().Cascade().SelectModel(userMsg, activeModel, "")
		}
		// Smart routing: cheap simple model for trivial turns, strong otherwise.
		if sr := s.LifecycleSvc().SmartRouting(); sr != nil && sr.Enabled {
			d := smartrouting.Route(smartrouting.Input{
				UserText:   userMsg,
				HasNonText: false,
				TurnNumber: turnCount,
			}, *sr)
			if d.Model != "" {
				activeModel = d.Model
			}
		}
		if strings.TrimSpace(activeModel) == "" {
			emit(StreamEvent{Type: "error", Content: "no model selected — open /config → Models and pick one"})
			return
		}

		// Recall and refresh memories before every LLM call through the
		// memory service boundary.
		if len(s.Persistence().RawMessages()) > 0 {
			lastMsg := s.Persistence().RawMessages()[len(s.Persistence().RawMessages())-1].Content
			recall := func() string {
				return s.MemorySvc().RecallContext(ctx, lastMsg, 3000)
			}
			if incrementalContextEnabled() {
				if s.incremental == nil {
					if mi, err := newMemoryIncremental(recall); err == nil {
						s.incremental = mi
					}
				}
				if s.incremental != nil {
					if content, changed := s.incremental.prepare(); changed && content != "" {
						s.ReplaceSystemContextSection("## Relevant Memories\n", content)
					}
					goto memoryDone
				}
			}
			if remembered := recall(); remembered != "" {
				s.ReplaceSystemContextSection("## Relevant Memories\n", remembered)
			}
		}
	memoryDone:

		// Payload tiering: classify the latest user request once per turn.
		// Early conversational turns get a minimal system prompt and no tool
		// schemas; anything resembling work keeps the full prompt and the
		// promoted tool surface. This keeps simple turns cheap on slow
		// (local/remote) models without starving real requests of tools.
		lastUserMsg := ""
		for i := len(s.Persistence().RawMessages()) - 1; i >= 0; i-- {
			m := s.Persistence().RawMessages()[i]
			if m.Role == "user" && len(m.ToolResults) == 0 {
				lastUserMsg = m.Content
				break
			}
		}
		smallTalk := lastUserMsg != "" && isSmallTalkPrompt(lastUserMsg) && !sessionHasToolUse(s.Persistence().RawMessages())

		// Assemble the per-turn ChatOptions (system prompt tiering, ephemeral
		// injections, promoted tools) via the extracted helper so agentLoop
		// stays focused on the LLM call + stream consumption that follow.
		opts := s.buildTurnOptions(turnContext{
			ctx:         ctx,
			activeModel: activeModel,
			maxTok:      maxTok,
			smallTalk:   smallTalk,
			lastUserMsg: lastUserMsg,
			taskType:    taskType,
		})

		// Count actual input tokens for precise budget tracking
		inputTokens := 0
		for _, msg := range s.Persistence().RawMessages() {
			inputTokens += token.CountTokensFast(msg.Content)
			for _, tr := range msg.ToolResults {
				inputTokens += token.CountTokensFast(tr.Content)
			}
		}
		inputTokens += token.CountTokensFast(s.Persistence().System())
		s.Logger().Info("token count", map[string]interface{}{"input_tokens": inputTokens, "model": s.ChatLLM().Model()})

		// Cost warning for expensive calls
		inPrice, outPrice := cost.ModelPricing(s.ChatLLM().Model())
		estCost := float64(inputTokens)*inPrice/1_000_000 + float64(maxTok)*outPrice/1_000_000
		if estCost > 0.50 {
			emit(StreamEvent{Type: "blast_radius", Content: fmt.Sprintf("%s This request will use ~%d tokens (~$%.2f). Continue? The agent will proceed automatically.", icons.Alert(), inputTokens+maxTok, estCost)})
		}

		// Trace: start agent loop span for this turn
		var loopSpan *oteltrace.Span
		if s.TracerValue() != nil {
			ctx, loopSpan = oteltrace.StartAgentLoopSpan(ctx, s.TracerValue(), s.ChatLLM().Provider(), activeModel, len(s.Persistence().RawMessages()))
		}

		// Issue the LLM call via the ChatService. The service handles
		// rate limit, retry, and emergency compact internally; the
		// api.requests counter is incremented inside ChatService.Stream.
		// Rho records product-level latency; provider health and circuit
		// breaking are owned by Flux's routed transport.
		apiStart := time.Now()
		managesResilience := clientManagesResilience(s.ChatLLM().Client())
		result, err := s.ChatLLM().Stream(ctx, s.Persistence().RawMessages(), opts)
		apiDuration := time.Since(apiStart)
		s.Metrics().Timer("api.latency").Record(apiDuration)
		s.Metrics().Timer("api.last_latency").Record(apiDuration)

		if err != nil {
			// End trace span with error
			if loopSpan != nil {
				oteltrace.EndSpanWithError(loopSpan, err)
			}
			s.Logger().Error("stream error", map[string]interface{}{
				"error": err.Error(),
			})
			emit(StreamEvent{Type: "error", Content: err.Error()})
			return
		}

		var textContent strings.Builder
		var toolCalls []types.ToolCall
		var stopReason string
		var lastUsage *types.FluxUsage
		var usageLedger streamUsageLedger
		resolvedProvider := strings.TrimSpace(s.ChatLLM().Provider())
		resolvedModel := strings.TrimSpace(activeModel)

		// Compatibility clients retain Rho's historical stream retry and
		// reasoning-only recovery. Flux facade clients already normalize and
		// recover provider streams, so Rho must consume their result exactly once.
		const maxStreamRetries = 2
		var streamErr error
		var sawThinking bool
		for streamAttempt := 0; streamAttempt <= maxStreamRetries; streamAttempt++ {
			streamErr = nil
			sawThinking = false
			// Emit stream block-start lifecycle event (DSH block-start seam).
			if j := s.Persistence().Journal(); j != nil {
				j.AppendStreamBlockStart(turnCount, streamAttempt+1)
			}
		eventLoop:
			for {
				select {
				case <-ctx.Done():
					result.Close()
					return
				case ev, ok := <-result.Events:
					if !ok {
						break eventLoop
					}
					switch ev.Type {
					case "route_selected", "route_changed":
						var changed bool
						resolvedProvider, resolvedModel, changed = updateResolvedRoute(resolvedProvider, resolvedModel, ev.Route)
						if changed {
							s.Logger().Info("engine route selected", map[string]interface{}{
								"provider": resolvedProvider,
								"model":    resolvedModel,
							})
						}
						if loopSpan != nil {
							loopSpan.SetTag("provider", resolvedProvider)
							loopSpan.SetTag("model", resolvedModel)
						}
					case "content":
						textContent.WriteString(ev.Content)
						if j := s.Persistence().Journal(); j != nil {
							j.AppendAssistantChunk(turnCount, 1, ev.Content)
						}
						ch <- StreamEvent{Type: "content", Content: ev.Content}
					case "thinking":
						if strings.TrimSpace(ev.Thinking) != "" {
							sawThinking = true
							if j := s.Persistence().Journal(); j != nil {
								j.AppendAssistantThinkingChunk(turnCount, 1, ev.Thinking)
							}
						}
						ch <- StreamEvent{Type: "thinking", Content: ev.Thinking}
					case "tool_call":
						if ev.ToolCall != nil {
							toolCalls = append(toolCalls, *ev.ToolCall)
							// Emit incremental tool-call delta (DSH tool-call-delta seam).
							if j := s.Persistence().Journal(); j != nil {
								var deltaArgs string
								if raw, ok := ev.ToolCall.Arguments["arguments"]; ok {
									if str, ok := raw.(string); ok {
										deltaArgs = str
									}
								}
								j.AppendToolCallDelta(turnCount, 1, ev.ToolCall.Name, deltaArgs)
							}
						}
					case "usage":
						if ev.Usage != nil {
							lastUsage = ev.Usage
							if usageLedger.shouldRecord(ev.Usage, false) {
								s.recordStreamUsage(ch, ev.Usage.PromptTokens, ev.Usage.CompletionTokens, resolvedProvider, resolvedModel, taskType, apiStart)
							}
						}
					case "error":
						streamErr = fmt.Errorf("%s", ev.Error)
						if isRetryableStreamError(streamErr) {
							break // break switch, will check in outer loop
						}
						ch <- StreamEvent{Type: "error", Content: ev.Error}
						result.Close()
						return
					case "done":
						if ev.StopReason != "" {
							stopReason = ev.StopReason
						}
						if ev.Usage != nil {
							lastUsage = ev.Usage
							if usageLedger.shouldRecord(ev.Usage, true) {
								s.recordStreamUsage(ch, ev.Usage.PromptTokens, ev.Usage.CompletionTokens, resolvedProvider, resolvedModel, taskType, apiStart)
							}
						}
					}
				}
			}
			result.Close()

			thinkingOnly := streamErr == nil && textContent.Len() == 0 && len(toolCalls) == 0 && sawThinking
			if thinkingOnly && !managesResilience {
				if resp, chatErr := s.ChatLLM().Chat(ctx, s.Persistence().RawMessages(), opts); chatErr == nil && resp != nil && strings.TrimSpace(resp.Content) != "" {
					resolvedProvider, resolvedModel, _ = updateResolvedRoute(resolvedProvider, resolvedModel, resp.Route)
					content := resp.Content
					textContent.WriteString(content)
					ch <- StreamEvent{Type: "content", Content: content}
					if len(resp.ToolCalls) > 0 {
						toolCalls = append(toolCalls, resp.ToolCalls...)
					}
					if resp.FinishReason != "" {
						stopReason = resp.FinishReason
					}
					if resp.Usage != nil {
						lastUsage = resp.Usage
						s.recordStreamUsage(ch, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resolvedProvider, resolvedModel, taskType, apiStart)
					}
					streamErr = nil
					break
				}
				ch <- StreamEvent{Type: "error", Content: "The model produced internal reasoning but no reply."}
				result.Close()
				return
			}

			shouldRetry := !managesResilience && streamErr != nil && isRetryableStreamError(streamErr)
			if !shouldRetry {
				break
			}
			if streamAttempt >= maxStreamRetries {
				break
			}
			retryReason := "transient stream error"
			s.Logger().Warn("stream retry", map[string]interface{}{
				"attempt": streamAttempt + 1,
				"reason":  retryReason,
				"error":   streamErr.Error(),
			})
			// Emit llm.retry / llm.retry.started lifecycle events (DSH seam).
			if j := s.Persistence().Journal(); j != nil {
				j.AppendLLMRetry(streamAttempt+1, retryReason)
				j.AppendLLMRetryStarted(streamAttempt+1, retryReason)
			}
			retryTimer := time.NewTimer(streamRetryDelay(streamErr, streamAttempt))
			select {
			case <-retryTimer.C:
			case <-ctx.Done():
				retryTimer.Stop()
				emit(StreamEvent{Type: "error", Content: "stream retry cancelled: " + ctx.Err().Error()})
				result.Close()
				return
			}

			// Notify consumer to discard previously streamed content for this turn.
			emit(StreamEvent{Type: "retry", Content: fmt.Sprintf("retrying after %s (attempt %d)", retryReason, streamAttempt+2)})

			// Re-open the stream for retry. We bypass the ChatService
			// here on purpose: ChatService.Stream has its own retry
			// loop, and stacking that on top of this secondary
			// stream-error retry would double-retry network blips.
			// The session agent loop owns this layer.
			result, err = s.ChatLLM().Client().StreamChatContinue(ctx, s.Persistence().RawMessages(), opts, types.DefaultContinuationConfig())
			if err != nil {
				emit(StreamEvent{Type: "error", Content: err.Error()})
				return
			}
			// Reset accumulated state for the retry
			textContent.Reset()
			toolCalls = nil
			stopReason = ""
			lastUsage = nil
			usageLedger.reset()
			streamErr = nil
		}
		// Emit stream block-end + finish lifecycle events (DSH block-end/finish seam).
		if j := s.Persistence().Journal(); j != nil {
			j.AppendStreamBlockEnd(turnCount, 1)
			j.AppendStreamFinish(turnCount, 1, stopReason)
		}
		if streamErr != nil {
			emit(StreamEvent{Type: "error", Content: streamErr.Error()})
			return
		}

		// Providers like OpenCode Go often omit stream usage; estimate so billing footer updates.
		if lastUsage == nil && (textContent.Len() > 0 || len(toolCalls) > 0) {
			completionEst := estimateStreamCompletionTokens(textContent.String(), toolCalls)
			if inputTokens > 0 || completionEst > 0 {
				s.recordStreamUsage(ch, inputTokens, completionEst, resolvedProvider, resolvedModel, taskType, apiStart)
				lastUsage = &types.FluxUsage{
					PromptTokens:     inputTokens,
					CompletionTokens: completionEst,
				}
			}
		}

		// Snowball detector: record usage after each API response
		if lastUsage != nil {
			progress := 0.5
			if len(toolCalls) > 0 {
				progress = 1.0
			}
			snowball.RecordTurn(lastUsage.PromptTokens+lastUsage.CompletionTokens, progress)
			// Emit request.context: durable model route metadata at the
			// request boundary (DSH seam — provider, model, context window).
			if j := s.Persistence().Journal(); j != nil {
				j.AppendRequestContext(resolvedProvider, resolvedModel, s.ContextWindowSize())
			}
			s.recordFluxOperationObservation(
				resolvedProvider,
				resolvedModel,
				stopReason,
				textContent.String(),
				len(toolCalls),
				lastUsage,
			)
		}

		// Budget enforcement
		limits := s.LifecycleSvc().Limits()
		// Sync the tracker's cost accounting with the session's authoritative
		// cost accumulator so LimitTracker.IsExceeded() (checked every turn by
		// checkGuardConditions) enforces the same budget as this explicit check.
		limits.SetCostUSD(s.CostValue().TotalUSD())
		if limits.MaxBudgetUSD() > 0 && s.CostValue().TotalUSD() >= limits.MaxBudgetUSD() {
			emit(StreamEvent{Type: "content", Content: fmt.Sprintf("\n\nBudget limit reached ($%.2f spent of $%.2f).", s.CostValue().TotalUSD(), limits.MaxBudgetUSD())})
			emit(StreamEvent{Type: "done"})
			return
		}

		// Check for inline tool calls in text (some providers embed tool calls in
		// text): Moonshot/kimi <|tool_calls_section_begin|> or Hermes/Nous
		// <tool_call> (Qwen and most OpenAI-compatible local models).
		if len(toolCalls) == 0 && (strings.Contains(textContent.String(), "<|tool_calls_section_begin|>") || strings.Contains(textContent.String(), "<tool_call>")) {
			cleanText, engineCalls := gateway.ParseInlineToolCalls(textContent.String())
			inlineCalls := engineCalls
			if len(inlineCalls) > 0 {
				textContent.Reset()
				textContent.WriteString(cleanText)
				toolCalls = append(toolCalls, inlineCalls...)
			}
		}

		// Cap tool calls per step
		const maxToolCallsPerStep = 32
		if len(toolCalls) > maxToolCallsPerStep {
			excess := toolCalls[maxToolCallsPerStep:]
			toolCalls = toolCalls[:maxToolCallsPerStep]
			for _, tc := range excess {
				emit(StreamEvent{Type: "tool_result", ToolName: tc.Name, Content: "Error: too many tool calls in one step (max 32). Retry with fewer calls."})
			}
		}

		// Post-query hook
		if j := s.Persistence().Journal(); j != nil {
			j.AppendHookInvoked(string(hooks.EventPostQuery))
		}
		hooks.ExecuteAsync(ctx, hooks.EventPostQuery, map[string]interface{}{
			"provider": resolvedProvider,
			"model":    resolvedModel,
			"content":  textContent.String(),
			"tools":    len(toolCalls),
		})

		// End agent loop trace span for this turn
		if loopSpan != nil {
			loopSpan.SetTag("tools", fmt.Sprintf("%d", len(toolCalls)))
			oteltrace.EndSpanWithError(loopSpan, nil)
		}

		// Compatibility-only max_tokens recovery. Flux's engine facade owns
		// continuation and exposes one normalized stream to Rho. Legacy clients
		// retain the historical synthetic turn so injected integrations do not
		// change behavior while they migrate to the facade.
		if !managesResilience && stopReason == "max_tokens" && len(toolCalls) == 0 && recoveryCount < maxRecoveryRetries {
			recoveryCount++
			s.Persistence().AppendAssistantJournaled(types.FluxMessage{Role: "assistant", Content: textContent.String()})
			s.Persistence().AppendUserJournaled(types.FluxMessage{Role: "user", Content: "Continue from where you left off."})
			continue
		}

		// No tool calls — done
		if len(toolCalls) == 0 {
			// This is the end of the stream; close the active boundaries.
			if j := s.Persistence().Journal(); j != nil {
				j.AppendStepEnd(turnCount, 1)
				j.AppendTurnEnd(turnCount, "completed")
			}
			// Integration pipeline: post-response (format, score, redact, cache, learn)
			if s.LifecycleSvc().Pipeline() != nil && textContent.Len() > 0 {
				postResult := s.LifecycleSvc().Pipeline().PostResponse(textContent.String(), s.Persistence().RawMessages())
				if postResult != nil {
					s.recordRedactionObservation(textContent.String(), postResult.SecretMatches, postResult.SecretTypes)
				}
				if postResult != nil && postResult.FormattedResponse != "" {
					textContent.Reset()
					textContent.WriteString(postResult.FormattedResponse)
				}
			}
			if textContent.Len() > 0 {
				s.Persistence().AppendAssistantJournaled(types.FluxMessage{Role: "assistant", Content: textContent.String()})
				// Auto-remember corrections and learnings. Best-effort
				// fire-and-forget, bounded so a hung backend cannot leak.
				if s.MemorySvc().Memory() != nil && shouldRemember(textContent.String()) {
					go func(content string) {
						rCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
						defer cancel()
						if err := s.MemorySvc().Memory().Remember(rCtx, content, "assistant_learning"); err != nil {
							slog.Warn("background assistant_learning remember failed", "error", err)
						}
					}(textContent.String())
				}
			}
			emit(StreamEvent{Type: "done"})
			// Integration pipeline: end-session (assess, learn, store experience)
			if s.LifecycleSvc().Pipeline() != nil {
				taskGoal := ""
				for _, m := range s.Persistence().RawMessages() {
					if m.Role == "user" && len(m.ToolResults) == 0 {
						taskGoal = m.Content
						break
					}
				}
				// End-session persistence must complete before the stream closes. A
				// detached goroutine can write after callers tear down their state
				// directory, losing feedback and racing test/process cleanup.
				s.LifecycleSvc().Pipeline().EndSession(context.WithoutCancel(ctx), ctx.Err() == nil, taskGoal)
			}
			// Session end hook
			if j := s.Persistence().Journal(); j != nil {
				j.AppendHookInvoked(string(hooks.EventSessionEnd))
			}
			hooks.ExecuteAsync(context.WithoutCancel(ctx), hooks.EventSessionEnd, map[string]interface{}{
				"provider": s.ChatLLM().Provider(),
				"model":    s.ChatLLM().Model(),
				"messages": len(s.Persistence().RawMessages()),
			})
			// Drain the async hook queue with a bounded wait so post-session
			// observers finish before the process exits; nothing new is
			// scheduled after this point (M19). Timeout guards a hung hook.
			waitCtx, waitCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer waitCancel()
			if err := hooks.WaitAsync(waitCtx); err != nil {
				slog.Warn("session end hooks", "error", err)
			}
			return
		}

		// Execute tools and collect results
		recoveryCount = 0
		if toolsUsedSet == nil {
			toolsUsedSet = map[string]bool{}
			filesModifiedSet = map[string]bool{}
		}
		for _, tc := range toolCalls {
			toolsUsedSet[tc.Name] = true
			cn := canonicalToolName(tc.Name)
			if cn == "Write" || cn == "Edit" {
				if p, ok := pathArgument(tc.Arguments); ok {
					filesModifiedSet[p] = true
				}
			}
		}
		toolTurns++

		// Backtrack: record decision point when tool calls are pending
		if s.LifecycleSvc().Backtrack() != nil && len(toolCalls) > 0 {
			var toolNames []string
			for _, tc := range toolCalls {
				toolNames = append(toolNames, tc.Name)
			}
			s.LifecycleSvc().Backtrack().RecordDecision(turnCount, strings.Join(toolNames, ", "), nil, s.Persistence().RawMessages())
		}

		// Emit command.run events for Bash tool calls before execution (DSH
		// command.run seam), then command.done after results return.
		// Also emit code.dispatch.start for code-execution Bash commands
		// (python, node, go run, etc.) — DSH code.dispatch.start seam.
		if j := s.Persistence().Journal(); j != nil {
			for _, tc := range toolCalls {
				cn := canonicalToolName(tc.Name)
				if cn == "Bash" {
					cmd := extractBashCommand(tc.Arguments)
					j.AppendCommandRun(cmd)
					if lang := detectCodeLanguage(cmd); lang != "" {
						j.AppendToolCodeDispatchStart(lang)
					}
				}
				if cn == "WebSearch" {
					j.AppendWebDeepSeekSearch(extractWebSearchQuery(tc.Arguments))
				}
			}
		}

		results := s.Tools().ExecuteAll(ctx, toolCalls, ch, turnCount, textContent.String())

		// Emit command.done events for Bash tool calls after execution (DSH
		// command.done seam). command.run fires before execution; command.done
		// fires after all tools complete.
		if j := s.Persistence().Journal(); j != nil {
			for _, r := range results {
				if canonicalToolName(r.tc.Name) == "Bash" {
					cmd := extractBashCommand(r.tc.Arguments)
					exitCode := 0
					if r.isErr {
						exitCode = 1
					}
					errMsg := ""
					if r.err != nil {
						errMsg = r.err.Error()
					}
					j.AppendCommandDone(cmd, exitCode, errMsg)
					// Emit code.dispatch (not just start) after execution
					// for code-execution Bash commands — DSH code.dispatch seam.
					if lang := detectCodeLanguage(cmd); lang != "" {
						j.AppendToolCodeDispatch(lang)
					}
				}
			}
		}

		// Emit todo.write events for TodoWrite tool calls (DSH todo.write seam).
		if j := s.Persistence().Journal(); j != nil {
			for _, tc := range toolCalls {
				if canonicalToolName(tc.Name) == "TodoWrite" {
					items := extractTodoWriteItems(tc.Arguments)
					if len(items) > 0 {
						j.AppendTodoWrite(items)
					}
				}
			}
		}

		// Auto-snapshot after write operations for granular undo
		if s.Tools().Snapshots() != nil && len(toolCalls) > 0 {
			var writeNames []string
			for _, tc := range toolCalls {
				if !tool.IsReadOnly(tc.Name) {
					writeNames = append(writeNames, tc.Name)
				}
			}
			if len(writeNames) > 0 {
				go func() {
					// Bound the snapshot so a slow filesystem doesn't
					// leak a goroutine after the session ends.
					snapCtx, snapCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
					defer snapCancel()
					_, _ = s.Tools().Snapshots().TrackCtx(snapCtx, strings.Join(writeNames, ", "))
				}()
			}
		}

		// Append assistant message with tool_use blocks
		assistContent := textContent.String()
		if assistContent == "" && len(toolCalls) > 0 {
			assistContent = " " // non-empty to satisfy APIs that reject empty content
		}
		s.Persistence().AppendAssistantJournaled(types.FluxMessage{
			Role:    "assistant",
			Content: assistContent,
			ToolUse: toolCalls,
		})
		// Append tool results as proper tool_result messages. Tool output is
		// redacted before it is appended so secrets never reach the model;
		// the user-facing stream events already carried the raw output.
		for _, r := range results {
			resultContent := r.output
			if resultContent == "" {
				resultContent = "(no output)"
			}
			resultContent = s.redactToolResult(resultContent)
			// Network-facing tool output is untrusted: wrap it with an explicit
			// boundary so prompt-injection text is treated as data.
			resultContent = wrapExternalToolResult(r.tc.Name, resultContent)
			msg := types.FluxMessage{
				Role:    "user",
				Content: resultContent,
				ToolResults: []types.ToolResult{{
					ToolUseID: r.tc.ID,
					Content:   resultContent,
					IsError:   r.isErr,
				}},
			}
			// Multi-modal vision: extract data URIs from tool results and attach as images
			if !r.isErr && strings.Contains(resultContent, "data:") && strings.Contains(resultContent, ";base64,") {
				if imgURI := extractDataURI(resultContent); imgURI != "" {
					msg.Images = []string{imgURI}
				}
			}
			s.Persistence().AppendUserJournaled(msg)
		}

		// --- STEERING: Inject user guidance between tool batches ---
		steerCount := 0
		if s.Persistence().Steering() != nil && s.Persistence().Steering().HasPending() {
			for _, steer := range s.Persistence().Steering().Drain() {
				s.Persistence().AppendUserJournaled(types.FluxMessage{
					Role:    "user",
					Content: "[User guidance during execution]: " + steer.Content,
				})
				emit(StreamEvent{Type: "content", Content: "\n[Steering received: " + steer.Content + "]\n"})
				steerCount++
			}
		}
		// Emit agent.inbox.splice when steering messages are spliced into the
		// conversation mid-execution (DSH agent.inbox.splice seam).
		if steerCount > 0 {
			if j := s.Persistence().Journal(); j != nil {
				j.AppendAgentInboxSpliced(steerCount, 0)
			}
		}

		// Loop detection: record this step's tool call signatures
		if len(results) > 0 {
			var ldNames, ldInputs, ldOutputs []string
			for _, r := range results {
				ldNames = append(ldNames, r.tc.Name)
				inputJSON, _ := json.Marshal(r.tc.Arguments)
				ldInputs = append(ldInputs, string(inputJSON))
				ldOutputs = append(ldOutputs, r.output)
			}
			loopDet.RecordStep(ldNames, ldInputs, ldOutputs)
		}

		// Auto-remember: save conversation context and insights to memory after each turn
		if s.MemorySvc().Memory() != nil {
			userMsg := ""
			assistantMsg := ""
			for i := len(s.Persistence().RawMessages()) - 1; i >= 0; i-- {
				m := s.Persistence().RawMessages()[i]
				if m.Role == "assistant" && m.Content != "" && assistantMsg == "" {
					assistantMsg = m.Content
				}
				if m.Role == "user" && len(m.ToolResults) == 0 && userMsg == "" {
					userMsg = m.Content
				}
				if userMsg != "" && assistantMsg != "" {
					break
				}
			}
			if userMsg != "" && assistantMsg != "" {
				condensed := fmt.Sprintf("Q: %s\nA: %s", truncate(userMsg, 200), truncate(assistantMsg, 300))
				if err := s.MemorySvc().Memory().Remember(ctx, condensed, "conversation"); err != nil {
					slog.Warn("conversation remember failed", "error", err)
				}
			}
			// Also save insights if the response has learning signals
			if assistantMsg != "" && shouldRemember(assistantMsg) {
				if err := s.MemorySvc().Memory().Remember(ctx, truncate(assistantMsg, 500), "insight"); err != nil {
					slog.Warn("insight remember failed", "error", err)
				}
			}
		}
	}
}

// isSmallTalkPrompt reports whether prompt is a pure conversational
// exchange (greeting, pleasantry, identity question) that needs neither the
// full system prompt nor any tool schemas. The system prompt instructs the
// model to answer these directly, so sending the tool surface would only
// add prompt-prefill cost.
// isSmallTalkPrompt detects short greetings/closings so the payload can be
// tiered down. The match must be conservative: this function decides whether
// to SKIP the full prompt context, so a false positive (dropping context for a
// real request like "hi there, who fixes this bug?") is far costlier than a
// false negative (sending a slightly bigger payload for genuine small talk).
//
// It returns true when the whole prompt is small talk: an exact match against
// a closed phrase list, or a phrase followed only by short filler (no comma or
// clause that introduces a real request).
func isSmallTalkPrompt(prompt string) bool {
	text := strings.ToLower(strings.TrimSpace(prompt))
	text = strings.Trim(text, " \t\r\n.,!?;:")
	for _, phrase := range smallTalkPhrases {
		if text == phrase {
			return true
		}
		// Allow a trailing filler word (e.g. "hi there") but stop at a comma or
		// anything that looks like a real request clause.
		if strings.HasPrefix(text, phrase+" ") {
			remainder := text[len(phrase)+1:]
			if !strings.ContainsAny(remainder, ",;:?") && len(remainder) <= 12 {
				return true
			}
		}
	}
	return false
}

var smallTalkPhrases = []string{
	"hi", "hi there", "hello", "hey", "hey there",
	"how are you", "how are you doing", "how's it going", "how's it going today",
	"what's up", "who are you", "what can you do",
	"thanks", "thank you", "thanks a lot",
	"good morning", "good afternoon", "good evening", "nice to meet you",
	"goodbye", "bye",
}

// sessionHasToolUse reports whether any message in the conversation already
// executed a tool. Once tools are in play, later turns keep the full prompt
// and tool surface even if they read like small talk ("thanks"), so the
// follow-up context is not lost.
func sessionHasToolUse(msgs []types.FluxMessage) bool {
	for _, m := range msgs {
		if len(m.ToolResults) > 0 {
			return true
		}
	}
	return false
}

// extractDataURI extracts the first base64 data URI from a string.
// Returns the full data URI (e.g., "data:image/png;base64,...") or empty string.
func extractDataURI(s string) string {
	idx := strings.Index(s, "data:")
	if idx < 0 {
		return ""
	}
	rest := s[idx:]
	endIdx := strings.IndexAny(rest, " \n\t\"'")
	if endIdx < 0 {
		return rest
	}
	return rest[:endIdx]
}

// extractBashCommand pulls the "command" argument from a Bash tool call's
// arguments map, defaulting to empty string when absent or non-string.
func extractBashCommand(args map[string]interface{}) string {
	if args == nil {
		return ""
	}
	if c, ok := args["command"]; ok {
		if s, ok := c.(string); ok {
			return s
		}
	}
	return ""
}

// extractWebSearchQuery pulls the "query" argument from a WebSearch tool call's
// arguments map, defaulting to empty string when absent or non-string.
func extractWebSearchQuery(args map[string]interface{}) string {
	if args == nil {
		return ""
	}
	if q, ok := args["query"]; ok {
		if s, ok := q.(string); ok {
			return s
		}
	}
	return ""
}

// extractTodoWriteItems pulls the "todos" array from a TodoWrite tool call's
// arguments and converts them into eventlog TodoItem values.
func extractTodoWriteItems(args map[string]interface{}) []eventlog.TodoItem {
	if args == nil {
		return nil
	}
	raw, ok := args["todos"]
	if !ok {
		return nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var items []eventlog.TodoItem
	for _, v := range arr {
		m, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		content, _ := m["content"].(string)
		if content == "" {
			if t, ok := m["task"].(string); ok {
				content = t
			}
		}
		status, _ := m["status"].(string)
		items = append(items, eventlog.TodoItem{Content: content, Status: status})
	}
	return items
}

// detectCodeLanguage inspects a Bash command and returns the script language
// if it looks like a code-execution invocation (e.g. python3, node, go run).
// Returns empty string for non-code commands.
func detectCodeLanguage(cmd string) string {
	if cmd == "" {
		return ""
	}
	// Take only the first shell token to avoid matching comments or strings.
	rest := cmd
	if idx := strings.IndexAny(cmd, " \t"); idx >= 0 {
		rest = cmd[:idx]
	}
	base := strings.TrimSpace(rest)
	// Strip common path prefixes and suffixes.
	if dirIdx := strings.LastIndex(base, "/"); dirIdx >= 0 {
		base = base[dirIdx+1:]
	}
	if strings.HasSuffix(base, ".sh") || strings.HasSuffix(base, ".bash") {
		return "bash"
	}
	switch base {
	case "python", "python3", "python2", "py":
		return "python"
	case "node", "nodejs", "npm", "npx":
		return "javascript"
	case "ruby":
		return "ruby"
	case "go":
		// "go run" / "go test" are code dispatch; plain "go build" is not.
		if strings.Contains(strings.ToLower(cmd), "go run") || strings.Contains(strings.ToLower(cmd), "go test") {
			return "go"
		}
		return ""
	case "perl":
		return "perl"
	case "php":
		return "php"
	case "rustc":
		return "rust"
	case "cargo":
		// Only cargo run/test are code dispatch; cargo build/bench/clippy are not.
		lower := strings.ToLower(cmd)
		if strings.Contains(lower, "run") || strings.Contains(lower, "test") {
			return "rust"
		}
		return ""
	case "javac", "java":
		return "java"
	default:
		return ""
	}
}
