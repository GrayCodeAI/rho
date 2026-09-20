package fingerprint

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectConventions_EditorConfig(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".editorconfig"), `root = true

[*]
indent_style = tab
indent_size = 4
`)

	convs := detectConventions(dir, "Go")

	found := false
	for _, c := range convs {
		if c.Name == "indentation" {
			found = true
			if !strings.Contains(c.Description, "Tab") {
				t.Errorf("expected tab indentation, got %q", c.Description)
			}
			if c.Confidence != 1.0 {
				t.Errorf("expected confidence 1.0, got %f", c.Confidence)
			}
		}
	}
	if !found {
		t.Error("expected indentation convention to be detected from .editorconfig")
	}
}

func TestDetectConventions_SpacesEditorConfig(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".editorconfig"), `root = true

[*]
indent_style = space
indent_size = 2
`)

	convs := detectConventions(dir, "TypeScript")

	found := false
	for _, c := range convs {
		if c.Name == "indentation" {
			found = true
			if !strings.Contains(c.Description, "2-space") {
				t.Errorf("expected 2-space indentation, got %q", c.Description)
			}
		}
	}
	if !found {
		t.Error("expected indentation convention from .editorconfig")
	}
}

func TestDetectConventions_GoNaming(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")

	convs := detectConventions(dir, "Go")

	found := false
	for _, c := range convs {
		if c.Name == "naming" {
			found = true
			if !strings.Contains(c.Description, "camelCase") {
				t.Errorf("expected camelCase/PascalCase for Go, got %q", c.Description)
			}
		}
	}
	if !found {
		t.Error("expected naming convention for Go")
	}
}

func TestDetectConventions_GoErrorWrapping(t *testing.T) {
	dir := t.TempDir()
	content := `package main

import "fmt"

func foo() error {
	err := bar()
	if err != nil {
		return fmt.Errorf("foo: %w", err)
	}
	err2 := baz()
	if err2 != nil {
		return fmt.Errorf("baz failed: %w", err2)
	}
	return nil
}
`
	writeTestFile(t, filepath.Join(dir, "main.go"), content)

	convs := detectConventions(dir, "Go")

	found := false
	for _, c := range convs {
		if c.Name == "error-handling" {
			found = true
			if !strings.Contains(c.Description, "wrapping") {
				t.Errorf("expected error wrapping convention, got %q", c.Description)
			}
		}
	}
	if !found {
		t.Error("expected error-handling convention to be detected")
	}
}

func TestDetectConventions_PythonNaming(t *testing.T) {
	dir := t.TempDir()
	content := `def get_user_name():
    pass

def calculate_total_price():
    pass

def handle_request_error():
    pass
`
	writeTestFile(t, filepath.Join(dir, "app.py"), content)

	convs := detectConventions(dir, "Python")

	found := false
	for _, c := range convs {
		if c.Name == "naming" {
			found = true
			if !strings.Contains(c.Description, "snake_case") {
				t.Errorf("expected snake_case for Python, got %q", c.Description)
			}
		}
	}
	if !found {
		t.Error("expected naming convention for Python")
	}
}
