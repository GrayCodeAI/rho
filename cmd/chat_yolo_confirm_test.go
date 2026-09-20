package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/GrayCodeAI/rho/internal/engine/safety"
)

func TestYOLOConfirm_PendingConsumesNextInput(t *testing.T) {
	m := newTestChatModel()
	m.pendingYOLOConfirm = true

	// Non-matching input cancels and stays on the previous tier.
	m.input.SetValue("nope")
	next, _ := m.submitUserMessage()
	if next.pendingYOLOConfirm {
		t.Error("pendingYOLOConfirm should be cleared after any submission")
	}
	if next.session.PermSvc().RuntimeState().Autonomy == safety.AutonomyYOLO {
		t.Error("autonomy should NOT be YOLO after a non-matching confirmation")
	}
	if len(next.messages) == 0 || !strings.Contains(next.messages[len(next.messages)-1].content, "cancelled") {
		t.Errorf("expected a cancellation message, got %d messages", len(next.messages))
	}
}

func TestYOLOConfirm_ExactTokenEnables(t *testing.T) {
	m := newTestChatModel()
	m.pendingYOLOConfirm = true

	m.input.SetValue(yoloConfirmToken)
	next, _ := m.submitUserMessage()
	if next.pendingYOLOConfirm {
		t.Error("pendingYOLOConfirm should be cleared after confirmation")
	}
	if next.session.PermSvc().RuntimeState().Autonomy != safety.AutonomyYOLO {
		t.Errorf("autonomy = %v, want YOLO after matching confirmation", next.session.PermSvc().RuntimeState().Autonomy)
	}
}

func TestYOLOConfirm_CaseInsensitive(t *testing.T) {
	m := newTestChatModel()
	m.pendingYOLOConfirm = true

	m.input.SetValue(strings.ToUpper(yoloConfirmToken))
	next, _ := m.submitUserMessage()
	if next.session.PermSvc().RuntimeState().Autonomy != safety.AutonomyYOLO {
		t.Error("confirmation token should match case-insensitively")
	}
}

func TestYOLOConfirm_DoesNotLeakIntoNormalSubmit(t *testing.T) {
	m := newTestChatModel()
	m.pendingYOLOConfirm = false

	m.input.SetValue(yoloConfirmToken)
	next, _ := m.submitUserMessage()
	if next.session.PermSvc().RuntimeState().Autonomy == safety.AutonomyYOLO {
		t.Error("autonomy should not change without a pending confirmation")
	}
}

func TestRecordWALError_SurfacesOnce(t *testing.T) {
	m := newTestChatModel()
	if m.durabilityWarning != "" {
		t.Fatal("expected no warning initially")
	}

	m.recordWALError(fmt.Errorf("disk full"))
	if m.durabilityWarning == "" {
		t.Fatal("expected durability warning after first error")
	}

	// A second error must not overwrite (already surfaced once).
	m.recordWALError(fmt.Errorf("another failure"))
	if !strings.Contains(m.durabilityWarning, "persistence is failing") {
		t.Errorf("warning should keep the first message, got %q", m.durabilityWarning)
	}

	// Nil error must not set the warning.
	m2 := newTestChatModel()
	m2.recordWALError(nil)
	if m2.durabilityWarning != "" {
		t.Error("nil error should not set a durability warning")
	}
}
