package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateToolInput_MissingRequired(t *testing.T) {
	tool := BashTool{}
	err := ValidateToolInput(tool, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for missing required field")
	}
	if !strings.Contains(err.Error(), `"command"`) {
		t.Fatalf("expected error to mention command, got: %v", err)
	}
}

func TestValidateToolInput_EmptyRequired(t *testing.T) {
	tool := BashTool{}
	err := ValidateToolInput(tool, json.RawMessage(`{"command":""}`))
	if err == nil {
		t.Fatal("expected error for empty required field")
	}
}

func TestValidateToolInput_Valid(t *testing.T) {
	tool := BashTool{}
	err := ValidateToolInput(tool, json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Fatalf("expected no error for valid input, got: %v", err)
	}
}

func TestValidateToolInput_Root(t *testing.T) {
	tool := BashTool{}
	err := ValidateToolInput(tool, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestValidateToolInput_UnknownToolNoSchema(t *testing.T) {
	// No required array → no validation (e.g. WebSearch's exclusive-or is
	// validated inside Execute, so the schema declares no required fields).
	tool := WebSearchTool{}
	err := ValidateToolInput(tool, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("expected no error for tool with no required fields, got: %v", err)
	}
}

func TestValidateToolInput_InvalidJSON(t *testing.T) {
	tool := BashTool{}
	err := ValidateToolInput(tool, json.RawMessage(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("expected invalid JSON error, got: %v", err)
	}
}

func TestValidateToolInput_AliasSatisfiesRequired(t *testing.T) {
	// Read requires "path", which is satisfied by the file_path archive alias.
	tool := FileReadTool{}
	err := ValidateToolInput(tool, json.RawMessage(`{"file_path":"README.md"}`))
	if err != nil {
		t.Fatalf("expected file_path alias to satisfy required path, got: %v", err)
	}
	// ...and by path itself.
	err = ValidateToolInput(tool, json.RawMessage(`{"path":"README.md"}`))
	if err != nil {
		t.Fatalf("expected path to satisfy required path, got: %v", err)
	}
	// Missing entirely → error.
	err = ValidateToolInput(tool, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error when both path and file_path are absent")
	}
}

func TestValidateToolInput_WriteRequired(t *testing.T) {
	tool := FileWriteTool{}
	err := ValidateToolInput(tool, json.RawMessage(`{"path":"f.txt"}`))
	if err == nil {
		t.Fatal("expected error: content is required for Write")
	}
	err = ValidateToolInput(tool, json.RawMessage(`{"path":"f.txt","content":"x"}`))
	if err != nil {
		t.Fatalf("expected valid Write input to pass, got: %v", err)
	}
}

func TestValidateToolInput_EditRequiredUsesPrimaryFields(t *testing.T) {
	tool := FileEditTool{}
	// old_string is an archive alias for old_str.
	err := ValidateToolInput(tool, json.RawMessage(`{"path":"f.go","old_string":"a"}`))
	if err != nil {
		t.Fatalf("expected old_string alias to satisfy required old_str, got: %v", err)
	}
}

// TestRegistryExecuteValidatesInput verifies Registry.Execute rejects
// malformed input before dispatch (H5 wiring).
func TestRegistryExecuteValidatesInput(t *testing.T) {
	reg := NewRegistry(BashTool{})
	_, err := reg.Execute(context.Background(), "Bash", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected Registry.Execute to reject input missing required field")
	}
	// Valid input reaches the tool (Bash runs echo directly after policy checks).
	out, err := reg.Execute(context.Background(), "Bash", json.RawMessage(`{"command":"echo registry-ok"}`))
	if err != nil {
		t.Fatalf("expected valid input to execute, got: %v", err)
	}
	if !strings.Contains(out, "registry-ok") {
		t.Fatalf("expected output to contain registry-ok, got: %q", out)
	}
}
