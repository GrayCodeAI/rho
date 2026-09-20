package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	analytics "github.com/GrayCodeAI/rho/internal/observability"
	"github.com/GrayCodeAI/rho/internal/plugin"
	"github.com/GrayCodeAI/rho/internal/storage"
	"github.com/GrayCodeAI/rho/internal/theme"
	"github.com/GrayCodeAI/rho/internal/tool"
	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

// init() registers a large batch of simple /slash commands via
// the SubcommandRegistry. Each command's body is taken directly
// from the original case block in chat_commands.go; this file
// is the canonical location for those simple commands.

func init() {
	// /copy — copy chat or input
	subcommandRegistry.Register(&delegatingCommand{
		name:        "copy",
		description: "copy chat or input (all|input|last|assistant)",
		usage:       "/copy <all|input|last|assistant>",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			// handleCopyCommand expects parts[0] to be the command
			// name. The registry dispatcher strips it, so re-add it.
			return m.handleCopyCommand(append([]string{"/copy"}, args...))
		},
	})

	// /select — pause TUI for native text selection
	subcommandRegistry.Register(&delegatingCommand{
		name:        "select",
		description: "pause TUI for native text selection",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			return m, enterSelectionMode(m.ref, m.copyableTranscript(), m.mouseEnabled())
		},
	})

	// /mouse — toggle mouse capture
	subcommandRegistry.Register(&delegatingCommand{
		name:        "mouse",
		description: "toggle mouse capture (off = click-drag copy)",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			// handleMouseCommand expects parts[0] to be the command
			// name. The registry dispatcher strips it, so re-add it.
			m.handleMouseCommand(append([]string{"/mouse"}, args...))
			return m, nil
		},
	})

	// /undo — undo last exchange (file edits via tool.UndoLatest)
	subcommandRegistry.Register(&delegatingCommand{
		name:        "undo",
		description: "undo the last exchange (file edits)",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			restored, err := tool.UndoLatest()
			if err != nil {
				m.messages = append(m.messages, displayMsg{role: "system", content: "No file changes to undo"})
			} else {
				m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Undid %s", restored)})
			}
			return m, nil
		},
	})

	// /theme <t> — set theme (dark|light|auto)
	subcommandRegistry.Register(&delegatingCommand{
		name:        "theme",
		description: "set theme (dark|light|auto or any registered palette)",
		usage:       "/theme [name]",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			if len(args) < 1 {
				if m.themePicker == nil {
					m.themePicker = NewThemePicker()
				}
				// Pre-select the current saved theme.
				current := rhoconfig.LoadGlobalSettings().Theme
				m.themePicker.OpenWithCurrent(current)
				m.viewDirty = true
				m.updateViewportContent()
				return m, nil
			}
			// Inline: /theme <name>
			themeName := args[0]
			if err := rhoconfig.SetGlobalSetting("theme", themeName); err != nil {
				m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
			} else {
				// Apply immediately — full palette swap, no restart needed.
				ApplyTheme(themeName)
				m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("%s Theme set to: %s", icons.CheckBold(), themeName)})
			}
			return m, nil
		},
	})

	// /color <hex> — set agent color
	subcommandRegistry.Register(&delegatingCommand{
		name:        "color",
		description: "set agent color (hex value)",
		usage:       "/color <hex-color>",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			if len(args) < 1 {
				m.messages = append(m.messages, displayMsg{role: "system", content: "Usage: /color <hex-color>"})
				return m, nil
			}
			if err := rhoconfig.SetGlobalSetting("agentColor", args[0]); err != nil {
				m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
			} else {
				m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Agent color set to: %s", args[0])})
			}
			return m, nil
		},
	})

	// /fast — toggle fast mode
	subcommandRegistry.Register(&delegatingCommand{
		name:        "fast",
		description: "toggle fast mode (cheapest model for this provider)",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			savedModel := rhoconfig.ActiveModel(context.Background())
			if m.session.Model() == savedModel {
				providerName := strings.TrimSpace(m.session.Provider())
				fastModel := rhoconfig.CheapestModelForProvider(providerName, m.session.Model())
				if strings.TrimSpace(fastModel) == "" {
					fastModel = rhoconfig.DefaultModelForProvider(providerName)
				}
				if strings.TrimSpace(fastModel) == "" {
					m.messages = append(m.messages, displayMsg{role: "error", content: "Fast mode: no catalog model resolved for this provider"})
					return m, nil
				}
				m.session.SetModel(fastModel)
				m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Fast mode on → %s", fastModel)})
			} else {
				m.session.SetModel(savedModel)
				m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Fast mode off → %s", savedModel)})
			}
			return m, nil
		},
	})

	// /effort <level> — set reasoning effort
	subcommandRegistry.Register(&delegatingCommand{
		name:        "effort",
		description: "set reasoning effort (low|medium|high)",
		usage:       "/effort <low|medium|high>",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			if len(args) < 1 {
				m.messages = append(m.messages, displayMsg{role: "error", content: "Usage: /effort <low|medium|high>"})
				return m, nil
			}
			level := strings.ToLower(args[0])
			switch level {
			case "low", "medium", "high":
				_ = rhoconfig.SetGlobalSetting("reasoningEffort", level)
				m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Reasoning effort → %s", level)})
			default:
				m.messages = append(m.messages, displayMsg{role: "error", content: "Valid levels: low, medium, high"})
			}
			return m, nil
		},
	})

	// /agents — list active agents/teammates
	subcommandRegistry.Register(&delegatingCommand{
		name:        "agents",
		description: "list active agents/teammates",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			return m.startPromptCommand("/agents", "List all active agents and teammates in the current session. Show their status and assigned tasks.")
		},
	})

	// /parallel — run commands in parallel
	subcommandRegistry.Register(&delegatingCommand{
		name:        "parallel",
		description: "run commands in parallel (delegates to handleParallelCommand)",
		usage:       "/parallel [args...]",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			return m.handleParallelCommand(append([]string{"/parallel"}, args...), text)
		},
	})

	// /skills — list, search, install, remove skills
	subcommandRegistry.Register(&delegatingCommand{
		name:        "skills",
		description: "list, search, install, remove skills",
		usage:       "/skills [subcommand]",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			return m.handleSkillsCommand(append([]string{"/skills"}, args...), text)
		},
	})

	// /tasks — show task list
	subcommandRegistry.Register(&delegatingCommand{
		name:        "tasks",
		description: "show the current task list",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			tasks := tool.GetTaskStore().List()
			if len(tasks) == 0 {
				m.messages = append(m.messages, displayMsg{role: "system", content: "No tasks."})
				return m, nil
			}
			var b strings.Builder
			for _, t := range tasks {
				status := string(t.Status)
				icon := "○"
				if t.Status == tool.TaskStatusCompleted {
					icon = "●"
				} else if t.Status == tool.TaskStatusInProgress {
					icon = "◐"
				}
				b.WriteString(fmt.Sprintf("  %s %s [%s] %s\n", icon, t.ID, status, t.Subject))
			}
			m.messages = append(m.messages, displayMsg{role: "system", content: b.String()})
			return m, nil
		},
	})

	// /vibe — start an iterative build workflow while retaining the active
	// permission policy. This is a prompt preset, not an execution bypass.
	subcommandRegistry.Register(&delegatingCommand{
		name:        "vibe",
		description: "start an iterative build workflow (permission policy applies)",
		usage:       "/vibe [additional prompt]",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			prompt := "Start an iterative build workflow under the active permission policy. Read the project structure, make the requested changes, run relevant tests after each change, and iterate until tests pass."
			if len(args) > 0 {
				prompt = strings.TrimSpace(strings.TrimPrefix(text, "/vibe"))
			}
			return m.startPromptCommand("/vibe", prompt)
		},
	})

	// /learn — LLM-powered skill advisor
	subcommandRegistry.Register(&delegatingCommand{
		name:        "learn",
		description: "LLM-powered skill advisor (deep, update)",
		usage:       "/learn [deep|update]",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			cwd, _ := os.Getwd()
			deep := len(args) >= 1 && args[0] == "deep"
			update := len(args) >= 1 && args[0] == "update"
			ctx := plugin.GatherLearnContext(cwd)
			if deep || update {
				ctx.SourceInfo = plugin.GatherDeepSourceInfo(cwd)
			}
			if update {
				summary := plugin.FormatLearnSummary(ctx, true)
				prompt := plugin.BuildLearnUpdatePrompt(ctx)
				return m.startPromptCommand(summary, prompt)
			}
			summary := plugin.FormatLearnSummary(ctx, deep)
			prompt := plugin.BuildLearnPrompt(ctx)
			return m.startPromptCommand(summary, prompt)
		},
	})

	// /cron — list scheduled cron jobs
	subcommandRegistry.Register(&delegatingCommand{
		name:        "cron",
		description: "list scheduled cron jobs",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			jobs := tool.GetCronScheduler().List()
			if len(jobs) == 0 {
				m.messages = append(m.messages, displayMsg{role: "system", content: "No scheduled jobs."})
				return m, nil
			}
			var b strings.Builder
			for _, j := range jobs {
				jtype := "recurring"
				if !j.Recurring {
					jtype = "one-shot"
				}
				b.WriteString(fmt.Sprintf("  %s [%s] %s next: %s\n", j.ID, jtype, j.Schedule, j.NextRun.Format("Jan 02 15:04")))
			}
			m.messages = append(m.messages, displayMsg{role: "system", content: b.String()})
			return m, nil
		},
	})

	// /vim — toggle vim mode
	subcommandRegistry.Register(&delegatingCommand{
		name:        "vim",
		description: "toggle vim mode",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			if m.vim == nil {
				m.vim = NewVimState()
			}
			m.vim.SetEnabled(!m.vim.IsEnabled())
			state := "disabled"
			if m.vim.IsEnabled() {
				state = "enabled (press Esc for NORMAL mode)"
			}
			m.messages = append(m.messages, displayMsg{role: "system", content: "Vim mode " + state})
			return m, nil
		},
	})

	// /compact-mode — toggle compact mode
	subcommandRegistry.Register(&delegatingCommand{
		name:        "compact-mode",
		aliases:     nil, // no aliases: "compact" is taken by /compact (session compaction)
		description: "toggle compact mode (removes outer padding)",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			settings := rhoconfig.LoadGlobalSettings()
			current := settings.CompactMode
			newVal := !current
			valStr := "false"
			if newVal {
				valStr = "true"
			}
			if err := rhoconfig.SetGlobalSetting("compact_mode", valStr); err != nil {
				m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
			} else {
				state := "disabled"
				if newVal {
					state = "enabled"
				}
				m.messages = append(m.messages, displayMsg{role: "system", content: "Compact mode " + state})
				m.viewDirty = true
				m.updateViewportContent()
			}
			return m, nil
		},
	})

	// /scroll-speed <n> — set scroll speed (1-100)
	subcommandRegistry.Register(&delegatingCommand{
		name:        "scroll-speed",
		description: "set scroll speed (1-100)",
		usage:       "/scroll-speed <1-100>",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			if len(args) < 1 {
				m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Usage: /scroll-speed <1-100>\nCurrent: %d", rhoconfig.LoadGlobalSettings().ScrollSpeed)})
				return m, nil
			}
			speed, err := strconv.Atoi(args[0])
			if err != nil || speed < 1 || speed > 100 {
				m.messages = append(m.messages, displayMsg{role: "error", content: "Scroll speed must be 1-100"})
				return m, nil
			}
			if err := rhoconfig.SetGlobalSetting("scroll_speed", args[0]); err != nil {
				m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
			} else {
				m.messages = append(m.messages, displayMsg{role: "system", content: "Scroll speed → " + args[0]})
			}
			return m, nil
		},
	})

	// /scroll-invert — toggle scroll direction invert
	subcommandRegistry.Register(&delegatingCommand{
		name:        "scroll-invert",
		description: "toggle natural scrolling (invert direction)",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			settings := rhoconfig.LoadGlobalSettings()
			current := settings.InvertScroll
			newVal := !current
			valStr := "false"
			if newVal {
				valStr = "true"
			}
			enabled := "disabled"
			if newVal {
				enabled = "enabled"
			}
			if err := rhoconfig.SetGlobalSetting("invert_scroll", valStr); err != nil {
				m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
			} else {
				m.messages = append(m.messages, displayMsg{role: "system", content: "Natural scrolling " + enabled})
			}
			return m, nil
		},
	})

	// /scroll-mode — set scroll mode (auto, wheel, trackpad)
	subcommandRegistry.Register(&delegatingCommand{
		name:        "scroll-mode",
		description: "set scroll mode (auto, wheel, trackpad)",
		usage:       "/scroll-mode <auto|wheel|trackpad>",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			if len(args) < 1 {
				m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Usage: /scroll-mode <auto|wheel|trackpad>\nCurrent: %s", rhoconfig.LoadGlobalSettings().ScrollMode)})
				return m, nil
			}
			mode := strings.ToLower(args[0])
			switch mode {
			case "auto", "wheel", "trackpad":
				if err := rhoconfig.SetGlobalSetting("scrollmode", mode); err != nil {
					m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
				} else {
					m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Scroll mode → %s", mode)})
				}
				return m, nil
			default:
				m.messages = append(m.messages, displayMsg{role: "error", content: "Valid modes: auto, wheel, trackpad"})
				return m, nil
			}
		},
	})

	// /hooks — show configured hooks
	subcommandRegistry.Register(&delegatingCommand{
		name:        "hooks",
		description: "show configured hooks",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			m.messages = append(m.messages, displayMsg{role: "system", content: hooksSummary()})
			return m, nil
		},
	})

	// /terminal-setup — show terminal configuration recommendations
	subcommandRegistry.Register(&delegatingCommand{
		name:        "terminal-setup",
		description: "show terminal configuration recommendations",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			caps := theme.DetectTerminalCapabilities()
			level := "basic (16 colors)"
			switch caps.ColorLevel {
			case theme.ColorTruecolor:
				level = "truecolor (24-bit RGB)"
			case theme.Color256:
				level = "256-color"
			}

			m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Terminal Setup Recommendations:\n\nColor Support: %s\nScroll Mode: %s (use /scroll-mode to change)\nScroll Speed: %d (use /scroll-speed to change)\nCompact Mode: %v (use /compact-mode to toggle)\n\nTips:\n- Set COLORTERM=truecolor for best color experience\n- Use tmux with set -g default-terminal \"tmux-256color\" for 256-color support\n- Enable mouse reporting in your terminal for full TUI interaction", level, rhoconfig.LoadGlobalSettings().ScrollMode, rhoconfig.LoadGlobalSettings().ScrollSpeed, rhoconfig.LoadGlobalSettings().CompactMode)})
			return m, nil
		},
	})

	// /pager-config — configure scrollback pager
	subcommandRegistry.Register(&delegatingCommand{
		name:        "pager-config",
		description: "configure scrollback pager (lines|linenumbers)",
		usage:       "/pager-config <lines|linenumbers> <value>",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			if len(args) < 2 {
				s := rhoconfig.LoadGlobalSettings()
				ln := false
				messages := fmt.Sprintf("Pager Configuration:\n  lines: %d (0 = unlimited)\n  linenumbers: %v\n\nUsage: /pager-config <lines|linenumbers> <value>", s.PaginatorLines, ln)
				if s.PaginatorShowLineNums != nil {
					messages = fmt.Sprintf("Pager Configuration:\n  lines: %d (0 = unlimited)\n  linenumbers: %v\n\nUsage: /pager-config <lines|linenumbers> <value>", s.PaginatorLines, *s.PaginatorShowLineNums)
				}
				m.messages = append(m.messages, displayMsg{role: "system", content: messages})
				return m, nil
			}
			subcmd := strings.ToLower(args[0])
			value := args[1]
			switch subcmd {
			case "lines":
				lines, err := strconv.Atoi(value)
				if err != nil || lines < 0 {
					m.messages = append(m.messages, displayMsg{role: "error", content: "Lines must be a positive number (0 = unlimited)"})
					return m, nil
				}
				if err := rhoconfig.SetGlobalSetting("paginatorlines", value); err != nil {
					m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
				} else {
					m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Pager lines → %d", lines)})
				}
				return m, nil
			case "linenumbers", "linenums", "ln":
				switch strings.ToLower(value) {
				case "1", "true", "yes", "on":
					if err := rhoconfig.SetGlobalSetting("paginatorshowlinenumbers", "true"); err != nil {
						m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
					} else {
						m.messages = append(m.messages, displayMsg{role: "system", content: "Pager line numbers → enabled"})
					}
				case "0", "false", "no", "off":
					if err := rhoconfig.SetGlobalSetting("paginatorshowlinenumbers", "false"); err != nil {
						m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
					} else {
						m.messages = append(m.messages, displayMsg{role: "system", content: "Pager line numbers → disabled"})
					}
				default:
					m.messages = append(m.messages, displayMsg{role: "error", content: "Valid values: true, false"})
				}
				return m, nil
			default:
				m.messages = append(m.messages, displayMsg{role: "error", content: fmt.Sprintf("Unknown option %q. Use: lines, linenumbers", subcmd)})
				return m, nil
			}
		},
	})

	// /plugins — list installed plugins
	subcommandRegistry.Register(&delegatingCommand{
		name:        "plugins",
		description: "list installed plugins",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			m.messages = append(m.messages, displayMsg{role: "system", content: pluginsSummary(m.pluginRuntime)})
			return m, nil
		},
	})

	// /plugin — alias for /plugins
	subcommandRegistry.Register(&delegatingCommand{
		name:        "plugin",
		description: "list installed plugins (alias for /plugins)",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			m.messages = append(m.messages, displayMsg{role: "system", content: pluginsSummary(m.pluginRuntime)})
			return m, nil
		},
	})

	// /upgrade — check for updates
	subcommandRegistry.Register(&delegatingCommand{
		name:        "upgrade",
		description: "check for rho updates",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			return m.startPromptCommand("/upgrade", "Check for rho updates and show the latest available version.")
		},
	})

	// /announcements — show system announcements
	subcommandRegistry.Register(&delegatingCommand{
		name:        "announcements",
		description: "show system announcements (release notes, updates)",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			state, err := tool.ReadAnnouncements()
			if err != nil {
				m.messages = append(m.messages, displayMsg{role: "error", content: fmt.Sprintf("Failed to read announcements: %v", err)})
				return m, nil
			}
			visible := tool.VisibleAnnouncements(nil, state.HiddenIDs)
			if len(visible) == 0 {
				m.messages = append(m.messages, displayMsg{role: "system", content: "No announcements."})
			} else {
				var b strings.Builder
				b.WriteString("Announcements:\n")
				for i, a := range visible {
					if a.Title != "" {
						b.WriteString(fmt.Sprintf("  [%d] %s\n", i+1, a.Title))
					}
					b.WriteString(fmt.Sprintf("    %s\n", a.Message))
					if a.CTA != nil && a.CTA.URL != "" {
						b.WriteString(fmt.Sprintf("    → %s (%s)\n", a.CTA.Label, a.CTA.URL))
					}
				}
				m.messages = append(m.messages, displayMsg{role: "system", content: b.String()})
			}
			return m, nil
		},
	})

	// /prompt-queue — manage queued prompts
	subcommandRegistry.Register(&delegatingCommand{
		name:        "prompt-queue",
		aliases:     []string{"queue"},
		description: "manage queued prompts (add|list|clear|remove)",
		usage:       "/prompt-queue <add|list|clear|remove> [args]",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			if len(args) < 1 {
				m.messages = append(m.messages, displayMsg{role: "system", content: "Usage: /prompt-queue add <prompt>\n       /prompt-queue list\n       /prompt-queue clear\n       /prompt-queue remove <index>"})
				return m, nil
			}
			subcmd := strings.ToLower(args[0])
			switch subcmd {
			case "add", "queue":
				if len(args) < 2 {
					m.messages = append(m.messages, displayMsg{role: "error", content: "Usage: /prompt-queue add <prompt>"})
					return m, nil
				}
				prompt := strings.TrimSpace(strings.TrimPrefix(text, "/prompt-queue add"))
				if prompt == "" {
					m.messages = append(m.messages, displayMsg{role: "error", content: "Prompt cannot be empty"})
					return m, nil
				}
				if err := tool.EnqueuePrompt(prompt, ""); err != nil {
					m.messages = append(m.messages, displayMsg{role: "error", content: fmt.Sprintf("Failed to queue prompt: %v", err)})
				} else {
					m.messages = append(m.messages, displayMsg{role: "system", content: "Prompt queued."})
				}
				return m, nil
			case "list", "ls":
				items := tool.GetPromptQueue()
				if len(items) == 0 {
					m.messages = append(m.messages, displayMsg{role: "system", content: "Queue is empty."})
					return m, nil
				}
				var b strings.Builder
				b.WriteString(fmt.Sprintf("Prompt queue (%d items):\n", len(items)))
				for i, item := range items {
					if item.Subject != "" {
						b.WriteString(fmt.Sprintf("  [%d] %s\n", i+1, item.Subject))
					} else {
						preview := truncatePromptPreview(item.Prompt, 50)
						b.WriteString(fmt.Sprintf("  [%d] %s\n", i+1, preview))
					}
				}
				m.messages = append(m.messages, displayMsg{role: "system", content: b.String()})
				return m, nil
			case "clear":
				if err := tool.ClearPromptQueue(); err != nil {
					m.messages = append(m.messages, displayMsg{role: "error", content: fmt.Sprintf("Failed to clear queue: %v", err)})
				} else {
					m.messages = append(m.messages, displayMsg{role: "system", content: "Queue cleared."})
				}
				return m, nil
			case "remove", "rm":
				if len(args) < 2 {
					m.messages = append(m.messages, displayMsg{role: "error", content: "Usage: /prompt-queue remove <index>"})
					return m, nil
				}
				idx, err := strconv.Atoi(args[1])
				if err != nil || idx < 1 {
					m.messages = append(m.messages, displayMsg{role: "error", content: "Invalid index"})
					return m, nil
				}
				if err := tool.RemovePromptFromQueue(idx - 1); err != nil {
					m.messages = append(m.messages, displayMsg{role: "error", content: fmt.Sprintf("Failed to remove: %v", err)})
				} else {
					m.messages = append(m.messages, displayMsg{role: "system", content: "Removed from queue."})
				}
				return m, nil
			default:
				m.messages = append(m.messages, displayMsg{role: "error", content: fmt.Sprintf("Unknown subcommand %q. Use: add, list, clear, remove", subcmd)})
				return m, nil
			}
		},
	})

	// /keybindings — show keybindings
	subcommandRegistry.Register(&delegatingCommand{
		name:        "keybindings",
		description: "show keybindings",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			m.messages = append(m.messages, displayMsg{role: "system", content: "Keybindings:\n  Enter           — Submit\n  Ctrl+C          — Cancel/Exit\n  Ctrl+Shift+C    — Copy (input draft or chat)\n  Ctrl+K          — Command palette\n  Ctrl+L          — Cycle autonomy tier\n  Up/Down         — History\n  Tab             — Complete\n  /select         — Native text selection\n  /mouse off      — Enable click-drag copy"})
			return m, nil
		},
	})

	// /statusline — print compact status line
	subcommandRegistry.Register(&delegatingCommand{
		name:        "statusline",
		description: "print a compact status line",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			m.messages = append(m.messages, displayMsg{role: "system", content: statusLineSummary(m)})
			return m, nil
		},
	})

	// /remote-env — show remote env summary
	subcommandRegistry.Register(&delegatingCommand{
		name:        "remote-env",
		description: "show the remote env summary",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			m.messages = append(m.messages, displayMsg{role: "system", content: envSummary(m.session.Provider(), m.session.Model())})
			return m, nil
		},
	})

	// /thinkback, /think-back, /thinkback-play — review reasoning
	subcommandRegistry.Register(&delegatingCommand{
		name:        "thinkback",
		aliases:     []string{"think-back", "thinkback-play"},
		description: "review reasoning decisions (thinkback/think-back/thinkback-play)",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			summary := "Review the thinking/reasoning from this conversation and highlight key decision points and alternatives considered."
			return m.startPromptCommand("/thinkback", summary)
		},
	})

	// /ultrareview — adversarial code review
	subcommandRegistry.Register(&delegatingCommand{
		name:        "ultrareview",
		description: "perform a deep, adversarial code review",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			return m.startPromptCommand("/ultrareview", "Perform a deep, adversarial code review of this change set. Prioritize correctness, security, regressions, and missing tests.")
		},
	})

	// --- session-delegating commands ---
	//
	// These all dispatch to m.handleSessionCommand (a chatModel
	// method that owns the per-name session-management logic).
	// Each /command is registered separately so the dispatcher
	// can route them by name.

	sessionDelegates := []struct {
		name        string
		description string
	}{
		{"export", "export session to JSON (delegates to handleSessionCommand)"},
		{"rename", "rename the current session (delegates to handleSessionCommand)"},
		{"tag", "tag the current session (delegates to handleSessionCommand)"},
		{"session", "show current session info (delegates to handleSessionCommand)"},
		{"share", "share session (delegates to handleSessionCommand)"},
		{"search", "search across sessions (delegates to handleSessionCommand)"},
		{"clean", "clean up old sessions (delegates to handleSessionCommand)"},
		{"compress", "compress session storage (delegates to handleSessionCommand)"},
		{"integrity", "verify session integrity (delegates to handleSessionCommand)"},
		{"retry", "retry the last failed action (delegates to handleSessionCommand)"},
		{"rewind", "rewind to a previous checkpoint (delegates to handleSessionCommand)"},
		{"fork", "fork the current session (delegates to handleSessionCommand)"},
		{"new", "start a new session (delegates to handleSessionCommand)"},
	}
	for _, sd := range sessionDelegates {
		name := sd.name
		subcommandRegistry.Register(&delegatingCommand{
			name:        name,
			description: sd.description,
			usage:       "/" + name,
			handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
				return m.handleSessionCommand("/"+name, append([]string{"/" + name}, args...), text)
			},
		})
	}

	// /audit — show audit summary (delegates to tool.FormatAuditSummary)
	subcommandRegistry.Register(&delegatingCommand{
		name:        "audit",
		description: "show audit summary",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			m.messages = append(m.messages, displayMsg{role: "system", content: tool.FormatAuditSummary()})
			return m, nil
		},
	})

	// /power <1-10> — set reasoning power level
	subcommandRegistry.Register(&delegatingCommand{
		name:        "power",
		description: "set reasoning power level (1-10)",
		usage:       "/power <1-10>",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			if len(args) < 1 {
				m.messages = append(m.messages, displayMsg{role: "system", content: "Usage: /power <1-10>\n" + DescribePower(5)})
				return m, nil
			}
			level, err := strconv.Atoi(args[0])
			if err != nil || level < 1 || level > 10 {
				m.messages = append(m.messages, displayMsg{role: "error", content: "Power level must be 1-10."})
				return m, nil
			}
			ApplyPowerLevel(m.session, level)
			m.messages = append(m.messages, displayMsg{role: "system", content: DescribePower(level)})
			return m, nil
		},
	})

	// /output-style <style> — set output verbosity
	subcommandRegistry.Register(&delegatingCommand{
		name:        "output-style",
		description: "set output verbosity (concise|normal|detailed)",
		usage:       "/output-style <concise|normal|detailed>",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			if len(args) < 1 {
				m.messages = append(m.messages, displayMsg{role: "system", content: "Usage: /output-style <concise|normal|detailed>"})
				return m, nil
			}
			style := strings.ToLower(args[0])
			switch style {
			case "concise", "normal", "detailed":
				_ = rhoconfig.SetGlobalSetting("outputStyle", style)
				m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Output style → %s", style)})
			default:
				m.messages = append(m.messages, displayMsg{role: "error", content: "Valid styles: concise, normal, detailed"})
			}
			return m, nil
		},
	})

	// /reload-plugins — reload the plugin runtime
	subcommandRegistry.Register(&delegatingCommand{
		name:        "reload-plugins",
		description: "reload installed plugins",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			if m.pluginRuntime != nil {
				_ = m.pluginRuntime.LoadAll()
			}
			// Invalidate slash suggestion cache: plugins may register new slash commands
			m.invalidateSlashSugCache()
			m.messages = append(m.messages, displayMsg{role: "system", content: "Plugins reloaded."})
			return m, nil
		},
	})

	// /autonomy — show/set trust tier, permissions, and rules
	subcommandRegistry.Register(&delegatingCommand{
		name:        "autonomy",
		description: "show/set trust tier, permissions, and rules (delegates to handleAutonomyCommand)",
		usage:       "/autonomy [subcommand]",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			next, cmd := m.handleAutonomyCommand(append([]string{"/autonomy"}, args...))
			return next, cmd
		},
	})

	// /permission — focused control plane for access decisions and folder trust.
	subcommandRegistry.Register(&delegatingCommand{
		name:        "permission",
		description: "show or change permissions and folder trust",
		usage:       "/permission [status|trust|reset]",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			next, cmd := m.handlePolicyCommand(args)
			return &next, cmd
		},
	})

	// /add <file...> — add file content to context
	subcommandRegistry.Register(&delegatingCommand{
		name:        "add",
		description: "add file content to the model context",
		usage:       "/add <file-path> [file-path...]",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			if len(args) < 1 {
				m.messages = append(m.messages, displayMsg{role: "error", content: "Usage: /add <file-path> [file-path...]"})
				return m, nil
			}
			var added []string
			for _, f := range args {
				content, err := os.ReadFile(f) // #nosec G304 -- f is a user-specified file path from the /add command, intentional read
				if err != nil {
					m.messages = append(m.messages, displayMsg{role: "error", content: fmt.Sprintf("Cannot read %s: %v", f, err)})
					continue
				}
				m.session.AddUser(fmt.Sprintf("[File: %s]\n```\n%s\n```", f, string(content)))
				added = append(added, f)
			}
			if len(added) > 0 {
				m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Added to context: %s", strings.Join(added, ", "))})
			}
			return m, nil
		},
	})

	// /drop — drop the last N messages from context
	subcommandRegistry.Register(&delegatingCommand{
		name:        "drop",
		description: "drop the last N messages from context (delegates to handleSessionCommand)",
		usage:       "/drop [N]",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			return m.handleSessionCommand("/drop", append([]string{"/drop"}, args...), text)
		},
	})

	// /tokens — show estimated token usage
	subcommandRegistry.Register(&delegatingCommand{
		name:        "tokens",
		description: "show estimated token usage",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Messages: %d\nEstimated tokens: ~%d", m.session.MessageCount(), m.session.MessageCount()*200)})
			return m, nil
		},
	})

	// /research — set up a research experiment
	subcommandRegistry.Register(&delegatingCommand{
		name:        "research",
		description: "set up a research experiment (--grep, --direction, --budget, --branch, --results)",
		usage:       "/research [flags] <metric-command>",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			if len(args) < 1 {
				m.messages = append(m.messages, displayMsg{role: "error", content: "Usage: /research [--grep <pattern>] [--direction lower|higher] [--budget <min>] [--branch <prefix>] [--results <file>] <metric-command>\nExample: /research go test -bench .\nExample: /research --grep '^val_bpb:' --direction lower uv run train.py"})
				return m, nil
			}
			argText := strings.TrimSpace(strings.TrimPrefix(text, "/research"))
			cfg := parseResearchArgs(argText)
			if cfg.MetricCmd == "" {
				m.messages = append(m.messages, displayMsg{role: "error", content: "Metric command is required."})
				return m, nil
			}
			prompt := BuildResearchPrompt(cfg)
			return m.startPromptCommand("/research", prompt)
		},
	})

	// /explain <file>:<line> — trace code back to the commit that created it
	subcommandRegistry.Register(&delegatingCommand{
		name:        "explain",
		description: "trace code back to the commit that created it",
		usage:       "/explain <file>:<line>",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			if len(args) < 1 {
				m.messages = append(m.messages, displayMsg{role: "system", content: "Usage: /explain <file>:<line>  — trace code back to the commit that created it"})
				return m, nil
			}
			arg := args[0]
			path := arg
			line := 1
			if idx := strings.LastIndex(arg, ":"); idx > 0 {
				path = arg[:idx]
				if n, err := strconv.Atoi(arg[idx+1:]); err == nil {
					line = n
				}
			}
			result, err := explainCode(path, line)
			if err != nil {
				m.messages = append(m.messages, displayMsg{role: "error", content: err.Error()})
			} else {
				m.messages = append(m.messages, displayMsg{role: "assistant", content: result})
			}
			return m, nil
		},
	})

	// /feedback <msg> — submit feedback saved to Rho user state.
	subcommandRegistry.Register(&delegatingCommand{
		name:        "feedback",
		description: "submit feedback (saved to Rho user state)",
		usage:       "/feedback <message>",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			body := strings.TrimSpace(strings.TrimPrefix(text, "/feedback"))
			if body == "" {
				m.messages = append(m.messages, displayMsg{role: "system", content: "Usage: /feedback <message>\nCaptures session context and saves feedback to Rho user state."})
				return m, nil
			}
			feedDir := filepath.Join(storage.StateDir(), "feedback")
			_ = os.MkdirAll(feedDir, 0o750)
			report := fmt.Sprintf(`{"timestamp":%q,"version":%q,"model":%q,"provider":%q,"category":"session","body":%q,"session_id":%q}`,
				time.Now().Format(time.RFC3339), version, m.session.Model(), m.session.Provider(), body, m.sessionID)
			fname := fmt.Sprintf("feedback-%s.json", time.Now().Format("20060102-150405"))
			fpath := filepath.Join(feedDir, fname)
			if err := os.WriteFile(fpath, []byte(report), 0o600); err != nil {
				m.messages = append(m.messages, displayMsg{role: "error", content: "Failed to save feedback: " + err.Error()})
			} else {
				m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Feedback saved to %s", fpath)})
			}
			return m, nil
		},
	})

	// /stale [duration] — show stale permission rules
	subcommandRegistry.Register(&delegatingCommand{
		name:        "stale",
		description: "show stale permission rules that may need removal",
		usage:       "/stale [duration]",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			if m.stalenessDetector == nil {
				m.messages = append(m.messages, displayMsg{role: "system", content: "No staleness data available yet. Rules will be tracked as they are used."})
				return m, nil
			}
			threshold := 7 * 24 * time.Hour // 7 days default
			if len(args) >= 1 {
				if d, err := time.ParseDuration(args[0]); err == nil {
					threshold = d
				}
			}
			staleRules := m.stalenessDetector.CheckStaleness(threshold)
			if len(staleRules) == 0 {
				m.messages = append(m.messages, displayMsg{role: "system", content: "No stale rules detected. All rules have been used within the threshold."})
			} else {
				m.messages = append(m.messages, displayMsg{role: "system", content: stalenessFormatReport(staleRules)})
			}
			return m, nil
		},
	})

	// /taste — show learned coding style preferences
	subcommandRegistry.Register(&delegatingCommand{
		name:        "taste",
		description: "show learned coding style preferences",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			store, err := tasteStoreForSession()
			if err != nil {
				m.messages = append(m.messages, displayMsg{role: "error", content: "Taste store error: " + err.Error()})
				return m, nil
			}
			cwd, _ := os.Getwd()
			projectID := filepath.Base(cwd)
			profile, err := store.Load(projectID)
			if err != nil {
				m.messages = append(m.messages, displayMsg{role: "error", content: "Load taste profile: " + err.Error()})
				return m, nil
			}
			m.messages = append(m.messages, displayMsg{role: "system", content: profile.Summary()})
			return m, nil
		},
	})

	// /stats [days] — session statistics
	subcommandRegistry.Register(&delegatingCommand{
		name:        "stats",
		description: "show session statistics (analytics.ComputeStats)",
		usage:       "/stats [days]",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			days := 30
			if len(args) >= 1 {
				if d, err := strconv.Atoi(args[0]); err == nil && d > 0 {
					days = d
				}
			}
			stats, err := analytics.ComputeStats(days)
			if err != nil {
				m.messages = append(m.messages, displayMsg{role: "system", content: sessionStats(m.session, m.sessionID)})
			} else {
				m.messages = append(m.messages, displayMsg{role: "system", content: analytics.FormatStats(stats)})
			}
			return m, nil
		},
	})

	// /image — handle image input
	subcommandRegistry.Register(&delegatingCommand{
		name:        "image",
		description: "add an image to the conversation (delegates to handleImageCommand)",
		usage:       "/image <path>",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			return m.handleImageCommand(append([]string{"/image"}, args...), text)
		},
	})

	// /provider-status — show deployment status
	subcommandRegistry.Register(&delegatingCommand{
		name:        "provider-status",
		description: "show provider deployment status",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			report, err := rhoconfig.DeploymentStatusReport(context.Background(), m.session.Model())
			if err != nil {
				m.messages = append(m.messages, displayMsg{role: "error", content: fmt.Sprintf("Provider status failed: %v", err)})
				return m, nil
			}
			m.messages = append(m.messages, displayMsg{role: "system", content: report})
			return m, nil
		},
	})

	// /refresh-model-catalog — refresh the model catalog
	subcommandRegistry.Register(&delegatingCommand{
		name:        "refresh-model-catalog",
		description: "refresh the model catalog",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			summary, err := rhoconfig.RefreshModelCatalogV1(context.Background())
			if err != nil {
				m.messages = append(m.messages, displayMsg{role: "error", content: fmt.Sprintf("Model catalog refresh failed: %v", err)})
				return m, nil
			}
			m.messages = append(m.messages, displayMsg{role: "system", content: summary})
			return m, nil
		},
	})

	// /insights [days] — generate session insights
	subcommandRegistry.Register(&delegatingCommand{
		name:        "insights",
		description: "generate session insights report",
		usage:       "/insights [days]",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			days := 30
			if len(args) >= 1 {
				if d, err := strconv.Atoi(args[0]); err == nil && d > 0 {
					days = d
				}
			}
			report, err := analytics.GenerateInsights(days, nil)
			if err != nil {
				return m.startPromptCommand("/insights", "Generate a concise report of patterns, friction, wins, and suggested improvements from this session.")
			}
			path, saveErr := analytics.SaveInsightsReport(report)
			if saveErr != nil {
				m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Insights: %d sessions scanned, %d patterns found. (Failed to save: %v)", report.SessionsScanned, len(report.TopPatterns), saveErr)})
			} else {
				m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Insights report saved: %s\n%d sessions scanned, %d patterns.", path, report.SessionsScanned, len(report.TopPatterns))})
			}
			return m, nil
		},
	})

	// /ctx, /ctx-viz — show session context usage
	subcommandRegistry.Register(&delegatingCommand{
		name:        "ctx",
		aliases:     []string{"ctx-viz"},
		description: "show session context usage",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			m.messages = append(m.messages, displayMsg{role: "system", content: formatSessionContextUsage(m)})
			m.viewDirty = true
			return m, nil
		},
	})

	// /home — go to home view
	subcommandRegistry.Register(&delegatingCommand{
		name:        "home",
		description: "go to the home view",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			m.goHome()
			m.updateViewportContent()
			return m, nil
		},
	})

	// /follow — toggle stream-follow mode
	subcommandRegistry.Register(&delegatingCommand{
		name:        "follow",
		description: "toggle stream-follow mode (auto-scroll during replies)",
		usage:       "",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			m.streamFollow = !m.streamFollow
			if m.streamFollow {
				m.autoScroll = true
				m.viewport.GotoBottom()
			}
			state := "off"
			if m.streamFollow {
				state = "on"
			}
			m.messages = append(m.messages, displayMsg{role: "system", content: "Stream follow: " + state + " (scroll up or Tab→scrollback freezes view during replies)"})
			m.viewDirty = true
			return m, nil
		},
	})

	// /btw <note> — add a background note the model should keep in mind
	subcommandRegistry.Register(&delegatingCommand{
		name:        "btw",
		description: "add a background note the model should keep in mind",
		usage:       "/btw <note>",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			if len(args) < 1 {
				m.messages = append(m.messages, displayMsg{role: "error", content: "Usage: /btw <message>"})
				return m, nil
			}
			note := strings.TrimSpace(strings.TrimPrefix(text, "/btw"))
			m.session.AddUser(fmt.Sprintf("[Background note — do not respond to this directly, just acknowledge and keep it in mind]\n%s", note))
			m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Noted: %s", note)})
			return m, nil
		},
	})

	// /loop <interval> <command> — run a command on an interval
	subcommandRegistry.Register(&delegatingCommand{
		name:        "loop",
		description: "run a command on an interval (e.g., /loop 5m /doctor)",
		usage:       "/loop <interval> <command>",
		handler: func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
			if len(args) < 2 {
				m.messages = append(m.messages, displayMsg{role: "system", content: "Usage: /loop <interval> <command> (e.g., /loop 5m /doctor)"})
				return m, nil
			}
			interval, err := time.ParseDuration(args[0])
			if err != nil {
				m.messages = append(m.messages, displayMsg{role: "error", content: fmt.Sprintf("Invalid interval %q: %v", args[0], err)})
				return m, nil
			}
			loopCmd := strings.Join(args[1:], " ")
			if m.loopCancel != nil {
				m.loopCancel()
			}
			loopCtx, loopCancel := context.WithCancel(context.Background())
			m.loopCancel = loopCancel
			m.messages = append(m.messages, displayMsg{role: "system", content: fmt.Sprintf("Loop started: %s every %s (stop with /clear)", loopCmd, interval)})
			go func() {
				ticker := time.NewTicker(interval)
				defer ticker.Stop()
				for {
					select {
					case <-loopCtx.Done():
						return
					case <-ticker.C:
						m.ref.Send(loopTickMsg{command: loopCmd})
					}
				}
			}()
			return m, nil
		},
	})
}

// delegatingCommand is a ChatSubcommand implementation that wraps
// a handler function. Used for the many simple /slash commands
// that just dispatch to a small body. Avoids the boilerplate of
// defining a struct per command.
type delegatingCommand struct {
	name        string
	aliases     []string
	description string
	usage       string
	handler     func(m *chatModel, args []string, text string) (tea.Model, tea.Cmd)
}

func (d *delegatingCommand) Name() string        { return d.name }
func (d *delegatingCommand) Aliases() []string   { return d.aliases }
func (d *delegatingCommand) Description() string { return d.description }
func (d *delegatingCommand) Usage() string       { return d.usage }
func (d *delegatingCommand) Handle(m *chatModel, args []string, text string) (tea.Model, tea.Cmd) {
	return d.handler(m, args, text)
}
