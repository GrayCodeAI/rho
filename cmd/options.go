package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/GrayCodeAI/rho/internal/engine/safety"

	"github.com/GrayCodeAI/rho/internal/engine/scaffold"

	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	ctxrepomap "github.com/GrayCodeAI/rho/internal/context/repomap"
	"github.com/GrayCodeAI/rho/internal/engine"
	"github.com/GrayCodeAI/rho/internal/engine/branching"
	"github.com/GrayCodeAI/rho/internal/engine/lifecycle"
	"github.com/GrayCodeAI/rho/internal/intelligence/memory"
	"github.com/GrayCodeAI/rho/internal/intelligence/repomap"
	"github.com/GrayCodeAI/rho/internal/observability/logger"
	"github.com/GrayCodeAI/rho/internal/permissions"
	"github.com/GrayCodeAI/rho/internal/prompt"
	"github.com/GrayCodeAI/rho/internal/prompts"
	rhomodel "github.com/GrayCodeAI/rho/internal/provider/routing"
	"github.com/GrayCodeAI/rho/internal/snapshot"
	"github.com/GrayCodeAI/rho/internal/tool"
)

func buildSystemPrompt() (string, error) {
	return buildSystemPromptWithOptions(true, true)
}

func buildStartupSystemPrompt() (string, error) {
	return buildSystemPromptWithOptions(false, false)
}

func buildSystemPromptWithOptions(includeWorkspaceContext, includeRepoMap bool) (string, error) {
	if systemPromptFlag != "" && systemPromptFile != "" {
		return "", fmt.Errorf("cannot use both --system-prompt and --system-prompt-file")
	}
	if appendSystemPromptFlag != "" && appendSystemPromptFile != "" {
		return "", fmt.Errorf("cannot use both --append-system-prompt and --append-system-prompt-file")
	}

	// Build modular template-based system prompt
	ctx := prompts.DefaultContext()

	var ws *prompts.WorkspaceContext
	cwd, _ := os.Getwd()
	if includeWorkspaceContext {
		// Gather workspace context and inject into prompt context
		ws = prompts.GatherWorkspaceContext(cwd)
		if ws != nil {
			ctx.GitBranch = ws.GitBranch
			ctx.GitStatus = ws.GitStatus
			if len(ws.RecentCommits) > 0 {
				ctx.RecentCommits = strings.Join(ws.RecentCommits, " / ")
			}
			if len(ws.TopFiles) > 0 {
				ctx.TopFiles = strings.Join(ws.TopFiles, " ")
			}
		}
	}

	// Assemble modular prompt from templates (primary source for tools,
	// practices, communication). prompt.System() provides only the identity
	// preamble and system-level instructions the templates don't cover.
	modularPrompt, err := prompts.BuildSystemPrompt(ctx)
	if err != nil {
		// Fall back to preamble-only if templates fail
		modularPrompt = ""
	}

	base := prompt.System() + "\n\n" + rhoconfig.BuildStartupContextWithDirs(addDirs)
	if modularPrompt != "" {
		base += "\n\n" + modularPrompt
	}
	if ws != nil {
		wsFormatted := ws.Format()
		if wsFormatted != "" {
			base += "\n\n" + wsFormatted
		}
	}

	if systemPromptFile != "" {
		data, err := os.ReadFile(systemPromptFile)
		if err != nil {
			return "", fmt.Errorf("read --system-prompt-file: %w", err)
		}
		base = string(data)
	} else if systemPromptFlag != "" {
		base = systemPromptFlag
	}

	appendPrompt := appendSystemPromptFlag
	if appendSystemPromptFile != "" {
		data, err := os.ReadFile(appendSystemPromptFile)
		if err != nil {
			return "", fmt.Errorf("read --append-system-prompt-file: %w", err)
		}
		appendPrompt = string(data)
	}
	if appendPrompt != "" {
		if base != "" {
			base += "\n\n"
		}
		base += appendPrompt
	}

	// Inject repo map into system prompt if enabled in settings.
	if includeRepoMap {
		base = injectRepoMap(base)
	}

	return base, nil
}

