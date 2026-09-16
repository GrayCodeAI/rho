package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/GrayCodeAI/rho/internal/terminal"
)

func resolveStore(custom *terminal.Store) *terminal.Store {
	if custom != nil {
		return custom
	}
	return terminal.DefaultStore()
}

// TerminalCreateTool spawns a persistent interactive PTY terminal.
type TerminalCreateTool struct {
	Store *terminal.Store
}

func (TerminalCreateTool) Name() string { return "TerminalCreate" }

// TerminalCreateInput is the typed input for TerminalCreateTool.
type TerminalCreateInput struct {
	Command   string `json:"command"`
	CWD       string `json:"cwd"`
	Rows      int    `json:"rows"`
	Cols      int    `json:"cols"`
	SessionID string `json:"session_id"`
}

func (TerminalCreateTool) Aliases() []string { return []string{"terminal_create", "pty_create"} }
func (TerminalCreateTool) Description() string {
	return "Spawn a persistent interactive PTY terminal session whose state persists across tool calls."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (TerminalCreateTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"command":    {Type: "string", Description: "Shell or command to run (defaults to system shell e.g. /bin/bash or powershell)"},
			"cwd":        {Type: "string", Description: "Working directory for the terminal session"},
			"rows":       {Type: "integer", Description: "Initial terminal rows (default 24)"},
			"cols":       {Type: "integer", Description: "Initial terminal columns (default 80)"},
			"session_id": {Type: "string", Description: "Session ID establishing ownership for this terminal"},
		},
	}
}

func (TerminalCreateTool) Parameters() map[string]interface{} {
	return terminalCreateSchema.ToJSONSchema()
}

// terminalCreateSchema is the single source of truth for TerminalCreate's input schema.
var terminalCreateSchema = TerminalCreateTool{}.Schema()

func (t TerminalCreateTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var p TerminalCreateInput
	if len(input) > 0 {
		decoded, err := DecodeInput[TerminalCreateInput]("TerminalCreate", input)
		if err != nil {
			return "", err
		}
		p = decoded
	}

	// A non-empty command is handed to a shell, so it must pass the same
	// static safety stack as BashTool. An empty command just spawns the
	// user's default shell and needs no command validation.
	if p.Command != "" {
		if err := validateShellCommand(ctx, p.Command); err != nil {
			return "", err
		}
	}

	sessionID := strings.TrimSpace(p.SessionID)
	if sessionID == "" {
		sessionID = "default"
	}
	rows := p.Rows
	if rows <= 0 {
		rows = 24
	}
	cols := p.Cols
	if cols <= 0 {
		cols = 80
	}

	term, err := resolveStore(t.Store).Create(ctx, sessionID, p.CWD, p.Command, rows, cols)
	if err != nil {
		return "", fmt.Errorf("failed to create terminal: %w", err)
	}

	res, _ := json.Marshal(map[string]any{
		"terminal_id": term.ID,
		"session_id":  term.SessionID,
		"cwd":         term.CWD,
		"message":     fmt.Sprintf("Terminal %s created successfully.", term.ID),
	})
	return string(res), nil
}

// TerminalSendTool writes user input to an active terminal.
type TerminalSendTool struct {
	Store *terminal.Store
}

func (TerminalSendTool) Name() string { return "TerminalSend" }

// TerminalSendInput is the typed input for TerminalSendTool.
type TerminalSendInput struct {
	TerminalID string `json:"terminal_id"`
	Input      string `json:"input"`
	SendEnter  *bool  `json:"send_enter"`
	SessionID  string `json:"session_id"`
}

func (TerminalSendTool) Aliases() []string { return []string{"terminal_send", "pty_send"} }
func (TerminalSendTool) Description() string {
	return "Send input characters or commands to an active persistent terminal."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (TerminalSendTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"terminal_id": {Type: "string", Description: "Branded terminal identifier (e.g. 'terminal-1')"},
			"input":       {Type: "string", Description: "Characters, keystrokes, or command string to send to stdin"},
			"send_enter":  {Type: "boolean", Description: "Whether to append a newline (Enter) at the end of input (default true)"},
			"session_id":  {Type: "string", Description: "Owner session ID for authorization"},
		},
		Required: []string{"terminal_id", "input"},
	}
}

func (TerminalSendTool) Parameters() map[string]interface{} {
	return terminalSendSchema.ToJSONSchema()
}

// terminalSendSchema is the single source of truth for TerminalSend's input schema.
var terminalSendSchema = TerminalSendTool{}.Schema()

