package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"

	commandfeature "github.com/GrayCodeAI/rho/internal/features/commands"
	parallelfeature "github.com/GrayCodeAI/rho/internal/features/parallel"
	"github.com/GrayCodeAI/rho/internal/multiagent/parallel"
	"github.com/GrayCodeAI/rho/internal/plugin"
	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

// slashCmdCache caches the slash commands list to avoid rebuilding.
var (
	slashCmdCache      []string
	slashCmdCacheBuilt = false
	slashCmdMutex      sync.Mutex
)

// slashCommands returns the static slash-command list. Runtime plugin commands
// are added by slashCommandsFor so the cache cannot become stale when plugins
// load or reload during a session.
func slashCommands() []string {
	return slashCommandsFor(nil)
}

func slashCommandsFor(runtime *plugin.Runtime) []string {
	slashCmdMutex.Lock()
	if !slashCmdCacheBuilt {
		builtIns := commandfeature.BuiltInNames()
		seen := make(map[string]bool, len(builtIns)+subcommandRegistry.Size())
		out := make([]string, 0, len(builtIns)+subcommandRegistry.Size())
		add := func(name string) {
			name = strings.TrimSpace(name)
			if name == "" {
				return
			}
			if !strings.HasPrefix(name, "/") {
				name = "/" + name
			}
			if seen[name] {
				return
			}
			seen[name] = true
			out = append(out, name)
		}
		for _, name := range builtIns {
			add(name)
		}
		for alias := range commandfeature.BuiltInAliases() {
			add(alias)
		}
		for _, cmd := range subcommandRegistry.All() {
			add(cmd.Name())
			for _, alias := range cmd.Aliases() {
				add(alias)
			}
		}
		sort.Strings(out)
		slashCmdCache = out
		slashCmdCacheBuilt = true
	}
	static := append([]string(nil), slashCmdCache...)
	slashCmdMutex.Unlock()

	if runtime == nil {
		return static
	}
	seen := make(map[string]struct{}, len(static))
	for _, name := range static {
		seen[name] = struct{}{}
	}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		if _, exists := seen[name]; exists {
			return
		}
		seen[name] = struct{}{}
		static = append(static, name)
	}
	for _, cmd := range runtime.CommandList() {
		add(cmd.Name)
	}
	sort.Strings(static)
	return static
}

func slashDescriptionsFor(runtime *plugin.Runtime) map[string]string {
	descriptions := make(map[string]string, len(slashDescriptions))
	for name, description := range slashDescriptions {
		descriptions[name] = description
	}
	if runtime != nil {
		for _, cmd := range runtime.CommandList() {
			name := strings.TrimSpace(cmd.Name)
			if name == "" {
				continue
			}
			if !strings.HasPrefix(name, "/") {
				name = "/" + name
			}
			descriptions[name] = cmd.Description
		}
	}
	return descriptions
}

func (m *chatModel) slashSuggestionsFor(input string) []string {
	if input == m.slashSugInput && m.slashSugGen == m.slashSugCachedGen {
		return m.slashSugCache
	}
	m.slashSugInput = input
	m.slashSugCachedGen = m.slashSugGen
	m.slashSugCache = slashSuggestionsFor(input, m.pluginRuntime)
	return m.slashSugCache
}

// invalidateSlashSugCache bumps the generation counter so the next
// slashSuggestionsFor call recomputes suggestions. Call this when the
// command set may have changed (e.g. new messages, plugin reload).
func (m *chatModel) invalidateSlashSugCache() {
	m.slashSugGen++
}

// slashMenuOpen is true while the / command picker is visible (Cursor hides the footer then).
func (m *chatModel) slashMenuOpen() bool {
	return len(m.slashSuggestionsFor(m.input.Value())) > 0
}

func (m *chatModel) visibleSlashSuggestionLines() int {
	n := len(m.slashSuggestionsFor(m.input.Value()))
	if n > 6 {
		return 6
	}
	return n
}

func (m chatModel) inputAreaLayoutKey() int {
	if m.configOpen {
		return 0
	}
	lines := strings.Count(m.input.Value(), "\n") + 1
	if lines > 10 {
		lines = 10
	}
	key := lines<<16 | m.visibleSlashSuggestionLines()
	if m.manualCompacting {
		key |= 1 << 15
	}
	if m.inScrollbackFocus() {
		key |= 1 << 14
	}
	if m.ghostText != nil {
		if ghost := m.ghostText.Get(); ghost != "" && m.input.Value() == "" {
			key |= 1 << 13
		}
	}
	return key
}

