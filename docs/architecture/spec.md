# Rho Architecture Specification

## Problem Statement

Rho is an AI-powered coding agent for the terminal. This specification defines the complete architecture: repository structure, agent loop, agile workflow, feedback loops, and edge case handling. It serves as the authoritative reference for how rho works.

## Scope

- Repository structure and module organization
- Agent loop lifecycle (stream.go)
- Permission system and safety gates
- Spec-driven development workflow
- Context management and compaction
- Memory and self-improvement feedback loops
- Error recovery and resilience patterns
- Tool execution pipeline
- Multi-agent coordination

## Requirements

### REQ-1: Repository Structure

Rho SHALL be organized as a Go repository and workspace entry point within a
multi-repository ecosystem. The Rho repository has the following top-level
layout:

| Directory | Purpose |
|-----------|---------|
| `cmd/` | CLI entry point (Cobra) and TUI (Bubble Tea) |
| `internal/` | Private Go packages (not importable by external repos) |
| Parent `go.work` | Resolves the nine local Go siblings: rho, eagle, falcon, flux, harrier (Harrier), shrike (Shrike), swift (Swift), kestrel (Kestrel), and merlin (Merlin) |
| `spec/` | OpenSpec schema consumed by `internal/spec` |
| `docs/` | Architecture docs, design docs, plans |
| `rules/` | User-defined rules |
| `testdata/` | Test fixtures |

### REQ-2: Internal Package Organization

The `internal/` directory SHALL contain the following packages:

| Package | Purpose |
|---------|---------|
| `engine/` | Agent loop, session management, all sub-systems |
| `tool/` | 40+ built-in tools (file edit, git, codegen, spec tools, etc.) |
| `permissions/` | Guardian, rules DSL, boundary checker |
| `plugin/` | Skills loader, registry, auto-skill, marketplace install |
| `config/` | Product settings, Flux composition, state migration |
| `session/` | SQLite persistence, search, export, replay |
| `hooks/` | Event-driven plugin system |
| `mcp/` | Model Context Protocol client/server |
| `daemon/` | Background HTTP/SSE server |
| `resilience/` | Circuit breaker, rate limiting, health checks |
| `permissions/` | Permission tiers, allow/deny rules, approval gates |
| `intelligence/` | Repo map, AST analysis, dependency graphs |
| `multiagent/` | Personas, inter-agent messaging, sub-agents |
| `feature/` | Eval, fingerprint, voice, taste, shellmode |
| `observability/` | Logger, metrics, OTEL tracing, alerts |
| `bridge/` | Integrations (merlin, kestrel, swift, sessioncapture) |
| `prompt/` | Identity preamble |
| `prompts/` | Modular template system (role, execution, tools, etc.) |
| `provider/` | Task-semantic roles, cascade intent, and product policy |
| `rules/` | Rules DSL engine |
| `system/` | Bus, shutdown, retention, cron, staleness |
| `storage/` | State directory management |
| `snapshot/` | File snapshots for undo |
| `graycode-skills/` | Bundled skills (32 skills) |

### REQ-3: Engine Sub-Systems

The `internal/engine/` package SHALL contain the following sub-systems:

| Sub-System | Purpose |
|------------|---------|
| `stream.go` | The agent loop (agentLoop) - main orchestration |
| `session.go` | Session struct and sub-services |
| `chat_service.go` | Rho ChatClient port, engine adapter coordination, compact |
| `safety/` | Permission engine, trust tiers, spec gate |
| `compact/` | Context compaction (collapse, micro, smart, truncate) |
| `ctxmgr/` | Context providers, packing, visualization |
| `token/` | Budget allocation, prediction |
| `streaming/` | Response cache, stream optimizer, thinking |
| `spec/` | Spec DAG, schema, validator, delta, archive |
| `branching/` | Conversation branching, snowball detection |
| `lifecycle/` | Self-improvement loop, few-shot, adaptive prompt |
| `memory/` | Harrier bridge, enhanced memory, skill distillation |
| `planning/` | Goals, task decomposition |
| `workflow/` | JSON-defined automation pipelines |
| `review/` | Code review bot, quality scorer |
| `observability/` | Profiler, debug recorder |
| `validation/` | Lint loop, test loop |
| `scaffold/` | Skill registry, few-shot, learned skills |

Provider boundary invariant:

```text
Rho CLI/TUI + conversation + tools
                  |
        Rho-owned ports/DTOs
                  |
                  v
            flux/engine
 credentials -> catalog -> routing -> generate/stream
```

No production Rho package may import a lower Flux package. Custom gateways
are supplied per Engine instance, and Flux DTOs are not Rho persistence or
CLI output schemas.

### REQ-4: Agent Loop Lifecycle

The agent loop in `stream.go` SHALL execute the following phases:

**Phase 1: Session Start (once)**
1. Execute session start hooks
2. Inject learned guidelines from Lifecycle.OnSessionStart()
3. Inject remembered context from Harrier.Recall()
4. Inject few-shot examples from FewShotStore
5. Inject user preferences from AdaptivePrompt
6. Inject previous learnings from AgentsAccum
7. Load smart skills from DefaultSkillDirs()

