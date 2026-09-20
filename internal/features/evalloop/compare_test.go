package evalloop

import (
	"strings"
	"testing"
)

func TestReproHashDeterministic(t *testing.T) {
	in := Inputs{Model: "m", Provider: "p", Prompt: "task", ConfigVersion: 1}
	tx := []byte("transcript")
	a := ReproHashOf(in, tx)
	b := ReproHashOf(in, tx)
	if a != b {
		t.Fatalf("hash must be deterministic, got %q vs %q", a, b)
	}
	if a == "" {
		t.Fatal("hash must be non-empty")
	}
}

func TestReproHashChangesOnInput(t *testing.T) {
	in := Inputs{Model: "m", Provider: "p", Prompt: "task", ConfigVersion: 1}
	base := ReproHashOf(in, []byte("tx"))
	if ReproHashOf(in, []byte("other")) == base {
		t.Fatal("hash must change when the transcript changes")
	}
	changed := in
	changed.Prompt = "different"
	if ReproHashOf(changed, []byte("tx")) == base {
		t.Fatal("hash must change when inputs change")
	}
}

func TestCompareAggregatesAndSorts(t *testing.T) {
	results := []Result{
		{Model: "m1", Provider: "p", TokensUsed: 100, CostUSD: 0.1, Duration: 1, ReproHash: "b"},
		{Model: "m2", Provider: "p", TokensUsed: 200, CostUSD: 0.3, Duration: 2, ReproHash: "a"},
	}
	cmp := Compare(results)
	if cmp.TotalRuns != 2 {
		t.Fatalf("total runs = %d, want 2", cmp.TotalRuns)
	}
	if cmp.UniqueHashes != 2 {
		t.Fatalf("unique hashes = %d, want 2", cmp.UniqueHashes)
	}
	if cmp.MinTokens != 100 || cmp.MaxTokens != 200 {
		t.Fatalf("token range = %d..%d, want 100..200", cmp.MinTokens, cmp.MaxTokens)
	}
	if cmp.MinCostUSD != 0.1 || cmp.MaxCostUSD != 0.3 {
		t.Fatalf("cost range = %f..%f, want 0.1..0.3", cmp.MinCostUSD, cmp.MaxCostUSD)
	}
	// Sorted by repro hash: "a" first.
	if cmp.Runs[0].Model != "m2" {
		t.Fatalf("first run = %s, want m2 (sorted by repro)", cmp.Runs[0].Model)
	}
}

func TestFormatComparison(t *testing.T) {
	cmp := Compare([]Result{
		{Model: "m", Provider: "p", TokensUsed: 50, CostUSD: 0.05, Duration: 1, ReproHash: "abc123"},
	})
	out := FormatComparison(cmp)
	for _, want := range []string{"Runs: 1", "unique repro hashes: 1", "Tokens: 50", "p/m"} {
		if !strings.Contains(out, want) {
			t.Errorf("format missing %q: %s", want, out)
		}
	}
}
