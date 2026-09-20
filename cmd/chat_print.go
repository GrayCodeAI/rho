package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	lipgloss "charm.land/lipgloss/v2"
	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	"github.com/GrayCodeAI/rho/internal/engine"
	aiwatch "github.com/GrayCodeAI/rho/internal/engine/io"
	"github.com/GrayCodeAI/rho/internal/engine/lifecycle"
	chatfeature "github.com/GrayCodeAI/rho/internal/features/chat"
	"github.com/GrayCodeAI/rho/internal/observability/logger"
	"github.com/GrayCodeAI/rho/internal/session"
)

// Print mode and session persistence functions extracted from chat.go

func runPrint(text string) error {
	systemPrompt, err := buildSystemPrompt()
	if err != nil {
		return err
	}

	settings, err := loadEffectiveSettings()
	if err != nil {
		return err
	}
	effectiveModel, effectiveProvider := effectiveModelAndProvider(settings)
	registry, err := defaultRegistry(settings)
	if err != nil {
		return err
	}

	sess, cfgErr := newConfiguredRhoSession(settings, effectiveProvider, effectiveModel, systemPrompt, registry, logger.New(io.Discard, logger.Error))
	if cfgErr != nil {
		return cfgErr
	}
	if _, err := os.Getwd(); err != nil {
		return fmt.Errorf("resolve project directory: %w", err)
	}

	promptInput := openPromptInput()
	defer promptInput.close()
	configureInteractivePrompts(sess, promptInput)

	sessionID, _, err := prepareSession(sess)
	if err != nil {
		return err
	}

	sess.AddUser(text)

	// Wire timeout if --timeout flag is set
	ctx := context.Background()
	var countdown bool
	if timeout > 0 {
		cfg := lifecycle.TimeoutConfig{Total: timeout, Countdown: true}
		var cancel context.CancelFunc
		ctx, cancel = lifecycle.WithTimeout(ctx, cfg)
		defer cancel()
		countdown = cfg.Countdown
	}

	ch, streamErrors := chatfeature.StreamChannel(ctx, sess)

	var printed strings.Builder
	var countdownShown bool
	var lastUsage *engine.StreamUsage
	turns := 0
	started := time.Now()
	for ev := range ch {
		switch ev.Type {
		case "content":
			if outputFormat == "text" && !printMarkdown {
				fmt.Print(ev.Content)
			} else if outputFormat == "stream-json" {
				writePrintEvent(sessionID, "content", ev.Content, "")
			}
			printed.WriteString(ev.Content)
			// Honour the Countdown flag (was previously set but unread):
			// surface the remaining time budget once, on the first content.
			if countdown && !countdownShown {
				if rem := lifecycle.RemainingTime(ctx); rem != "" {
					fmt.Fprintf(os.Stderr, "%s\n", auditTint("[time remaining] "+rem, warnAmber))
					countdownShown = true
				}
			}
		case "tool_use":
			if outputFormat == "stream-json" {
				writePrintEvent(sessionID, "tool_use", "", ev.ToolName)
			} else if !IsQuiet() {
				_, _ = fmt.Fprintf(os.Stderr, "\n%s\n", auditTint("["+ev.ToolName+"]", infoSky))
			}
		case "tool_result":
			content := ev.Content
			// Rune-based truncation to avoid splitting multi-byte UTF-8.
			if utf8.RuneCountInString(content) > 500 {
				content = string([]rune(content)[:500]) + "..."
			}
			if outputFormat == "stream-json" {
				writePrintEvent(sessionID, "tool_result", content, ev.ToolName)
			} else if !IsQuiet() {
				_, _ = fmt.Fprintf(os.Stderr, "%s %s\n", auditTint("["+ev.ToolName+"]", infoSky), content)
			}
		case "usage":
			if ev.Usage != nil {
				lastUsage = ev.Usage
				turns++
			}
			if outputFormat == "stream-json" && ev.Usage != nil {
				writePrintUsageEvent(sessionID, ev.Usage)
			}
		case "error":
			if outputFormat == "stream-json" {
				writePrintResult(printed.String(), sessionID, sess, true, []string{ev.Content})
			}
			return fmt.Errorf("%s", ev.Content)
		case "done":
			switch outputFormat {
			case "text":
				printTextResponse(printed.String())
				printTextUsageFooter(lastUsage, started, turns, effectiveModel)
			case "transcript":
				printTranscript(sessionID, effectiveModel, printed.String(), lastUsage, started, turns)
			case "json":
				writePrintResult(printed.String(), sessionID, sess, false, nil)
			case "stream-json":
				writePrintResult(printed.String(), sessionID, sess, false, nil)
			}
			if !noSessionPersistence {
				saveFluxSession(sessionID, sess)
			}
			return nil
		}
	}
	if err := <-streamErrors; err != nil {
		return err
	}
	switch outputFormat {
	case "text":
		printTextResponse(printed.String())
		printTextUsageFooter(lastUsage, started, turns, effectiveModel)
	case "json":
		writePrintResult(printed.String(), sessionID, sess, false, nil)
	case "stream-json":
		writePrintResult(printed.String(), sessionID, sess, false, nil)
	}
	if !noSessionPersistence {
		saveFluxSession(sessionID, sess)
	}
	return nil
}