**Phase 2: Per-Turn Loop**
1. Check guard conditions (budget, doom loop, snowball)
2. Manage context before turn (compact if needed)
3. Run integration pipeline PreQuery (intent, cache, injection scan)
4. Find last user message
5. Match skills by context (auto-skill)
6. Refresh Harrier memories
7. Build LLM ChatOptions (system prompt, tools, model)
8. Inject ephemeral context (beliefs, matched skills, spec stage)
9. Execute the LLM call via `ChatService.Stream()` and the `flux/engine` adapter
10. Process response (tool calls, messages, cost tracking)
11. Check termination conditions

### REQ-5: Permission System

The permission system SHALL enforce the following gates:

**Spec Stage Gate:**
- During Specify/Plan/Tasks stages, only spec tools and read-only tools are allowed
- All write/edit/bash tools are blocked until ApproveImplementation
- Even YOLO autonomy cannot bypass the spec gate
- ApproveImplementation always prompts the user

**Trust Tier Gate:**
- Tier 0 (read-only): always allowed
- Tier 1 (non-destructive): depends on autonomy level
- Tier 2 (destructive): needs approval unless YOLO
- Tier 3 (system): always needs approval

**Additional Gates:**
- Rules DSL evaluation
- Boundary checker (allowed directories)
- Guardian audit

### REQ-6: Spec-Driven Development Workflow

The spec workflow SHALL follow a five-stage pipeline:

| Stage | Tool | Output | Gate |
|-------|------|--------|------|
| Specify | `Specify` | `spec.md` | Write tools blocked |
| Plan | `Plan` | `plan.md` | Write tools blocked |
| Tasks | `Tasks` | `tasks.md` | Write tools blocked |
| Approve | `ApproveImplementation` | User approval | User prompted |
| Implement | (all tools) | Code changes | All tools unblocked |

The spec system SHALL support:
- DAG-based artifact dependencies
- Delta specs for incremental changes
- Cross-artifact consistency analysis
- Quality scoring (0-100)
- Definition of Done validation
- Constitution rules validation

### REQ-7: Context Management

The context management system SHALL:

- Estimate token count before each LLM call
- Compact context when threshold is exceeded
- Support four compaction strategies: collapse, micro, smart, truncate
- Protect pinned messages from compaction
- Track cumulative file changes across compactions
- Inject fresh context (memories, skills) after compaction

### REQ-8: Memory and Self-Improvement

The memory system SHALL implement the following feedback loops:

**Session End Loop:**
1. Extract successful patterns → FewShotStore
2. Learn user corrections → AdaptivePrompt
3. Distill skills → SkillDistiller
4. Capture trajectory → TrajectoryDistiller
5. Store memories → Harrier

**Session Start Loop:**
1. Inject learned guidelines
2. Inject few-shot examples
3. Inject user preferences
4. Inject remembered context

**Per-Turn Loop:**
1. Recall relevant memories
2. Match skills by context
3. Refresh adaptive prompt

### REQ-9: Error Recovery and Resilience

The system SHALL handle the following edge cases:

| Edge Case | Recovery Strategy |
|-----------|-------------------|
| Context overflow | Emergency compact in ChatService.Stream |
| Doom loop | LoopDetector: 3 identical responses in 10-step window → error |
| Snowball | SnowballDetector: 500K token ceiling → force compact |
| API errors | Retry with exponential backoff + jitter |
| Network failure | Circuit breaker → provider failover |
| Corrupted state | Checkpoint manager, snapshot system for undo |
| Concurrent access | Session.mu RWMutex protects messages/system |
| Spec rejected | Stage stays closed, agent must revise |
| Tool permission denied | Blocked with message, agent retries or asks user |
| Budget exceeded | MaxTurns, MaxBudgetUSD limits → stop |
| Rate limit | Token bucket rate limiter on LLM calls |
| Model cascade | Selects cheaper model for simple tasks |
| Auto-compact | Triggers at configurable token % threshold |
| Branching | Conversation forking/merging via ConvoDAG |
| Skill not found | Graceful error, suggests install |
| gh CLI missing | TasksToIssuesTool returns friendly error |
| Prompt injection | Pipeline detects and blocks high-risk injections |

### REQ-10: Tool Execution Pipeline

The tool execution pipeline SHALL:

1. Receive tool call from LLM response
2. Check permissions (spec stage, trust tier, rules, boundary)
3. Execute tool via ToolService
4. Advance spec stage if spec tool
5. Record tool usage metrics
6. Return result to LLM
7. Handle errors gracefully

### REQ-11: Quality Assurance Loops

The system SHALL implement the following quality loops:

| Loop | Purpose |
|------|---------|
| LintLoop | Code → lint → fix → re-lint |
| TestLoop | Code → test → fix → re-test |
| Review | Code → review → fix → re-review |
| Critic | Patches → pre-screen → approve/reject |
| Backtrack | Decisions → record → undo if needed |

### REQ-12: Multi-Agent Coordination

The multi-agent system SHALL support:

- Agent personas (code-reviewer, security-auditor, test-engineer, web-performance-auditor)
- Inter-agent messaging via MessageBus
- Shared memory via SharedMemory
- Sub-agent spawning
- Parallel execution
- Mission coordination

## Success Criteria

- All tools execute within permission boundaries
- Spec workflow gates are enforced
- Context never exceeds model limits
- Memory persists across sessions
- Error recovery is automatic where possible
- Quality loops catch regressions
- Multi-agent coordination works without deadlocks
