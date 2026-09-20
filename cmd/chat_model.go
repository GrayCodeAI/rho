package cmd

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/GrayCodeAI/rho/internal/engine/safety"
	chatfeature "github.com/GrayCodeAI/rho/internal/features/chat"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/GrayCodeAI/rho/internal/bridge/sessioncapture"
	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	"github.com/GrayCodeAI/rho/internal/engine"
	"github.com/GrayCodeAI/rho/internal/features/shellmode"
	"github.com/GrayCodeAI/rho/internal/features/taste"
	"github.com/GrayCodeAI/rho/internal/plugin"
	"github.com/GrayCodeAI/rho/internal/session"
	"github.com/GrayCodeAI/rho/internal/system/staleness"
	"github.com/GrayCodeAI/rho/internal/tool"
)

// sessionWAL is the durability surface the chat model needs from its
// write-ahead log. Both *session.WAL (per-append fsync) and
// *session.BatchedWAL (timer-batched fsync) satisfy it; the model uses the
// batched form so UI-thread appends don't stall on disk.
type sessionWAL interface {
	Append(msg session.Message) error
	Remove() error
	Close() error
}

func (m *chatModel) ensureWAL() {
	if m == nil || m.wal != nil || m.sessionID == "" {
		return
	}
	wal, err := session.NewWAL(m.sessionID)
	if err != nil {
		m.recordWALError(err)
		return
	}
	m.wal = wal
}

// recordWALError captures the first persistence failure so the user can be
// told their message may not survive a crash. Subsequent failures are dropped
// (the first is surfaced once, in the status area).
func (m *chatModel) recordWALError(err error) {
	if err == nil || m.durabilityWarning != "" {
		return
	}
	m.durabilityWarning = "Warning: session persistence is failing — recent messages may be lost if rho crashes. Check disk space and permissions."
}

// All rho color/icon/glyph constants live in theme.go. This file holds
// the pre-built lipgloss styles that combine a color with attributes
// (bold, italic, border, etc.) for the most common patterns.

var (
	// Pre-built styles for the most common patterns. The color constants
	// themselves are defined in theme.go; this block only attaches
	// attributes (bold, italic, border) on top.
	dimStyle     = lipgloss.NewStyle().Foreground(textDisabled)
	errorStyle   = lipgloss.NewStyle().Foreground(errorCoral)
	warnStyle    = lipgloss.NewStyle().Foreground(warnAmber)
	toolStyle    = lipgloss.NewStyle().Foreground(toolGold).Bold(true)
	toolDimStyle = lipgloss.NewStyle().Foreground(textDisabled)

	slashCmdStyle     = lipgloss.NewStyle().Foreground(textDisabled)
	slashDescStyle    = lipgloss.NewStyle().Foreground(textDisabled)
	slashSelCmdStyle  = lipgloss.NewStyle().Foreground(rhoColor).Bold(true)
	slashSelDescStyle = lipgloss.NewStyle().Foreground(rhoColor)
	inputBorderStyle  = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true, false, true, false).BorderForeground(borderDim)

	// Backwards-compatible alias for callers that still use the old name.
	// New code should use the purpose-named constants in theme.go.
	dimColor = textDisabled
)

func ghostHintStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(textDisabled).Italic(true)
}

// rhoSpinnerFrames feeds the bubbles spinner (matches BrailleSpinner default).
var rhoSpinnerFrames = rhoSpinnerGlyphs

// rhoSpinnerFrameInterval — compass frame cadence.
const rhoSpinnerFrameInterval = 80 * time.Millisecond

// Spinner verbs (from rho-archive) — picked randomly per session
var spinnerVerbs = []string{
	"Abstracting", "Architecting", "Brewing", "Calculating", "Cogitating",
	"Compiling", "Computing", "Conjuring", "Contemplating", "Cooking",
	"Crafting", "Crunching", "Debugging", "Deciphering", "Deliberating",
	"Distilling", "Elucidating", "Encoding", "Envisioning", "Forging",
	"Generating", "Hatching", "Ideating", "Imagining", "Incubating",
	"Inferencing", "Infusing", "Linting", "Manifesting", "Mulling",
	"Musing", "Optimizing", "Orchestrating", "Parsing", "Pondering",
	"Processing", "Reasoning", "Refactoring", "Refining", "Reticulating",
	"Ruminating", "Scaffolding", "Simmering", "Sketching", "Spelunking",
	"Spinning", "Synthesizing", "Tempering", "Thinking", "Tinkering",
	"Tokenizing", "Transpiling", "Unfurling", "Validating", "Vibing",
	"Weaving", "Whisking", "Wizarding", "Working", "Wrangling",
}

