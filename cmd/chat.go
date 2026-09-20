package cmd

import (
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine/safety"

	"golang.org/x/term"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/GrayCodeAI/rho/internal/bridge/sessioncapture"
	"github.com/GrayCodeAI/rho/internal/codegraph"
	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	"github.com/GrayCodeAI/rho/internal/engine"
	chatfeature "github.com/GrayCodeAI/rho/internal/features/chat"
	sessionfeature "github.com/GrayCodeAI/rho/internal/features/session"
	"github.com/GrayCodeAI/rho/internal/features/shellmode"
	"github.com/GrayCodeAI/rho/internal/features/taste"
	"github.com/GrayCodeAI/rho/internal/intelligence/repomap"
	"github.com/GrayCodeAI/rho/internal/plugin"
	"github.com/GrayCodeAI/rho/internal/session"
	"github.com/GrayCodeAI/rho/internal/startup"
	rhostorage "github.com/GrayCodeAI/rho/internal/storage"
	"github.com/GrayCodeAI/rho/internal/system/staleness"
	"github.com/GrayCodeAI/rho/internal/tool"
	"github.com/GrayCodeAI/rho/internal/tui"
	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

// Types, styles, and model struct are in chat_model.go
// Welcome message and config summary helpers are in chat_welcome.go
// Slash command handling and helpers are in chat_commands.go
// Tool-registry construction (essential/optional tools) is in chat_tools.go
// The Bubble Tea event loop (Update, applyPromptArrowKey) is in chat_update.go

const workInputPlaceholder = `Ask Rho to inspect, edit, or run something... (Shift+Enter for newline, ? for help)`

func genID() string {
	b := make([]byte, 8)
	if _, err := cryptorand.Read(b); err != nil {
		// CSPRNG failed — fall back to timestamp-based ID to avoid all-zeros.
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}

func prepareSession(sess *engine.Session) (string, *session.Session, error) {
	id := genID()
	if sessionIDFlag != "" && resumeID == "" && !continueFlag {
		if err := session.ValidateID(sessionIDFlag); err != nil {
			return "", nil, fmt.Errorf("invalid --session-id: %w", err)
		}
		id = sessionIDFlag
	}
	if sessionIDFlag != "" && (resumeID != "" || continueFlag) {
		// --session-id is ignored when --resume or --continue is also given.
		fmt.Fprintf(os.Stderr, "%s\n", auditTint("rho: --session-id ignored during resume/continue", textMuted))
	}
	if resumeID == "" && !continueFlag {
		return id, nil, nil
	}

	var (
		saved *session.Session
		err   error
	)
	if resumeID != "" {
		saved, _, err = session.ResumeSession(resumeID)
		// Second return value (recovery note) is intentionally unused:
		// /recover command handles listing; here we just need the session.
	} else {
		cwd, _ := os.Getwd()
		saved, err = session.LoadLatestForCWD(cwd)
	}
	if err != nil {
		return "", nil, err
	}
	sess.LoadMessages(sessionfeature.Hydrate(saved).Runtime)
	if forkSessionFlag {
		if sessionIDFlag != "" {
			if err := session.ValidateID(sessionIDFlag); err != nil {
				return "", nil, fmt.Errorf("invalid --session-id: %w", err)
			}
			id = sessionIDFlag
		}
		return id, saved, nil
	}
	return saved.ID, saved, nil
}

func newChatModelWithRegistry(ref *progRef, systemPrompt string, settings rhoconfig.Settings, registry *tool.Registry) (chatModel, error) {
	startup.MarkPhase("newChatModel:total")

	startup.MarkPhase("newChatModel:ui-init")
	ta := textarea.New()
	ta.Placeholder = workInputPlaceholder
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.MaxHeight = 10
	ta.SetHeight(1)
	taWidth := 80
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 10 {
		taWidth = w
	}
	ta.SetWidth(taWidth - 4)
	ta.SetStyles(textarea.Styles{
		Focused: textarea.StyleState{
			CursorLine:  lipgloss.NewStyle(),
			Base:        lipgloss.NewStyle().Foreground(textPrimary),
			Placeholder: lipgloss.NewStyle().Foreground(textPlaceholder),
			Prompt:      lipgloss.NewStyle().Foreground(rhoColor).Bold(true),
		},
		Blurred: textarea.StyleState{
			Base:        lipgloss.NewStyle().Foreground(textPlaceholder),
			Placeholder: lipgloss.NewStyle().Foreground(textPlaceholder),
			Prompt:      lipgloss.NewStyle().Foreground(rhoColor).Bold(true),
		},
		Cursor: textarea.CursorStyle{
			Color: rhoColor,
		},
	})
	ta.Prompt = icons.ChevronRight() + " "
	// Enter submits; Shift+Enter inserts newline
	ta.KeyMap.InsertNewline.SetKeys("shift+enter")

	// Secondary textinput for config panel password entry
	ci := textinput.New()
	ci.EchoMode = textinput.EchoNormal

	sp := spinner.New()
	sp.Spinner = spinner.Spinner{Frames: rhoSpinnerFrames, FPS: rhoSpinnerFrameInterval}
	sp.Style = lipgloss.NewStyle().Foreground(rhoColor).Bold(true)
	startup.EndPhase("newChatModel:ui-init")

	startup.MarkPhase("newChatModel:effectiveModelAndProvider")
	selection := startupSelection(settings)
	effectiveModel, effectiveProvider := selection.Model, selection.Provider
	startup.EndPhase("newChatModel:effectiveModelAndProvider")

	startup.MarkPhase("newChatModel:defaultRegistry")
	startup.EndPhase("newChatModel:defaultRegistry")

	startup.MarkPhase("newChatModel:newRhoSession")
	sess := newStartupRhoSession(selection, systemPrompt, registry)
	startup.EndPhase("newChatModel:newRhoSession")

	startup.MarkPhase("newChatModel:configureSession")
	if cfgErr := prepareInteractiveSessionStartup(sess, settings); cfgErr != nil {
		return chatModel{}, cfgErr
	}
	startup.EndPhase("newChatModel:configureSession")

	startup.MarkPhase("newChatModel:prepareSession")
	sid, saved, err := prepareSession(sess)
	if err != nil {
		return chatModel{}, err
	}
	startup.EndPhase("newChatModel:prepareSession")

	// Conversation arc: durable per-session sidecar of goals/decisions/milestones.
	arc, _ := sessionfeature.LoadArc(sid)
	if arc == nil {
		arc = sessionfeature.NewArc()
	}
	sess.SetArc(arc)

	// Initialize conversation DAG for branching support
	startup.MarkPhase("newChatModel:dag")
	graphPath := filepath.Join(rhostorage.SessionsDir(), "conversations", sid+".json")
	if graph, err := session.OpenConversationGraph(graphPath, sid); err == nil {
		sess.SetConversationGraph(graph)
	}
	startup.EndPhase("newChatModel:dag")

	initWidth := 80
	initHeight := 24
	if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		initWidth = w
		if h > 0 {
			initHeight = h
		}
	}
	vp := viewport.New(viewport.WithWidth(initWidth), viewport.WithHeight(minChatViewportLines))

	now := time.Now()
	// Create a cancel function for background goroutines that gets called on quit.
	_, bgCancel := context.WithCancel(context.Background())
	m := chatModel{input: ta, configInput: ci, spinner: sp, viewport: vp, session: sess, registry: registry, settings: settings, ref: ref, sessionID: sid, partial: &strings.Builder{}, spinnerVerb: spinnerVerbs[rand.Intn(len(spinnerVerbs))], width: initWidth, height: initHeight, history: chatfeature.NewHistory(nil, chatfeature.DefaultHistoryLimit), autoScroll: true, streamFollow: true, uiFocus: focusPrompt, startedAt: now, sessionStartedAt: now, activeSkills: make(map[string]plugin.SmartSkill), toolResultExpanded: make(map[int]bool), bgCancel: bgCancel, mascotEnabled: rhoMascotEnabled()} // #nosec G404 -- non-cryptographic use: non-cryptographic spinner selection
	applyLiveModelMetadata(sess, effectiveProvider, effectiveModel)

	startup.MarkPhase("newChatModel:commandPalette")
	m.commandPalette = NewCommandPaletteWithRuntime(initWidth, m.pluginRuntime)
	startup.EndPhase("newChatModel:commandPalette")

	// Give the footer an immediate cwd value without paying for a git probe
	// before first paint. The background warmup fills in branch/provider data.
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		m.statusLeftKey = cwd
		m.statusLeftVal = shortenHomePath(cwd)
		m.statusLeftAt = time.Now()
	}
	m.invalidateInputLayoutCache()
	(&m).refreshInputLayoutIfNeeded()
	m = m.syncViewportMouseWheel().withSyncedLayout()
	bindChatSession(sess, sid)

	// Surface startup warnings (missing API key, network, sessions dir).
	// validateStartup is fully implemented but was previously never called.
	if warnings := validateStartup(settings); len(warnings) > 0 {
		var warnText strings.Builder
		warnText.WriteString("Startup check:\n")
		for _, w := range warnings {
			warnText.WriteString("  ! " + w.Message + "\n")
		}
		m.messages = append(m.messages, displayMsg{role: "warning", content: warnText.String()})
	}

	// Set initial input placeholder based on mode.
	m.refreshInputPlaceholder()

	// Initialize lacy-inspired features
	startup.MarkPhase("newChatModel:lacy-features")
	m.termCtx = sessioncapture.NewTerminalContext()
	m.inputIndicator = &InputIndicator{}
	m.ghostText = NewGhostText()
	m.contextualHelp = NewContextualHelp()
	m.modeManager = shellmode.NewModeManager()
	m.modeManager.LoadPersistedMode()
	m.brailleSpinner = NewBrailleSpinner(SpinnerRho, "")
	m.brailleSpinner.SetLabel(m.spinnerVerb)
	startup.EndPhase("newChatModel:lacy-features")

	// Initialize BMAD/Aeon features
	startup.MarkPhase("newChatModel:bmad-features")
	m.hintsLoader = engine.NewHintsLoader()
	m.sourceRoots = engine.NewSourceRoots()
	m.selfImprover = engine.NewSelfImprover()
	// Close the self-improvement loop: failure reflections produced by the
	// engine's Reflector are persisted to the cross-session lesson store and
	// injected back via ForPrompt on later submits.
	if sess != nil {
		sess.SetLearnFn(m.selfImprover.Learn)
	}
	m.codingSoul = engine.LoadCodingSoul()
	startup.EndPhase("newChatModel:bmad-features")

	// Initialize taste and staleness subsystems.
	startup.MarkPhase("newChatModel:taste-staleness")
	m.stalenessDetector = staleness.NewDetector()
	if store, err := taste.NewStore(""); err == nil {
		cwd, _ := os.Getwd()
		projectID := filepath.Base(cwd)
		if hooks, err := taste.NewHooks(projectID, store); err == nil {
			m.tasteHooks = hooks
		}
	}
	startup.EndPhase("newChatModel:taste-staleness")

	// Initialize write-ahead log for crash recovery. BatchedWAL batches
	// appends + fsync on a timer so per-message writes don't stall the UI
	// thread (the plain WAL syncs on every append).
	startup.MarkPhase("newChatModel:wal")
	if wal, err := session.NewWAL(sid); err == nil {
		m.wal = session.NewBatchedWAL(wal)
		_ = wal.AppendMeta(effectiveModel, effectiveProvider, "")
	}
	startup.EndPhase("newChatModel:wal")

	// Prefetch live models for the active provider so footer ctx/pricing stay current.
	go func() {
		providerName := effectiveProvider
		entries, _ := rhoconfig.ListEngineModels(context.Background(), providerName, false)
		opts := configModelOptionsFromFlux(entries)
		if len(opts) > 0 {
			modelCacheMu.Lock()
			modelCache[providerName] = opts
			modelCacheMu.Unlock()
			if ref != nil {
				ref.Send(modelsFetchedMsg{options: opts, provider: providerName})
			}
		}
	}()

	// Start with an empty plugin runtime so first paint stays fast.
	startup.MarkPhase("newChatModel:plugin-runtime")
	pr := plugin.NewRuntime()
	startup.EndPhase("newChatModel:plugin-runtime")
	m.pluginRuntime = pr

	// Welcome message inside TUI. Use a cheap initial snapshot and let the
	// async warmup fill in setup/agents status after the first frame.
	startup.MarkPhase("newChatModel:welcome")
	quickSnapshot := welcomeStatusSnapshot{}
	m.welcomeSetupState = quickSnapshot.setup
	m.welcomeAgentsOK = quickSnapshot.agentsOK
	m.welcomeCache = buildWelcomeMessageWithSnapshotAndMascot(sess, sid, registry, saved, settings, 0, connectedMCPCount(registry), 0, initWidth, initHeight, quickSnapshot, "", m.mascotEnabled)
	m.messages = append(m.messages, displayMsg{role: "welcome", content: m.welcomeCache})
	// First-session control-plane tip (skip when resuming history or when quiet env var is set).
	if saved == nil && os.Getenv("RHO_QUIET_START") == "" && os.Getenv("RHO_SUPPRESS_HINTS") == "" && os.Getenv("RHO_QUIET") == "" {
		m.messages = append(m.messages, displayMsg{role: "system", content: controlPlaneOnboardingHint(sess)})
	}
	startup.EndPhase("newChatModel:welcome")

	// Wire permission system
	sess.PermSvc().SetPermissionFn(func(req safety.PermissionRequest) {
		ref.Send(permissionAskMsg{req: req})
	})

	// High-risk action gate (network, destructive bash) — additive layer on top
	// of the permission engine; falls back to AskUserFn for confirmation.
	sess.SetApproval(&engine.ApprovalGate{
		Enabled:        true,
		MaxAutoApprove: safety.AutonomySemi,
		ConfirmFn: func(req engine.ApprovalRequest) engine.ApprovalResponse {
			if sess.PermSvc() != nil && sess.PermSvc().RuntimeState().Autonomy == safety.AutonomyYOLO {
				return engine.ApprovalApprove
			}
			response := make(chan engine.ApprovalResponse, 1)
			ref.Send(approvalAskMsg{req: req, response: response})
			select {
			case answer := <-response:
				return answer
			case <-time.After(5 * time.Minute):
				return engine.ApprovalReject
			}
		},
	})

	// Wire ask_user tool (5-minute timeout matches permission prompts).
	sess.SetAskUserFn(func(question string) (string, error) {
		resp := make(chan string, 1)
		ref.Send(askUserMsg{question: question, response: resp})
		select {
		case answer := <-resp:
			return answer, nil
		case <-time.After(5 * time.Minute):
			return "", fmt.Errorf("question timed out")
		}
	})

	// Wire credential gate: the tool calls this to prompt the user for access
	// to a host credential.
	SetCredentialGate(func(req tool.CredentialRequest) tool.CredentialResponse {
		resp := make(chan tool.CredentialResponse, 1)
		ref.Send(credentialAskMsg{req: req, response: resp})
		select {
		case r := <-resp:
			return r
		case <-time.After(5 * time.Minute):
			return tool.CredentialResponse{Approved: false, Reason: "timed out"}
		}
	})

	if saved != nil {
		for _, message := range sessionfeature.Hydrate(saved).Display {
			m.messages = append(m.messages, displayMsg{role: message.Role, content: message.Content})
		}
	}

	startup.MarkPhase("newChatModel:history")
	m.history.SetEntries(loadInputHistory())
	startup.EndPhase("newChatModel:history")

	startup.MarkPhase("newChatModel:first-paint")
	m.primeInitialViewportContent()
	startup.EndPhase("newChatModel:first-paint")

	// Recover interrupted sessions after first paint so cleanup never delays
	// the initial UI.
	go func(currentSessionID string) {
		if recovered := session.CheckForRecovery(); len(recovered) > 0 {
			walDir := rhostorage.SessionsDir()
			for _, rid := range recovered {
				if rid == currentSessionID {
					continue // current session WAL
				}
				if rs, err := session.RecoverFromWAL(rid); rs != nil && err == nil {
					_ = session.Save(rs)
					_ = os.Remove(filepath.Join(walDir, rid+".wal"))
				}
			}
		}
	}(sid)

	// Warm footer data and the model catalog after the first frame.
	go func(model chatModel) {
		startup.MarkPhase("newChatModel:ui-cache-warm")
		rhoconfig.RefreshConfigCredSnapshot(context.Background())
		// Network reachability runs off the startup critical path: an offline
		// machine stalls here (background) instead of before first paint.
		if msg := checkNetworkReachability(model.settings); msg != "" {
			model.ref.Send(displayMsg{role: "warning", content: "Startup check:\n  ! " + msg})
		}
		welcomeSnapshot := loadWelcomeStatusSnapshot()
		_, _ = model.refreshStatusBarLeft(true)
		connStatusVal := ""
		connStatusKey := ""
		if model.session != nil {
			connStatusVal = model.buildConnectionStatusPlain()
			connStatusKey = model.connStatusFingerprint()
		}
		startup.EndPhase("newChatModel:ui-cache-warm")
		if model.ref != nil {
			model.ref.Send(startupWarmMsg{
				statusLeftKey:    model.statusLeftKey,
				statusLeftVal:    model.statusLeftVal,
				statusLeftBranch: model.statusLeftBranch,
				connStatusVal:    connStatusVal,
				connStatusKey:    connStatusKey,
				welcomeSetup:     welcomeSnapshot.setup,
				welcomeAgentsOK:  welcomeSnapshot.agentsOK,
			})
		}
	}(m)

	// Load plugins/skills after startup and refresh welcome indicators when ready.
	go func() {
		runtime := plugin.NewRuntime()
		if err := runtime.LoadAll(); err != nil {
			// Surface plugin load failure so users know plugins are missing.
			fmt.Fprintf(os.Stderr, "%s\n", auditTint(fmt.Sprintf("Warning: failed to load plugins: %v", err), warnAmber))
			return
		}
		runtime.RegisterHooks()
		if ref != nil {
			ref.Send(pluginRuntimeReadyMsg{runtime: runtime})
		}
	}()

	// --watch: build initial symbol graph and start file watcher for incremental PageRank updates
	startup.MarkPhase("newChatModel:watch")
	if watchFlag {
		cwd, err := os.Getwd()
		if err == nil {
			sg, graphErr := repomap.BuildSymbolGraph(cwd, repomap.Options{
				MaxFiles:  500,
				MaxTokens: 2000,
			})
			if graphErr == nil && sg != nil {
				changes := make(chan string, 100)
				done := make(chan struct{})

				go func() {
					ticker := time.NewTicker(time.Second)
					defer ticker.Stop()
					var pending []string
					for {
						select {
						case <-done:
							if len(pending) > 0 {
								sg.UpdateGraph(cwd, pending)
							}
							return
						case <-ticker.C:
							if len(pending) > 0 {
								sg.UpdateGraph(cwd, pending)
								pending = nil
							}
						case p, ok := <-changes:
							if !ok {
								if len(pending) > 0 {
									sg.UpdateGraph(cwd, pending)
								}
								return
							}
							pending = append(pending, p)
						}
					}
				}()

				fw, watcherErr := repomap.NewFileWatcher(cwd, func(path string) {
					rel, err := filepath.Rel(cwd, path)
					if err == nil {
						select {
						case changes <- rel:
						default:
						}
					}
				})
				if watcherErr == nil {
					var stopOnce sync.Once
					fw.Start()
					m.watcherStop = func() {
						stopOnce.Do(func() {
							fw.Stop()
							close(done)
						})
					}
				} else {
					close(done)
				}
			}
		}
	}
	startup.EndPhase("newChatModel:watch")

	startup.EndPhase("newChatModel:total")

	// Print startup profile after the full synchronous chat init path completes.
	if startupProfileFlag {
		startup.PrintReport()
	}

	return m, nil
}

