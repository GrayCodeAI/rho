package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/GrayCodeAI/rho/internal/engine/safety"

	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
	"github.com/GrayCodeAI/rho/internal/errhint"
	"github.com/GrayCodeAI/rho/internal/multiagent/agents"
	"github.com/GrayCodeAI/rho/internal/notify"
	"github.com/GrayCodeAI/rho/internal/observability/logger"
	cloud "github.com/GrayCodeAI/rho/internal/platform/cloud"
	"github.com/GrayCodeAI/rho/internal/plugin"
	"github.com/GrayCodeAI/rho/internal/session"
	"github.com/GrayCodeAI/rho/internal/ui/icons"
	"github.com/spf13/cobra"
)

var (
	execOutputFormat string
	execAutoLevel    string
	execModel        string
	execMaxTurns     int
	execCWD          string
	execAgent        string
	execSessionID    string
	execTag          string
	execWorktree     bool
	execWorktreeName string
	execFanout       int
	execEphemeral    bool
	execJSON         bool
)

// ExitCodeError wraps a non-zero exit code so it can be returned from RunE
// instead of calling os.Exit directly. This allows deferred cleanup (worktree
// removal, global state restoration) to run before the process exits.
type ExitCodeError struct {
	Code int
	Err  error
}

func (e *ExitCodeError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit code %d", e.Code)
}

func (e *ExitCodeError) Unwrap() error { return e.Err }

// ExecResult is the structured output for --output-format json.
type ExecResult struct {
	SessionID  string `json:"session_id"`
	Response   string `json:"response"`
	ExitCode   int    `json:"exit_code"`
	TokensIn   int    `json:"tokens_in,omitempty"`
	TokensOut  int    `json:"tokens_out,omitempty"`
	TurnsTaken int    `json:"turns_taken"`
	Duration   string `json:"duration"`
	Model      string `json:"model,omitempty"`
	Worktree   string `json:"worktree,omitempty"`
	Branch     string `json:"branch,omitempty"`
}

var execCmd = &cobra.Command{
	Use:   "exec [prompt]",
	Short: "Execute a single command non-interactively",
	Long: `Execute a prompt in non-interactive mode for scripting and CI/CD.

Supports piping from stdin, JSON output format, and autonomy levels.

Use --ephemeral to skip session persistence (ideal for CI runs).
Use --json for JSON output (alias for --output-format json).
Use --output-format stream-json for newline-delimited progress events.

Autonomy Levels:
  supervised (default)  Ask for permission on every tool call
  basic                 Auto-allow read-only tools
  semi                  Auto-allow reads and writes, ask for Bash
  full                  Auto-allow everything except destructive commands
  yolo                  Never ask for permission

Examples:
  rho exec "analyze this codebase"
  rho exec --auto full "fix the tests and commit"
  rho exec --json "what files are in src/"
  rho exec --ephemeral --json "run tests and report" > result.json
  echo "explain main.go" | rho exec -
  rho exec --agent reviewer "review the latest commit"
  rho exec --model claude-sonnet-4-6 "quick fix: typo in README"`,
	Args: cobra.MaximumNArgs(1),
	RunE: runExec,
}

func init() {
	execCmd.Flags().StringVarP(&execOutputFormat, "output-format", "o", "text", "Output format: text, json, or stream-json")
	execCmd.Flags().StringVar(&execAutoLevel, "auto", "", "Autonomy level: supervised|basic|semi|full|yolo")
	execCmd.Flags().StringVarP(&execModel, "model", "m", "", "Model ID to use")
	execCmd.Flags().IntVar(&execMaxTurns, "max-turns", 0, "Maximum agentic turns (0 = unlimited)")
	execCmd.Flags().StringVar(&execCWD, "cwd", "", "Working directory")
	execCmd.Flags().StringVar(&execAgent, "agent", "", "Agent persona to use (from Rho user state)")
	execCmd.Flags().StringVarP(&execSessionID, "session-id", "s", "", "Continue an existing session")
	execCmd.Flags().StringVar(&execTag, "tag", "", "Session tag for categorization")
	execCmd.Flags().BoolVarP(&execWorktree, "worktree", "w", false, "Run in an isolated git worktree")
	execCmd.Flags().IntVar(&execFanout, "fanout", 0, "Best-of-N: run the same prompt N times (2-5) in parallel-kept worktrees and print a comparison report; pick the winner to merge")
	execCmd.Flags().StringVar(&execWorktreeName, "worktree-name", "", "Branch name for worktree (auto-generated if empty)")
	execCmd.Flags().BoolVar(&execEphemeral, "ephemeral", false, "Skip session persistence (CI mode)")
	execCmd.Flags().BoolVarP(&execJSON, "json", "j", false, "JSON output (alias for --output-format json)")
}