type (
	streamChunkMsg      string
	streamRenderTickMsg struct{}
	streamDoneMsg       struct{}
	streamRetryMsg      struct{ content string }
	streamErrMsg        struct{ err error }
	spinnerVerbTickMsg  struct{}
	promptKeepAliveMsg  struct{}
	eyeBlinkTickMsg     struct{}
	eyeFrameNextMsg     struct{ frame int }
	statusLeftPRsMsg    struct {
		branch string
		nums   []string
	}
	usageUpdateMsg  struct{ usage *engine.StreamUsage }
	compactStartMsg struct{}
	compactMsg      struct {
		strategy                  string
		tokensBefore, tokensAfter int
	}
	compactTickMsg struct{}
	compactDoneMsg struct {
		strategy                  string
		tokensBefore, tokensAfter int
		err                       error
		beforeCount, afterCount   int
	}
)

type (
	modelsFetchedMsg struct {
		options  []configModelOption
		provider string
		err      error
	}
	pluginRuntimeReadyMsg struct {
		runtime *plugin.Runtime
	}
	startupWarmMsg struct {
		statusLeftKey    string
		statusLeftVal    string
		statusLeftBranch string
		connStatusVal    string
		connStatusKey    string
		welcomeSetup     rhoconfig.SetupState
		welcomeAgentsOK  bool
	}
	processArrowTickMsg struct {
		seq int
	}
	systemPromptContextReadyMsg struct {
		context string
	}
	loopTickMsg                struct{ command string }
	toolUseMsg                 struct{ name, id string }
	toolResultMsg              struct{ name, content string }
	permissionAskMsg           struct{ req safety.PermissionRequest }
	permissionPromptTimeoutMsg struct{ seq int }
	promptCountdownTickMsg     struct{ generation uint64 }
	approvalAskMsg             struct {
		req      engine.ApprovalRequest
		response chan engine.ApprovalResponse
	}
	approvalPromptTimeoutMsg struct{ seq int }
	thinkingMsg              string
	blastRadiusMsg           struct{ message string }
	askUserMsg               struct {
		question string
		response chan string
	}
	askUserPromptTimeoutMsg struct{ seq int }
	credentialAskMsg        struct {
		req      tool.CredentialRequest
		response chan tool.CredentialResponse
	}
	credentialPromptTimeoutMsg struct{ seq int }
)

type displayMsg struct {
	role      string
	content   string
	timeoutAt time.Time // deadline for permission prompts (zero = none)
}

const interactivePromptTimeout = 5 * time.Minute

type progRef struct {
	mu sync.Mutex
	p  *tea.Program
}

func (r *progRef) Set(p *tea.Program) { r.mu.Lock(); r.p = p; r.mu.Unlock() }
func (r *progRef) Send(msg tea.Msg) {
	r.mu.Lock()
	p := r.p
	r.mu.Unlock()
	if p != nil {
		p.Send(msg)
	}
}

