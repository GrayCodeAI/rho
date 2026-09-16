package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDebuggerTool_Metadata(t *testing.T) {
	d := DebuggerTool{}
	if d.Name() != "Debug" {
		t.Errorf("expected name Debug, got %s", d.Name())
	}
	if d.Description() == "" {
		t.Error("expected non-empty description")
	}
	aliases := d.Aliases()
	if len(aliases) != 2 {
		t.Errorf("expected 2 aliases, got %d", len(aliases))
	}

	params := d.Parameters()
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties map")
	}
	if _, ok := props["action"]; !ok {
		t.Error("expected action parameter")
	}
	if _, ok := props["file"]; !ok {
		t.Error("expected file parameter")
	}
	if _, ok := props["line"]; !ok {
		t.Error("expected line parameter")
	}
	if _, ok := props["expression"]; !ok {
		t.Error("expected expression parameter")
	}
}

func TestDebuggerTool_ValidateParams(t *testing.T) {
	tests := []struct {
		name    string
		params  DebuggerInput
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty action",
			params:  DebuggerInput{},
			wantErr: true,
			errMsg:  "action is required",
		},
		{
			name:    "breakpoint without file",
			params:  DebuggerInput{Action: "breakpoint", Line: 10},
			wantErr: true,
			errMsg:  "file is required",
		},
		{
			name:    "breakpoint without line",
			params:  DebuggerInput{Action: "breakpoint", File: "main.go"},
			wantErr: true,
			errMsg:  "line must be a positive integer",
		},
		{
			name:    "breakpoint with negative line",
			params:  DebuggerInput{Action: "breakpoint", File: "main.go", Line: -1},
			wantErr: true,
			errMsg:  "line must be a positive integer",
		},
		{
			name:    "inspect without expression",
			params:  DebuggerInput{Action: "inspect"},
			wantErr: true,
			errMsg:  "expression is required",
		},
		{
			name:    "valid breakpoint",
			params:  DebuggerInput{Action: "breakpoint", File: "main.go", Line: 10},
			wantErr: false,
		},
		{
			name:    "valid inspect",
			params:  DebuggerInput{Action: "inspect", Expression: "x + 1"},
			wantErr: false,
		},
		{
			name:    "valid run",
			params:  DebuggerInput{Action: "run"},
			wantErr: false,
		},
		{
			name:    "valid step",
			params:  DebuggerInput{Action: "step"},
			wantErr: false,
		},
		{
			name:    "valid continue",
			params:  DebuggerInput{Action: "continue"},
			wantErr: false,
		},
		{
			name:    "valid stack",
			params:  DebuggerInput{Action: "stack"},
			wantErr: false,
		},
		{
			name:    "unknown action",
			params:  DebuggerInput{Action: "dance"},
			wantErr: true,
			errMsg:  "unknown action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDebugParams(tt.params)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestDebuggerTool_ExecuteInvalidJSON(t *testing.T) {
	d := DebuggerTool{}
	_, err := d.Execute(context.Background(), json.RawMessage(`{invalid`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestDebuggerTool_ExecuteMissingAction(t *testing.T) {
	d := DebuggerTool{}
	input, _ := json.Marshal(map[string]interface{}{"file": "main.go", "line": 10})
	_, err := d.Execute(context.Background(), input)
	if err == nil {
		t.Error("expected error for missing action")
	}
}
