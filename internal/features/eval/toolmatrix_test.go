package eval

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

// scriptedCaller returns observations keyed by case ID.
func scriptedCaller(script map[string]ObservedCall, errs map[string]error) ToolCaller {
	return func(_ context.Context, c ToolUseCase) (ObservedCall, error) {
		if errs != nil {
			if e, ok := errs[c.ID]; ok {
				return ObservedCall{}, e
			}
		}
		return script[c.ID], nil
	}
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestScoreToolUse_AllMatrixCells(t *testing.T) {
	readFile := &ExpectedCall{Tool: "read_file"}
	cases := []ToolUseCase{
		{ID: "tp", Prompt: "show me main.go", Expected: readFile},  // expects tool, calls it
		{ID: "fn", Prompt: "open config.yaml", Expected: readFile}, // expects tool, calls none
		{ID: "fp", Prompt: "what is 2+2?", Expected: nil},          // expects none, calls one
		{ID: "tn", Prompt: "say hello", Expected: nil},             // expects none, calls none
	}
	script := map[string]ObservedCall{
		"tp": {Tool: "read_file", Args: map[string]any{"path": "main.go"}},
		"fn": {Tool: ""},
		"fp": {Tool: "bash", Args: map[string]any{"cmd": "echo 4"}},
		"tn": {Tool: ""},
	}

	rep := ScoreToolUse(context.Background(), cases, scriptedCaller(script, nil))
	m := rep.Matrix
	if m.TP != 1 || m.FN != 1 || m.FP != 1 || m.TN != 1 {
		t.Fatalf("matrix = %+v, want one of each", m)
	}
	if !approx(m.Precision(), 0.5) {
		t.Errorf("precision = %v, want 0.5", m.Precision())
	}
	if !approx(m.Recall(), 0.5) {
		t.Errorf("recall = %v, want 0.5", m.Recall())
	}
	if !approx(m.F1(), 0.5) {
		t.Errorf("F1 = %v, want 0.5", m.F1())
	}
}

func TestScoreToolUse_PayloadAccuracy(t *testing.T) {
	exp := &ExpectedCall{
		Tool:      "read_file",
		ArgsMatch: func(a map[string]any) bool { return a["path"] == "main.go" },
	}
	cases := []ToolUseCase{
		{ID: "good", Expected: exp},      // right tool, right args
		{ID: "badargs", Expected: exp},   // right tool, wrong args
		{ID: "wrongtool", Expected: exp}, // wrong tool
	}
	script := map[string]ObservedCall{
		"good":      {Tool: "read_file", Args: map[string]any{"path": "main.go"}},
		"badargs":   {Tool: "read_file", Args: map[string]any{"path": "other.go"}},
		"wrongtool": {Tool: "bash", Args: map[string]any{"cmd": "cat main.go"}},
	}

	rep := ScoreToolUse(context.Background(), cases, scriptedCaller(script, nil))
	if rep.Matrix.TP != 3 {
		t.Fatalf("all three triggered, TP = %d", rep.Matrix.TP)
	}
	p := rep.Payload
	if p.Evaluated != 3 {
		t.Errorf("Evaluated = %d, want 3", p.Evaluated)
	}
	if p.CorrectTool != 2 { // good + badargs picked read_file
		t.Errorf("CorrectTool = %d, want 2", p.CorrectTool)
	}
	if p.CorrectArgs != 1 { // only good matched args
		t.Errorf("CorrectArgs = %d, want 1", p.CorrectArgs)
	}
	if !approx(p.ToolNameRate(), 2.0/3.0) {
		t.Errorf("ToolNameRate = %v", p.ToolNameRate())
	}
	if !approx(p.ArgsRate(), 1.0/3.0) {
		t.Errorf("ArgsRate = %v", p.ArgsRate())
	}
}

func TestScoreToolUse_ErrorCountsAsNoCall(t *testing.T) {
	cases := []ToolUseCase{
		{ID: "boom", Prompt: "do a thing", Expected: &ExpectedCall{Tool: "read_file"}},
	}
	errs := map[string]error{"boom": errors.New("provider exploded")}
	rep := ScoreToolUse(context.Background(), cases, scriptedCaller(nil, errs))

	// Expected a tool, model errored (=no call) → false negative, not a crash.
	if rep.Matrix.FN != 1 || rep.Matrix.TP != 0 {
		t.Errorf("matrix = %+v, want FN=1", rep.Matrix)
	}
	if rep.Outcomes[0].Err == "" {
		t.Error("outcome should record the error")
	}
}

func TestTriggerMatrix_EmptyDivisors(t *testing.T) {
	// No positive predictions / expectations → precision & recall default to 1.
	m := TriggerMatrix{TN: 5}
	if !approx(m.Precision(), 1.0) || !approx(m.Recall(), 1.0) {
		t.Errorf("empty matrix precision/recall = %v/%v, want 1/1", m.Precision(), m.Recall())
	}
}

func TestToolUseReport_MarkdownListsFailures(t *testing.T) {
	cases := []ToolUseCase{
		{ID: "z-miss", Expected: &ExpectedCall{Tool: "read_file"}},
		{ID: "a-spurious", Expected: nil},
	}
	script := map[string]ObservedCall{
		"z-miss":     {Tool: ""},     // FN
		"a-spurious": {Tool: "bash"}, // FP
	}
	rep := ScoreToolUse(context.Background(), cases, scriptedCaller(script, nil))
	md := rep.Markdown()
	if !strings.Contains(md, "confusion matrix") {
		t.Error("markdown missing matrix section")
	}
	if !strings.Contains(md, "Failures") {
		t.Error("markdown missing failures section")
	}
	// Failures must be sorted by ID: a-spurious before z-miss.
	if strings.Index(md, "a-spurious") > strings.Index(md, "z-miss") {
		t.Error("failures not sorted by ID")
	}
}