func buildDeferredWorkspacePromptContext() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	var sections []string
	if deferred := strings.TrimSpace(rhoconfig.BuildDeferredContextWithDirs(addDirs)); deferred != "" {
		sections = append(sections, deferred)
	}
	if ws := prompts.GatherWorkspaceContext(cwd); ws != nil {
		if formatted := strings.TrimSpace(ws.Format()); formatted != "" {
			sections = append(sections, formatted)
		}
	}
	// Repo map remains opt-in and is appended after first paint for chat startup.
	base := injectRepoMap("")
	if trimmed := strings.TrimSpace(base); trimmed != "" {
		sections = append(sections, trimmed)
	}
	return strings.Join(sections, "\n\n")
}

const startupPlaceholderProvider = "anthropic"

// injectRepoMap generates a repo map of the current directory and appends
// it to the system prompt when the repo_map setting is enabled.
func injectRepoMap(base string) string {
	// --repo-map opts into the AST-ranked, PageRank-weighted overview (Aider
	// parity) at the --map-tokens budget, independent of the settings toggle.
	if repoMapFlag {
		cwd, err := os.Getwd()
		if err == nil {
			budget := mapTokensFlag
			if budget <= 0 {
				budget = ctxrepomap.DefaultBudget
			}
			if formatted, err := ctxrepomap.RepoMap(cwd, budget); err == nil && formatted != "" {
				base += "\n\n# Repository Map\n" + formatted
			}
		}
		return base
	}

	settings := rhoconfig.LoadSettings()
	if settings.RepoMap == nil || !*settings.RepoMap {
		return base
	}
	maxTokens := settings.RepoMapMaxTokens
	if maxTokens <= 0 {
		maxTokens = 2000
	}
	cwd, err := os.Getwd()
	if err != nil {
		return base
	}
	rm, err := repomap.Generate(cwd, repomap.Options{
		MaxFiles:  500,
		MaxTokens: maxTokens,
	})
	if err != nil || rm == nil || len(rm.Files) == 0 {
		return base
	}
	formatted := rm.Format(maxTokens)
	if formatted == "" {
		return base
	}
	return base + "\n\n# Repository Map\n" + formatted
}

func loadEffectiveSettings() (rhoconfig.Settings, error) {
	return rhoconfig.LoadSettingsWithOverride(settingsFlag)
}

func resolveSelection(settings rhoconfig.Settings) rhoconfig.Selection {
	return rhoconfig.EffectiveSelectionWithSettings(context.Background(), settings, rhoconfig.SelectionOptions{
		ProviderOverride: firstNonEmptyTrimmed(provider, settings.Provider),
		ModelOverride:    firstNonEmptyTrimmed(model, settings.Model),
	})
}

func startupSelection(settings rhoconfig.Settings) rhoconfig.Selection {
	providerOverride := firstNonEmptyTrimmed(provider, settings.Provider)
	modelOverride := firstNonEmptyTrimmed(model, settings.Model)

	explicitProvider, explicitModel := explicitSelection(context.Background())

	providerID := rhoconfig.ActiveProviderID(firstNonEmptyTrimmed(providerOverride, explicitProvider))
	modelID := strings.TrimSpace(firstNonEmptyTrimmed(modelOverride, explicitModel))

	if providerID == "" && modelID != "" {
		providerID = rhoconfig.ActiveProviderID(rhoconfig.ProviderOfModelWithSettings(settings, modelID))
	}
	if modelID == "" && providerID != "" {
		modelID = strings.TrimSpace(rhoconfig.DefaultModelForProviderWithSettings(settings, providerID))
	}
	if providerID == "" {
		providerID = startupPlaceholderProvider
	}

	return rhoconfig.Selection{
		Provider: providerID,
		Model:    modelID,
	}
}

