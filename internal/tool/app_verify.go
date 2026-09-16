package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/GrayCodeAI/rho/internal/appverify"
)

// AppVerifyTool implements the "prove it works" workflow: detect a recipe,
// persist it as the project manifest contract, and boot-smoke the app with
// bounded readiness polling. It complements ProjectVerify (build/test/lint)
// by covering the part that actually proves the app runs.
type AppVerifyTool struct{}

// AppVerifyInput is the typed input for AppVerifyTool.
type AppVerifyInput struct {
	Action           string `json:"action"`
	Path             string `json:"path"`
	ReadinessSeconds int    `json:"readiness_seconds"`
}

func (AppVerifyTool) Name() string      { return "AppVerify" }
func (AppVerifyTool) RiskLevel() string { return "medium" }
func (AppVerifyTool) Aliases() []string { return []string{"app-verify", "verify_app"} }
func (AppVerifyTool) Description() string {
	return "Detect how this project boots and prove it runs: infer an install/build/test/start recipe, persist it to .rho/verify/environment.json as the verification contract, and run a bounded boot smoke check with readiness polling. Use action=detect to inspect, action=manifest to write/update the contract, action=smoke to boot the app and verify readiness."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (AppVerifyTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action":            {Type: "string", Enum: []interface{}{"detect", "manifest", "smoke"}, Description: "detect infers the recipe; manifest loads-or-detects and persists .rho/verify/environment.json; smoke boots the app using the recipe's start command and polls readiness."},
			"path":              {Type: "string", Description: "Project directory (default: session working directory)."},
			"readiness_seconds": {Type: "integer", Minimum: 1, Maximum: 300, Description: "Max seconds to wait for the app to become ready during smoke (default 60)."},
		},
		Required: []string{"action"},
	}
}

func (AppVerifyTool) Parameters() map[string]interface{} {
	return appVerifySchema.ToJSONSchema()
}

// appVerifySchema is the single source of truth for AppVerify's input schema.
var appVerifySchema = AppVerifyTool{}.Schema()

type smokeResult struct {
	Status    string `json:"status"` // passed | failed | skipped
	StartCmd  string `json:"start_command,omitempty"`
	SmokeKind string `json:"smoke_kind"`
	Target    string `json:"smoke_target,omitempty"`
	Duration  string `json:"duration,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (AppVerifyTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	params, err := DecodeInput[AppVerifyInput]("AppVerify", input)
	if err != nil {
		return "", err
	}
	switch params.Action {
	case "detect", "manifest", "smoke":
	default:
		return "", fmt.Errorf("unsupported action %q (use detect, manifest, or smoke)", params.Action)
	}

	root := params.Path
	if root == "" {
		if tc := GetToolContext(ctx); tc != nil && tc.WorkingDir != "" {
			root = tc.WorkingDir
		} else {
			var err error
			root, err = os.Getwd()
			if err != nil {
				return "", fmt.Errorf("resolve working directory: %w", err)
			}
		}
	}
	if err := validatePathAllowed(ctx, root); err != nil {
		return "", err
	}

	switch params.Action {
	case "detect":
		return encodeJSON(appverify.Detect(root))
	case "manifest":
		r, existed, err := appverify.LoadOrDetect(root)
		if err != nil {
			return "", err
		}
		path := appverify.ManifestPath(root)
		source := "detected now"
		if existed {
			source = "loaded existing"
		}
		out, err := encodeJSON(map[string]interface{}{"source": source, "path": path, "recipe": r})
		if err != nil {
			return "", err
		}
		return out, nil
	default: // smoke
		return runBootSmoke(ctx, root, params.ReadinessSeconds)
	}
}

// runBootSmoke starts the recipe's start command, polls readiness until it
// succeeds or the budget expires, then always stops the process. Only fixed
// argv lists from the (normalized) recipe are executed — no shell.
func runBootSmoke(ctx context.Context, root string, readinessSeconds int) (string, error) {
	r, _, err := appverify.LoadOrDetect(root)
	if err != nil {
		return "", err
	}
	res := smokeResult{SmokeKind: string(r.SmokeKind), Target: r.SmokeTarget()}
	if len(r.Start) == 0 {
		res.Status = "skipped"
		res.Error = "no start command in recipe; run action=manifest after confirming how the app boots"
		return encodeJSON(res)
	}
	res.StartCmd = joinArgs(r.Start)
	if readinessSeconds <= 0 {
		readinessSeconds = 60
	}
	if readinessSeconds > 300 {
		readinessSeconds = 300
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(readinessSeconds+30)*time.Second)
	defer cancel()
	started := time.Now()

	cmd := exec.CommandContext(runCtx, r.Start[0], r.Start[1:]...) // #nosec G204 -- fixed argv from normalized recipe; no shell
	cmd.Dir = root
	if err := cmd.Start(); err != nil {
		res.Status = "failed"
		res.Error = fmt.Sprintf("start failed: %v", err)
		return encodeJSON(res)
	}
	// Always tear the process down before returning.
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}()

	ready := false
	switch r.SmokeKind {
	case appverify.SmokeHTTP:
		client := &http.Client{Timeout: 2 * time.Second}
		deadline := time.Now().Add(time.Duration(readinessSeconds) * time.Second)
		for time.Now().Before(deadline) {
			req, err := http.NewRequestWithContext(runCtx, http.MethodGet, res.Target, nil)
			if err != nil {
				res.Error = fmt.Sprintf("build readiness request: %v", err)
				break
			}
			resp, err := client.Do(req)
			if err == nil {
				_ = resp.Body.Close()
				ready = true
				break
			}
			select {
			case <-runCtx.Done():
			case <-time.After(500 * time.Millisecond):
			}
		}
	default:
		// CLI/none: reaching here without the process exiting early is the best
		// available signal for long-lived commands; short-lived ones are judged
		// by Wait below.
		ready = true
	}

	exitErr := cmd.Wait()
	res.Duration = time.Since(started).Round(time.Millisecond).String()
	switch {
	case r.SmokeKind == appverify.SmokeHTTP && ready && exitErr == nil:
		res.Status = "passed"
	case r.SmokeKind == appverify.SmokeCLI && exitErr == nil:
		res.Status = "passed"
	case exitErr != nil:
		res.Status = "failed"
		ee := &exec.ExitError{}
		if errors.As(exitErr, &ee) {
			res.Error = fmt.Sprintf("app exited with code %d before/during readiness", ee.ExitCode())
		} else {
			res.Error = exitErr.Error()
		}
	default:
		res.Status = "failed"
		res.Error = "readiness not observed within budget"
	}
	return encodeJSON(res)
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}
