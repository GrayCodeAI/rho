package eval

import (
	"context"
	"fmt"
	"sort"
)

// Tool-use evaluation.
//
// A coding agent's tool behavior has two independent failure modes that a
// single pass/fail score conflates:
//
//  1. Triggering — did the agent invoke a tool when it should have (and refrain
//     when it should not)? This is a binary classification problem, scored as a
//     confusion matrix (TP/FN/FP/TN) with precision/recall.
//  2. Well-formedness — given that a tool was triggered, did the agent pick the
//     right tool and format the arguments correctly? Scored separately, and only
//     over cases where a trigger actually fired.
//
// With 40+ tools and model-swapping via flux, "picked the wrong moment to call
// a tool" and "called the right tool with bad args" are distinct regressions.
// Scoring them apart makes which one regressed legible.

// ExpectedCall describes the tool behavior a case expects. A nil ExpectedCall
// means "the agent should NOT call any tool" (a negative case).
type ExpectedCall struct {
	Tool string // expected tool name
	// ArgsMatch optionally validates the arguments the model supplied. If nil,
	// only the tool name is checked for payload accuracy.
	ArgsMatch func(args map[string]any) bool
}

// ToolUseCase is a single model-in-the-loop tool-selection test.
type ToolUseCase struct {
	ID     string
	Prompt string
	// Expected is the tool the model should call, or nil if it should not call
	// any tool for this prompt.
	Expected *ExpectedCall
}

// ObservedCall is what the model actually did for a case: the tool it invoked
// (empty Tool means it invoked none) and the arguments it passed.
type ObservedCall struct {
	Tool string
	Args map[string]any
}

// ToolCaller runs one case against the model under test and reports what tool
// (if any) the model chose to call. Injecting this keeps scoring testable
// without a live model.
type ToolCaller func(ctx context.Context, c ToolUseCase) (ObservedCall, error)

// TriggerMatrix is the confusion matrix for the binary "should a tool fire?"
// decision, aggregated across cases.
type TriggerMatrix struct {
	TP int // expected a tool and the model called one
	FN int // expected a tool but the model called none
	FP int // expected no tool but the model called one
	TN int // expected no tool and the model called none
}

// Precision = TP / (TP+FP); 1.0 when there are no positive predictions.
func (m TriggerMatrix) Precision() float64 {
	d := m.TP + m.FP
	if d == 0 {
		return 1.0
	}
	return float64(m.TP) / float64(d)
}

// Recall = TP / (TP+FN); 1.0 when there are no positive expectations.
func (m TriggerMatrix) Recall() float64 {
	d := m.TP + m.FN
	if d == 0 {
		return 1.0
	}
	return float64(m.TP) / float64(d)
}

