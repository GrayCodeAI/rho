package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/GrayCodeAI/rho/internal/bridge/sessioncapture"
	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	"github.com/GrayCodeAI/rho/internal/engine"
	chatfeature "github.com/GrayCodeAI/rho/internal/features/chat"
	"github.com/GrayCodeAI/rho/internal/features/shellmode"
	"github.com/GrayCodeAI/rho/internal/provider/gateway"
	"github.com/GrayCodeAI/rho/internal/session"
	"github.com/GrayCodeAI/rho/internal/storage"
	"github.com/GrayCodeAI/rho/internal/tool"
)

func newTestChatModel() *chatModel {
	sess := engine.NewSession("", "test-model", "you are helpful", nil)
	_ = sess.SetMaxTurns(1)
	sess.SetTestClient(engine.NewMockClientForTest())

	m := &chatModel{
		input:             textarea.New(),
		viewport:          viewport.New(viewport.WithWidth(120), viewport.WithHeight(12)),
		session:           sess,
		registry:          tool.NewRegistry(),
		partial:           &strings.Builder{},
		sessionID:         "test-session",
		width:             120,
		height:            40,
		ref:               &progRef{},
		modeManager:       shellmode.NewModeManager(),
		termCtx:           sessioncapture.NewTerminalContext(),
		ghostText:         NewGhostText(),
		inputIndicator:    &InputIndicator{},
		hintsLoader:       engine.NewHintsLoader(),
		selfImprover:      engine.NewSelfImprover(),
		codingSoul:        engine.LoadCodingSoul(),
		brailleSpinner:    NewBrailleSpinner(SpinnerRho, "Thinking"),
		testStreamStarter: func() {},
	}
	return m
}

func TestNewTestChatModel_DisablesAsyncStreamLauncher(t *testing.T) {
	m := newTestChatModel()
	m.startStream()
	if m.cancel != nil {
		t.Fatal("test model should not start a background stream")
	}
}

func isolateChatCommandSweepEnv(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	storage.SetTestDirs(t, root)
	isolateCredentialHome(t)
	rhoconfig.InvalidateConfigUICache()
	gateway.SetDefaultStore(&gateway.MapStore{})
	restoreThemeGlobals(t)
	t.Cleanup(func() {
		gateway.SetDefaultStore(nil)
		rhoconfig.InvalidateConfigUICache()
	})
}

