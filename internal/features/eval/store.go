package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/GrayCodeAI/rho/internal/storage"
)

// StoredResult is the persistent JSON format for eval results.
type StoredResult struct {
	Version   string             `json:"version"`
	Timestamp time.Time          `json:"timestamp"`
	Suite     string             `json:"suite"`
	Model     string             `json:"model"`
	Provider  string             `json:"provider"`
	Hash      *ResultHash        `json:"hash,omitempty"`
	Summary   ResultSummary      `json:"summary"`
	Tasks     []StoredTaskResult `json:"tasks"`
}

// ResultSummary is the top-level metrics.
type ResultSummary struct {
	TotalTasks    int     `json:"total_tasks"`
	Passed        int     `json:"passed"`
	Failed        int     `json:"failed"`
	PassRate      float64 `json:"pass_rate"`
	TotalDuration string  `json:"total_duration"`
	TotalTokens   int     `json:"total_tokens"`
	TotalCostUSD  float64 `json:"total_cost_usd"`
}

// StoredTaskResult is the per-task persistent format.
type StoredTaskResult struct {
	TaskID   string   `json:"task_id"`
	Passed   bool     `json:"passed"`
	Duration string   `json:"duration"`
	Tokens   int      `json:"tokens"`
	CostUSD  float64  `json:"cost_usd"`
	Attempts int      `json:"attempts"`
	Error    string   `json:"error,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

// ResultStore handles reading/writing eval results to disk.
type ResultStore struct {
	Dir string
}

// DefaultResultStore returns a store in Rho's user state directory.
func DefaultResultStore() *ResultStore {
	return &ResultStore{Dir: filepath.Join(storage.StateDir(), "eval", "results")}
}

// Save writes a SuiteResult to disk as JSON.
func (s *ResultStore) Save(result *SuiteResult, model, provider string, hash *ResultHash) (string, error) {
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return "", err
	}

	stored := &StoredResult{
		Version:   "1",
		Timestamp: time.Now(),
		Suite:     result.Suite,
		Model:     model,
		Provider:  provider,
		Hash:      hash,
		Summary: ResultSummary{
			TotalTasks:    result.TotalTasks,
			Passed:        result.Passed,
			Failed:        result.Failed,
			PassRate:      result.PassRate,
			TotalDuration: result.TotalDuration.String(),
			TotalTokens:   result.TotalTokens,
			TotalCostUSD:  result.TotalCostUSD,
		},
	}

	for _, tr := range result.Results {
		stored.Tasks = append(stored.Tasks, StoredTaskResult{
			TaskID:   tr.TaskID,
			Passed:   tr.Passed,
			Duration: tr.Duration.String(),
			Tokens:   tr.TokensUsed,
			CostUSD:  tr.CostUSD,
			Attempts: tr.Attempts,
			Error:    tr.Error,
		})
	}

	filename := fmt.Sprintf("%s_%s_%s.json", time.Now().Format("20060102_150405"), model, result.Suite)
	path := filepath.Join(s.Dir, filename)

	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o600)
}

// Load reads a stored result from a JSON file.
func (s *ResultStore) Load(path string) (*StoredResult, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path comes from List(), which enumerates files under the local result store directory
	if err != nil {
		return nil, err
	}
	var stored StoredResult
	return &stored, json.Unmarshal(data, &stored)
}

// List returns all result files in the store directory.
func (s *ResultStore) List() ([]string, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			files = append(files, filepath.Join(s.Dir, e.Name()))
		}
	}
	return files, nil
}