func runExec(_ *cobra.Command, args []string) error {
	start := time.Now()

	if execJSON {
		execOutputFormat = "json"
	}
	if execOutputFormat != "text" && execOutputFormat != "json" && execOutputFormat != "stream-json" {
		return fmt.Errorf("--output-format must be one of: text, json, stream-json")
	}

	prompt, err := resolveExecPrompt(args)
	if err != nil {
		return err
	}

	// GitHub Actions integration: when running inside a runner, derive the
	// prompt and mode (interactive vs automation) from the triggering event.
	ghaCtx := detectGitHubActions(os.Getenv, os.ReadFile)
	if ghaCtx.Active {
		if ghaCtx.Prompt != "" {
			prompt = ghaCtx.Prompt
		}
		if ghaCtx.Mode == GHAModeInteractive {
			prompt = "Respond conversationally to the following GitHub comment that mentioned you:\n\n" + prompt
		}
	}

	// Skill dispatch: a prompt beginning with "/" is a slash-command skill
	// invocation. Expand it into the skill's instructions before running.
	if isSkillDispatch(prompt) {
		expanded, derr := dispatchSkill(defaultSkillRunner, prompt)
		if derr != nil {
			return derr
		}
		prompt = expanded
	}

	// Best-of-N fan-out: run the same prompt in N isolated worktrees and
	// print a comparison report so the winner can be picked and merged.
	if execFanout > 1 {
		return runExecFanout(prompt, execFanout)
	}

	if execCWD != "" {
		if chdirErr := os.Chdir(execCWD); chdirErr != nil {
			return fmt.Errorf("chdir %s: %w", execCWD, chdirErr)
		}
	}

	// Worktree isolation: create a temporary worktree and chdir into it
	var wtPath, wtBranch string
	if execWorktree {
		cwd, _ := os.Getwd()
		base := getCurrentBranch(cwd)
		branch := execWorktreeName
		if branch == "" {
			branch = fmt.Sprintf("rho-exec/%d-%s", start.UnixMilli(), randomHex(4))
		}
		var wtErr error
		wtPath, wtErr = createExecWorktree(cwd, base, branch)
		if wtErr != nil {
			return fmt.Errorf("worktree: %w", wtErr)
		}
		wtBranch = branch
		defer cleanupExecWorktree(cwd, wtPath)
		if chdirErr := os.Chdir(wtPath); chdirErr != nil {
			return fmt.Errorf("chdir worktree: %w", chdirErr)
		}
	}

	// Load settings
	settings := rhoconfig.LoadSettings()

	// Build system prompt
	systemPrompt, err := buildSystemPrompt()
	if err != nil {
		return err
	}

	// If --agent is specified, prepend the agent persona
	var agentModel string
	if execAgent != "" {
		agentDef, lookupErr := agents.Get(execAgent)
		if lookupErr != nil {
			return fmt.Errorf("agent %q: %w", execAgent, lookupErr)
		}
		systemPrompt = agentDef.Prompt + "\n\n" + systemPrompt
		agentModel = agentDef.Model
	}

	// Create tool registry
	registry, err := defaultRegistry(settings)
	if err != nil {
		return err
	}

	// Resolve effective model/provider without mutating globals.
	// Priority: agent model > exec model > settings > global > auto-detect.
	effectiveModel, effectiveProvider := effectiveModelAndProvider(settings)
	if execModel != "" {
		effectiveModel = execModel
	}
	if agentModel != "" {
		effectiveModel = agentModel
	}

	// Create engine session
	sess, cfgErr := newConfiguredRhoSession(settings, effectiveProvider, effectiveModel, systemPrompt, registry, logger.New(io.Discard, logger.Error), execMaxTurns)
	if cfgErr != nil {
		return cfgErr
	}
	if _, err := os.Getwd(); err != nil {
		return fmt.Errorf("resolve project directory: %w", err)
	}

	// Apply autonomy level
	if execAutoLevel != "" {
		sess.PermSvc().SetAutonomy(safety.ParseAutonomyLevel(execAutoLevel))
	}

	// Prompt-injection guard: when the prompt originates from an untrusted
	// GitHub Actions event (an outside contributor's issue/PR/comment body),
	// clamp autonomy to read-only auto-approval so attacker-controlled text
	// cannot drive writes or Bash. Maintainers can opt out with
	// RHO_GHA_TRUST_EVENT=1.
	if ghaCtx.Active && !ghaCtx.Trusted {
		const ceiling = safety.AutonomyBasic
		if sess.PermSvc().RuntimeState().Autonomy > ceiling {
			fmt.Fprintf(os.Stderr, "%s\n", auditTint(fmt.Sprintf("rho: untrusted GitHub event (author_association=%q); capping autonomy at %s", ghaCtx.AuthorAssociation, ceiling), warnAmber))
			sess.PermSvc().SetAutonomy(ceiling)
		}
	}

	// Exec has no interactive approval UI. The central PermissionEngine already
	// auto-allows calls permitted by the active profile; reaching this callback
	// means a prompt is required, so fail closed instead of re-implementing
	// autonomy with a second policy implementation.
	sess.PermSvc().SetPermissionFn(func(req safety.PermissionRequest) {
		if req.Response != nil {
			req.Response <- false
		}
	})

	// Resume existing session if --session-id provided
	if execSessionID != "" {
		saved, lookupErr := session.Load(execSessionID)
		if lookupErr != nil {
			return fmt.Errorf("resume session %s: %w", execSessionID, lookupErr)
		}
		sess.LoadMessages(session.ToRuntimeMessages(saved.Messages))
	}

	// Add the user prompt
	sess.AddUser(prompt)

	// Run the agent loop
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	events, err := sess.Stream(ctx)
	if err != nil {
		return fmt.Errorf("stream: %w", err)
	}

	// Collect response
	var response strings.Builder
	var totalIn, totalOut, turns, cacheRead, cacheWrite int
	var execErr string
	jsonEnc := json.NewEncoder(os.Stdout)

	for ev := range events {
		switch ev.Type {
		case "content":
			response.WriteString(ev.Content)
			if execOutputFormat == "text" {
				fmt.Print(ev.Content)
			}
			if execOutputFormat == "stream-json" {
				_ = jsonEnc.Encode(map[string]interface{}{
					"type":    "content",
					"content": ev.Content,
				})
			}
		case "usage":
			if ev.Usage != nil {
				totalIn += ev.Usage.PromptTokens
				totalOut += ev.Usage.CompletionTokens
				cacheRead += ev.Usage.CacheReadTokens
				cacheWrite += ev.Usage.CacheWriteTokens
				turns++
			}
		case "error":
			execErr = ev.Content
			if execOutputFormat == "text" {
				_, _ = fmt.Fprintf(os.Stderr, "\n%s\n", auditTint("error: "+ev.Content, errorCoral))
				if h := errhint.CLIHint(errors.New(ev.Content)); h != "" {
					_, _ = fmt.Fprintf(os.Stderr, "%s\n", auditTint("  hint: "+h, textMuted))
				}
			}
			if execOutputFormat == "stream-json" {
				_ = jsonEnc.Encode(map[string]interface{}{
					"type":    "error",
					"content": ev.Content,
				})
			}
		case "tool_use":
			if execOutputFormat == "text" && !IsQuiet() {
				_, _ = fmt.Fprintf(os.Stderr, "\n%s\n", auditTint("["+ev.ToolName+"]", infoSky))
			}
			if execOutputFormat == "stream-json" {
				_ = jsonEnc.Encode(map[string]interface{}{
					"type": "tool_use",
					"tool": ev.ToolName,
				})
			}
		case "tool_result":
			if execOutputFormat == "text" && !IsQuiet() {
				content := ev.Content
				if utf8.RuneCountInString(content) > 500 {
					content = string([]rune(content)[:500]) + "..."
				}
				_, _ = fmt.Fprintf(os.Stderr, "%s %s\n", auditTint("["+ev.ToolName+"]", infoSky), content)
			}
			if execOutputFormat == "stream-json" {
				_ = jsonEnc.Encode(map[string]interface{}{
					"type":   "tool_result",
					"tool":   ev.ToolName,
					"result": ev.Content,
				})
			}
		case "done":
			if execOutputFormat == "stream-json" {
				_ = jsonEnc.Encode(map[string]interface{}{
					"type": "done",
				})
			}
		}
	}

	exitCode := 0
	if execErr != "" {
		exitCode = 1
	}
	sessionID := fmt.Sprintf("exec-%d-%s", start.UnixMilli(), randomHex(4))

	// Optional cloud accounting is best-effort and never affects local execution.
	if client, cfg, loadErr := cloud.LoadClient(); loadErr == nil && client.Enabled() {
		go client.RecordUsage(context.Background(), cloud.UsageEvent{
			EventID:      fmt.Sprintf("exec-%d-%s", start.UnixMilli(), randomHex(8)),
			DeviceID:     cfg.DeviceID,
			ProjectID:    cfg.ProjectID,
			SessionID:    sessionID,
			Capability:   "rho",
			Model:        effectiveModel,
			InputTokens:  totalIn,
			OutputTokens: totalOut,
			TokensUsed:   totalIn + totalOut,
			DurationMS:   int(time.Since(start).Milliseconds()),
			Status:       map[bool]string{true: "failed", false: "completed"}[exitCode != 0],
			OccurredAt:   time.Now().UTC().Format(time.RFC3339),
		})
	}

	// Persist session for resume/search (skip in ephemeral/CI mode)
	if !execEphemeral {
		persistExecSession(sessionID, effectiveModel, effectiveProvider, prompt, response.String())
	}

	if execOutputFormat == "text" {
		if !strings.HasSuffix(response.String(), "\n") {
			fmt.Println()
		}
		if !IsQuiet() {
			summary := fmt.Sprintf("rho: %d tokens in / %d out · %d turn(s) · %s · %s",
				totalIn, totalOut, turns, time.Since(start).Round(time.Millisecond), effectiveModel)
			if cacheRead > 0 || cacheWrite > 0 {
				summary = fmt.Sprintf("rho: %d tokens in / %d out (cache %d read · %d write) · %d turn(s) · %s · %s",
					totalIn, totalOut, cacheRead, cacheWrite, turns, time.Since(start).Round(time.Millisecond), effectiveModel)
			}
			fmt.Fprintf(os.Stderr, "%s\n", auditTint(summary, textMuted))
		}
		if exitCode != 0 {
			return fmt.Errorf("exec failed: %s", execErr)
		}
		return nil
	}

	if execOutputFormat == "json" || execOutputFormat == "stream-json" {
		result := ExecResult{
			SessionID:  sessionID,
			Response:   response.String(),
			ExitCode:   exitCode,
			TokensIn:   totalIn,
			TokensOut:  totalOut,
			TurnsTaken: turns,
			Duration:   time.Since(start).Round(time.Millisecond).String(),
			Model:      effectiveModel,
			Worktree:   wtPath,
			Branch:     wtBranch,
		}
		if execOutputFormat == "stream-json" {
			_ = jsonEnc.Encode(map[string]interface{}{
				"type":   "result",
				"result": result,
			})
		}
		if execOutputFormat == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		}
	}

	if exitCode != 0 {
		return &ExitCodeError{Code: exitCode}
	}
	return nil
}