// restoreThemeGlobals snapshots every package-level color var that
// ApplyTheme mutates and restores it when the test finishes, so commands
// like "/theme dark" cannot leak themed globals into unrelated tests
// (e.g. TestAdaptiveNeutralsPreserveDarkAppearance).
func restoreThemeGlobals(t *testing.T) {
	t.Helper()
	savedRho, savedSuccess, savedWarn, savedErr, savedInfo := rhoColor, successTeal, warnAmber, errorCoral, infoSky
	savedTool, savedAgent, savedDone, savedFileHeader := toolGold, agentGold, doneGreen, fileHeaderBlue
	savedInspect, savedEdit, savedRun, savedTrust := tierInspect, tierEdit, tierRun, tierTrust
	savedHudBorder, savedHudLabel := hudBorderPurple, hudLabelPink
	savedHUDColorBorder, savedHUDColorHeader, savedHUDColorLabel, savedHUDColorDim := hudBorderColor, hudHeaderColor, hudLabelColor, hudDimHUDColor
	savedHUDStyles := [4]lipgloss.Style{hudHeaderStyle, hudLabelStyle, hudDimHUDStyle, hudSectionStyle}
	savedCost, savedBranch, savedToken, savedCwd := costViolet, branchYellow, tokenSage, cwdBlue
	savedANSI := [10]string{ansiTeal, ansiCoral, ansiAmber, ansiGrayDim, ansiDone, ansiSky, ansiFileBlue, ansiPink, ansiLightPink, ansiVividGreen}
	savedPrimary, savedMuted, savedPlaceholder, savedDisabled := textPrimary, textMuted, textPlaceholder, textDisabled
	savedBorderDim, savedBgCode := borderDim, bgCode
	savedTextWhite, savedPermissionBg := textWhite, permissionBg
	savedInputBorder := inputBorderStyle
	savedStatusCWD, savedStatusBranch, savedStatusSpec := statusCWDColor, statusBranchColor, statusSpecColor
	savedStatusToken, savedStatusCost, savedStatusPR := statusTokenColor, statusCostColor, statusPRColor
	savedStatusCwdStyle, savedStatusPRStyle := statusCwdStyle, statusPRStyle
	savedStatusBranchStyle, savedStatusSpecStyle := statusBranchStyle, statusSpecStyle
	savedStatusTokenStyle, savedStatusCostStyle := statusTokenStyle, statusCostStyle
	savedStatusClockStyle, savedStatusFocusStyle := statusClockStyle, statusFocusStyle
	savedStatusDimStyle, savedDryRunStyle := statusDimStyle, dryRunStyle
	savedMinimalChrome := minimalChrome
	hasDark := lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	t.Cleanup(func() {
		rhoColor, successTeal, warnAmber, errorCoral, infoSky = savedRho, savedSuccess, savedWarn, savedErr, savedInfo
		toolGold, agentGold, doneGreen, fileHeaderBlue = savedTool, savedAgent, savedDone, savedFileHeader
		tierInspect, tierEdit, tierRun, tierTrust = savedInspect, savedEdit, savedRun, savedTrust
		hudBorderPurple, hudLabelPink = savedHudBorder, savedHudLabel
		hudBorderColor, hudHeaderColor, hudLabelColor, hudDimHUDColor = savedHUDColorBorder, savedHUDColorHeader, savedHUDColorLabel, savedHUDColorDim
		hudHeaderStyle, hudLabelStyle, hudDimHUDStyle, hudSectionStyle = savedHUDStyles[0], savedHUDStyles[1], savedHUDStyles[2], savedHUDStyles[3]
		costViolet, branchYellow, tokenSage, cwdBlue = savedCost, savedBranch, savedToken, savedCwd
		ansiTeal, ansiCoral, ansiAmber, ansiGrayDim, ansiDone, ansiSky, ansiFileBlue, ansiPink, ansiLightPink, ansiVividGreen = savedANSI[0], savedANSI[1], savedANSI[2], savedANSI[3], savedANSI[4], savedANSI[5], savedANSI[6], savedANSI[7], savedANSI[8], savedANSI[9]
		textPrimary, textMuted, textPlaceholder, textDisabled = savedPrimary, savedMuted, savedPlaceholder, savedDisabled
		borderDim, bgCode = savedBorderDim, savedBgCode
		textWhite, permissionBg = savedTextWhite, savedPermissionBg
		// ApplyTheme also rewrites the input border and chrome mode; restore
		// them so a palette swap cannot leak into layout tests.
		inputBorderStyle = savedInputBorder
		statusCWDColor, statusBranchColor, statusSpecColor = savedStatusCWD, savedStatusBranch, savedStatusSpec
		statusTokenColor, statusCostColor, statusPRColor = savedStatusToken, savedStatusCost, savedStatusPR
		statusCwdStyle, statusPRStyle = savedStatusCwdStyle, savedStatusPRStyle
		statusBranchStyle, statusSpecStyle = savedStatusBranchStyle, savedStatusSpecStyle
		statusTokenStyle, statusCostStyle = savedStatusTokenStyle, savedStatusCostStyle
		statusClockStyle, statusFocusStyle = savedStatusClockStyle, savedStatusFocusStyle
		statusDimStyle, dryRunStyle = savedStatusDimStyle, savedDryRunStyle
		minimalChrome = savedMinimalChrome
		// HasDarkBackground is now a function (no args), SetHasDarkBackground removed in v2
		_ = hasDark
	})
}

func TestChatModel_SlashHelp(t *testing.T) {
	m := newTestChatModel()
	result, _ := m.handleCommand("/help")
	if result == nil {
		t.Fatal("handleCommand(/help) returned nil model")
	}
	cm := result.(*chatModel)
	if len(cm.messages) == 0 {
		t.Error("/help should add a system message")
	}
}

func TestChatModel_SlashVersion(t *testing.T) {
	SetVersion("1.0.0-test")
	m := newTestChatModel()
	result, _ := m.handleCommand("/version")
	cm := result.(*chatModel)
	found := false
	for _, msg := range cm.messages {
		if strings.Contains(msg.content, "1.0.0") {
			found = true
			break
		}
	}
	if !found {
		t.Error("/version should display version string")
	}
}

func TestChatModel_SlashClear(t *testing.T) {
	m := newTestChatModel()
	m.messages = append(m.messages, displayMsg{role: "user", content: "hello"})
	m.messages = append(m.messages, displayMsg{role: "assistant", content: "hi"})

	result, _ := m.handleCommand("/clear")
	cm := result.(*chatModel)
	if len(cm.messages) > 1 {
		t.Errorf("/clear should clear messages, got %d", len(cm.messages))
	}
}

func TestFormatQuitResumeMessage(t *testing.T) {
	got := formatQuitResumeMessage("44418bdd52745678")
	want := "Thank you for using Rho!\n\nTo resume this session, run: rho --resume 44418bdd52745678\n"
	if got != want {
		t.Fatalf("quit message mismatch:\nwant %q\ngot  %q", want, got)
	}
}

