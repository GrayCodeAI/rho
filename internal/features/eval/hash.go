package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"runtime"
	"strings"
)

// ResultHash captures reproducibility information for a benchmark run.
type ResultHash struct {
	TasksHash  string `json:"tasks_hash"`
	PromptHash string `json:"prompt_hash"`
	GitCommit  string `json:"git_commit"`
	GoVersion  string `json:"go_version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
}

// ComputeHash generates reproducibility hashes for a set of tasks.
func ComputeHash(tasks []BenchmarkTask) *ResultHash {
	h := &ResultHash{
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}

	// Hash all task prompts concatenated
	promptHasher := sha256.New()
	taskHasher := sha256.New()
	for _, t := range tasks {
		promptHasher.Write([]byte(t.Prompt))
		taskHasher.Write([]byte(t.ID + "\x00" + t.Description + "\x00" + t.Prompt))
	}
	h.PromptHash = hex.EncodeToString(promptHasher.Sum(nil)[:8])
	h.TasksHash = hex.EncodeToString(taskHasher.Sum(nil)[:8])

	// Git commit
	if out, err := exec.CommandContext(context.Background(), "git", "rev-parse", "--short", "HEAD").Output(); err == nil {
		h.GitCommit = strings.TrimSpace(string(out))
	}

	return h
}