// F1 is the harmonic mean of precision and recall.
func (m TriggerMatrix) F1() float64 {
	p, r := m.Precision(), m.Recall()
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

// PayloadAccuracy scores well-formedness, conditioned on a trigger firing.
type PayloadAccuracy struct {
	Evaluated   int // cases where a tool was both expected and called (TP)
	CorrectTool int // of those, the model picked the expected tool
	CorrectArgs int // of those with the right tool, args also matched
}

// ToolName returns the fraction of triggered cases with the correct tool name.
func (p PayloadAccuracy) ToolNameRate() float64 {
	if p.Evaluated == 0 {
		return 1.0
	}
	return float64(p.CorrectTool) / float64(p.Evaluated)
}

// ArgsRate returns the fraction of triggered cases with correct tool AND args.
func (p PayloadAccuracy) ArgsRate() float64 {
	if p.Evaluated == 0 {
		return 1.0
	}
	return float64(p.CorrectArgs) / float64(p.Evaluated)
}

// CaseOutcome records the per-case verdict, useful for reporting which prompts
// regressed.
type CaseOutcome struct {
	ID            string
	ExpectedTool  string // "" means no tool expected
	ObservedTool  string // "" means no tool called
	TriggerResult string // "TP" | "FN" | "FP" | "TN"
	CorrectTool   bool   // only meaningful when TriggerResult == "TP"
	CorrectArgs   bool   // only meaningful when CorrectTool
	Err           string
}

// ToolUseReport is the full result of scoring a set of cases.
type ToolUseReport struct {
	Matrix   TriggerMatrix
	Payload  PayloadAccuracy
	Outcomes []CaseOutcome
}

// ScoreToolUse runs each case through the caller and produces the trigger
// confusion matrix and payload accuracy. A caller error is recorded on the
// outcome and treated as "no tool called" for trigger purposes, so a model that
// errors when it should have acted is counted as a false negative rather than
// silently dropped.
func ScoreToolUse(ctx context.Context, cases []ToolUseCase, caller ToolCaller) ToolUseReport {
	rep := ToolUseReport{Outcomes: make([]CaseOutcome, 0, len(cases))}

	for _, c := range cases {
		obs, err := caller(ctx, c)
		oc := CaseOutcome{ID: c.ID, ObservedTool: obs.Tool}
		if err != nil {
			oc.Err = err.Error()
			obs.Tool = "" // an error means no usable tool call
		}

		expectsTool := c.Expected != nil
		calledTool := obs.Tool != ""
		if expectsTool {
			oc.ExpectedTool = c.Expected.Tool
		}

		switch {
		case expectsTool && calledTool:
			rep.Matrix.TP++
			oc.TriggerResult = "TP"
			rep.Payload.Evaluated++
			if obs.Tool == c.Expected.Tool {
				oc.CorrectTool = true
				rep.Payload.CorrectTool++
				if c.Expected.ArgsMatch == nil || c.Expected.ArgsMatch(obs.Args) {
					oc.CorrectArgs = true
					rep.Payload.CorrectArgs++
				}
			}
		case expectsTool && !calledTool:
			rep.Matrix.FN++
			oc.TriggerResult = "FN"
		case !expectsTool && calledTool:
			rep.Matrix.FP++
			oc.TriggerResult = "FP"
		default:
			rep.Matrix.TN++
			oc.TriggerResult = "TN"
		}

		rep.Outcomes = append(rep.Outcomes, oc)
	}

	return rep
}

// Markdown renders the report as a compact, human-readable summary.
func (r ToolUseReport) Markdown() string {
	m := r.Matrix
	out := "## Tool-use evaluation\n\n"
	out += "### Trigger confusion matrix\n\n"
	out += "| | tool called | no tool called |\n"
	out += "|---|---|---|\n"
	out += fmt.Sprintf("| **tool expected** | %d (TP) | %d (FN) |\n", m.TP, m.FN)
	out += fmt.Sprintf("| **no tool expected** | %d (FP) | %d (TN) |\n\n", m.FP, m.TN)
	out += fmt.Sprintf("- precision: %.2f  recall: %.2f  F1: %.2f\n\n", m.Precision(), m.Recall(), m.F1())
	out += "### Payload accuracy (over triggered cases)\n\n"
	out += fmt.Sprintf("- correct tool: %.2f (%d/%d)\n", r.Payload.ToolNameRate(), r.Payload.CorrectTool, r.Payload.Evaluated)
	out += fmt.Sprintf("- correct tool + args: %.2f (%d/%d)\n", r.Payload.ArgsRate(), r.Payload.CorrectArgs, r.Payload.Evaluated)

	// List failures (deterministic order) so a regression is actionable.
	var failures []CaseOutcome
	for _, oc := range r.Outcomes {
		if oc.TriggerResult == "FN" || oc.TriggerResult == "FP" || (oc.TriggerResult == "TP" && !oc.CorrectArgs) {
			failures = append(failures, oc)
		}
	}
	if len(failures) > 0 {
		sort.Slice(failures, func(i, j int) bool { return failures[i].ID < failures[j].ID })
		out += "\n### Failures\n\n"
		for _, oc := range failures {
			detail := oc.TriggerResult
			if oc.TriggerResult == "TP" {
				detail = "wrong tool/args"
			}
			out += fmt.Sprintf("- %s: %s (expected %q, got %q)\n",
				oc.ID, detail, orNone(oc.ExpectedTool), orNone(oc.ObservedTool))
		}
	}
	return out
}

func orNone(s string) string {
	if s == "" {
		return "<none>"
	}
	return s
}