func (t TerminalSendTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[TerminalSendInput]("TerminalSend", input)
	if err != nil {
		return "", err
	}

	if p.TerminalID == "" {
		return "", fmt.Errorf("terminal_id is required")
	}

	// Input is written to a live shell, so block destructive commands and
	// nested AST dangers before it reaches stdin.
	if err := validateTerminalInput(p.Input); err != nil {
		return "", err
	}

	sessionID := strings.TrimSpace(p.SessionID)
	if sessionID == "" {
		sessionID = "default"
	}

	term, err := resolveStore(t.Store).Get(sessionID, p.TerminalID)
	if err != nil {
		return "", err
	}

	enter := true
	if p.SendEnter != nil {
		enter = *p.SendEnter
	}

	if err := term.Send(p.Input, enter); err != nil {
		return "", fmt.Errorf("failed to send input: %w", err)
	}

	res, _ := json.Marshal(map[string]any{
		"terminal_id": p.TerminalID,
		"status":      "ok",
	})
	return string(res), nil
}

// TerminalReadTool reads bounded output from an active terminal.
type TerminalReadTool struct {
	Store *terminal.Store
}

func (TerminalReadTool) Name() string { return "TerminalRead" }

// TerminalReadInput is the typed input for TerminalReadTool.
type TerminalReadInput struct {
	TerminalID string `json:"terminal_id"`
	MaxBytes   int    `json:"max_bytes"`
	TimeoutMS  int    `json:"timeout_ms"`
	SessionID  string `json:"session_id"`
}

func (TerminalReadTool) Aliases() []string { return []string{"terminal_read", "pty_read"} }
func (TerminalReadTool) Description() string {
	return "Read newly emitted output from an active persistent terminal with an optional timeout."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (TerminalReadTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"terminal_id": {Type: "string", Description: "Branded terminal identifier (e.g. 'terminal-1')"},
			"max_bytes":   {Type: "integer", Description: "Maximum bytes to read (default 65536)"},
			"timeout_ms":  {Type: "integer", Description: "Milliseconds to wait for output if buffer is empty (default 500ms)"},
			"session_id":  {Type: "string", Description: "Owner session ID for authorization"},
		},
		Required: []string{"terminal_id"},
	}
}

func (TerminalReadTool) Parameters() map[string]interface{} {
	return terminalReadSchema.ToJSONSchema()
}

// terminalReadSchema is the single source of truth for TerminalRead's input schema.
var terminalReadSchema = TerminalReadTool{}.Schema()

func (t TerminalReadTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[TerminalReadInput]("TerminalRead", input)
	if err != nil {
		return "", err
	}

	if p.TerminalID == "" {
		return "", fmt.Errorf("terminal_id is required")
	}

	sessionID := strings.TrimSpace(p.SessionID)
	if sessionID == "" {
		sessionID = "default"
	}

	term, err := resolveStore(t.Store).Get(sessionID, p.TerminalID)
	if err != nil {
		return "", err
	}

	timeout := 500 * time.Millisecond
	if p.TimeoutMS > 0 {
		timeout = time.Duration(p.TimeoutMS) * time.Millisecond
	}

	out, alive, err := term.Read(p.MaxBytes, timeout)
	if err != nil {
		return "", fmt.Errorf("failed to read terminal: %w", err)
	}

	res, _ := json.Marshal(map[string]any{
		"terminal_id": p.TerminalID,
		"output":      out,
		"alive":       alive,
	})
	return string(res), nil
}

// TerminalListTool lists active persistent terminals for the calling session.
type TerminalListTool struct {
	Store *terminal.Store
}

func (TerminalListTool) Name() string { return "TerminalList" }

// TerminalListInput is the typed input for TerminalListTool.
type TerminalListInput struct {
	SessionID string `json:"session_id"`
}

func (TerminalListTool) Aliases() []string { return []string{"terminal_list", "pty_list"} }
func (TerminalListTool) Description() string {
	return "List active persistent PTY terminals owned by the current session."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (TerminalListTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"session_id": {Type: "string", Description: "Session ID to filter terminals by"},
		},
	}
}

func (TerminalListTool) Parameters() map[string]interface{} {
	return terminalListSchema.ToJSONSchema()
}

// terminalListSchema is the single source of truth for TerminalList's input schema.
var terminalListSchema = TerminalListTool{}.Schema()