func (m *chatModel) invalidateInputLayoutCache() {
	m.layoutKey = -1
	m.cachedBottomBarLines = 0
}

func (m *chatModel) refreshInputLayoutIfNeeded() bool {
	if m.configOpen {
		return false
	}
	key := m.inputAreaLayoutKey()
	if key == m.layoutKey && m.cachedBottomBarLines > 0 {
		return false
	}
	m.layoutKey = key
	m.cachedBottomBarLines = m.computeChatBottomBarLines()
	return true
}

func (m *chatModel) syncInputLayout() bool {
	return m.refreshInputLayoutIfNeeded()
}

func slashAliases() map[string]string {
	return commandfeature.BuiltInAliases()
}

var slashDescriptions = commandfeature.BuiltInDescriptions()

func slashSuggestions(input string) []string {
	return slashSuggestionsFor(input, nil)
}

func slashSuggestionsFor(input string, runtime *plugin.Runtime) []string {
	return commandfeature.Suggestions(input, slashCommandsFor(runtime), slashDescriptionsFor(runtime), slashAliases())
}

func applySlashSuggestion(input string) string {
	return commandfeature.ApplySuggestion(input, slashAliases())
}

func (m *chatModel) handleCommand(text string) (tea.Model, tea.Cmd) {
	parsed := commandfeature.Parse(text)
	resolved := commandfeature.Resolve(parsed, slashAliases())
	text = resolved.Parsed.Text
	parts := resolved.Parsed.Parts
	if len(parts) == 0 {
		return m, nil
	}
	cmd := resolved.Command

	// Track the last command for context-aware tips and recent-command history.
	if strings.HasPrefix(cmd, "/") {
		m.lastCommand = cmd
		recordCommandUsed(cmd)
	}

	// Namespaced skill invocation: /vendor:skill-name [args...]
	if resolved.Namespaced {
		return m.handleNamespacedSkill(cmd, text)
	}

	// SubcommandRegistry dispatch. Migrations live in
	// chat_subcommand_<name>.go files. Each registers itself in
	// init(); we look up by the slash name minus the leading "/".
	// If the registry has a handler, dispatch and return.
	if resolved.IsSlash {
		if sub, ok := subcommandRegistry.Lookup(resolved.Name); ok {
			args := parts[1:]
			return sub.Handle(m, args, text)
		}
	}

	// Fallback: plugin commands and unknown-command error.
	if strings.HasPrefix(cmd, "/") && m.pluginRuntime != nil && m.pluginRuntime.IsCommand(cmd[1:]) {
		out, err := m.pluginRuntime.ExecuteCommand(cmd[1:], parts[1:])
		if err != nil {
			m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
		} else {
			m.messages = append(m.messages, displayMsg{role: "system", content: out})
		}
		return m, nil
	}
	// "Did you mean?" — fuzzy-match against known slash commands so a typo
	// like /commmit suggests /commit instead of just saying "unknown".
	suggestion := suggestCommandFor(cmd, m.pluginRuntime)
	if suggestion != "" {
		m.messages = append(m.messages, displayMsg{role: "error", content: fmt.Sprintf("Unknown command: %s — did you mean %s?\nType /help for all commands.", cmd, suggestion)})
	} else {
		m.messages = append(m.messages, displayMsg{role: "error", content: fmt.Sprintf("Unknown command: %s (type /help)", cmd)})
	}
	return m, nil
}

func suggestCommandFor(typo string, runtime *plugin.Runtime) string {
	return commandfeature.SuggestTypo(typo, slashCommandsFor(runtime))
}