func writePrintUsageEvent(sessionID string, usage *engine.StreamUsage) {
	usageEvent := map[string]int{
		"prompt_tokens":     usage.PromptTokens,
		"completion_tokens": usage.CompletionTokens,
	}
	event := map[string]interface{}{
		"type":       "usage",
		"uuid":       genID(),
		"session_id": sessionID,
		"usage":      usageEvent,
	}
	if usage.CacheReadTokens > 0 {
		usageEvent["cache_read_tokens"] = usage.CacheReadTokens
	}
	if usage.CacheWriteTokens > 0 {
		usageEvent["cache_write_tokens"] = usage.CacheWriteTokens
	}
	data, _ := json.Marshal(event)
	fmt.Println(string(data))
}

// printTextUsageFooter renders a muted token/elapsed summary to stderr after a
// one-shot text-mode run. It writes to stderr so stdout stays pure for scripts,
// and is skipped entirely when no usage event was received or --quiet is set.
func printTextUsageFooter(usage *engine.StreamUsage, started time.Time, turns int, model string) {
	if usage == nil || IsQuiet() {
		return
	}
	parts := []string{fmt.Sprintf("%d in · %d out", usage.PromptTokens, usage.CompletionTokens)}
	if usage.CacheReadTokens > 0 || usage.CacheWriteTokens > 0 {
		parts = append(parts, fmt.Sprintf("cache %d read · %d write", usage.CacheReadTokens, usage.CacheWriteTokens))
	}
	parts = append(parts, fmt.Sprintf("%d turn(s)", turns))
	parts = append(parts, time.Since(started).Round(time.Second).String())
	if model != "" {
		parts = append(parts, model)
	}
	_, _ = fmt.Fprintf(os.Stderr, "%s\n", auditTint("tokens: "+strings.Join(parts, " · "), textMuted))
}

// printTranscript emits a plain, timestamped transcript of the turn to stdout.
// It is the "transcript" output format: human-readable, stable, and free of
// ANSI so it can be pasted into a bug report or piped to a file.
func printTranscript(sessionID, model, response string, usage *engine.StreamUsage, started time.Time, turns int) {
	var b strings.Builder
	fmt.Fprintf(&b, "# rho transcript\n")
	fmt.Fprintf(&b, "session: %s\n", sessionID)
	if model != "" {
		fmt.Fprintf(&b, "model:   %s\n", model)
	}
	fmt.Fprintf(&b, "started: %s\n", started.Format(time.RFC3339))
	fmt.Fprintf(&b, "turns:   %d\n", turns)
	if usage != nil {
		fmt.Fprintf(&b, "tokens:  %d in · %d out\n", usage.PromptTokens, usage.CompletionTokens)
	}
	fmt.Fprintf(&b, "\n---\n\n")
	b.WriteString(strings.TrimRight(response, "\n"))
	b.WriteString("\n")
	fmt.Print(b.String())
}

// printTextResponse emits the final text-mode response, rendering markdown to
// styled ANSI when --markdown is set and color is enabled. Raw markdown is
// preserved for piped/NO_COLOR output so scripts stay machine-parseable.
func printTextResponse(s string) {
	s = renderPrintResponse(s, printMarkdown, ShouldColor())
	fmt.Print(s)
	if !strings.HasSuffix(s, "\n") {
		fmt.Println()
	}
}

// renderPrintResponse applies markdown rendering when both markdown and color
// are requested; otherwise it returns the raw response unchanged.
func renderPrintResponse(s string, markdown, color bool) string {
	if !markdown || !color || s == "" {
		return s
	}
	w, _ := TermSize()
	if w <= 0 {
		w = 80
	}
	return renderMarkdown(s, w)
}

