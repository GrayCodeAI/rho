package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"

	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	"github.com/GrayCodeAI/rho/internal/tool"
	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

type welcomeMCPStub struct {
	name   string
	server string
}

// TestWelcomeScreenNerdIconsUnique renders the full welcome in Nerd mode
// for every execution state and asserts each PUA icon glyph appears at most
// once. Guards the "one icon per concept" rule on the welcome screen so the
// mode/iso/trust segments and the badge never reuse a glyph.
func TestWelcomeScreenNerdIconsUnique(t *testing.T) {
	icons.SetMode(icons.ModeNerd)
	defer icons.SetMode(icons.ModeASCII)

	out := buildWelcomeMessage(nil, "", nil, nil, rhoconfig.Settings{}, 0, false, 100, 24)
	seen := make(map[rune]struct{})
	for _, r := range out {
		if r < 0xE000 || r > 0xF8FF {
			continue
		}
		if _, dup := seen[r]; dup {
			t.Fatalf("PUA glyph %U reused on welcome screen:\n%s", r, out)
		}
		seen[r] = struct{}{}
	}
}

func (s welcomeMCPStub) Name() string                       { return s.name }
func (s welcomeMCPStub) Description() string                { return "test tool" }
func (s welcomeMCPStub) Parameters() map[string]interface{} { return nil }
func (s welcomeMCPStub) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}
func (s welcomeMCPStub) MCPServerName() string { return s.server }

func TestBuildWelcomeMessage_InlineShowsSetupGuidance(t *testing.T) {
	out := buildWelcomeMessage(nil, "", nil, nil, rhoconfig.Settings{}, 0, false, 100, 24)
	if !strings.Contains(out, "v") {
		t.Fatalf("inline welcome should show version, got:\n%s", out)
	}
	if strings.Contains(out, "WELCOME TO") {
		t.Fatalf("inline welcome should not render the old gate banner, got:\n%s", out)
	}
}

func TestBuildWelcomeMessage_InlineShowsGuidance(t *testing.T) {
	out := buildWelcomeMessage(nil, "", nil, nil, rhoconfig.Settings{}, 0, false, 100, 24)
	for _, want := range []string{"Skills (0)", "AGENTS.md", "MCPs (0)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("minimal welcome missing %q in:\n%s", want, out)
		}
	}
	for _, wantIcon := range []string{icons.Robot(), icons.Network()} {
		if !strings.Contains(out, wantIcon) {
			t.Fatalf("minimal welcome missing semantic icon %q in:\n%s", wantIcon, out)
		}
	}
	for mode, rowIcons := range map[string][]string{
		"active": {icons.Bolt(), icons.Robot(), icons.Network()},
		"nerd":   {icons.Nerd("bolt"), icons.Nerd("robot"), icons.Nerd("network")},
		"ascii":  {icons.ASCII("bolt"), icons.ASCII("robot"), icons.ASCII("network")},
	} {
		seenIcons := make(map[string]struct{}, len(rowIcons))
		for _, icon := range rowIcons {
			if _, exists := seenIcons[icon]; exists {
				t.Fatalf("%s welcome-row icon %q is reused; Skills, AGENTS.md, and MCPs must be unique", mode, icon)
			}
			seenIcons[icon] = struct{}{}
		}
	}
	for concept, footerIcon := range map[string]string{
		"tokens":   icons.Database(),
		"cost":     icons.Ruby(),
		"duration": icons.ClockOutline(),
		"branch":   icons.Branch(),
	} {
		if icons.Network() == footerIcon {
			t.Fatalf("MCP and footer %s must use distinct icons", concept)
		}
	}
	for _, noise := range []string{"TIP:", "ctrl+N", "/config", "Esc to dismiss"} {
		if strings.Contains(out, noise) {
			t.Fatalf("minimal welcome should omit %q, got:\n%s", noise, out)
		}
	}
	for _, guidance := range []string{"Inspect, change, or test this codebase with rho.", `Try: "explain this repo"`} {
		if !strings.Contains(out, guidance) {
			t.Fatalf("minimal welcome missing first-run guidance %q in:\n%s", guidance, out)
		}
	}
}

func TestBuildWelcomeMessage_ShortTerminalUsesCompactCopy(t *testing.T) {
	out := buildWelcomeMessage(nil, "", nil, nil, rhoconfig.Settings{}, 0, false, 72, 20)
	if strings.Contains(out, "PgUp/Dn scroll chat") || strings.Contains(out, "for new session") {
		t.Fatalf("compact welcome should drop verbose descriptions, got:\n%s", out)
	}
	if !strings.Contains(out, "v") {
		t.Fatalf("compact welcome should keep version and execution mode, got:\n%s", out)
	}
}

