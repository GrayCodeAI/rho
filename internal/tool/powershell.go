package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// PowerShellTool executes PowerShell commands (Windows/cross-platform pwsh).
type PowerShellTool struct{}

// PowerShellInput is the typed input for PowerShellTool.
type PowerShellInput struct {
	Command string `json:"command"`
	Timeout int64  `json:"timeout"`
}

func (PowerShellTool) Name() string      { return "PowerShell" }
func (PowerShellTool) RiskLevel() string { return "high" }
func (PowerShellTool) Aliases() []string { return []string{"powershell"} }
func (PowerShellTool) Description() string {
	return "Execute a PowerShell command. Use this instead of Bash when running on Windows or when PowerShell-specific cmdlets are needed."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (PowerShellTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"command": {Type: "string", Description: "The PowerShell command to execute"},
			"timeout": {Type: "number", Description: "Timeout in milliseconds (max 600000, default 120000)"},
		},
		Required: []string{"command"},
	}
}

func (PowerShellTool) Parameters() map[string]interface{} {
	return powershellSchema.ToJSONSchema()
}

// powershellSchema is the single source of truth for PowerShell's input schema.
var powershellSchema = PowerShellTool{}.Schema()

var (
	powershellDestructiveRe = regexp.MustCompile(`(?i)(?:^|[\s;&|])(?:remove-item|ri|del|erase|rd|rmdir|clear-content|clear-item|format-volume|clear-disk|stop-computer|restart-computer)\b`)
	powershellSuspiciousRe  = regexp.MustCompile(`(?i)(?:^|[;&|])\s*(?:iex|invoke-expression)\b|\b(?:invoke-webrequest|iwr|invoke-restmethod|irm|start-bitstransfer|set-executionpolicy)\b|\b(?:start-process|saps)\b[^\n;&|]*-verb\s+runas\b`)
)

// IsPowerShellDestructive reports PowerShell cmdlets that delete data, alter
// storage, or stop/restart the host. These are hard-denied independently of
// autonomy and remembered approvals.
func IsPowerShellDestructive(command string) bool {
	return powershellDestructiveRe.MatchString(command)
}

// IsPowerShellSuspicious identifies PowerShell constructs that are not covered
// by Bash's command heuristics. These constructs can evaluate arbitrary code,
// fetch remote content, alter execution policy, or elevate a child process.
// Keep this classifier shared by the policy layer and the executor.
func IsPowerShellSuspicious(command string) bool {
	return IsPowerShellDestructive(command) || powershellSuspiciousRe.MatchString(command)
}

func (PowerShellTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[PowerShellInput]("PowerShell", input)
	if err != nil {
		return "", err
	}
	if p.Command == "" {
		return "", fmt.Errorf("command is required")
	}
	// Safety: check for destructive commands (same as Bash tool)
	if IsDestructiveCommand(p.Command) || IsPowerShellDestructive(p.Command) {
		return "", fmt.Errorf("command blocked: contains a destructive pattern")
	}
	if IsSuspicious(p.Command) || IsPowerShellSuspicious(p.Command) {
		return "", fmt.Errorf("command blocked: flagged as suspicious")
	}

	timeout := 120 * time.Second
	if p.Timeout > 0 {
		if p.Timeout > 600_000 {
			p.Timeout = 600_000
		}
		timeout = time.Duration(p.Timeout) * time.Millisecond
	}

	shell := findPowerShell()
	if shell == "" {
		return "", fmt.Errorf("PowerShell not found (install pwsh for cross-platform support)")
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, shell, "-NoProfile", "-NonInteractive", "-Command", p.Command) // #nosec G204 -- shell invocation with user-supplied command
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	result := stdout.String()
	if stderr.Len() > 0 {
		if result != "" {
			result += "\n"
		}
		result += stderr.String()
	}

	if ctx.Err() == context.DeadlineExceeded {
		return result + "\n(command timed out)", nil
	}
	if err != nil && result == "" {
		return "", fmt.Errorf("powershell error: %w", err)
	}

	const maxOutput = 200_000
	if len(result) > maxOutput {
		half := maxOutput / 2
		result = result[:half] + "\n...(output truncated)...\n" + result[len(result)-half:]
	}

	return strings.TrimRight(result, "\n"), nil
}

func findPowerShell() string {
	// Prefer pwsh (PowerShell Core / cross-platform)
	if path, err := exec.LookPath("pwsh"); err == nil {
		return path
	}
	// Fall back to Windows PowerShell
	if runtime.GOOS == "windows" {
		if path, err := exec.LookPath("powershell.exe"); err == nil {
			return path
		}
	}
	return ""
}

// IsPowerShellAvailable returns whether a PowerShell runtime is available.
func IsPowerShellAvailable() bool {
	return findPowerShell() != ""
}