func effectiveModelAndProvider(settings rhoconfig.Settings) (string, string) {
	selection := resolveSelection(settings)
	if !selection.HasConfiguredDeployment {
		return "", ""
	}
	return selection.Model, selection.Provider
}

func newStartupRhoSession(selection rhoconfig.Selection, systemPrompt string, registry *tool.Registry) *engine.Session {
	providerID := strings.TrimSpace(selection.Provider)
	if providerID == "" {
		providerID = startupPlaceholderProvider
	}
	return engine.NewSession(providerID, strings.TrimSpace(selection.Model), systemPrompt, registry)
}

func newRhoSession(settings rhoconfig.Settings, effectiveProvider, effectiveModel, systemPrompt string, registry *tool.Registry) *engine.Session {
	selection := resolveSelection(settings)
	if strings.TrimSpace(selection.Provider) == "" {
		selection.Provider = effectiveProvider
	}
	if strings.TrimSpace(selection.Model) == "" {
		selection.Model = effectiveModel
	}
	sess := engine.NewRhoSessionForSettings(context.Background(), settings, selection, selection.Provider, selection.Model, systemPrompt, registry)
	return sess
}

// newConfiguredRhoSession is the non-interactive command composition root.
// Interactive chat intentionally keeps its lightweight startup and deferred
// heavy configuration split; batch/daemon/ACP callers use this atomic path.
func newConfiguredRhoSession(settings rhoconfig.Settings, effectiveProvider, effectiveModel, systemPrompt string, registry *tool.Registry, sessionLogger *logger.Logger, maxTurnsOverride ...int) (*engine.Session, error) {
	sess := newRhoSession(settings, effectiveProvider, effectiveModel, systemPrompt, registry)
	if sessionLogger != nil {
		sess.SetLogger(sessionLogger)
	}
	if err := configureSession(sess, settings, maxTurnsOverride...); err != nil {
		return nil, err
	}
	return sess, nil
}

// newConfiguredRhoSessionFactory is the shared composition seam for
// non-interactive protocol/server entry points. It owns registry creation and
// settings-based model selection while allowing each protocol to provide its
// own prompt and optional model override.
func newConfiguredRhoSessionFactory(settings rhoconfig.Settings, sessionLogger *logger.Logger) func(string, string, ...int) (*engine.Session, error) {
	return func(systemPrompt, modelOverride string, maxTurnsOverride ...int) (*engine.Session, error) {
		registry, err := defaultRegistry(settings)
		if err != nil {
			return nil, err
		}
		effectiveModel, effectiveProvider := effectiveModelAndProvider(settings)
		if strings.TrimSpace(modelOverride) != "" {
			effectiveModel = modelOverride
		}
		return newConfiguredRhoSession(settings, effectiveProvider, effectiveModel, systemPrompt, registry, sessionLogger, maxTurnsOverride...)
	}
}

