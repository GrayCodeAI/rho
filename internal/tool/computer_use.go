package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ComputerUseTool exposes host desktop automation to the agent through a
// pluggable backend. Adopted from Prime Agent / Orca computer-use: the tool
// surface (snapshot, click, type, scroll, press, screenshot) is provider-
// neutral; a host wires the actual backend (e.g. a native macOS accessibility
// backend via SetComputerBackend). Without a wired backend the tool reports a
// clear error — the seam is the deliverable.
type ComputerUseTool struct{}

func (ComputerUseTool) Name() string      { return "ComputerUse" }
func (ComputerUseTool) RiskLevel() string { return "high" }
func (ComputerUseTool) Aliases() []string { return []string{"computer_use", "computer"} }
func (ComputerUseTool) Description() string {
	return "Operate the host desktop (snapshot UI, click, type, scroll, keypress, screenshot) via a pluggable backend. Requires a wired computer backend (see SetComputerBackend)."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (ComputerUseTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action": {Type: "string", Enum: []interface{}{"snapshot", "click", "type", "scroll", "press", "screenshot"}, Description: "snapshot: dump the UI; click: click an element/ref; type: enter text; scroll: scroll; press: send key chord; screenshot: capture screen."},
			"target": {Type: "string", Description: "Element ref (e.g. @e1) or label for click/type/scroll."},
			"text":   {Type: "string", Description: "Text to type or key chord for press."},
		},
		Required: []string{"action"},
	}
}

func (ComputerUseTool) Parameters() map[string]interface{} {
	return computerUseSchema.ToJSONSchema()
}

// computerUseSchema is the single source of truth for ComputerUse's input schema.
var computerUseSchema = ComputerUseTool{}.Schema()

// ComputerBackend is the pluggable host-desktop automation backend.
type ComputerBackend interface {
	// Name identifies the backend for provenance.
	Name() string
	// Snapshot returns a representation of the current UI.
	Snapshot(ctx context.Context) (string, error)
	// Click activates the element identified by ref.
	Click(ctx context.Context, ref string) error
	// Type enters text into the focused field.
	Type(ctx context.Context, text string) error
	// Scroll scrolls (direction: up/down/left/right).
	Scroll(ctx context.Context, ref, direction string) error
	// Press sends a key chord (e.g. cmd+k).
	Press(ctx context.Context, chord string) error
	// Screenshot captures the screen and returns a path or data URL.
	Screenshot(ctx context.Context) (string, error)
}

var computerBackend ComputerBackend

// SetComputerBackend installs the host-desktop backend. Nil by default; the
// tool reports a clear error until wired.
func SetComputerBackend(b ComputerBackend) { computerBackend = b }

// ComputerBackendName returns the active backend name, or "" when none.
func ComputerBackendName() string {
	if computerBackend == nil {
		return ""
	}
	return computerBackend.Name()
}

func (ComputerUseTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var p struct {
		Action    string `json:"action"`
		Target    string `json:"target"`
		Text      string `json:"text"`
		Direction string `json:"direction"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	p.Action = strings.ToLower(strings.TrimSpace(p.Action))
	if p.Action == "" {
		return "", fmt.Errorf("action is required")
	}
	if computerBackend == nil {
		return "", fmt.Errorf("no computer backend installed — wire one via SetComputerBackend")
	}
	switch p.Action {
	case "snapshot":
		out, err := computerBackend.Snapshot(ctx)
		if err != nil {
			return "", fmt.Errorf("computer snapshot: %w", err)
		}
		return out, nil
	case "click":
		if p.Target == "" {
			return "", fmt.Errorf("click requires a target ref")
		}
		if err := computerBackend.Click(ctx, p.Target); err != nil {
			return "", fmt.Errorf("computer click: %w", err)
		}
		return fmt.Sprintf("Clicked %s", p.Target), nil
	case "type":
		if p.Text == "" {
			return "", fmt.Errorf("type requires text")
		}
		if err := computerBackend.Type(ctx, p.Text); err != nil {
			return "", fmt.Errorf("computer type: %w", err)
		}
		return fmt.Sprintf("Typed %d characters", len(p.Text)), nil
	case "scroll":
		if err := computerBackend.Scroll(ctx, p.Target, p.Direction); err != nil {
			return "", fmt.Errorf("computer scroll: %w", err)
		}
		return "Scrolled", nil
	case "press":
		if p.Text == "" {
			return "", fmt.Errorf("press requires a key chord")
		}
		if err := computerBackend.Press(ctx, p.Text); err != nil {
			return "", fmt.Errorf("computer press: %w", err)
		}
		return fmt.Sprintf("Pressed %s", p.Text), nil
	case "screenshot":
		out, err := computerBackend.Screenshot(ctx)
		if err != nil {
			return "", fmt.Errorf("computer screenshot: %w", err)
		}
		return out, nil
	default:
		return "", fmt.Errorf("unsupported action %q (use snapshot, click, type, scroll, press, or screenshot)", p.Action)
	}
}
