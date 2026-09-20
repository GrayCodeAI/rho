package eventlog

import (
	"errors"
	"fmt"
)

// ErrUnknownType is returned when a durable record carries an event kind this
// build does not know. The stance is fail-loud: a record the accountable
// build cannot project must not be silently reinterpreted.
var ErrUnknownType = errors.New("eventlog: unknown event type")

// Validate checks the snapshot for invariant violations before it is trusted:
// every event kind must be known, the format version (read from the SessionMeta
// head event) must match SessionFormatVersion, surface-eligible events must
// carry a SurfaceOp marker (and non-surface events must not), and sequence numbers
// must be strictly increasing (the record is append-only). It returns the first
// violation.
func Validate(events []Event) error {
	var prev uint64
	for i, ev := range events {
		if !ev.Type.Known() {
			return fmt.Errorf("%w: %q at index %d", ErrUnknownType, ev.Type, i)
		}
		if ev.Type == SessionMeta {
			if meta, ok := ev.Data.(Meta); ok {
				if meta.FormatVersion != 0 && meta.FormatVersion != SessionFormatVersion {
					return fmt.Errorf("%w: log version %d, this build reads %d (session event at index %d)",
						ErrForeignFormatVersion, meta.FormatVersion, SessionFormatVersion, i)
				}
			}
		}
		// DSH invariant: surface-eligible events must carry a surfaceOp marker;
		// non-surface events must not. A log without markers is treated as
		// all-append (backward compatible with version-1 logs written before
		// this invariant was enforced).
		if ev.Type.IsSurfaceEligible() {
			if ev.SurfaceOp != nil && ev.SurfaceOp.Op != "append" && ev.SurfaceOp.Op != "replace" {
				return fmt.Errorf("eventlog: invalid surfaceOp %q at index %d (must be append or replace)", ev.SurfaceOp.Op, i)
			}
		} else if ev.SurfaceOp != nil {
			return fmt.Errorf("eventlog: non-surface event %q at index %d carries a surfaceOp marker", ev.Type, i)
		}
		if i > 0 && ev.Seq <= prev {
			return fmt.Errorf("eventlog: sequence not monotonic at index %d: seq %d after %d", i, ev.Seq, prev)
		}
		prev = ev.Seq
	}
	return nil
}

// relationalTrace tracks the open turn/step state and pending tool calls
// during relational validation, ported from DSH's session-trace bookkeeping.
type relationalTrace struct {
	lastSeq      uint64
	openTurn     int    // 0 = no open turn
	openStep     int    // 0 = no open step
	nextTurn     uint64 // expected next turn number
	nextStep     uint64 // expected next step number
	pendingCalls map[string]bool
}

// ToolNotStarted is the synthetic error code DSH emits when a tool call
// never started (e.g. fail-closed denial before execution). A tool/result
// with this code is exempt from the pending-call pairing check. Ported from
// DSH's repair.ts.
const ToolNotStarted = "TOOL_NOT_STARTED"