type chatModel struct {
	input                      textarea.Model
	configInput                textinput.Model // secondary input for config panel password entry
	useConfigInput             bool            // true when config panel needs textinput (e.g. password)
	spinner                    spinner.Model
	viewport                   viewport.Model
	session                    *engine.Session
	registry                   *tool.Registry
	settings                   rhoconfig.Settings
	ref                        *progRef
	cancel                     context.CancelFunc // cancel current stream
	sessionID                  string
	messages                   []displayMsg
	partial                    *strings.Builder
	waiting                    bool
	streamCancelled            bool // user cancelled; suppress late streamDone side effects
	arrowSeq                   int
	pendingArrow               *tea.KeyMsg
	arrowBurstActive           bool
	lastArrowTime              time.Time
	processingGenuineArrow     bool
	turn                       chatfeature.TurnState
	messageQueue               []string                   // queued messages while agent is working
	permReq                    *safety.PermissionRequest  // pending permission prompt
	permQueue                  []safety.PermissionRequest // approvals waiting behind the active prompt
	permReqSeq                 int
	permTimeoutAt              time.Time // deadline for the active permission prompt (zero = none)
	approvalReq                *approvalAskMsg
	approvalQueue              []approvalAskMsg
	approvalReqSeq             int
	approvalTimeoutAt          time.Time
	askReq                     *askUserMsg // pending ask_user prompt
	askQueue                   []askUserMsg
	askReqSeq                  int
	askTimeoutAt               time.Time
	credentialReq              *credentialAskMsg // pending credential prompt
	credentialQueue            []credentialAskMsg
	credentialReqSeq           int
	credentialTimeoutAt        time.Time
	promptGeneration           uint64 // invalidates stale countdown ticks across prompt transitions
	pendingYOLOConfirm         bool   // user selected YOLO in the picker; awaiting typed confirmation
	durabilityWarning          string // first WAL persistence failure, surfaced once to the user
	walSeq                     uint64 // increments for each append; guards async WAL rotation
	width                      int
	height                     int
	quitting                   bool
	mascotEnabled              bool // inline RHO mascot is available in the active terminal
	blinkClosed                bool
	eyeFrame                   int
	slashSel                   int
	hudOpen                    bool    // Agent Status HUD overlay (Ctrl+A)
	hudData                    HUDData // latest HUD snapshot
	configOpen                 bool
	configTab                  int // configTabGateways, configTabModels
	configSel                  int
	configScroll               int // scroll offset for long lists
	configNotice               string
	configEntry                string              // configEntryNone, configEntryAPIKeyPaste, configEntryOllamaURL
	configProvider             string              // e.g. configProviderOllama while entry overlay is open
	configModelOptions         []configModelOption // labels + ids from flux catalog
	configModelProvider        string              // filter models after API key paste
	configModelSearch          string              // active model filter query
	configModelSearchActive    bool                // typing into model search input
	configGuideAfterKey        bool                // open model picker when discover finishes
	configGatewayFocus         int                 // last highlighted gateway row (for refresh action)
	configGatewayRowsCache     []configGatewayRow
	configGatewayRowsDirty     bool
	configKeysPendingRemove    string // provider awaiting delete confirmation
	configKeysRemoveStep       int    // 1 = first prompt, 2 = final confirm
	configReplaceProvider      string // replace-key flow target gateway
	configPostSaveKeysProvider string // return to Gateways tab after replace save
	configSaving               bool   // blocks hub/list input while async credential work runs
	configPendingOllamaURL     string
	configZAIRegionSel         int // Z.AI (general or coding) region picker index
	configGatewayRegionSel     int // Generic gateway region picker index
	pluginRuntime              *plugin.Runtime
	spinnerVerb                string
	// Per-turn token counters shown next to the spinner (↑ input, ↓ output).
	// Reset each time the user submits a message; updated by usageUpdateMsg.
	turnInputTokens          int
	turnOutputTokens         int
	turnEstimatedOutputRunes int
	compacting               bool // stream auto-compact: spinner label = Compacting context
	manualCompacting         bool // user ran /compact: show progress panel above input
	compactBarUsed           int  // context tokens snapshot at /compact start
	compactBarWindow         int  // context window snapshot at /compact start
	compactCancel            context.CancelFunc
	// Display values lerped toward the turn targets each render frame
	// (factor 0.10). Smooths the counter animation.
	displayInTok                 float64
	displayOutTok                float64
	lastCtrlC                    time.Time
	supervisedPending            bool      // Ctrl+L guard: waiting for confirmation to land on Supervised
	supervisedPendingAt          time.Time // when the pending confirmation was set
	history                      chatfeature.History
	lastCommand                  string // most recent slash command (for context-aware tips)
	autoScroll                   bool   // whether viewport is pinned to bottom
	streamFollow                 bool   // follow streaming output (Grok-style; toggle with /follow)
	sleepCancel                  func() // cancel function to re-enable sleep (nil if not prevented)
	backgrounded                 bool   // terminal lost focus during current turn (for completion notification)
	notifiedComplete             bool   // completion notification was sent this turn (prevent duplicates)
	uiFocus                      uiFocusArea
	contentLines                 int   // total lines in scrollback content (for footer position)
	lastMouseY                   int   // last pointer row (0-based); -1 = unknown; used when Cursor reports stale wheel Y
	mouseOverride                *bool // runtime /mouse toggle; persisted via settings
	vim                          *VimState
	wal                          sessionWAL
	startedAt                    time.Time // per-turn timer (spinner + turn elapsed)
	sessionStartedAt             time.Time // whole chat session (footer duration)
	sessionBootstrapDone         bool
	toolStartTime                time.Time
	welcomeCache                 string
	welcomeSetupState            rhoconfig.SetupState
	welcomeAgentsOK              bool
	viewDirty                    bool
	layoutKey                    int    // input lines + slash menu height fingerprint
	cachedBottomBarLines         int    // memoized chatBottomBarLines; refresh via refreshInputLayoutIfNeeded
	slashSugInput                string // memoize slashSuggestions per keystroke
	slashSugCache                []string
	slashSugGen                  int             // generation counter; bumped to invalidate slashSugCache
	slashSugCachedGen            int             // generation at the time slashSugCache was computed
	contextualHelp               *ContextualHelp // rich help entries for /help <topic>
	toolResultExpanded           map[int]bool    // per-message index: expanded state for long tool results
	connStatusKey                string          // gateway+model+creds fingerprint
	connStatusVal                string
	deferredSystemContext        string
	deferredSystemContextReady   bool
	deferredSystemContextApplied bool
	partialDirty                 bool // stream text changed since last viewport paint
	lastPartialRender            time.Time
	partialRenderPending         bool
	statusLeftKey                string
	statusLeftVal                string
	statusLeftBranch             string
	statusLeftAt                 time.Time // last branch lookup; refreshed on a short TTL
	statusLeftPRs                []string  // open PR numbers ("#184") for the current branch
	statusLeftPRAt               time.Time // last PR lookup; refreshed on a longer TTL

	// Incremental viewport cache (see chat_viewport_render.go).
	vpStableContent string
	vpRenderedMsgs  int
	vpRenderWidth   int
	vpLastMsgLen    int

	// Cached expanded map for viewport position math (avoids repeated allocations).
	cachedExpandedMap map[int]bool
	cachedExpandedLen int

	// Streaming-partial render cache: rendered output of the completed
	// markdown blocks of m.partial (see renderStreamTail).
	streamMDPrefixRaw string
	streamMDPrefixOut string
	streamMDWidth     int

	activeSkills map[string]plugin.SmartSkill // per-session activated skills

	// Taste & staleness tracking
	tasteHooks        *taste.Hooks
	stalenessDetector *staleness.Detector

	// Lacy-inspired features
	termCtx        *sessioncapture.TerminalContext
	inputIndicator *InputIndicator
	ghostText      *GhostText
	modeManager    *shellmode.ModeManager
	brailleSpinner *BrailleSpinner
	// testStreamStarter overrides the async stream launcher in tests that
	// manually inject stream events and need deterministic cleanup.
	testStreamStarter func()

	// BMAD/Aeon-inspired features
	hintsLoader  *engine.HintsLoader
	sourceRoots  *engine.SourceRoots
	selfImprover *engine.SelfImprover
	codingSoul   *engine.CodingSoul

	// Loop cancellation
	loopCancel context.CancelFunc // cancels the current /loop goroutine

	// Parallel agents cancellation
	parallelCancel context.CancelFunc // cancels running /parallel agents

	// Background goroutine cancellation
	bgCancel context.CancelFunc // cancels all background goroutines on quit

	// PageRank file watcher
	watcherStop func() // stops the incremental symbol graph file watcher

	// Command palette (Ctrl+K)
	commandPalette *CommandPalette
	autonomyPicker *AutonomyPicker
	specPicker     *SpecPicker
	themePicker    *ThemePicker

	// Input history search (Ctrl+R) — overlay for searching through
	// previous inputs, similar to bash reverse-i-search.
	historySearchOpen     bool
	historySearchInput    string
	historySearchQuery    string
	historySearchFiltered []string
	historySearchSel      int

	// Session picker (Ctrl+S) — fuzzy search through saved sessions
	// with context preview for quick session switching.
	sessionPickerOpen     bool
	sessionPickerInput    string
	sessionPickerEntries  []session.Entry
	sessionPickerFiltered []session.Entry
	sessionPickerSel      int
	// sessionPickerDetail caches the selected session's share detail (deeplink +
	// export path) so it is computed once per selection, not per render frame.
	sessionPickerDetailID     string
	sessionPickerDetailCached string
}

