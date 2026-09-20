package engine

import (
	"context"
	"os"

	"github.com/GrayCodeAI/rho/internal/types"
)

const (
	maxContextMessages = 100 // auto-compact threshold
	maxRecoveryRetries = 3   // max_tokens recovery attempts

	// compactBoundaryKeepEnd is the number of trailing messages preserved by
	// boundary-aware truncation (compact).
	compactBoundaryKeepEnd = 16
	// smartCompactKeepEnd is the number of trailing messages preserved by
	// summary-based compaction (smartCompact and its variants).
	smartCompactKeepEnd = 10
)

// Compact reduces conversation history using boundary-aware truncation.
// Accepts ctx for cancellation and timeout control; if nil, defaults to context.Background.
// Preserves tool_use/tool_result pairs during compaction.
func (s *Session) Compact(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.compact(ctx)
}

// SmartCompact reduces conversation history using LLM-generated summaries.
// Accepts ctx for cancellation and timeout control; if nil, defaults to context.Background.
func (s *Session) SmartCompact(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.smartCompact(ctx)
}

// compact removes older messages while preserving tool_use/tool_result pairing.
func (s *Session) compact(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	keepEnd := compactBoundaryKeepEnd
	if pinned := s.Persistence().PinnedMessages(); pinned > keepEnd {
		keepEnd = pinned
	}
	// Single immutable snapshot for the whole method: repeated RawMessages()
	// calls deep-clone each time (O(n²) on long transcripts) and can observe
	// different states while a concurrent AddUser appends (TOCTOU).
	raw := s.Persistence().RawMessages()
	if len(raw) <= keepEnd+4 {
		return
	}
	// Keep first 4 and last keepEnd, but ensure we don't break tool pairs.
	cutStart := 4
	cutEnd := len(raw) - keepEnd

	// Ensure cutEnd doesn't land in the middle of a tool_use/tool_result pair.
	// A tool_result (user msg with ToolResult) must follow its tool_use (assistant msg with ToolUse).
	// Walk cutEnd forward until we're at a clean boundary.
	for cutEnd < len(raw) {
		if ctx.Err() != nil {
			return
		}
		msg := raw[cutEnd]
		if len(msg.ToolResults) > 0 {
			// This is a tool_result — we'd orphan it. Include it.
			cutEnd++
			continue
		}
		if msg.Role == "assistant" && len(msg.ToolUse) > 0 {
			// This is a tool_use — we need its results too. Skip past them.
			cutEnd++
			continue
		}
		break
	}

	// Also walk cutStart forward to not orphan pairs at the beginning
	for cutStart < cutEnd {
		if ctx.Err() != nil {
			return
		}
		msg := raw[cutStart]
		if len(msg.ToolResults) > 0 {
			// A tool_result at the boundary: its tool_use is earlier in the
			// kept head, so include it too (keeps the pair complete).
			cutStart++
			continue
		}
		if msg.Role == "assistant" && len(msg.ToolUse) > 0 {
			// Include the tool results that follow
			cutStart++
			for cutStart < cutEnd && len(raw[cutStart].ToolResults) > 0 {
				cutStart++
			}
			continue
		}
		break
	}

	if cutStart >= cutEnd {
		return // nothing to compact
	}

	keep := make([]types.FluxMessage, 0, len(raw)-(cutEnd-cutStart)+1)
	keep = append(keep, raw[:cutStart]...)
	keep = append(keep, types.FluxMessage{
		Role:    "user",
		Content: "[Earlier conversation compacted to save context.]",
	})
	keep = append(keep, raw[cutEnd:]...)
	s.Persistence().ApplyCompaction(keep, len(raw))
}

// readFileContent reads a file from disk and returns its content as a string.
// Used by critic and validation to capture original file state.
func readFileContent(path string) (string, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return "", err
	}
	return string(data), nil
}