// ValidateRelations performs DSH-compatible relational invariant checking beyond
// the structural Validate: turn/step nesting, sequence contiguity, and tool
// call↔result pairing. This ports the per-event relational checks from
// DSH's session/src/invariant.ts.
func ValidateRelations(events []Event) error {
	trace := &relationalTrace{
		lastSeq:      0,
		openTurn:     0,
		openStep:     0,
		nextTurn:     1,
		nextStep:     1,
		pendingCalls: make(map[string]bool),
	}

	for i, ev := range events {
		if ev.Seq <= trace.lastSeq {
			return fmt.Errorf("eventlog: seq must strictly increase: saw %d after %d (index %d)", ev.Seq, trace.lastSeq, i)
		}
		trace.lastSeq = ev.Seq

		switch ev.Type {
		case TurnStart:
			f, ok := ev.Data.(BoundaryFact)
			if !ok {
				return fmt.Errorf("eventlog: turn/start has wrong payload type at index %d", i)
			}
			if trace.openTurn != 0 {
				return fmt.Errorf("eventlog: turn/start turn %d while turn %d is still open (index %d)", f.Turn, trace.openTurn, i)
			}
			if uint64(f.Turn) != trace.nextTurn {
				return fmt.Errorf("eventlog: turn/start expected turn %d, got %d (index %d)", trace.nextTurn, f.Turn, i)
			}
			trace.openTurn = f.Turn
			trace.nextStep = 1
		case TurnEnd:
			f, ok := ev.Data.(TurnEndFact)
			if !ok {
				return fmt.Errorf("eventlog: turn/end has wrong payload type at index %d", i)
			}
			if trace.openTurn != f.Turn {
				return fmt.Errorf("eventlog: turn/end turn %d does not match open turn %d (index %d)", f.Turn, trace.openTurn, i)
			}
			if trace.openStep != 0 {
				return fmt.Errorf("eventlog: turn/end turn %d while step %d is still open (index %d)", f.Turn, trace.openStep, i)
			}
			trace.openTurn = 0
			trace.nextTurn++
		case StepStart:
			f, ok := ev.Data.(BoundaryFact)
			if !ok {
				return fmt.Errorf("eventlog: step/start has wrong payload type at index %d", i)
			}
			if trace.openTurn != f.Turn {
				return fmt.Errorf("eventlog: step/start turn %d but open turn is %d (index %d)", f.Turn, trace.openTurn, i)
			}
			if trace.openStep != 0 {
				return fmt.Errorf("eventlog: step/start step %d while step %d is still open (index %d)", f.Step, trace.openStep, i)
			}
			if uint64(f.Step) != trace.nextStep {
				return fmt.Errorf("eventlog: step/start expected step %d in turn %d, got %d (index %d)", trace.nextStep, f.Turn, f.Step, i)
			}
			trace.openStep = f.Step
			trace.pendingCalls = make(map[string]bool)
		case StepEnd:
			f, ok := ev.Data.(BoundaryFact)
			if !ok {
				return fmt.Errorf("eventlog: step/end has wrong payload type at index %d", i)
			}
			if trace.openTurn != f.Turn {
				return fmt.Errorf("eventlog: step/end turn %d does not match open turn %d (index %d)", f.Turn, trace.openTurn, i)
			}
			if trace.openStep != f.Step {
				return fmt.Errorf("eventlog: step/end step %d doesn't match open step %d (index %d)", f.Step, trace.openStep, i)
			}
			trace.pendingCalls = make(map[string]bool)
			trace.openStep = 0
			trace.nextStep++
		case AssistantChunk:
			f, ok := ev.Data.(ChunkFact)
			if !ok {
				return fmt.Errorf("eventlog: assistant/chunk has wrong payload type at index %d", i)
			}
			if trace.openTurn != f.Turn {
				return fmt.Errorf("eventlog: assistant/chunk names turn %d but open is %d (index %d)", f.Turn, trace.openTurn, i)
			}
			if trace.openStep != f.Step {
				return fmt.Errorf("eventlog: assistant/chunk names step %d but open is %d (index %d)", f.Step, trace.openStep, i)
			}
		case AssistantMsg:
			f, ok := ev.Data.(Message)
			if !ok {
				return fmt.Errorf("eventlog: assistant/message has wrong payload type at index %d", i)
			}
			if trace.openTurn != f.Turn {
				return fmt.Errorf("eventlog: assistant/message names turn %d but open is %d (index %d)", f.Turn, trace.openTurn, i)
			}
			if trace.openStep != f.Step {
				return fmt.Errorf("eventlog: assistant/message names step %d but open is %d (index %d)", f.Step, trace.openStep, i)
			}
		case ToolCall:
			f, ok := ev.Data.(ToolCallPayload)
			if !ok {
				return fmt.Errorf("eventlog: tool/call has wrong payload type at index %d", i)
			}
			if trace.openTurn != f.Turn {
				return fmt.Errorf("eventlog: tool/call names turn %d but open is %d (index %d)", f.Turn, trace.openTurn, i)
			}
			if trace.openStep != f.Step {
				return fmt.Errorf("eventlog: tool/call names step %d but open is %d (index %d)", f.Step, trace.openStep, i)
			}
			if f.ID != "" {
				trace.pendingCalls[f.ID] = true
			}
		case ToolResult:
			// Surface replacements (e.g. compaction rewrites) are exempt from
			// pairing — they are durable edits, not fresh tool results.
			if ev.SurfaceOp != nil && ev.SurfaceOp.Op == "replace" {
				continue
			}
			f, ok := ev.Data.(ToolResultPayload)
			if !ok {
				return fmt.Errorf("eventlog: tool/result has wrong payload type at index %d", i)
			}
			if trace.openTurn != f.Turn {
				return fmt.Errorf("eventlog: tool/result names turn %d but open is %d (index %d)", f.Turn, trace.openTurn, i)
			}
			if trace.openStep != f.Step {
				return fmt.Errorf("eventlog: tool/result names step %d but open is %d (index %d)", f.Step, trace.openStep, i)
			}
			callID := f.ToolUseID
			if callID != "" && !trace.pendingCalls[callID] {
				// Synthetic tool-not-started results are exempt (fail-closed
				// denials before execution) — they carry the error code but
				// have no real prior tool/call.
				if f.IsError && f.ToolUseID == "" {
					continue
				}
				return fmt.Errorf("eventlog: tool/result for %q with no prior tool/call in this step (index %d)", callID, i)
			}
			if callID != "" {
				delete(trace.pendingCalls, callID)
			}
		case TodoWrite, RequestHeader, RequestContext:
			// Core execution events must be turn-enclosed.
			if trace.openTurn == 0 {
				return fmt.Errorf("eventlog: %s appended outside any open turn (index %d)", ev.Type, i)
			}
		default:
			// All other types (context.injected, compaction/*, goal.change,
			// other metadata events are unconstrained — they may be appended
			// between model executions.
		}
	}
	return nil
}