// handleParallelCommand spawns multiple agents in parallel on independent tasks.
// Usage: /parallel <N> <task1> | <task2> | ...
func (m *chatModel) handleParallelCommand(parts []string, text string) (tea.Model, tea.Cmd) {
	request, err := parallelfeature.ParseRequest(parts)
	if err != nil {
		role := "error"
		var parseErr *parallelfeature.ParseError
		if errors.As(err, &parseErr) && parseErr.Usage {
			role = "system"
		}
		m.messages = append(m.messages, displayMsg{role: role, content: err.Error()})
		return m, nil
	}
	workers := request.Workers
	taskDescs := request.Tasks

	// Get repo root for worktree pool
	cwd, _ := os.Getwd()

	// Create grid UI
	grid := NewAgentGrid(taskDescs, m.width, m.height-10)
	m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("%s Spawning %d parallel agents for %d tasks...", icons.Bolt(), workers, len(taskDescs))})

	// Create a cancellable context for the parallel agents.
	// This ensures agents are cancelled when the user quits.
	// Cancel any prior parallel run first — only one runs at a time
	// (concurrent runs would also clobber each other's grid display),
	// and this prevents orphaning the previous run's agents on quit.
	if m.parallelCancel != nil {
		m.parallelCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.parallelCancel = cancel

	// Run parallel agents in background
	go func() {
		defer cancel() // Ensure cleanup when goroutine exits

		pool := parallel.NewPool(cwd, "main", workers)
		for _, desc := range taskDescs {
			pool.AddTask(desc)
		}

		// Use atomic counter for task index to avoid race condition.
		var taskIdx int32

		// Update grid as agents run
		err := pool.Run(ctx, func(ctx context.Context, worktreePath string, task *parallel.Task) (string, error) {
			idx := int(atomic.AddInt32(&taskIdx, 1) - 1)

			pane := grid.GetPane(fmt.Sprintf("%d", idx+1))
			if pane != nil {
				pane.SetState(AgentRunning)
				pane.Append(fmt.Sprintf("Starting in worktree: %s", worktreePath))
				m.ref.Send(streamChunkMsg(grid.Render()))
			}

			// Clone the engine-backed transport so parallel agents cannot bypass
			// the parent session's resolved gateway policy.
			agentSession := m.session.SubSession(
				m.session.Model(),
				"You are a coding agent working in an isolated git worktree. Complete the assigned task.",
				m.registry,
			)
			agentSession.AddUser(fmt.Sprintf("Working in isolated worktree: %s\nTask: %s", worktreePath, task.Description))

			// Stream the agent's work
			ch, err := agentSession.Stream(ctx)
			if err != nil {
				if pane != nil {
					pane.SetState(AgentFailed)
					pane.Append(fmt.Sprintf("Error: %v", err))
				}
				return "", err
			}

			var result strings.Builder
			for ev := range ch {
				select {
				case <-ctx.Done():
					return result.String(), ctx.Err()
				default:
				}
				switch ev.Type {
				case "content":
					result.WriteString(ev.Content)
					if pane != nil {
						pane.Append(ev.Content)
					}
				case "done":
					if pane != nil {
						pane.SetState(AgentDone)
						pane.Append("Task completed")
					}
					return result.String(), nil
				case "error":
					if pane != nil {
						pane.SetState(AgentFailed)
						pane.Append(fmt.Sprintf("Error: %s", ev.Content))
					}
					return result.String(), fmt.Errorf("%s", ev.Content)
				}
			}
			return result.String(), nil
		})

		// Send final grid state
		m.ref.Send(streamChunkMsg(grid.Render()))

		if err != nil {
			m.ref.Send(streamErrMsg{err: err})
		} else {
			m.ref.Send(streamDoneMsg{})
		}
	}()

	return m, nil
}

// handleRefactorCommand runs agent-driven refactoring on the codebase.
func (m *chatModel) handleRefactorCommand(parts []string, text string) (tea.Model, tea.Cmd) {
	// Default refactoring scope
	scope := "."
	if len(parts) > 1 {
		scope = parts[1]
	}

	prompt := fmt.Sprintf(`You are in REFACTOR mode. Perform agent-driven refactoring on the codebase.

## Scope
%s

## Tasks (execute in order)
1. **Dead code removal**: Find and remove unused functions, variables, imports
2. **Deduplication**: Identify and consolidate duplicate code patterns
3. **Lint fixes**: Run linter and fix all issues
4. **Import cleanup**: Remove unused imports, organize import groups
5. **Test coverage**: Add tests for untested critical paths

## Rules
- Make minimal, safe changes
- Run tests after each change to verify no regressions
- If a change is risky, skip it and note why
- Commit each category of change separately

## Output
After completing, provide a summary of:
- Files modified
- Lines removed/added
- Issues fixed
- Any skipped changes and why`, scope)

	return m.startPromptCommand("/refactor", prompt)
}
