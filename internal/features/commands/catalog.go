package commands

import (
	"fmt"
	"sort"
	"strings"
)

// BuiltInNames returns the command names that are always available in rho.
// Dynamic plugin and subcommand names are supplied by the composition root.
func BuiltInNames() []string {
	descriptions := BuiltInDescriptions()
	aliases := BuiltInAliases()
	names := make([]string, 0, len(descriptions))
	for name := range descriptions {
		if _, isAlias := aliases[name]; !isAlias {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// BuiltInAliases returns aliases for built-in commands.
func BuiltInAliases() map[string]string {
	return map[string]string{"/themes": "/theme"}
}

// BuiltInDescriptions returns the help text for built-in commands.
func BuiltInDescriptions() map[string]string {
	return map[string]string{
		"/add": "Add files to conversation context", "/add-dir": "Add a directory to context", "/agents": "List active agents", "/agents-init": "Generate AGENTS.md from project template", "/audit": "Show tool audit summary", "/permission": "Manage permissions and folder trust", "/autonomy": "Choose how independently the agent acts", "/branch": "Show git branch info", "/btw": "Side note without triggering a response", "/bughunter": "Hunt for bugs in the codebase", "/check": "Review diff, find issues, auto-fix safe ones, verify before ship", "/design": "Build or improve UI — use /design screenshot|system|component|regress for advanced modes",
		"/hunt": "Diagnose root cause of errors before fixing (Waza method)", "/think": "Turn rough idea into approved plan before coding (Waza method)", "/clean": "Delete old sessions", "/clear": "Clear conversation", "/color": "Change agent color", "/commit": "Auto-commit changes with AI message", "/compact": "Compress conversation to save tokens", "/compress": "Compress old sessions", "/config": "Open settings panel", "/context": "Show current context", "/copy": "Copy chat or input to clipboard (/copy all|input|last|assistant)", "/cost": "Show token usage and cost", "/council": "Run LLM Council (multi-model consensus)", "/diff": "Show git diff (preview changes)", "/doctor": "Run diagnostics (build, test, lint)", "/drop": "Remove file from context", "/effort": "Set reasoning effort level", "/env": "Show environment info", "/exit": "Save and exit", "/explain": "Swift code back to the commit that created it", "/export": "Export session", "/follow": "Toggle stream follow (auto-scroll)", "/home": "Jump to top of chat and welcome header", "/feedback": "Submit feedback about rho", "/fast": "Toggle fast mode", "/files": "Show modified files", "/focus": "Narrow agent attention to specific files/dirs", "/fork": "Fork conversation to try a different approach", "/branches": "List or switch conversation branches",
		"/help": "Show all commands", "/history": "List saved sessions", "/hooks": "Show configured hooks", "/init": "Analyze project structure", "/integrity": "Validate session integrity", "/lint": "Run linter, add issues to context", "/login": "Authenticate a provider (opens the config panel)", "/loop": "Schedule recurring command", "/mcp": "Show MCP server status", "/memory": "Show AGENTS.md project instructions", "/metrics": "Show session metrics", "/model": "Browse/switch models; press t to toggle Think", "/new": "Start a fresh session", "/pin": "Pin last N messages to protect from compaction", "/parallel": "Run N agents in parallel on independent tasks", "/plugins": "List installed plugins", "/power": "Set power level (1-10)", "/quit": "Save and exit", "/recover": "Scan for interrupted sessions and resume", "/refactor": "Agent-driven refactoring: dedup, dead code, lint fixes", "/resume": "Resume a saved session", "/retry": "Redo last message", "/review": "Code review for bugs and issues", "/rewind": "Undo last exchange", "/run": "Run command, add output to context", "/search": "Search across sessions", "/select": "Pause TUI for native text selection", "/mouse": "Toggle TUI mouse capture for native click-drag copy", "/snapshot": "Manage file snapshots: list, restore <hash>, diff <hash>", "/stale": "Show stale rules that may need updating or removal", "/security-review": "Security audit",
		"/skills": "List skills or manage: search, install, trending, info, remove, update, feedback, publish, audit", "/learn": "LLM-powered skill advisor (/learn deep for source analysis)", "/stats": "Show analytics stats", "/status": "Show session info (mode, trust, cost)", "/start": "Guided setup: trust, mode, branch, first tasks", "/trust": "Folder trust status / add / remove", "/branch-agent": "Create rho/agent-* branch if on main/master", "/auto-commit": "Toggle git auto-commit after Write/Edit (on|off)", "/summary": "Summarize the session", "/tasks": "Show task list", "/test": "Run tests, add failures to context", "/tokens": "Show token estimate", "/tools": "List enabled tools", "/undo": "Undo the most recent file change", "/usage": "Show cost summary", "/version": "Show rho version", "/vim": "Toggle vim mode", "/welcome": "Re-print the welcome header", "/ecosystem": "Show flux and token-engine integration status", "/path": "Developer path readiness (setup, security)", "/cron": "Show scheduled jobs", "/keybindings": "Show keyboard shortcuts", "/output-style": "Change output style", "/plugin": "Manage plugins", "/pr-comments": "Address PR comments", "/provider-status": "Show provider info", "/release-notes": "Draft release notes", "/reload-plugins": "Reload all plugins", "/remote-env": "Show remote environment", "/rename": "Rename current session", "/render": "Export repo as CXML to clipboard", "/research": "Start autonomous research loop", "/session": "Show session info", "/share": "Share session", "/statusline": "Show status line info", "/tag": "Tag current session", "/taste": "Show learned taste preferences", "/theme": "Change visual theme (opens picker)", "/themes": "List all available themes", "/reflect": "Reflect on the current plan or result", "/think-back": "Review reasoning decisions", "/thinkback": "Review reasoning decisions", "/thinkback-play": "Replay reasoning path", "/upgrade": "Check for updates", "/vibe": "Start vibe coding loop", "/voice": "Toggle voice input", "/ctx": "Show conversation context visualization", "/insights": "Generate session patterns and improvements report", "/spec": "Start the spec-driven workflow (gates Write/Edit/Bash until approved)", "/ultrareview": "Deep adversarial code review", "/scroll-speed": "Set scroll speed (1-100)", "/scroll-invert": "Toggle scroll direction inversion", "/scroll-mode": "Switch scroll behavior mode", "/terminal-setup": "Configure terminal capabilities", "/pager-config": "Configure pager for long output", "/prompt-queue": "Manage queued prompts", "/brainstorm": "Brainstorm ideas with multi-model council", "/checkpoint": "Create a named checkpoint of current state", "/away": "Set away status with auto-reply message", "/investigate": "Deep-dive investigation of an issue", "/refresh-model-catalog": "Refresh the model catalog from providers", "/image": "Generate or process images", "/recipe": "Run a saved recipe (command template)", "/soul": "Show or update rho's personality/soul", "/mode": "Switch interaction mode", "/party": "Start a multi-agent party session",
	}
}

// BuiltInCategories returns the presentation category for built-in commands.
// Unknown commands intentionally fall back to Other so plugin commands remain
// usable without requiring a change to rho's catalog.
func BuiltInCategories() map[string]string {
	return map[string]string{
		"/help": "Core", "/model": "Core", "/config": "Core", "/quit": "Core", "/exit": "Core", "/clear": "Core", "/compact": "Core", "/undo": "Core", "/snapshot": "Core", "/recover": "Core", "/new": "Core", "/copy": "Core", "/welcome": "Core",
		"/review": "Workflow", "/commit": "Workflow", "/test": "Workflow", "/lint": "Workflow", "/diff": "Workflow", "/status": "Workflow", "/audit": "Workflow", "/security-review": "Workflow", "/check": "Workflow", "/bughunter": "Workflow", "/hunt": "Workflow", "/ultrareview": "Workflow", "/start": "Workflow", "/branch-agent": "Workflow", "/auto-commit": "Workflow",
		"/agents": "Agent", "/agents-init": "Agent", "/mission": "Agent", "/exec": "Agent", "/research": "Agent", "/loop": "Agent", "/council": "Agent", "/investigate": "Agent", "/vibe": "Agent",
		"/memory": "Memory", "/context": "Memory", "/ctx": "Memory", "/search": "Memory", "/history": "Memory", "/session": "Memory", "/sessions": "Memory", "/export": "Memory", "/share": "Memory", "/fork": "Memory", "/branches": "Memory", "/branch": "Memory",
		"/tools": "Tools", "/mcp": "Tools", "/plugin": "Tools", "/plugins": "Tools", "/skills": "Tools", "/files": "Tools", "/image": "Tools", "/render": "Tools", "/ecosystem": "Tools", "/path": "Tools",
		"/doctor": "Diagnostics", "/cost": "Diagnostics", "/usage": "Diagnostics", "/metrics": "Diagnostics", "/stats": "Diagnostics", "/integrity": "Diagnostics", "/stale": "Diagnostics", "/tokens": "Diagnostics", "/provider-status": "Diagnostics",
		"/permission": "Settings", "/autonomy": "Settings", "/spec": "Settings", "/vim": "Settings", "/theme": "Settings", "/color": "Settings", "/mouse": "Settings", "/select": "Settings", "/focus": "Settings", "/follow": "Settings", "/output-style": "Settings", "/statusline": "Settings", "/keybindings": "Settings", "/voice": "Settings", "/remote-env": "Settings", "/refresh-model-catalog": "Settings", "/mode": "Settings", "/trust": "Settings",
	}
}

// Category returns the stable display category for a command name.
func Category(name string) string {
	if target, isAlias := BuiltInAliases()[name]; isAlias {
		name = target
	}
	if category := BuiltInCategories()[name]; category != "" {
		return category
	}
	return "Other"
}

// ValidateBuiltIns checks the command metadata that powers completion and
// help. Keeping this invariant in the feature package prevents the CLI from
// advertising commands without descriptions or aliases pointing at commands
// that do not exist.
func ValidateBuiltIns() error {
	names := BuiltInNames()
	descriptions := BuiltInDescriptions()
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("built-in command has an empty name")
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("built-in command %q is registered twice", name)
		}
		seen[name] = struct{}{}
	}

	for name := range seen {
		if strings.TrimSpace(descriptions[name]) == "" {
			return fmt.Errorf("built-in command %q has no description", name)
		}
	}
	for name := range descriptions {
		if _, exists := seen[name]; !exists {
			if _, isAlias := BuiltInAliases()[name]; isAlias {
				continue
			}
			return fmt.Errorf("description exists for unadvertised built-in command %q", name)
		}
	}
	for alias, target := range BuiltInAliases() {
		if _, exists := seen[alias]; exists {
			return fmt.Errorf("alias %q collides with a built-in command", alias)
		}
		if _, exists := seen[target]; !exists {
			return fmt.Errorf("alias %q points to unknown command %q", alias, target)
		}
	}
	return nil
}