const streamRenderInterval = 50 * time.Millisecond

// maxDisplayMessages bounds the number of messages kept in memory for display.
// Older messages are trimmed to prevent unbounded memory growth in long sessions.
// The session file still contains the full history for /resume.
const maxDisplayMessages = 500

// messageTrimThreshold is the point at which we start trimming old messages.
// We trim in batches to avoid frequent reallocations.
const messageTrimThreshold = 450

// pushHistory records a submitted prompt in the bounded domain history.
func (m *chatModel) pushHistory(text string) {
	m.history.Add(text)
}

// maxQueuedMessages bounds the queue of prompts entered while the agent is
// working (M18: it grew without bound during long turns). The oldest queued
// prompts are dropped first so the most recent intent is preserved.
const maxQueuedMessages = 100

// enqueueMessage queues a prompt entered while the agent is working,
// dropping the oldest entries past the cap.
func (m *chatModel) enqueueMessage(text string) {
	if len(m.messageQueue) >= maxQueuedMessages {
		m.messageQueue = append(m.messageQueue[:0], m.messageQueue[1:]...)
	}
	m.messageQueue = append(m.messageQueue, text)
}

// trimOldMessages removes old messages when the count exceeds the threshold.
// Keeps the most recent messages and shows a hint about trimmed history.
func (m *chatModel) trimOldMessages() {
	if len(m.messages) <= messageTrimThreshold {
		return
	}
	projected := make([]chatfeature.Message, 0, len(m.messages))
	for _, message := range m.messages {
		projected = append(projected, chatfeature.Message{Role: message.role, Content: message.content})
	}
	trimmed, trimCount := chatfeature.TrimMessages(projected, messageTrimThreshold, maxDisplayMessages, "welcome")
	if trimCount == 0 {
		return
	}
	kept := make([]displayMsg, 0, len(trimmed))
	for _, message := range trimmed {
		kept = append(kept, displayMsg{role: message.Role, content: message.Content})
	}
	m.messages = kept
	startIdx := 0
	if len(projected) > 0 && projected[0].Role == "welcome" {
		startIdx = 1
	}
	// Expansion state is keyed by message index; reindex the survivors so
	// Enter-to-expand keeps targeting the right messages and stale keys for
	// trimmed messages are pruned (M18: the map grew without bound).
	m.toolResultExpanded = reindexExpandedMap(m.toolResultExpanded, startIdx, trimCount)
	m.invalidateViewportCache()
}

