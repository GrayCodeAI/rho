package evalloop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Inputs identifies the fixed inputs of a run for reproducibility hashing.
type Inputs struct {
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Prompt   string `json:"prompt"`
	// ConfigVersion is bumped whenever loop limits/system-prompt semantics
	// change, so a change in configuration invalidates cached hashes.
	ConfigVersion int `json:"config_version"`
}

// ReproHashOf returns a deterministic SHA-256 over the run inputs and the
// transcript. Identical inputs and outputs produce identical hashes.
func ReproHashOf(in Inputs, transcript []byte) string {
	payload := struct {
		Inputs     Inputs `json:"inputs"`
		Transcript []byte `json:"transcript"`
	}{Inputs: in, Transcript: transcript}
	data, err := json.Marshal(payload)
	if err != nil {
		// JSON of these types cannot fail; fall back to a hash of the inputs.
		raw, _ := json.Marshal(in)
		data = raw
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// RunRecord is one result annotated with its reproducibility hash.
type RunRecord struct {
	Model    string  `json:"model"`
	Provider string  `json:"provider"`
	Output   string  `json:"output"`
	Tokens   int     `json:"tokens_used"`
	CostUSD  float64 `json:"cost_usd"`
	Duration string  `json:"duration"`
	Repro    string  `json:"repro_hash"`
}

// Comparison summarizes a set of runs across models/providers.
type Comparison struct {
	Runs         []RunRecord   `json:"runs"`
	TotalRuns    int           `json:"total_runs"`
	UniqueHashes int           `json:"unique_repro_hashes"`
	MinTokens    int           `json:"min_tokens"`
	MaxTokens    int           `json:"max_tokens"`
	MinCostUSD   float64       `json:"min_cost_usd"`
	MaxCostUSD   float64       `json:"max_cost_usd"`
	MinDuration  time.Duration `json:"min_duration"`
	MaxDuration  time.Duration `json:"max_duration"`
}

// Compare aggregates multiple results into a comparative report. It returns a
// stable ordering (by reproducibility hash) so identical runs are adjacent.
func Compare(results []Result) Comparison {
	cmp := Comparison{Runs: make([]RunRecord, 0, len(results))}
	for _, r := range results {
		rec := RunRecord{
			Model: r.Model, Provider: r.Provider, Output: truncate(r.Output, 200),
			Tokens: r.TokensUsed, CostUSD: r.CostUSD,
			Duration: r.Duration.String(), Repro: r.ReproHash,
		}
		cmp.Runs = append(cmp.Runs, rec)
	}
	sort.SliceStable(cmp.Runs, func(i, j int) bool { return cmp.Runs[i].Repro < cmp.Runs[j].Repro })

	unique := map[string]bool{}
	for _, rec := range cmp.Runs {
		if rec.Repro != "" {
			unique[rec.Repro] = true
		}
		if cmp.MinTokens == 0 || rec.Tokens < cmp.MinTokens {
			cmp.MinTokens = rec.Tokens
		}
		if rec.Tokens > cmp.MaxTokens {
			cmp.MaxTokens = rec.Tokens
		}
		if cmp.MinCostUSD == 0 || rec.CostUSD < cmp.MinCostUSD {
			cmp.MinCostUSD = rec.CostUSD
		}
		if rec.CostUSD > cmp.MaxCostUSD {
			cmp.MaxCostUSD = rec.CostUSD
		}
	}
	cmp.TotalRuns = len(cmp.Runs)
	cmp.UniqueHashes = len(unique)
	return cmp
}

// FormatComparison renders a human-readable comparative report.
func FormatComparison(cmp Comparison) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Runs: %d (unique repro hashes: %d)\n", cmp.TotalRuns, cmp.UniqueHashes)
	fmt.Fprintf(&b, "Tokens: %d..%d | Cost USD: %.6f..%.6f\n", cmp.MinTokens, cmp.MaxTokens, cmp.MinCostUSD, cmp.MaxCostUSD)
	for _, rec := range cmp.Runs {
		fmt.Fprintf(&b, "- %s/%s tokens=%d cost=%.6f dur=%s repro=%s\n", rec.Provider, rec.Model, rec.Tokens, rec.CostUSD, rec.Duration, shortHash(rec.Repro))
	}
	return strings.TrimRight(b.String(), "\n")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