func resolveExecPrompt(args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("prompt is required (provide as argument or pipe via stdin with '-')")
	}
	if args[0] == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	return args[0], nil
}

// --- GitHub Actions integration ---------------------------------------------

// GHAMode is the operating mode derived from a GitHub Actions event.
type GHAMode string

const (
	// GHAModeNone means we are not running inside GitHub Actions.
	GHAModeNone GHAMode = ""
	// GHAModeInteractive is used when a human mentioned @rho in a comment and
	// expects a conversational reply.
	GHAModeInteractive GHAMode = "interactive"
	// GHAModeAutomation is used for label/issue triggers where rho should act
	// autonomously on the issue/PR body.
	GHAModeAutomation GHAMode = "automation"
)

// ghMention is the trigger token that promotes an event to interactive mode.
const ghMention = "@rho"

// ghTrustedAssociations are the GitHub author_association values that identify
// a repository insider. Everyone else (CONTRIBUTOR, FIRST_TIME_CONTRIBUTOR,
// NONE, …) is treated as untrusted external input.
var ghTrustedAssociations = map[string]bool{
	"OWNER":        true,
	"MEMBER":       true,
	"COLLABORATOR": true,
}

// GHAContext captures the relevant fields parsed from the GitHub Actions
// environment and event payload.
type GHAContext struct {
	Active            bool    // GITHUB_ACTIONS == "true"
	EventName         string  // GITHUB_EVENT_NAME
	Mode              GHAMode // resolved operating mode
	Prompt            string  // event-derived prompt body
	Mention           bool    // whether an @rho mention was found in a comment
	AuthorAssociation string  // GitHub author_association of the triggering actor
	Trusted           bool    // author is a repo insider (or explicitly trusted)
}