// prepareInteractiveSessionStartup applies only the cheap TUI startup slice.
// Transport rebuild and heavy memory setup remain deferred until the first
// real chat request in bootstrapSessionForChat.
func prepareInteractiveSessionStartup(sess *engine.Session, settings rhoconfig.Settings) error {
	syncSessionFromPersistedSelection(sess)
	sess.SetLogger(logger.New(io.Discard, logger.Error))
	return configureSessionStartup(sess, settings)
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func configureSession(sess *engine.Session, settings rhoconfig.Settings, maxTurnsOverride ...int) error {
	if err := configureSessionStartup(sess, settings, maxTurnsOverride...); err != nil {
		return err
	}
	configureSessionHeavy(sess)
	return nil
}

func configureSessionStartup(sess *engine.Session, settings rhoconfig.Settings, maxTurnsOverride ...int) error {
	sess.WireAgentTool()
	sess.SetAllowedDirs(addDirs)
	projectDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve project directory for permission rules: %w", err)
	}
	exactRules := permissions.NewStableRuleStore(permissions.DefaultStableRulesPath(projectDir))
	if err := exactRules.Load(); err != nil {
		return fmt.Errorf("load persisted permission rules: %w", err)
	}
	sess.PermSvc().SetExactRuleStore(exactRules)
	_ = sess.SetWorkMode(engine.WorkModeAct)
	// Auto-commit: CLI flag wins, else settings.auto_commit, default off.
	autoCommit := autoCommitFlag
	if !autoCommitFlag && settings.AutoCommit != nil {
		autoCommit = *settings.AutoCommit
	}
	sess.SetAutoCommit(autoCommit)

	allowSpecs := append(append([]string{}, settings.AutoAllow...), settings.AllowedTools...)
	allowSpecs = append(allowSpecs, parseToolListFromCLI(allowedToolsFlag)...)
	denySpecs := append([]string{}, settings.DisallowedTools...)
	denySpecs = append(denySpecs, parseToolListFromCLI(disallowedToolsFlag)...)
	if !sess.PermSvc().ReplaceSessionRules(allowSpecs, denySpecs) {
		return fmt.Errorf("initialize session permission rules: permission service unavailable")
	}

	if dryRunFlag {
		sess.PermSvc().SetDryRun(true)
	}
	effectiveMaxTurns := maxTurns
	if len(maxTurnsOverride) > 0 && maxTurnsOverride[0] > 0 {
		effectiveMaxTurns = maxTurnsOverride[0]
	}
	if err := sess.SetMaxTurns(effectiveMaxTurns); err != nil {
		return err
	}

	budget := maxBudgetUSD
	if budget == 0 && settings.MaxBudgetUSD > 0 {
		budget = settings.MaxBudgetUSD
	}
	if err := sess.SetMaxBudgetUSD(budget); err != nil {
		return err
	}
	sess.ApplyUsageSettings(settings.HourlyTokenLimit, settings.DailyTokenLimit, settings.SessionTokenLimit)

	// Teach mode: augment system prompt with explanation instructions
	if teachMode {
		sess.AppendSystemContext("\n\n## Teaching Mode\n" + engine.TeachPromptAugment(teachDepth))
	}

	// Model cascade router: automatically routes tasks to optimal model tier
	roles := rhomodel.DefaultRoles(sess.Model())
	if settings.ModelRoles != nil {
		roles = *settings.ModelRoles
	}
	cascade := branching.NewCascadeRouter(sess.Model(), roles)
	cascade.Enabled = true
	cascade.FrugalMode = settings.Frugal
	sess.LifecycleSvc().SetCascade(cascade)

	// Smart turn routing: opt-in cheap-simple / strong per-turn model choice.
	if settings.SmartRouting != nil {
		sess.LifecycleSvc().SetSmartRouting(settings.SmartRouting)
	}

	// Session lifecycle: self-improvement loop (learn from sessions)
	sess.LifecycleSvc().SetLifecycle(&lifecycle.SessionLifecycle{
		Memory: &lifecycle.EvolvingMemoryAdapter{EM: memory.NewEvolvingMemory()},
	})

	// Reflector: verbal self-reflection on tool failures (Reflexion-style)
	sess.LifecycleSvc().SetReflector(engine.NewReflector(sess, sess.Model()))

	// Few-shot learning: collect successful patterns from sessions
	sess.LifecycleSvc().SetFewShotStore(scaffold.NewFewShotStore())

	// Adaptive prompt: learn user preferences from corrections
	sess.LifecycleSvc().SetAdaptivePrompt(engine.NewAdaptivePrompt())

	if pct := settings.AutoCompactThresholdPct; pct > 0 {
		sess.SetAutoCompactThresholdPct(pct)
	}
	sess.EnsureAutoCompactor()

	if settings.AutonomyExplicit {
		sess.PermSvc().SetAutonomy(safety.AutonomySupervised)
	} else if lvl := autonomyFromSettings(settings.Autonomy); lvl != 0 {
		sess.PermSvc().SetAutonomy(lvl)
	}
	// CLI safety overrides saved settings: the explicit dangerous-skip flag
	// must not be silently downgraded by a persisted autonomy tier.
	if dangerouslySkipPermissions {
		sess.PermSvc().SetAutonomy(safety.AutonomyYOLO)
	}

	// Per-model thinking preference (Setup → Models Think column), with
	// provider-specific defaults (e.g. LongCat off).
	modelID := strings.TrimSpace(sess.Model())
	providerID := strings.TrimSpace(sess.Provider())
	sess.SetThinkingEnabled(rhoconfig.ResolveThinkingForModel(settings, modelID, providerID))

	return nil
}

