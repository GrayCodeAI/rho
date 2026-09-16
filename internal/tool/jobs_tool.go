package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GrayCodeAI/rho/internal/env"
	"github.com/GrayCodeAI/rho/internal/jobs"
)

// jobsRegistry is the process-wide background-job registry backing the
// JobsTool. The tool package owns the seam; engine integration (session
// teardown cleanup) reaches the same registry via SessionJobsRegistry.
var jobsRegistry = jobs.NewRegistry()

// SessionJobsRegistry exposes the shared job registry for engine integration
// — e.g. agent-disposal cleanup (DSH: "owner disposed" cancels and awaits
// every job owned by the session being torn down).
func SessionJobsRegistry() *jobs.Registry { return jobsRegistry }

// defaultJobOutputLimit caps stored output per job (DSH's producer-owned
// outputLimitBytes default).
const defaultJobOutputLimit = 100_000

// JobsTool manages DSH-style background jobs: `run` starts a command in the
// background, `list`/`read`/`wait`/`kill` observe and control it. It is the
// Go-native port of `@deepseek-ai/dsh-tool-jobs`.
type JobsTool struct{}

func (JobsTool) Name() string      { return "Jobs" }
func (JobsTool) Aliases() []string { return []string{"jobs"} }
func (JobsTool) Description() string {
	return "Manage background jobs. `run` starts a shell command in the background " +
		"and returns its job id; `list` shows visible jobs; `read` returns the output " +
		"delta since the previous read (or the final output once settled); `wait` blocks " +
		"until a job settles; `kill` requests termination. Jobs are scoped by the optional " +
		"session id: a session sees its own jobs plus every unowned job."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (JobsTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action":       {Type: "string", Enum: []interface{}{"list", "run", "read", "wait", "kill"}, Description: "Operation to perform"},
			"command":      {Type: "string", Description: "Shell command to run in the background (required for run)"},
			"label":        {Type: "string", Description: "One-line model-facing label for the job (defaults to the command)"},
			"session":      {Type: "string", Description: "Owner session id. Jobs owned by a session are visible only to it; omitted jobs are unowned and visible to any caller"},
			"id":           {Type: "string", Description: "Job id, e.g. bash-3 (required for read/wait/kill)"},
			"reason":       {Type: "string", Description: "Kill reason forwarded verbatim to the job (default: 'user requested')"},
			"timeout_sec":  {Type: "integer", Description: "Max seconds to wait before returning the current snapshot (default: 60)"},
			"output_limit": {Type: "integer", Description: "UTF-8 byte cap for stored output (default 100000)"},
		},
		Required: []string{"action"},
	}
}

func (JobsTool) Parameters() map[string]interface{} {
	return jobsSchema.ToJSONSchema()
}

// jobsSchema is the single source of truth for Jobs' input schema.
var jobsSchema = JobsTool{}.Schema()

// JobsInput is the typed input for JobsTool.
type JobsInput struct {
	Action      string `json:"action"`
	Command     string `json:"command"`
	Label       string `json:"label"`
	Session     string `json:"session"`
	ID          string `json:"id"`
	Reason      string `json:"reason"`
	TimeoutSec  int    `json:"timeout_sec"`
	OutputLimit int    `json:"output_limit"`
}

// jobsSnapshotJSON is the compact wire shape for a job snapshot.
type jobsSnapshotJSON struct {
	ID               string    `json:"id"`
	Kind             string    `json:"kind"`
	Label            string    `json:"label"`
	OutputLimitBytes int       `json:"output_limit_bytes"`
	OwnerSession     string    `json:"owner_session,omitempty"`
	Status           string    `json:"status"`
	Detail           string    `json:"detail,omitempty"`
	StartedAt        time.Time `json:"started_at"`
	FinishedAt       time.Time `json:"finished_at,omitempty"`
	Reported         bool      `json:"reported"`
}