// refreshInputPlaceholder updates the input placeholder based on the current
// work mode.
func (m *chatModel) refreshInputPlaceholder() {
	work := engine.WorkModeAct
	if m.session != nil {
		work = m.session.WorkMode()
	}
	switch work {
	case engine.WorkModePlan:
		m.input.Placeholder = "Design architecture or draft plan...  ·  /select copy  ·  ? help"
	case engine.WorkModeReview:
		m.input.Placeholder = "Audit diffs, security, or PRs...  ·  /select copy  ·  ? help"
	default:
		m.input.Placeholder = "Build, refactor, or run commands...  ·  /select copy  ·  ? help"
	}
}

func (m chatModel) Init() tea.Cmd {
	cmds := []tea.Cmd{initTerminalMouseCmd(m.mouseEnabled()), promptKeepAliveCmd(), eyeBlinkTickCmd()}
	if m.mascotEnabled {
		cmds = append(cmds, emitRhoMascotCmd())
	}
	if gw, _ := m.sessionGatewayModel(); strings.TrimSpace(gw) != "" {
		cmds = append(cmds, fetchModelsAsync(gw))
		if isXiaomiMimoProvider(gw) {
			cmds = append(cmds, fetchPlatformContextIndexCmd())
		}
	}
	cmds = append(cmds, m.input.Focus())
	return tea.Batch(cmds...)
}