func configureSessionHeavy(sess *engine.Session) {
	cwd, _ := os.Getwd()

	// Snapshot git repo setup is useful, but it should not block the first TUI frame.
	snap := snapshot.New(cwd)
	if err := snap.Init(); err == nil {
		sess.SetSnapshots(snap)
	}

	// Memory initialization touches persisted local state.
	enhancedMem := memory.NewEnhancedMemoryManager(cwd)
	sess.MemorySvc().SetMemory(enhancedMem)
	sess.MemorySvc().SetEnhanced(enhancedMem)
	// Use a real unique session ID (genID) rather than a fabricated
	// "session_<nanos>" placeholder — the persist ID may not be assigned
	// yet at this point in startup, but it must still be collision-safe
	// for the memory manager (LOW finding: fabricated session IDs).
	enhancedMem.StartSession(genID())
}

// bindChatSession wires TUI-only session metadata (persist id, compaction callbacks).
func bindChatSession(sess *engine.Session, sessionID string) {
	if sess == nil {
		return
	}
	if id := strings.TrimSpace(sessionID); id != "" {
		sess.SetPersistID(id)
	}
}

func validateRootFlags() error {
	if outputFormat != "text" && outputFormat != "json" && outputFormat != "stream-json" && outputFormat != "transcript" {
		return fmt.Errorf("--output-format must be one of: text, json, stream-json, transcript")
	}
	if inputFormat != "text" && inputFormat != "stream-json" {
		return fmt.Errorf("--input-format must be one of: text, stream-json")
	}
	if inputFormat == "stream-json" && outputFormat != "stream-json" {
		return fmt.Errorf("--input-format=stream-json requires --output-format=stream-json")
	}
	if continueFlag && resumeID != "" {
		return fmt.Errorf("--continue and --resume cannot be used together")
	}
	if sessionIDFlag != "" && (continueFlag || resumeID != "") && !forkSessionFlag {
		return fmt.Errorf("--session-id can only be used with --continue or --resume when --fork-session is also specified")
	}
	if maxTurns < 0 {
		return fmt.Errorf("--max-turns must be non-negative")
	}
	if maxBudgetUSD < 0 {
		return fmt.Errorf("--max-budget-usd must be non-negative")
	}
	if systemPromptFlag != "" && systemPromptFile != "" {
		return fmt.Errorf("cannot use both --system-prompt and --system-prompt-file")
	}
	if appendSystemPromptFlag != "" && appendSystemPromptFile != "" {
		return fmt.Errorf("cannot use both --append-system-prompt and --append-system-prompt-file")
	}
	return nil
}

func readPromptFromStdin(format string) (string, error) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeCharDevice != 0 {
		return "", nil
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	if format == "stream-json" {
		return promptFromStreamJSON(data)
	}
	return strings.TrimRight(string(data), "\r\n"), nil
}

func promptFromStreamJSON(data []byte) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	var parts []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		text, err := promptFromStreamJSONLine(line)
		if err != nil {
			return "", err
		}
		if text != "" {
			parts = append(parts, text)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return strings.Join(parts, "\n"), nil
}