func writePrintResult(result, sessionID string, sess *engine.Session, isError bool, errors []string) {
	event := map[string]interface{}{
		"type":           "result",
		"subtype":        "success",
		"is_error":       isError,
		"result":         result,
		"session_id":     sessionID,
		"uuid":           genID(),
		"total_cost_usd": sess.CostValue().Total(),
	}
	if isError {
		event["subtype"] = "error_during_execution"
		event["errors"] = errors
	}
	if outputFields != "" {
		event = filterFields(event, parseFieldList(outputFields))
	}
	data, _ := json.Marshal(event)
	fmt.Println(string(data))
}

// parseFieldList splits a comma-separated field list into a set.
func parseFieldList(fields string) map[string]bool {
	set := make(map[string]bool)
	for _, f := range strings.Split(fields, ",") {
		f = strings.TrimSpace(f)
		if f != "" {
			set[f] = true
		}
	}
	return set
}

// filterFields returns a new map containing only the whitelisted fields.
// The "type" field is always included since it identifies the event kind.
func filterFields(event map[string]interface{}, fields map[string]bool) map[string]interface{} {
	if len(fields) == 0 {
		return event
	}
	filtered := make(map[string]interface{}, len(fields)+1)
	// Always preserve "type" so the event kind is identifiable.
	fields["type"] = true
	for k, v := range event {
		if fields[k] {
			filtered[k] = v
		}
	}
	return filtered
}

func writePrintEvent(sessionID, eventType, content, toolName string) {
	event := map[string]string{
		"type":       eventType,
		"uuid":       genID(),
		"session_id": sessionID,
	}
	if content != "" {
		event["content"] = content
	}
	if toolName != "" {
		event["tool_name"] = toolName
	}
	data, _ := json.Marshal(event)
	fmt.Println(string(data))
}

func saveFluxSession(id string, sess *engine.Session) {
	raw := sess.RawMessages()
	if len(raw) == 0 {
		return
	}
	_ = session.Save(&session.Session{
		ID:        id,
		Model:     sess.Model(),
		Provider:  sess.Provider(),
		Messages:  session.FromRuntimeMessages(raw),
		CreatedAt: time.Now(),
	})
}

