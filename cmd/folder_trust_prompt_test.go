package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/GrayCodeAI/rho/internal/engine"
)

func TestPromptForInteractiveFolderTrustAccepts(t *testing.T) {
	var out bytes.Buffer
	trusted := false
	status := engine.ProjectTrustStatus{Path: "/repo", Blocked: true}

	if !promptForInteractiveFolderTrust(strings.NewReader("y\n"), &out, status, func() error {
		trusted = true
		return nil
	}) {
		t.Fatal("expected trust prompt to accept y")
	}
	if !trusted || !strings.Contains(out.String(), "Folder trusted") {
		t.Fatalf("accept output = %q, trusted=%v", out.String(), trusted)
	}
}

func TestPromptForInteractiveFolderTrustDeclinesAndContinuesRestricted(t *testing.T) {
	var out bytes.Buffer
	called := false
	status := engine.ProjectTrustStatus{Path: "/repo", Blocked: true}

	if promptForInteractiveFolderTrust(strings.NewReader("\n"), &out, status, func() error {
		called = true
		return nil
	}) {
		t.Fatal("expected empty answer to decline trust")
	}
	if called || !strings.Contains(out.String(), "restricted mode") {
		t.Fatalf("decline output = %q, trustFn called=%v", out.String(), called)
	}
}

func TestPromptForInteractiveFolderTrustHandlesSaveFailure(t *testing.T) {
	var out bytes.Buffer
	status := engine.ProjectTrustStatus{Path: "/repo", Blocked: true}

	if promptForInteractiveFolderTrust(strings.NewReader("y\n"), &out, status, func() error {
		return errors.New("read-only trust store")
	}) {
		t.Fatal("expected trust save failure to remain restricted")
	}
	if !strings.Contains(out.String(), "Continuing in restricted mode") {
		t.Fatalf("save failure output = %q", out.String())
	}
}

func TestPromptForInteractiveFolderTrustSkipsWhenNotBlocked(t *testing.T) {
	var out bytes.Buffer
	called := false
	if promptForInteractiveFolderTrust(strings.NewReader("y\n"), &out, engine.ProjectTrustStatus{}, func() error {
		called = true
		return nil
	}) {
		t.Fatal("unblocked status should not prompt")
	}
	if called || out.Len() != 0 {
		t.Fatalf("unblocked prompt wrote %q or called trustFn=%v", out.String(), called)
	}
}