// detectGitHubActions inspects the GitHub Actions environment and the event
// payload at GITHUB_EVENT_PATH to decide between interactive and automation
// mode. It returns a context whose Active field is false when not running under
// GitHub Actions (in which case the caller should keep the original prompt).
//
// getenv and readFile are injected for testability; pass os.Getenv and
// os.ReadFile in production.
func detectGitHubActions(getenv func(string) string, readFile func(string) ([]byte, error)) GHAContext {
	ctx := GHAContext{}
	if getenv("GITHUB_ACTIONS") != "true" {
		return ctx
	}
	ctx.Active = true
	ctx.EventName = getenv("GITHUB_EVENT_NAME")

	var payload map[string]interface{}
	if path := getenv("GITHUB_EVENT_PATH"); path != "" {
		if data, err := readFile(path); err == nil {
			_ = json.Unmarshal(data, &payload)
		}
	}

	commentBody := ghCommentBody(payload)
	ctx.Mention = strings.Contains(strings.ToLower(commentBody), ghMention)

	// Trust signal: GitHub reports the actor's relationship to the repo.
	// Only insiders are trusted to drive high-autonomy tool use; content
	// from outside contributors is untrusted (prompt-injection surface).
	// RHO_GHA_TRUST_EVENT=1 lets a maintainer opt into trusting all events.
	ctx.AuthorAssociation = ghAuthorAssociation(payload)
	ctx.Trusted = ghTrustedAssociations[strings.ToUpper(strings.TrimSpace(ctx.AuthorAssociation))] ||
		ghTrustEventOverride(getenv)

	switch ctx.EventName {
	case "issue_comment", "pull_request_review_comment":
		if ctx.Mention {
			ctx.Mode = GHAModeInteractive
			ctx.Prompt = ghStripMention(commentBody)
			return ctx
		}
		// A comment without a mention is treated as automation against the
		// comment body so the action can still be invoked deliberately.
		ctx.Mode = GHAModeAutomation
		ctx.Prompt = strings.TrimSpace(commentBody)
		return ctx
	default:
		// issues, pull_request, schedule, workflow_dispatch, etc.
		ctx.Mode = GHAModeAutomation
		ctx.Prompt = ghIssueBody(payload)
		return ctx
	}
}

