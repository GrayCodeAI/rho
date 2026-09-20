package eventlog

import (
	"encoding/json"
	"fmt"
	"time"
)

// WireEvent is the on-disk and over-the-wire shape of one durable event. It is
// distinct from the in-memory Event so the storage schema and the live record can
// stay byte-compatible and the log can decode its own replay without importing any
// product storage package.
type WireEvent struct {
	Type      Type            `json:"type"`
	Seq       uint64          `json:"seq"`
	At        time.Time       `json:"at"`
	Data      json.RawMessage `json:"data,omitempty"`
	Ignorable bool            `json:"ignorable,omitempty"`
	// SurfaceOp records how this event entered the model-visible surface.
	// Optional — only present on surface-eligible types (UserMessage,
	// AssistantMsg, ToolResult). Ported from DSH's surfaceOp invariant.
	SurfaceOp *SurfaceOp `json:"surface_op,omitempty"`
	// SourceEventSeqs records the earlier event seqs this event cites as
	// sources. Optional — used for replacement provenance. Ported from DSH.
	SourceEventSeqs []uint64 `json:"source_event_seqs,omitempty"`
}

// SurfaceOp describes how a surface-eligible event entered the model-visible
// surface, ported from DSH's SurfaceOp type.
type SurfaceOp struct {
	// Op is "append" (added to tail) or "replace" (shadows a range).
	Op string `json:"op"`
	// Start is the inclusive start seq of the replaced range (replace only).
	Start uint64 `json:"start,omitempty"`
	// End is the inclusive end seq of the replaced range (replace only).
	End uint64 `json:"end,omitempty"`
}

// IsSurfaceEligible reports whether t is one of the three message-producing
// event types that carry SurfaceOp. Ported from DSH's isSurfaceEligibleType.
func (t Type) IsSurfaceEligible() bool {
	switch t {
	case UserMessage, AssistantMsg, ToolResult:
		return true
	default:
		return false
	}
}

// AppendSurface appends a surface-eligible event (UserMessage, AssistantMsg,
// ToolResult) with a SurfaceOp marker, matching DSH's surface invariant:
// every message-producing event carries how it entered the surface.
// The op parameter is "append" or "replace"; start/end are the replaced range
// bounds (only used for "replace"). sourceSeqs are the cited earlier event
// seqs for replacement provenance.
func (l *Log) AppendSurface(typ Type, data any, op string, start, end uint64, sourceSeqs []uint64) {
	if l == nil {
		return
	}
	if !typ.IsSurfaceEligible() {
		panic(fmt.Sprintf("eventlog: %q is not surface-eligible", typ))
	}
	l.mu.Lock()
	l.seq++
	ev := Event{
		Type:            typ,
		Seq:             l.seq,
		At:              time.Now(),
		Data:            data,
		SurfaceOp:       &SurfaceOp{Op: op, Start: start, End: end},
		SourceEventSeqs: sourceSeqs,
	}
	l.events = append(l.events, ev)
	l.byType[typ] = append(l.byType[typ], ev)
	l.mu.Unlock()
	if l.listener != nil {
		l.listener(ev)
	}
}

// MarshalWire converts in-memory events to their persisted shape.
func MarshalWire(events []Event) ([]WireEvent, error) {
	out := make([]WireEvent, len(events))
	for i, ev := range events {
		var raw json.RawMessage
		if ev.Data != nil {
			b, err := json.Marshal(ev.Data)
			if err != nil {
				return nil, err
			}
			raw = b
		}
		out[i] = WireEvent{Type: ev.Type, Seq: ev.Seq, At: ev.At, Data: raw, Ignorable: ev.Ignorable, SurfaceOp: ev.SurfaceOp, SourceEventSeqs: ev.SourceEventSeqs}
	}
	return out, nil
}

// DecodeWire validates and loads a wire record. Unknown kinds fail loud via
// Validate. Each type's Data blob is decoded back into its typed in-memory payload
// (Meta, Message, ToolCallPayload, ToolResultPayload) so a loaded log can be
// projected without a second decode hop in the consumer.
func DecodeWire(wire []WireEvent) ([]Event, error) {
	events := make([]Event, len(wire))
	for i, w := range wire {
		data, err := decodePayload(w)
		if err != nil {
			return nil, err
		}
		events[i] = Event{Type: w.Type, Seq: w.Seq, At: w.At, Data: data, Ignorable: w.Ignorable, SurfaceOp: w.SurfaceOp, SourceEventSeqs: w.SourceEventSeqs}
	}
	return events, Validate(events)
}

// decodePayload converts a wire Data blob into the payload struct for w.Type.
// A nil or empty blob decodes to nil; a non-empty blob must unmarshal cleanly.
func decodePayload(w WireEvent) (any, error) {
	if len(w.Data) == 0 {
		return nil, nil
	}
	switch w.Type {
	case SessionMeta:
		var p Meta
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case UserMessage, AssistantMsg:
		var p Message
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case AssistantChunk:
		var p ChunkFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case ContextInjected:
		var p ContextInjectedFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case SessionCompacted:
		var p CompactionFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case ToolCall:
		var p ToolCallPayload
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case ToolResult:
		var p ToolResultPayload
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case SpecState:
		var p SpecFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case TurnStart, StepStart, StepEnd:
		var p BoundaryFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case TurnEnd:
		var p TurnEndFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case PermissionChange:
		var p PermissionFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case ApprovalAsked:
		var p ApprovalAskedFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case ApprovalDecided:
		var p ApprovalDecidedFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case ApprovalPolicy:
		var p ApprovalPolicyFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	// --- Lifecycle events ported from DeepSeek Harness ---
	case CompactionStart:
		var p CompactionStartFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case CompactionPrune:
		var p CompactionPruneFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case CompactionEnd:
		var p CompactionEndFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case CompactionSummary:
		var p CompactionSummaryFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case SessionEndSeed:
		return nil, nil
	case TodoWrite:
		var p TodoWriteFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case RequestHeader:
		var p RequestHeaderFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case HookInvoked:
		var p HookInvokedFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case HookResult:
		var p HookResultFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case FeedbackRecord:
		var p FeedbackFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case GoalChange:
		var p GoalChangeFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case PermissionPreset:
		var p PermissionPresetFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case ScheduleChange:
		var p ScheduleChangeFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case ScheduleCreate:
		var p ScheduleCreateFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case ScheduleUpdate:
		var p ScheduleUpdateFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case ScheduleDelete:
		var p ScheduleDeleteFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case ScheduleDue:
		var p ScheduleDueFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case SessionTitle:
		var p SessionTitleFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case SessionTitleLLMRequest:
		var p SessionTitleLLMRequestFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case SubagentDescriptor:
		var p SubagentDescriptorFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case AgentPresetSelected:
		var p AgentPresetFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case AgentInboxSpliced:
		var p AgentInboxSpliceFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case CommandRun:
		var p CommandRunFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case CommandDone:
		var p CommandDoneFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case ToolWorkflowAgentStart:
		var p ToolWorkflowAgentFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case ToolWorkflowAgentEnd:
		var p ToolWorkflowAgentFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case ToolCodeDispatch:
		var p CodeDispatchFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case ToolCodeDispatchStart:
		var p CodeDispatchFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case WebDeepSeekSearch:
		var p WebSearchFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	case PlanMode:
		var p PlanModeFact
		if err := json.Unmarshal(w.Data, &p); err != nil {
			return nil, err
		}
		return p, nil
	default:
		return nil, nil
	}
}