// reindexExpandedMap maps tool-result expansion state across
// trimOldMessages' reindex: indices below startIdx are untouched, trimmed
// indices are pruned, and survivors above the trim shift down by
// trimCount-1 because the trim hint takes one slot.
func reindexExpandedMap(expanded map[int]bool, startIdx, trimCount int) map[int]bool {
	reindexed := make(map[int]bool, len(expanded))
	for idx, expandedState := range expanded {
		switch {
		case idx < startIdx:
			reindexed[idx] = expandedState
		case idx >= startIdx+trimCount:
			reindexed[idx-trimCount+1] = expandedState
		}
	}
	return reindexed
}

func (m *chatModel) markPartialDirty() tea.Cmd {
	m.partialDirty = true
	if time.Since(m.lastPartialRender) >= streamRenderInterval {
		m.viewDirty = true
		m.lastPartialRender = time.Now()
		m.partialDirty = false
		m.partialRenderPending = false
		return nil
	}
	if m.partialRenderPending {
		return nil
	}
	m.partialRenderPending = true
	return tea.Tick(streamRenderInterval, func(time.Time) tea.Msg { return streamRenderTickMsg{} })
}

func (m *chatModel) flushPartialDirty() {
	if m.partialDirty {
		m.viewDirty = true
		m.partialDirty = false
	}
	m.partialRenderPending = false
}

func spinnerVerbTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return spinnerVerbTickMsg{} })
}

func promptKeepAliveCmd() tea.Cmd {
	return tea.Tick(15*time.Second, func(time.Time) tea.Msg { return promptKeepAliveMsg{} })
}

func eyeBlinkTickCmd() tea.Cmd {
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return eyeBlinkTickMsg{} })
}

func eyeFrameNextCmd(frame int, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return eyeFrameNextMsg{frame: frame} })
}

func permissionPromptTimeoutCmd(seq int) tea.Cmd {
	return tea.Tick(interactivePromptTimeout, func(time.Time) tea.Msg { return permissionPromptTimeoutMsg{seq: seq} })
}

func approvalPromptTimeoutCmd(seq int) tea.Cmd {
	return tea.Tick(interactivePromptTimeout, func(time.Time) tea.Msg { return approvalPromptTimeoutMsg{seq: seq} })
}

func askUserPromptTimeoutCmd(seq int) tea.Cmd {
	return tea.Tick(interactivePromptTimeout, func(time.Time) tea.Msg { return askUserPromptTimeoutMsg{seq: seq} })
}

func credentialPromptTimeoutCmd(seq int) tea.Cmd {
	return tea.Tick(interactivePromptTimeout, func(time.Time) tea.Msg { return credentialPromptTimeoutMsg{seq: seq} })
}

func promptCountdownTickCmd(generation uint64) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return promptCountdownTickMsg{generation: generation}
	})
}