func chatProgramOptions(mouseEnabled bool) []tea.ProgramOption {
	// In Bubble Tea v2, AltScreen, ReportFocus, and MouseMode are handled
	// declaratively in the View() method, so no program options are needed.
	return nil
}

// autoIndexCodegraph runs codegraph indexing in the background on startup.
// Only indexes if .codegraph/ already exists (user has initialized it before).
// Uses Sync for incremental updates (only re-indexes changed files).
func autoIndexCodegraph() {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}

	dbPath := filepath.Join(cwd, ".codegraph", "codegraph.db")
	if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
		return // Not initialized, skip
	}

	cg, err := codegraph.Open(cwd)
	if err != nil {
		return
	}
	defer cg.Close() //nolint:errcheck // best-effort close in background goroutine

	// Incremental sync — only processes changed files
	if _, err := cg.Sync(); err != nil {
		slog.Warn("codegraph sync failed", "error", err)
	}
}

func runChat() error {
	startup.Reset()
	startBackgroundCatalogRefresh(context.Background())

	// On an unexpected panic, persist the active session so a crash loses at
	// most the in-flight message. The closure captures the model once it
	// exists; before that, saveFn is a no-op (nothing to save).
	var active *chatModel
	panicSaveFn = func() {
		if active == nil {
			return
		}
		if active.session != nil && active.sessionID != "" {
			active.saveSession()
		}
	}
	defer func() { panicSaveFn = nil }()

	// Auto-index codegraph in background if .codegraph exists
	go autoIndexCodegraph()

	// One-time, gated codebase analysis for projects with no context file.
	// Runs in the background and never blocks startup; fully opt-out via
	// RHO_DISABLE_AUTO_INIT. No-op for projects that already have context.
	maybeAutoInit(context.Background())

	ref := &progRef{}
	type startupPromptResult struct {
		text string
		err  error
	}
	type startupSettingsResult struct {
		settings rhoconfig.Settings
		err      error
	}
	type startupRegistryResult struct {
		registry *tool.Registry
		err      error
	}
	promptCh := make(chan startupPromptResult, 1)
	settingsCh := make(chan startupSettingsResult, 1)
	registryCh := make(chan startupRegistryResult, 1)
	go func() {
		startup.MarkPhase("runChat:startup-prompt")
		text, err := buildStartupSystemPrompt()
		startup.EndPhase("runChat:startup-prompt")
		promptCh <- startupPromptResult{text: text, err: err}
	}()
	go func() {
		startup.MarkPhase("runChat:settings")
		settings, err := loadEffectiveSettings()
		if err != nil {
			startup.EndPhase("runChat:settings")
			settingsCh <- startupSettingsResult{err: err}
			registryCh <- startupRegistryResult{err: err}
			return
		}
		startup.EndPhase("runChat:settings")
		settingsCh <- startupSettingsResult{settings: settings}
		startup.MarkPhase("runChat:registry")
		registry, regErr := defaultRegistry(settings)
		startup.EndPhase("runChat:registry")
		registryCh <- startupRegistryResult{registry: registry, err: regErr}
	}()
	promptRes := <-promptCh
	settingsRes := <-settingsCh
	registryRes := <-registryCh
	if promptRes.err != nil {
		return promptRes.err
	}
	if settingsRes.err != nil {
		return settingsRes.err
	}
	if registryRes.err != nil {
		return registryRes.err
	}
	systemPrompt := promptRes.text
	settings := settingsRes.settings
	// Pass the registry already built by the runChat goroutine — rebuilding it
	// here would re-run MCP server startup (up to 1.5s each) a second time.
	m, err := newChatModelWithRegistry(ref, systemPrompt, settings, registryRes.registry)
	if err != nil {
		return err
	}
	active = &m

	if promptFlag != "" {
		if e := (&m).ensureSessionReadyForChat(); e != nil {
			return e
		}
		m.messages = append(m.messages, displayMsg{role: "user", content: promptFlag})
		m.session.AddUser(promptFlag)
		m.turn.Reset()
		m.waiting = true
	}

	// Suppress library log output (e.g. flux retry warnings) from corrupting the TUI.
	// Must be set BEFORE tea.NewProgram so no initialization logs leak through.
	log.SetOutput(io.Discard)

	p := tea.NewProgram(m)

	// Enable terminal tab progress bar (OSC 9;4) for long-running operations.
	EnableTabProgress()
	ref.Set(p)

	// Forward SIGHUP (terminal close, ssh drop, window manager exit) into the
	// TUI as a tea.QuitMsg so the session is saved and cleaned up instead of
	// dying silently mid-run. Bubble Tea only handles SIGINT and SIGTERM.
	// The forwarder deregisters itself after the first SIGHUP so repeated
	// runChat() invocations do not leak signal handlers or goroutines.
	{
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGHUP)
		go func() {
			<-sigCh
			signal.Stop(sigCh)
			ref.Send(tea.QuitMsg{})
		}()
	}

	go func() {
		if extra := strings.TrimSpace(buildDeferredWorkspacePromptContext()); extra != "" {
			ref.Send(systemPromptContextReadyMsg{context: extra})
		}
	}()

	if promptFlag != "" {
		sess := m.session
		ctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		go func() {
			streamErr := chatfeature.RunStream(ctx, sess, func(event engine.StreamEvent) {
				dispatchStreamEvent(ref, event)
			})
			if streamErr != nil {
				p.Send(streamErrMsg{err: streamErr})
			}
		}()
	}

	finalModel, err := p.Run()
	writeTerminalMouse(disableMouseCSI)
	// Kitty graphics are not scoped to Bubble Tea's alternate screen. Remove
	// the optional welcome mascot before the farewell is written so shutdown
	// leaves only the final text message in the terminal.
	_ = tui.ClearGraphics(os.Stderr)
	if err != nil {
		return err
	}
	fm, err := finalChatModel(finalModel)
	if err != nil {
		return err
	}
	if fm.quitting {
		fm.saveSession()
		// Bubble Tea restores the previous screen before returning. Clear that
		// frame and any terminal-persistent mascot so shutdown has one clean,
		// predictable final state.
		fmt.Print("\x1b[2J\x1b[H")
		fmt.Println("Thank you for using Rho!")
		return nil
	}
	rhoC := ansiOrange
	rst := ansiReset

	fmt.Print(fm.welcomeCache)
	fmt.Println()
	for _, msg := range fm.messages {
		switch msg.role {
		case "user":
			fmt.Println(rhoC + "█" + rst + "  " + msg.content)
			fmt.Println()
		case "assistant":
			fmt.Println(rhoC + icons.Robot() + " " + rst + msg.content)
			fmt.Println()
		case "system":
			fmt.Println(dimStyle.Render("●  " + msg.content))
			fmt.Println()
		case "error":
			fmt.Println(errorStyle.Render("●  " + msg.content))
			fmt.Println()
		}
	}

	viewWidth := fm.width
	if viewWidth <= 0 {
		viewWidth = 80
	}
	leftBold := "Auto (Off)"
	leftDim := " - all actions require approval"
	rightStatus := fmt.Sprintf("%s %s", fm.session.Provider(), fm.session.Model())
	leftVisLen := len(leftBold) + len(leftDim)
	gap := viewWidth - leftVisLen - len(rightStatus)
	if gap < 1 {
		gap = 1
	}
	fmt.Printf("%s%s%s%s\n",
		lipgloss.NewStyle().Bold(true).Render(leftBold),
		dimStyle.Render(leftDim),
		strings.Repeat(" ", gap),
		dimStyle.Render(rightStatus))

	border := strings.Repeat("─", viewWidth)
	borderStyle := lipgloss.NewStyle().Foreground(borderDim)
	fmt.Println(borderStyle.Render(border))
	fmt.Println(lipgloss.NewStyle().Foreground(rhoColor).Bold(true).Render(">") + " ")
	fmt.Println(borderStyle.Render(border))
	fmt.Println(dimStyle.Render("? for help"))

	if fm.sessionID != "" {
		fmt.Println(dimStyle.Render(fmt.Sprintf("To resume this session, run: rho --resume %s", fm.sessionID)))
	}
	return nil
}

// finalChatModel normalizes Bubble Tea's shutdown model. Bubble Tea may return
// either the value passed to NewProgram or its pointer after updates; both are
// valid representations of the same chat state.
func finalChatModel(model tea.Model) (chatModel, error) {
	switch m := model.(type) {
	case chatModel:
		return m, nil
	case *chatModel:
		if m != nil {
			return *m, nil
		}
	}
	return chatModel{}, fmt.Errorf("unexpected final model type: %T", model)
}