func (t TerminalListTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	var p TerminalListInput
	if len(input) > 0 && string(input) != "null" {
		decoded, err := DecodeInput[TerminalListInput]("TerminalList", input)
		if err != nil {
			return "", err
		}
		p = decoded
	}

	sessionID := strings.TrimSpace(p.SessionID)
	if sessionID == "" {
		sessionID = "default"
	}

	terms := resolveStore(t.Store).List(sessionID)
	res, _ := json.Marshal(map[string]any{
		"terminals": terms,
		"count":     len(terms),
	})
	return string(res), nil
}

// TerminalResizeTool resizes an active terminal PTY window.
type TerminalResizeTool struct {
	Store *terminal.Store
}

func (TerminalResizeTool) Name() string { return "TerminalResize" }

// TerminalResizeInput is the typed input for TerminalResizeTool.
type TerminalResizeInput struct {
	TerminalID string `json:"terminal_id"`
	Rows       int    `json:"rows"`
	Cols       int    `json:"cols"`
	SessionID  string `json:"session_id"`
}

func (TerminalResizeTool) Aliases() []string { return []string{"terminal_resize", "pty_resize"} }
func (TerminalResizeTool) Description() string {
	return "Resize the rows and columns of an active persistent PTY terminal."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (TerminalResizeTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"terminal_id": {Type: "string", Description: "Branded terminal identifier"},
			"rows":        {Type: "integer", Description: "New terminal row count"},
			"cols":        {Type: "integer", Description: "New terminal column count"},
			"session_id":  {Type: "string", Description: "Owner session ID for authorization"},
		},
		Required: []string{"terminal_id", "rows", "cols"},
	}
}

func (TerminalResizeTool) Parameters() map[string]interface{} {
	return terminalResizeSchema.ToJSONSchema()
}

// terminalResizeSchema is the single source of truth for TerminalResize's input schema.
var terminalResizeSchema = TerminalResizeTool{}.Schema()

func (t TerminalResizeTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[TerminalResizeInput]("TerminalResize", input)
	if err != nil {
		return "", err
	}

	if p.TerminalID == "" {
		return "", fmt.Errorf("terminal_id is required")
	}

	sessionID := strings.TrimSpace(p.SessionID)
	if sessionID == "" {
		sessionID = "default"
	}

	term, err := resolveStore(t.Store).Get(sessionID, p.TerminalID)
	if err != nil {
		return "", err
	}

	if err := term.Resize(p.Rows, p.Cols); err != nil {
		return "", fmt.Errorf("failed to resize terminal: %w", err)
	}

	res, _ := json.Marshal(map[string]any{
		"terminal_id": p.TerminalID,
		"rows":        p.Rows,
		"cols":        p.Cols,
		"status":      "ok",
	})
	return string(res), nil
}

// TerminalKillTool terminates an active terminal and frees resources.
type TerminalKillTool struct {
	Store *terminal.Store
}

func (TerminalKillTool) Name() string { return "TerminalKill" }

// TerminalKillInput is the typed input for TerminalKillTool.
type TerminalKillInput struct {
	TerminalID string `json:"terminal_id"`
	SessionID  string `json:"session_id"`
}

func (TerminalKillTool) Aliases() []string { return []string{"terminal_kill", "pty_kill"} }
func (TerminalKillTool) Description() string {
	return "Terminate an active persistent terminal session."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (TerminalKillTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"terminal_id": {Type: "string", Description: "Branded terminal identifier to terminate"},
			"session_id":  {Type: "string", Description: "Owner session ID for authorization"},
		},
		Required: []string{"terminal_id"},
	}
}

func (TerminalKillTool) Parameters() map[string]interface{} {
	return terminalKillSchema.ToJSONSchema()
}

// terminalKillSchema is the single source of truth for TerminalKill's input schema.
var terminalKillSchema = TerminalKillTool{}.Schema()

func (t TerminalKillTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[TerminalKillInput]("TerminalKill", input)
	if err != nil {
		return "", err
	}

	if p.TerminalID == "" {
		return "", fmt.Errorf("terminal_id is required")
	}

	sessionID := strings.TrimSpace(p.SessionID)
	if sessionID == "" {
		sessionID = "default"
	}

	if err := resolveStore(t.Store).Delete(sessionID, p.TerminalID); err != nil {
		return "", err
	}

	res, _ := json.Marshal(map[string]any{
		"terminal_id": p.TerminalID,
		"status":      "killed",
	})
	return string(res), nil
}