func promptFromStreamJSONLine(line string) (string, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return "", fmt.Errorf("invalid stream-json input: %w", err)
	}

	eventType := jsonString(obj["type"])
	switch eventType {
	case "", "user", "user_message", "message", "prompt":
	default:
		return "", nil
	}
	for _, key := range []string{"prompt", "content", "text"} {
		if s := jsonString(obj[key]); s != "" {
			return s, nil
		}
	}
	if raw, ok := obj["message"]; ok {
		if s := jsonString(raw); s != "" {
			return s, nil
		}
		var nested map[string]json.RawMessage
		if json.Unmarshal(raw, &nested) == nil {
			for _, key := range []string{"content", "text", "prompt"} {
				if s := jsonString(nested[key]); s != "" {
					return s, nil
				}
			}
		}
	}
	return "", nil
}

func jsonString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

func parseToolListFromCLI(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	var result []string
	for _, value := range values {
		if value == "" {
			continue
		}
		var current strings.Builder
		depth := 0
		for _, r := range value {
			switch r {
			case '(':
				depth++
				current.WriteRune(r)
			case ')':
				if depth > 0 {
					depth--
				}
				current.WriteRune(r)
			case ',', ' ', '\t', '\n':
				if depth > 0 {
					current.WriteRune(r)
					continue
				}
				if spec := strings.TrimSpace(current.String()); spec != "" {
					result = append(result, spec)
				}
				current.Reset()
			default:
				current.WriteRune(r)
			}
		}
		if spec := strings.TrimSpace(current.String()); spec != "" {
			result = append(result, spec)
		}
	}
	return result
}

func filterAvailableTools(all []tool.Tool, toolsSpecified bool, toolSpecs []string, disallowedSpecs []string) ([]tool.Tool, error) {
	selected := all
	if toolsSpecified {
		if len(toolSpecs) == 0 {
			return nil, nil
		}
		if len(toolSpecs) == 1 && strings.EqualFold(toolSpecs[0], "default") {
			selected = all
		} else {
			var filtered []tool.Tool
			seen := make(map[string]bool)
			for _, spec := range toolSpecs {
				if strings.EqualFold(spec, "default") {
					for _, t := range all {
						if !seen[t.Name()] {
							filtered = append(filtered, t)
							seen[t.Name()] = true
						}
					}
					continue
				}
				match := findToolBySpec(all, spec)
				if match == nil {
					return nil, fmt.Errorf("unknown tool in --tools: %s", spec)
				}
				if !seen[match.Name()] {
					filtered = append(filtered, match)
					seen[match.Name()] = true
				}
			}
			selected = filtered
		}
	}

	for _, spec := range disallowedSpecs {
		if toolSpecHasPattern(spec) {
			continue
		}
		selected = removeToolBySpec(selected, spec)
	}
	return selected, nil
}

func findToolBySpec(tools []tool.Tool, spec string) tool.Tool {
	name := toolSpecName(spec)
	for _, t := range tools {
		if toolNameMatches(t, name) {
			return t
		}
	}
	return nil
}

func removeToolBySpec(tools []tool.Tool, spec string) []tool.Tool {
	name := toolSpecName(spec)
	out := tools[:0]
	for _, t := range tools {
		if !toolNameMatches(t, name) {
			out = append(out, t)
		}
	}
	return out
}

func toolNameMatches(t tool.Tool, name string) bool {
	if strings.EqualFold(t.Name(), name) {
		return true
	}
	aliased, ok := t.(tool.AliasedTool)
	if !ok {
		return false
	}
	for _, alias := range aliased.Aliases() {
		if strings.EqualFold(alias, name) {
			return true
		}
	}
	return false
}

func toolSpecName(spec string) string {
	spec = strings.TrimSpace(spec)
	if open := strings.Index(spec, "("); open >= 0 {
		return strings.TrimSpace(spec[:open])
	}
	return spec
}

func toolSpecHasPattern(spec string) bool {
	spec = strings.TrimSpace(spec)
	open := strings.Index(spec, "(")
	return open >= 0 && strings.HasSuffix(spec, ")") && strings.TrimSpace(spec[open+1:len(spec)-1]) != ""
}