func (JobsTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[JobsInput]("Jobs", input)
	if err != nil {
		return "", err
	}
	if p.Action == "" {
		return "", fmt.Errorf("action is required (list|run|read|wait|kill)")
	}
	if p.TimeoutSec <= 0 {
		p.TimeoutSec = 60
	}
	if p.OutputLimit <= 0 {
		p.OutputLimit = defaultJobOutputLimit
	}
	if p.Reason == "" {
		p.Reason = "user requested"
	}

	switch p.Action {
	case "list":
		snaps := jobsRegistry.List(p.Session)
		out := make([]jobsSnapshotJSON, 0, len(snaps))
		for _, s := range snaps {
			out = append(out, snapshotToJSON(s))
		}
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return "", err
		}
		return string(b), nil

	case "run":
		if strings.TrimSpace(p.Command) == "" {
			return "", fmt.Errorf("command is required for run")
		}
		label := p.Label
		if label == "" {
			label = p.Command
		}
		id, err := jobsRegistry.Start(jobs.Start{
			Kind:             jobs.Kind("bash"),
			Label:            label,
			OutputLimitBytes: p.OutputLimit,
			OwnerSession:     p.Session,
			Run:              bashJobRun(ctx, p.Command, p.OutputLimit),
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("job started: %s", id), nil

	case "read":
		if p.ID == "" {
			return "", fmt.Errorf("id is required for read")
		}
		rd, err := jobsRegistry.Read(jobs.ID(p.ID))
		if err != nil {
			return "", err
		}
		return formatRead(rd), nil

	case "wait":
		if p.ID == "" {
			return "", fmt.Errorf("id is required for wait")
		}
		waitCtx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutSec)*time.Second)
		defer cancel()
		snap, err := jobsRegistry.Wait(waitCtx, jobs.ID(p.ID))
		if err != nil {
			return "", err
		}
		b, err := json.MarshalIndent(snapshotToJSON(snap), "", "  ")
		if err != nil {
			return "", err
		}
		return string(b), nil

	case "kill":
		if p.ID == "" {
			return "", fmt.Errorf("id is required for kill")
		}
		if err := jobsRegistry.Kill(jobs.ID(p.ID), p.Reason); err != nil {
			return "", err
		}
		return fmt.Sprintf("job %s: termination requested (%s)", p.ID, p.Reason), nil

	default:
		return "", fmt.Errorf("unknown action %q (list|run|read|wait|kill)", p.Action)
	}
}

// bashJobRun returns a jobs producer that runs `command` with `bash -c` in a
// fresh process group, captures bounded output, and settles the completion
// promise with the process outcome. Cancel kills the whole process tree.
func bashJobRun(ctx context.Context, command string, outputLimit int) func() jobs.Hooks {
	return func() jobs.Hooks {
		lw := &limitedWriter{maxBytes: outputLimit}
		cmd := exec.Command("bash", "-c", command) // #nosec G204 -- model-issued command, same surface as Bash
		cmd.Env = env.SubprocessEnv()
		setCmdProcessGroup(cmd)
		if tc := GetToolContext(ctx); tc != nil && tc.WorkingDir != "" {
			cmd.Dir = tc.WorkingDir
		}
		cmd.Stdout = lw
		cmd.Stderr = lw

		done := make(chan jobs.Outcome, 1)
		var once sync.Once
		settle := func(o jobs.Outcome) { once.Do(func() { done <- o }) }

		if err := cmd.Start(); err != nil {
			// Start failure: settle failed immediately.
			settle(jobs.Outcome{Status: jobs.StatusFailed, Detail: fmt.Sprintf("start: %v", err)})
			return jobs.Hooks{
				Cancel:     func(string) {},
				Done:       done,
				ReadOutput: func() string { return lw.buf.String() },
			}
		}

		go func() {
			err := cmd.Wait()
			switch {
			case err == nil:
				settle(jobs.Outcome{Status: jobs.StatusCompleted, Detail: "exit code: 0"})
			case isSignalExit(err):
				settle(jobs.Outcome{Status: jobs.StatusKilled, Detail: "killed"})
			default:
				settle(jobs.Outcome{Status: jobs.StatusFailed, Detail: fmt.Sprintf("exit code: %s", exitCode(err))})
			}
		}()

		return jobs.Hooks{
			Cancel: func(reason string) {
				if cmd.Process != nil {
					_ = killProcessGroup(cmd.Process)
				}
			},
			Done:       done,
			ReadOutput: func() string { return lw.buf.String() },
		}
	}
}

func snapshotToJSON(s jobs.Snapshot) jobsSnapshotJSON {
	return jobsSnapshotJSON{
		ID:               string(s.ID),
		Kind:             string(s.Kind),
		Label:            s.Label,
		OutputLimitBytes: s.OutputLimitBytes,
		OwnerSession:     s.OwnerSession,
		Status:           string(s.Status),
		Detail:           s.Detail,
		StartedAt:        s.StartedAt,
		FinishedAt:       s.FinishedAt,
		Reported:         s.Reported,
	}
}

func formatRead(rd jobs.Read) string {
	var b strings.Builder
	b.WriteString("status: ")
	b.WriteString(string(rd.Snapshot.Status))
	if rd.Snapshot.Detail != "" {
		b.WriteString(" (")
		b.WriteString(rd.Snapshot.Detail)
		b.WriteString(")")
	}
	b.WriteString("\n")
	if rd.Text != "" {
		b.WriteString(rd.Text)
		if !strings.HasSuffix(rd.Text, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// isSignalExit reports whether err is an *exec.ExitError caused by a signal
// (the process was killed, not a normal non-zero exit). ExitCode() == -1 is
// the portable signal indicator across Unix and Windows.
func isSignalExit(err error) bool {
	var ee *exec.ExitError
	return errors.As(err, &ee) && ee.ProcessState != nil && ee.ProcessState.ExitCode() == -1
}

// exitCode renders a command error as a short status string: the numeric exit
// code when available, otherwise the error text.
func exitCode(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ProcessState != nil {
		return strconv.Itoa(ee.ExitCode())
	}
	return err.Error()
}