func TestBuildWelcomeMessage_WideTerminalUsesRhoWordmark(t *testing.T) {
	out := buildWelcomeMessage(nil, "", nil, nil, rhoconfig.Settings{}, 0, false, 120, 40)
	for _, want := range []string{
		" ____  _   _  ___ ",
		"|  _ \\| | | |/ _ \\ ",
		"|_| \\_\\_| |_|\\___/",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("wide welcome missing rho wordmark line %q in:\n%s", want, out)
		}
	}
}

func TestBuildWelcomeMessage_RhoWordmarkIsStatic(t *testing.T) {
	out := buildWelcomeMessage(nil, "", nil, nil, rhoconfig.Settings{}, 0, true, 120, 40)
	if !strings.Contains(out, " ____  _   _  ___ ") {
		t.Fatalf("blinking welcome should retain the RHO wordmark, got:\n%s", out)
	}
}

func TestBuildWelcomeMessage_GraphicalMascotKeepsTextFallback(t *testing.T) {
	out := buildWelcomeMessageWithSnapshotAndMascot(nil, "", nil, nil, rhoconfig.Settings{}, 0, 0, 0, 120, 40, welcomeStatusSnapshot{}, "", true)
	if !strings.Contains(out, " ____  _   _  ___ ") {
		t.Fatalf("graphical mascot mode must retain the text wordmark fallback, got:\n%s", out)
	}
}

func TestEyeBlinkTick_CyclesEyeFrameStates(t *testing.T) {
	m := chatModel{input: textarea.New(), width: 100, height: 40}
	m.rebuildWelcomeCache()
	next, cmd := m.Update(eyeBlinkTickMsg{})
	nextModel := next.(chatModel)
	if nextModel.eyeFrame != 1 {
		t.Fatalf("eyeBlinkTickMsg eyeFrame = %d, want 1", nextModel.eyeFrame)
	}
	if cmd == nil {
		t.Fatal("eyeBlinkTickMsg should return next commands")
	}

	next2, _ := nextModel.Update(eyeFrameNextMsg{frame: 2})
	nextModel2 := next2.(chatModel)
	if nextModel2.eyeFrame != 2 {
		t.Fatalf("eyeFrameNextMsg frame 2 eyeFrame = %d, want 2", nextModel2.eyeFrame)
	}

	next3, _ := nextModel2.Update(eyeFrameNextMsg{frame: 3})
	nextModel3 := next3.(chatModel)
	if nextModel3.eyeFrame != 3 {
		t.Fatalf("eyeFrameNextMsg frame 3 eyeFrame = %d, want 3", nextModel3.eyeFrame)
	}

	next4, _ := nextModel3.Update(eyeFrameNextMsg{frame: 0})
	nextModel4 := next4.(chatModel)
	if nextModel4.eyeFrame != 0 {
		t.Fatalf("eyeFrameNextMsg frame 0 eyeFrame = %d, want 0", nextModel4.eyeFrame)
	}
}

func TestWelcomeIndicatorRow_UsesSemanticStatesAndCounts(t *testing.T) {
	tests := []struct {
		name        string
		skillsCount int
		agentsOK    bool
		mcpCount    int
		want        []string
	}{
		{
			name: "nothing configured",
			want: []string{"Skills (0)</active> <none>", "AGENTS.md</active> <none>", "MCPs (0)</active> <none>"},
		},
		{
			name:        "active counts",
			skillsCount: 4,
			agentsOK:    true,
			mcpCount:    1,
			want:        []string{"Skills (4)</active> <ready>", "AGENTS.md</active> <ready>", "MCPs (1)</active> <ready>"},
		},
		{
			name:        "mixed state",
			skillsCount: 2,
			mcpCount:    3,
			want:        []string{"Skills (2)</active> <ready>", "AGENTS.md</active> <none>", "MCPs (3)</active> <ready>"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := welcomeIndicatorRow(tc.skillsCount, tc.agentsOK, tc.mcpCount, "<active>", "<idle>", "</active>", "<ready>", "<none>")
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("welcomeIndicatorRow() missing %q in %q", want, got)
				}
			}
		})
	}

	active := welcomeIndicatorRow(1, true, 1, "<active>", "<idle>", "</active>", "<ready>", "<none>")
	if strings.Count(active, ansiBold) != 3 {
		t.Fatalf("welcomeIndicatorRow() should bold all three semantic icons, got %q", active)
	}
}

func TestConnectedMCPCount_CountsDistinctUsableServers(t *testing.T) {
	registry := tool.NewRegistry(
		welcomeMCPStub{name: "alpha-one", server: "alpha"},
		welcomeMCPStub{name: "alpha-two", server: "alpha"},
		welcomeMCPStub{name: "beta-one", server: "beta"},
		welcomeMCPStub{name: "not-mcp"},
	)

	if got := connectedMCPCount(registry); got != 2 {
		t.Fatalf("connectedMCPCount() = %d, want 2 distinct connected servers", got)
	}
	if got := connectedMCPCount(nil); got != 0 {
		t.Fatalf("connectedMCPCount(nil) = %d, want 0", got)
	}
}