// runRepl starts an interactive REPL mode for multi-turn conversation without TUI.
func runRepl() error {
	fmt.Fprintln(os.Stderr, auditTint("Rho REPL", textPrimary)+auditTint(" — type 'exit' or 'quit' to leave, 'help' for commands", textMuted))
	fmt.Fprintln(os.Stderr)

	systemPrompt, err := buildSystemPrompt()
	if err != nil {
		return err
	}

	settings, err := loadEffectiveSettings()
	if err != nil {
		return err
	}

	effectiveModel, effectiveProvider := effectiveModelAndProvider(settings)
	registry, err := defaultRegistry(settings)
	if err != nil {
		return err
	}

	sess, cfgErr := newConfiguredRhoSession(settings, effectiveProvider, effectiveModel, systemPrompt, registry, logger.New(io.Discard, logger.Error))
	if cfgErr != nil {
		return cfgErr
	}
	if _, err := os.Getwd(); err != nil {
		return fmt.Errorf("resolve project directory: %w", err)
	}

	promptInput := openPromptInput()
	defer promptInput.close()
	configureInteractivePrompts(sess, promptInput)

	reader := bufio.NewReader(os.Stdin)

	sessionID, _, err := prepareSession(sess)
	if err != nil {
		return err
	}

	if recordPath != "" {
		restoreRec, recErr := startRecording(recordPath)
		if recErr != nil {
			return fmt.Errorf("record: %w", recErr)
		}
		defer restoreRec()
	}

	ctx := context.Background()
	var countdown bool
	if timeout > 0 {
		cfg := lifecycle.TimeoutConfig{Total: timeout, Countdown: true}
		var cancel context.CancelFunc
		ctx, cancel = lifecycle.WithTimeout(ctx, cfg)
		defer cancel()
		countdown = cfg.Countdown
	}

	for {
		_, _ = fmt.Fprint(os.Stderr, "\n> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(os.Stderr, "")
				return nil
			}
			return err
		}
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			fmt.Fprintln(os.Stderr, "Goodbye!")
			return nil
		}
		if input == "help" {
			fmt.Fprintln(os.Stderr, "Commands:")
			fmt.Fprintln(os.Stderr, "  exit, quit  - Leave REPL")
			fmt.Fprintln(os.Stderr, "  help        - Show this help")
			fmt.Fprintln(os.Stderr, "  /tools      - List available tools")
			fmt.Fprintln(os.Stderr, "  /models     - List available models")
			fmt.Fprintln(os.Stderr, "  /session    - Show session info")
			fmt.Fprintln(os.Stderr, "")
			continue
		}
		if output, handled, builtinErr := replBuiltinResponse(input, sess, settings, sessionID); handled {
			if builtinErr != nil {
				fmt.Fprintf(os.Stderr, "%s\n", auditTint(fmt.Sprintf("Error: %v", builtinErr), errorCoral))
				continue
			}
			if output != "" {
				_, _ = fmt.Fprintln(replOut, output)
			}
			continue
		}

		sess.AddUser(input)

		ch, streamErrors := chatfeature.StreamChannel(ctx, sess)

		var printed strings.Builder
		var countdownShown bool
		for ev := range ch {
			switch ev.Type {
			case "content":
				if outputFormat == "text" {
					_, _ = fmt.Fprint(replOut, ev.Content)
				} else if outputFormat == "stream-json" {
					writePrintEvent(sessionID, "content", ev.Content, "")
				}
				printed.WriteString(ev.Content)
				if countdown && !countdownShown {
					if rem := lifecycle.RemainingTime(ctx); rem != "" {
						fmt.Fprintf(os.Stderr, "[time remaining] %s\n", rem)
						countdownShown = true
					}
				}
			case "tool_use":
				if outputFormat == "stream-json" {
					writePrintEvent(sessionID, "tool_use", "", ev.ToolName)
				} else {
					_, _ = fmt.Fprintf(os.Stderr, "\n[%s]\n", ev.ToolName)
				}
			case "tool_result":
				content := ev.Content
				// Rune-based truncation to avoid splitting multi-byte UTF-8.
				if utf8.RuneCountInString(content) > 500 {
					content = string([]rune(content)[:500]) + "..."
				}
				if outputFormat == "stream-json" {
					writePrintEvent(sessionID, "tool_result", content, ev.ToolName)
				} else {
					_, _ = fmt.Fprintf(os.Stderr, "[%s] %s\n", ev.ToolName, content)
				}
			case "usage":
				if outputFormat == "stream-json" && ev.Usage != nil {
					writePrintUsageEvent(sessionID, ev.Usage)
				}
			case "error":
				if outputFormat == "stream-json" {
					writePrintResult(printed.String(), sessionID, sess, true, []string{ev.Content})
				}
				fmt.Fprintf(os.Stderr, "%s\n", auditTint(fmt.Sprintf("Error: %s", ev.Content), errorCoral))
			case "done":
				switch outputFormat {
				case "text":
					if !strings.HasSuffix(printed.String(), "\n") {
						fmt.Println()
					}
				case "json":
					writePrintResult(printed.String(), sessionID, sess, false, nil)
				case "stream-json":
					writePrintResult(printed.String(), sessionID, sess, false, nil)
				}
				if !noSessionPersistence {
					saveFluxSession(sessionID, sess)
				}
			}
		}
		if err := <-streamErrors; err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", auditTint(fmt.Sprintf("Error: %v", err), errorCoral))
		}
	}
}

func replBuiltinResponse(input string, sess *engine.Session, settings rhoconfig.Settings, sessionID string) (string, bool, error) {
	switch strings.TrimSpace(input) {
	case "/tools":
		return builtInToolsSummary(), true, nil
	case "/session":
		var b strings.Builder
		b.WriteString("Session info:\n")
		b.WriteString(fmt.Sprintf("  ID: %s\n", sessionID))
		b.WriteString(fmt.Sprintf("  Provider: %s\n", sess.Provider()))
		b.WriteString(fmt.Sprintf("  Model: %s\n", sess.Model()))
		b.WriteString(fmt.Sprintf("  Messages: %d", len(sess.RawMessages())))
		return b.String(), true, nil
	case "/models":
		return replModelsSummary(settings, sess.Provider())
	default:
		return "", false, nil
	}
}

func replModelsSummary(settings rhoconfig.Settings, sessionProvider string) (string, bool, error) {
	providerName := effectiveProviderForREPL(settings, sessionProvider)
	if providerName == "" {
		return "No active provider selected. Set one with `rho config provider <name>` or start REPL with `--provider`.", true, nil
	}
	models, err := rhoconfig.FetchModelsForProvider(providerName)
	if err != nil {
		return "", true, err
	}
	if len(models) == 0 {
		if providerName == "" {
			return "No models available in the catalog cache.", true, nil
		}
		return fmt.Sprintf("No models available in the catalog cache for provider %q.", providerName), true, nil
	}

	rows := make([]modelTableRow, len(models))
	for i, m := range models {
		rows[i] = modelTableRowFromCatalogEntry(m)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("%d models", len(models)))
	if providerName != "" {
		b.WriteString(fmt.Sprintf(" for provider %q", providerName))
	}
	b.WriteString("\n")
	b.WriteString(formatModelTablePlain(rows))
	return b.String(), true, nil
}