func TestFormatQuitResumeMessage_NoSession(t *testing.T) {
	got := formatQuitResumeMessage("")
	want := "Thank you for using Rho!\n"
	if got != want {
		t.Fatalf("quit message mismatch:\nwant %q\ngot  %q", want, got)
	}
}

func TestChatModel_SlashModel(t *testing.T) {
	m := newTestChatModel()
	result, _ := m.handleCommand("/model")
	cm := result.(*chatModel)
	if len(cm.messages) == 0 && !cm.configOpen {
		t.Error("/model should either add a message or open config")
	}
}

func TestChatModel_SlashCost(t *testing.T) {
	m := newTestChatModel()
	result, _ := m.handleCommand("/cost")
	cm := result.(*chatModel)
	if len(cm.messages) == 0 {
		t.Error("/cost should add a message")
	}
}

func TestChatModel_SlashTokens(t *testing.T) {
	m := newTestChatModel()
	result, _ := m.handleCommand("/tokens")
	cm := result.(*chatModel)
	if len(cm.messages) == 0 {
		t.Error("/tokens should add a message")
	}
}

func TestChatModel_SlashTools(t *testing.T) {
	m := newTestChatModel()
	result, _ := m.handleCommand("/tools")
	cm := result.(*chatModel)
	if len(cm.messages) == 0 {
		t.Error("/tools should list tools")
	}
}

func TestChatModel_SlashStatus(t *testing.T) {
	m := newTestChatModel()
	result, _ := m.handleCommand("/status")
	cm := result.(*chatModel)
	if len(cm.messages) == 0 {
		t.Error("/status should show session info")
	}
}

func TestChatModel_SlashUnknown(t *testing.T) {
	m := newTestChatModel()
	result, _ := m.handleCommand("/nonexistent-command-xyz")
	cm := result.(*chatModel)
	found := false
	for _, msg := range cm.messages {
		if strings.Contains(msg.content, "unknown") || strings.Contains(msg.content, "Unknown") || msg.role == "error" {
			found = true
			break
		}
	}
	if !found {
		t.Error("/nonexistent should show unknown command message")
	}
}

func TestChatModel_ManyCommands(t *testing.T) {
	commands := []string{
		"/context", "/env", "/hooks", "/stats",
		"/compact", "/diff", "/branch", "/vim",
		"/power", "/fast", "/effort",
		"/memory", "/plugins", "/mcp",
		"/autonomy",
		"/usage", "/metrics", "/integrity",
		"/keybindings", "/cron", "/tasks",
		"/files", "/branches", "/provider-status",
		"/output-style plain",
		"/copy", "/export", "/fork",
		"/rewind", "/undo", "/taste",
		"/theme dark", "/btw hello",
		"/focus src/", "/pin",
		"/rename test-session",
		"/tag important", "/color green",
		"/clean", "/clear", "/cost",
		"/drop main.go", "/history",
		"/model", "/new", "/quit",
		"/session", "/share", "/skills",
		"/snapshot", "/stale", "/status",
		"/statusline", "/tokens", "/tools",
		"/upgrade", "/version", "/welcome",
		"/yolo", "/voice", "/agents",
		"/audit",
		"/release-notes", "/reload-plugins",
		"/remote-env", "/render",
		"/add main.go", "/add-dir .",
		"/compress", "/loop",
		"/feedback", "/plugin",
		"/pr-comments", "/thinkback",
		"/think-back", "/thinkback-play",
	}

	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			isolateChatCommandSweepEnv(t)
			m := newTestChatModel()
			result, _ := m.handleCommand(cmd)
			if result == nil {
				t.Errorf("%s returned nil model", cmd)
			}
			cm := requireChatModel(t, result)
			if cm.cancel != nil {
				cm.cancel()
			}
			if cm.loopCancel != nil {
				cm.loopCancel()
			}
			time.Sleep(10 * time.Millisecond)
		})
	}
}

func TestChatModel_SlashNew(t *testing.T) {
	m := newTestChatModel()
	m.messages = append(m.messages, displayMsg{role: "user", content: "old"})
	result, _ := m.handleCommand("/new")
	if result == nil {
		t.Error("/new returned nil")
	}
}

func TestChatModel_SlashCopy(t *testing.T) {
	m := newTestChatModel()
	m.messages = append(m.messages, displayMsg{role: "assistant", content: "copy this"})
	result, _ := m.handleCommand("/copy")
	if result == nil {
		t.Error("/copy returned nil")
	}
}

func TestChatModel_SlashExport(t *testing.T) {
	m := newTestChatModel()
	m.messages = append(m.messages, displayMsg{role: "user", content: "hello"})
	result, _ := m.handleCommand("/export")
	if result == nil {
		t.Error("/export returned nil")
	}
}