// ghTrustEventOverride reports whether the maintainer has opted into
// trusting GitHub Actions event content regardless of author association.
func ghTrustEventOverride(getenv func(string) string) bool {
	switch strings.ToLower(strings.TrimSpace(getenv("RHO_GHA_TRUST_EVENT"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// ghAuthorAssociation extracts the author_association from the triggering
// object (comment, then issue, then pull_request).
func ghAuthorAssociation(payload map[string]interface{}) string {
	if payload == nil {
		return ""
	}
	for _, key := range []string{"comment", "issue", "pull_request", "review"} {
		if obj, ok := payload[key].(map[string]interface{}); ok {
			if assoc, ok := obj["author_association"].(string); ok && assoc != "" {
				return assoc
			}
		}
	}
	return ""
}

// ghCommentBody extracts the comment text from an issue_comment or
// pull_request_review_comment payload.
func ghCommentBody(payload map[string]interface{}) string {
	if payload == nil {
		return ""
	}
	if c, ok := payload["comment"].(map[string]interface{}); ok {
		if body, ok := c["body"].(string); ok {
			return body
		}
	}
	return ""
}

// ghIssueBody extracts a title + body prompt from an issues or pull_request
// payload, falling back to the comment body.
func ghIssueBody(payload map[string]interface{}) string {
	if payload == nil {
		return ""
	}
	for _, key := range []string{"issue", "pull_request"} {
		if obj, ok := payload[key].(map[string]interface{}); ok {
			title, _ := obj["title"].(string)
			body, _ := obj["body"].(string)
			combined := strings.TrimSpace(title + "\n\n" + body)
			if combined != "" {
				return combined
			}
		}
	}
	return strings.TrimSpace(ghCommentBody(payload))
}

// ghStripMention removes the leading @rho mention from a comment so the
// remaining text becomes the prompt.
func ghStripMention(body string) string {
	out := body
	// indexFold returns a byte span in the original string, so removing the
	// mention never splits a multi-byte rune that precedes it in the body.
	if idx, matchLen := indexFold(out, ghMention); idx >= 0 {
		out = out[:idx] + out[idx+matchLen:]
	}
	return strings.TrimSpace(out)
}

// --- Skill dispatch ---------------------------------------------------------

// skillRunner resolves and renders a skill into a prompt prefix. It is an
// interface so tests can substitute a mock implementation.
type skillRunner interface {
	// Run returns the rendered skill content for the given skill name, or an
	// error if the skill cannot be found.
	Run(name string) (string, error)
}

// pluginSkillRunner is the production skillRunner backed by Rho skill storage.
type pluginSkillRunner struct{}

func (pluginSkillRunner) Run(name string) (string, error) {
	skills := plugin.LoadSmartSkills(plugin.DefaultSkillDirs())
	for _, s := range skills {
		if strings.EqualFold(s.Name, name) {
			return fmt.Sprintf("[Skill: %s]\n\n%s", s.Name, s.Content), nil
		}
	}
	return "", fmt.Errorf("skill %q not found (run `rho skills` to list available skills)", name)
}

// defaultSkillRunner is overridable in tests.
var defaultSkillRunner skillRunner = pluginSkillRunner{}

// isSkillDispatch reports whether a resolved prompt is a slash-command skill
// invocation (e.g. "/deep-research foo bar").
func isSkillDispatch(prompt string) bool {
	p := strings.TrimSpace(prompt)
	return strings.HasPrefix(p, "/") && len(p) > 1 && !strings.HasPrefix(p, "//")
}

// parseSkillInvocation splits a "/name arg1 arg2" prompt into the skill name
// and trailing argument string.
func parseSkillInvocation(prompt string) (name, args string) {
	p := strings.TrimSpace(prompt)
	p = strings.TrimPrefix(p, "/")
	fields := strings.SplitN(p, " ", 2)
	name = strings.TrimSpace(fields[0])
	if len(fields) > 1 {
		args = strings.TrimSpace(fields[1])
	}
	return name, args
}

// dispatchSkill renders a slash-command prompt into an expanded prompt by
// resolving the named skill through the runner and appending any user args.
func dispatchSkill(runner skillRunner, prompt string) (string, error) {
	name, args := parseSkillInvocation(prompt)
	if name == "" {
		return "", fmt.Errorf("empty skill name in %q", prompt)
	}
	rendered, err := runner.Run(name)
	if err != nil {
		return "", err
	}
	if args != "" {
		return rendered + "\n\nArguments: " + args, nil
	}
	return rendered, nil
}

func persistExecSession(id, model, provider, userMsg, assistantMsg string) {
	s := &session.Session{
		ID:       id,
		Model:    model,
		Provider: provider,
		Messages: []session.Message{
			{Role: "user", Content: userMsg},
			{Role: "assistant", Content: assistantMsg},
		},
	}
	if err := session.Save(s); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", auditTint(fmt.Sprintf("warning: failed to persist exec session %s: %v", id, err), warnAmber))
	}
}

func createExecWorktree(repoDir, baseBranch, branch string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "mktemp", "-d")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	wtPath := strings.TrimSpace(string(out))

	gitCmd := exec.CommandContext(context.Background(), "git", "worktree", "add", "-b", branch, wtPath, baseBranch) // #nosec G204 -- fixed command 'git' with args, not user-controlled binary
	gitCmd.Dir = repoDir
	if errOut, err := gitCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%s: %w", strings.TrimSpace(string(errOut)), err)
	}
	return wtPath, nil
}

func cleanupExecWorktree(repoDir, wtPath string) {
	if wtPath == "" {
		return
	}
	cmd := exec.CommandContext(context.Background(), "git", "worktree", "remove", "--force", wtPath) // #nosec G204 -- fixed git executable
	cmd.Dir = repoDir
	_ = cmd.Run()
}

// randomHex returns a hex-encoded string of n random bytes.
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- Best-of-N fan-out -------------------------------------------------------

// fanoutAttempt is one best-of-N run's outcome.
type fanoutAttempt struct {
	Attempt    int    `json:"attempt"`
	Branch     string `json:"branch"`
	Worktree   string `json:"worktree"`
	OK         bool   `json:"ok"`
	SessionID  string `json:"session_id,omitempty"`
	Response   string `json:"response_tail,omitempty"`
	TokensIn   int    `json:"tokens_in,omitempty"`
	TokensOut  int    `json:"tokens_out,omitempty"`
	TurnsTaken int    `json:"turns_taken"`
	Duration   string `json:"duration"`
	Model      string `json:"model,omitempty"`
	Error      string `json:"error,omitempty"`
}

// runExecFanout runs the same prompt in N sequentially-executed, isolated
// worktrees and prints a comparison report. Worktrees are deliberately KEPT
// (never cleaned up) so the user can diff branches and merge the winner —
// quality judgment stays with the human.
func runExecFanout(prompt string, n int) error {
	if n > 5 {
		n = 5 // bounded: each attempt is a full agent run
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}
	base := getCurrentBranch(cwd)
	start := time.Now()

	attempts := make([]fanoutAttempt, 0, n)
	anyOK := false
	for i := 1; i <= n; i++ {
		fmt.Fprintf(os.Stderr, "\n%s\n", auditTint(fmt.Sprintf("=== fanout attempt %d/%d ===", i, n), infoSky))
		att := fanoutAttempt{Attempt: i}

		branch := fmt.Sprintf("rho-exec/%d-fanout%d-%s", start.UnixMilli(), i, randomHex(4))
		wtPath, wtErr := createExecWorktree(cwd, base, branch)
		if wtErr != nil {
			att.Error = fmt.Sprintf("worktree: %v", wtErr)
			attempts = append(attempts, att)
			continue
		}
		att.Branch = branch
		att.Worktree = wtPath

		origWd, wdErr := os.Getwd()
		if wdErr != nil {
			origWd = cwd
		}
		if chdirErr := os.Chdir(wtPath); chdirErr != nil {
			att.Error = fmt.Sprintf("chdir: %v", chdirErr)
			attempts = append(attempts, att)
			continue
		}

		attemptStart := time.Now()
		res, runErr := execOnceInWorktree(prompt, i)
		if chdirErr := os.Chdir(origWd); chdirErr != nil {
			if att.Error == "" {
				att.Error = "restore cwd: " + chdirErr.Error()
			}
		}
		if res != nil {
			att.OK = res.ExitCode == 0 && runErr == nil
			att.SessionID = res.SessionID
			att.TokensIn = res.TokensIn
			att.TokensOut = res.TokensOut
			att.TurnsTaken = res.TurnsTaken
			att.Duration = time.Since(attemptStart).Round(time.Millisecond).String()
			att.Model = res.Model
			tail := res.Response
			if len(tail) > 400 {
				tail = tail[:400] + "…"
			}
			att.Response = tail
		}
		if runErr != nil && att.Error == "" {
			att.Error = runErr.Error()
		}
		if att.OK {
			anyOK = true
		}
		attempts = append(attempts, att)
	}

	if execOutputFormat == "json" || execJSON {
		out := map[string]interface{}{
			"mode":     "fanout",
			"attempts": attempts,
			"any_ok":   anyOK,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return err
		}
	} else {
		printFanoutReport(attempts)
	}

	// Completion notification (Orca's "know when your agent finishes"),
	// best-effort and only when a channel is configured.
	title := fmt.Sprintf("Fan-out finished: %d/%d attempts succeeded", countOK(attempts), n)
	_ = notify.SendCompletion(notify.Completion{
		Title: title, Source: "rho exec --fanout", OK: anyOK, Body: fanoutSummaryLines(attempts),
	})

	if !anyOK {
		return &ExitCodeError{Code: 1}
	}
	return nil
}

func countOK(attempts []fanoutAttempt) int {
	n := 0
	for _, a := range attempts {
		if a.OK {
			n++
		}
	}
	return n
}

func fanoutSummaryLines(attempts []fanoutAttempt) string {
	lines := make([]string, 0, len(attempts))
	for _, a := range attempts {
		status := "ok"
		if !a.OK {
			status = "failed"
			if a.Error != "" {
				status += ": " + a.Error
			}
		}
		lines = append(lines, fmt.Sprintf("#%d %s [%s]", a.Attempt, status, a.Branch))
	}
	return strings.Join(lines, "\n")
}

func printFanoutReport(attempts []fanoutAttempt) {
	fmt.Fprintln(os.Stderr, auditTint("\n=== fan-out comparison (worktrees kept for inspection) ===", infoSky))
	for _, a := range attempts {
		status := auditTint(icons.Check()+" ok", doneGreen)
		if !a.OK {
			status = auditTint(icons.Close()+" failed", errorCoral)
			if a.Error != "" {
				status += auditTint(" — "+a.Error, errorCoral)
			}
		}
		fmt.Fprintf(os.Stderr, "\n#%d %s\n  branch:   %s\n  worktree: %s\n  tokens:   in=%d out=%d turns=%d\n  duration: %s\n",
			a.Attempt, status, a.Branch, a.Worktree, a.TokensIn, a.TokensOut, a.TurnsTaken, a.Duration)
		if a.Response != "" {
			fmt.Fprintf(os.Stderr, "  tail:     %s\n", strings.ReplaceAll(a.Response, "\n", " "))
		}
		fmt.Fprintf(os.Stderr, "  compare:  git diff main...%s\n", a.Branch)
	}
	fmt.Fprintln(os.Stderr, "\nPick the winner, then merge its branch (e.g. git merge <branch>) and remove the rest:")
	for _, a := range attempts {
		if a.Branch != "" {
			fmt.Fprintf(os.Stderr, "  git worktree remove --force %s && git branch -D %s\n", a.Worktree, a.Branch)
		}
	}
}

// execOnceInWorktree runs the full single-attempt pipeline (settings → system
// prompt → registry → session → stream) inside the current working directory
// (expected to be the attempt's worktree) and returns the structured result.
// Stream events are captured rather than printed so N attempts do not interleave.
func execOnceInWorktree(prompt string, attemptIdx int) (*ExecResult, error) {
	settings := rhoconfig.LoadSettings()

	systemPrompt, err := buildSystemPrompt()
	if err != nil {
		return nil, err
	}
	effectiveModel, effectiveProvider := effectiveModelAndProvider(settings)
	if execModel != "" {
		effectiveModel = execModel
	}
	if execAgent != "" {
		agentDef, lookupErr := agents.Get(execAgent)
		if lookupErr != nil {
			return nil, fmt.Errorf("agent %q: %w", execAgent, lookupErr)
		}
		systemPrompt = agentDef.Prompt + "\n\n" + systemPrompt
		effectiveModel = agentDef.Model
	}

	registry, err := defaultRegistry(settings)
	if err != nil {
		return nil, err
	}
	sess, cfgErr := newConfiguredRhoSession(settings, effectiveProvider, effectiveModel, systemPrompt, registry, logger.New(io.Discard, logger.Error), execMaxTurns)
	if cfgErr != nil {
		return nil, cfgErr
	}
	if _, err := os.Getwd(); err != nil {
		return nil, fmt.Errorf("resolve project directory: %w", err)
	}

	if execAutoLevel != "" {
		sess.PermSvc().SetAutonomy(safety.ParseAutonomyLevel(execAutoLevel))
	} else {
		sess.PermSvc().SetAutonomy(safety.AutonomyFull)
	}
	sess.PermSvc().SetPermissionFn(func(req safety.PermissionRequest) {
		if req.Response != nil {
			req.Response <- false
		}
	})

	sess.AddUser(prompt)

	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	events, err := sess.Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("stream: %w", err)
	}

	var response strings.Builder
	var totalIn, totalOut, turns int
	var execErr string
	for ev := range events {
		switch ev.Type {
		case "content":
			response.WriteString(ev.Content)
		case "usage":
			if ev.Usage != nil {
				totalIn += ev.Usage.PromptTokens
				totalOut += ev.Usage.CompletionTokens
				turns++
			}
		case "error":
			execErr = ev.Content
		}
	}

	exitCode := 0
	if execErr != "" {
		exitCode = 1
	}
	sessionID := fmt.Sprintf("exec-fanout%d-%d-%s", attemptIdx, time.Now().UnixMilli(), randomHex(4))
	if !execEphemeral {
		persistExecSession(sessionID, effectiveModel, effectiveProvider, prompt, response.String())
	}
	return &ExecResult{
		SessionID:  sessionID,
		Response:   response.String(),
		ExitCode:   exitCode,
		TokensIn:   totalIn,
		TokensOut:  totalOut,
		TurnsTaken: turns,
		Duration:   time.Since(time.Now()).Round(time.Millisecond).String(),
		Model:      effectiveModel,
	}, nil
}