func effectiveProviderForREPL(settings rhoconfig.Settings, sessionProvider string) string {
	if provider != "" {
		return strings.TrimSpace(provider)
	}
	if settings.Provider != "" {
		return strings.TrimSpace(settings.Provider)
	}
	return strings.TrimSpace(sessionProvider)
}

func formatModelTablePlain(rows []modelTableRow) string {
	layout := computeModelTableLayout(100, rows)
	header := lipgloss.NewStyle().Bold(true)
	meta := lipgloss.NewStyle()

	var b strings.Builder
	b.WriteString(renderModelTableHeader(layout, header, meta))
	for _, row := range rows {
		b.WriteByte('\n')
		b.WriteString(renderModelTableRow(row, false, false, layout, lipgloss.NewStyle(), lipgloss.NewStyle(), lipgloss.NewStyle(), meta, meta))
	}
	return b.String()
}

// watchIgnoreDirs are directory names skipped when scanning for AI directives.
var watchIgnoreDirs = []string{".git", "node_modules", "vendor", "__pycache__", ".rho"}

// runWatch watches the working directory for AI!/AI? comment directives and
// dispatches a targeted LLM edit for each one as files change (Aider-style
// "watch files" flow). For AI! directives the agent acts immediately; for AI?
// directives it answers the embedded question. After a successful dispatch the AI
// comment token is stripped from the file.
//
// If an initialPrompt is supplied it is run once up front (preserving the prior
// behaviour of seeding the session). Detection prefers the event-driven fsnotify
// backend (io.AIWatcher.StartFsnotify); if that backend is unavailable on the
// platform it falls back to a 2s polling loop.
func runWatch(initialPrompt string) error {
	// Optional initial run to seed context, matching the prior behaviour.
	if strings.TrimSpace(initialPrompt) != "" {
		if err := runPrint(initialPrompt); err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", auditTint("Initial run failed: "+err.Error(), errorCoral))
		}
	}

	root := "."
	fmt.Fprintln(os.Stderr, "\n"+auditTint("[Watching for AI!/AI? comment directives — press Ctrl+C to stop]", textPrimary))

	// Process any directives already present before the first change event.
	if n := processAIDirectives(root, watchIgnoreDirs); n > 0 {
		fmt.Fprintf(os.Stderr, "%s\n", auditTint(fmt.Sprintf("[%s] processed %d AI directive(s)", time.Now().Format("15:04:05"), n), textPrimary))
	}

	// Prefer the fsnotify event-driven backend. The AI!/AI? directive grammar
	// differs from AIWatcher's ai: grammar, so we hook OnChange (fires once per
	// debounced filesystem-change burst) and run our own directive sweep — rather
	// than relying on OnComment, which only matches ai: comments.
	watcher := aiwatch.NewAIWatcher(root, nil)
	watcher.OnChange = func() {
		if n := processAIDirectives(root, watchIgnoreDirs); n > 0 {
			fmt.Fprintf(os.Stderr, "%s\n", auditTint(fmt.Sprintf("[%s] processed %d AI directive(s)", time.Now().Format("15:04:05"), n), textPrimary))
		}
	}

	ctx := context.Background()
	if err := watcher.StartFsnotify(ctx); err != nil {
		// fsnotify unavailable — fall back to the polling backstop.
		fmt.Fprintf(os.Stderr, "%s\n", auditTint(fmt.Sprintf("[watch] fsnotify unavailable (%v), using polling fallback", err), warnAmber))
		return runWatchPolling(root)
	}
	return nil
}

// runWatchPolling is the polling backstop used when the fsnotify backend is not
// available. It re-scans the tree every 2 seconds and processes any directives
// found in files modified since the last sweep.
func runWatchPolling(root string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	lastMod := getLastModTime(root)
	for range ticker.C {
		currentMod := getLastModTime(root)
		if currentMod.After(lastMod) {
			lastMod = currentMod
			if n := processAIDirectives(root, watchIgnoreDirs); n > 0 {
				fmt.Fprintf(os.Stderr, "%s\n", auditTint(fmt.Sprintf("[%s] processed %d AI directive(s)", time.Now().Format("15:04:05"), n), textPrimary))
			}
		}
	}
	return nil
}

func getLastModTime(root string) time.Time {
	var latest time.Time
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		return nil
	})
	return latest
}