func TestChatModel_SaveSessionPersistsPersistenceMessages(t *testing.T) {
	isolateChatCommandSweepEnv(t)
	m := newTestChatModel()
	m.session.AddUser("hello")
	m.session.AddAssistant("hi")

	m.saveSession()

	saved, err := session.Load(m.sessionID)
	if err != nil {
		t.Fatalf("Load(%q) after saveSession() error = %v", m.sessionID, err)
	}
	if len(saved.Messages) != 2 {
		t.Fatalf("saved messages = %d, want 2", len(saved.Messages))
	}
	if saved.Messages[0].Role != "user" || saved.Messages[0].Content != "hello" {
		t.Fatalf("saved.Messages[0] = %#v, want user hello", saved.Messages[0])
	}
	if saved.Messages[1].Role != "assistant" || saved.Messages[1].Content != "hi" {
		t.Fatalf("saved.Messages[1] = %#v, want assistant hi", saved.Messages[1])
	}
}

func TestChatModel_SlashExportRedactsAndPrivatizesFile(t *testing.T) {
	isolateChatCommandSweepEnv(t)
	m := newTestChatModel()
	secret := "sk-1234567890abcdefghijklmnop"
	m.session.AddUser("my key is " + secret)

	result, _ := m.handleCommand("/export")
	cm := requireChatModel(t, result)
	last := cm.messages[len(cm.messages)-1]
	if !strings.Contains(last.content, "Exported to:") {
		t.Fatalf("export message = %q", last.content)
	}
	exportPath := filepath.Join(storage.StateDir(), "exports", cm.sessionID+".md")
	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatalf("export contains unredacted secret: %s", data)
	}
	if !strings.Contains(string(data), "[REDACTED]") {
		t.Fatalf("export missing redaction marker: %s", data)
	}
	info, err := os.Stat(exportPath)
	if err != nil {
		t.Fatalf("stat export: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("export file mode = %v, want 0600", got)
	}
}

func TestChatModel_StreamingCommands(t *testing.T) {
	// These trigger startStream; cancel promptly so the test doesn't leak workers.
	commands := []string{
		"/doctor", "/commit", "/review",
		"/summary", "/security-review",
		"/bughunter", "/check", "/hunt",
		"/design", "/ultrareview",
	}

	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			isolateChatCommandSweepEnv(t)
			m := newTestChatModel()
			m.session.AddUser("some context for the command")
			result, _ := m.handleCommand(cmd)
			if result == nil {
				t.Errorf("%s returned nil model", cmd)
			}
			cm := requireChatModel(t, result)
			if cm.cancel != nil {
				cm.cancel()
			}
			time.Sleep(10 * time.Millisecond)
		})
	}
}

func TestChatModel_PushHistoryCapsAtMax(t *testing.T) {
	m := newTestChatModel()
	for i := 0; i < chatfeature.DefaultHistoryLimit+50; i++ {
		m.pushHistory(fmt.Sprintf("prompt-%d", i))
	}
	entries := m.history.Entries()
	if len(entries) != chatfeature.DefaultHistoryLimit {
		t.Fatalf("history len = %d, want %d", len(entries), chatfeature.DefaultHistoryLimit)
	}
	if entries[0] != "prompt-50" || entries[len(entries)-1] != fmt.Sprintf("prompt-%d", chatfeature.DefaultHistoryLimit+49) {
		t.Fatalf("history did not keep the most recent prompts: first=%q last=%q", entries[0], entries[len(entries)-1])
	}
	if m.history.Index() != len(entries) {
		t.Fatalf("history index = %d, want %d", m.history.Index(), len(entries))
	}
}

func TestChatModel_EnqueueMessageCapsAtMax(t *testing.T) {
	m := newTestChatModel()
	for i := 0; i < maxQueuedMessages+25; i++ {
		m.enqueueMessage(fmt.Sprintf("queued-%d", i))
	}
	if len(m.messageQueue) != maxQueuedMessages {
		t.Fatalf("queue len = %d, want %d", len(m.messageQueue), maxQueuedMessages)
	}
	if m.messageQueue[0] != "queued-25" {
		t.Fatalf("oldest queued prompt not dropped: first=%q", m.messageQueue[0])
	}
}

func TestChatModel_ReindexExpandedMap(t *testing.T) {
	// startIdx=1 (welcome preserved), trimCount=3: old idx 4 → 2, old idx 8 → 6.
	expanded := map[int]bool{0: true, 1: true, 3: false, 4: true, 8: true}
	got := reindexExpandedMap(expanded, 1, 3)
	want := map[int]bool{0: true, 2: true, 6: true}
	if len(got) != len(want) {
		t.Fatalf("reindexed map len = %d, want %d: %v", len(got), len(want), got)
	}
	for idx, state := range want {
		if got[idx] != state {
			t.Fatalf("reindexed[%d] = %v, want %v", idx, got[idx], state)
		}
	}
}
