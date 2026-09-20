package errs

import (
	"strings"
	"sync"
	"testing"
)

func TestNewErrorContext(t *testing.T) {
	ec := NewErrorContext()
	if ec == nil {
		t.Fatal("NewErrorContext returned nil")
	}
	if len(ec.Patterns) < 30 {
		t.Errorf("expected at least 30 patterns, got %d", len(ec.Patterns))
	}
}

func TestEnrich_GoNilPointer(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("runtime error: invalid memory address or nil pointer dereference")
	if enriched == nil {
		t.Fatal("expected enriched error for nil pointer dereference")
	}
	if enriched.Title != "Nil pointer dereference" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
	if enriched.Severity != "CRITICAL" {
		t.Errorf("expected CRITICAL severity, got %s", enriched.Severity)
	}
	if enriched.Recoverable {
		t.Error("nil pointer dereference should not be recoverable")
	}
	if len(enriched.Suggestions) == 0 {
		t.Error("expected suggestions for nil pointer dereference")
	}
}

func TestEnrich_GoUndefined(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("./main.go:10: undefined: myFunc")
	if enriched == nil {
		t.Fatal("expected enriched error for undefined identifier")
	}
	if enriched.Title != "Undefined identifier" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
	if enriched.Severity != "MEDIUM" {
		t.Errorf("expected MEDIUM severity, got %s", enriched.Severity)
	}
}

func TestEnrich_GoTypeMismatch(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("cannot use x (type int) as type string in argument")
	if enriched == nil {
		t.Fatal("expected enriched error for type mismatch")
	}
	if enriched.Title != "Type mismatch" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_GoImportCycle(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("import cycle not allowed: pkg/a -> pkg/b -> pkg/a")
	if enriched == nil {
		t.Fatal("expected enriched error for import cycle")
	}
	if enriched.Title != "Import cycle detected" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
	if enriched.Severity != "HIGH" {
		t.Errorf("expected HIGH severity, got %s", enriched.Severity)
	}
}

func TestEnrich_GoTooManyArgs(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("too many arguments in call to foo")
	if enriched == nil {
		t.Fatal("expected enriched error for too many arguments")
	}
	if enriched.Title != "Too many arguments in function call" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_GoNotEnoughArgs(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("not enough arguments in call to bar")
	if enriched == nil {
		t.Fatal("expected enriched error for not enough arguments")
	}
	if enriched.Title != "Not enough arguments in function call" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_GoDeadlock(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("fatal error: all goroutines are asleep - deadlock!")
	if enriched == nil {
		t.Fatal("expected enriched error for deadlock")
	}
	if enriched.Title != "Goroutine deadlock" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
	if enriched.Severity != "CRITICAL" {
		t.Errorf("expected CRITICAL severity, got %s", enriched.Severity)
	}
	if enriched.Recoverable {
		t.Error("deadlock should not be recoverable")
	}
}

func TestEnrich_PythonIndentation(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("IndentationError: unexpected indent")
	if enriched == nil {
		t.Fatal("expected enriched error for IndentationError")
	}
	if enriched.Title != "Python indentation error" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_PythonImport(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("ImportError: No module named 'requests'")
	if enriched == nil {
		t.Fatal("expected enriched error for ImportError")
	}
	if enriched.Title != "Python import error" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_PythonType(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("TypeError: unsupported operand type(s) for +: 'int' and 'str'")
	if enriched == nil {
		t.Fatal("expected enriched error for TypeError")
	}
	if enriched.Title != "Python type error" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_PythonAttribute(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("AttributeError: 'NoneType' object has no attribute 'split'")
	if enriched == nil {
		t.Fatal("expected enriched error for AttributeError")
	}
	if enriched.Title != "Python attribute error" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_JSModuleNotFound(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("Error: Cannot find module 'lodash'")
	if enriched == nil {
		t.Fatal("expected enriched error for Cannot find module")
	}
	if enriched.Title != "Module not found" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_JSNotAFunction(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("TypeError: foo.bar is not a function")
	if enriched == nil {
		t.Fatal("expected enriched error for is not a function")
	}
	if enriched.Title != "Not a function" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_JSUndefinedNotObject(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("TypeError: undefined is not an object (evaluating 'a.b')")
	if enriched == nil {
		t.Fatal("expected enriched error for undefined is not an object")
	}
	if enriched.Title != "Cannot access property of undefined" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_GitMergeConflict(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("CONFLICT (content): Merge conflict in main.go")
	if enriched == nil {
		t.Fatal("expected enriched error for merge conflict")
	}
	if enriched.Title != "Git merge conflict" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
	if enriched.Severity != "HIGH" {
		t.Errorf("expected HIGH severity, got %s", enriched.Severity)
	}
}

func TestEnrich_GitNotARepo(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("fatal: not a git repository (or any parent up to mount point /)")
	if enriched == nil {
		t.Fatal("expected enriched error for not a git repository")
	}
	if enriched.Title != "Not a git repository" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_GitNothingToCommit(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("nothing to commit, working tree clean")
	if enriched == nil {
		t.Fatal("expected enriched error for nothing to commit")
	}
	if enriched.Title != "Nothing to commit" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
	if enriched.Severity != "LOW" {
		t.Errorf("expected LOW severity, got %s", enriched.Severity)
	}
}

func TestEnrich_SysPermissionDenied(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("open /etc/shadow: permission denied")
	if enriched == nil {
		t.Fatal("expected enriched error for permission denied")
	}
	if enriched.Title != "Permission denied" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
	if enriched.Severity != "HIGH" {
		t.Errorf("expected HIGH severity, got %s", enriched.Severity)
	}
}

func TestEnrich_SysNoSuchFile(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("stat /foo/bar: no such file or directory")
	if enriched == nil {
		t.Fatal("expected enriched error for no such file")
	}
	if enriched.Title != "File or directory not found" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_SysAddressInUse(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("listen tcp :8080: bind: address already in use")
	if enriched == nil {
		t.Fatal("expected enriched error for address already in use")
	}
	if enriched.Title != "Address already in use" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_SysConnectionRefused(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("dial tcp 127.0.0.1:3000: connection refused")
	if enriched == nil {
		t.Fatal("expected enriched error for connection refused")
	}
	if enriched.Title != "Connection refused" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_RhoOldStrNotFound(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("edit failed: old_str not found in file.go")
	if enriched == nil {
		t.Fatal("expected enriched error for old_str not found")
	}
	if enriched.Title != "Edit target string not found" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
	if !enriched.Recoverable {
		t.Error("old_str not found should be recoverable")
	}
}

func TestEnrich_RhoFileTooLarge(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("cannot read: file too large (50MB)")
	if enriched == nil {
		t.Fatal("expected enriched error for file too large")
	}
	if enriched.Title != "File exceeds size limit" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_RhoBudgetExceeded(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("session terminated: budget exceeded ($5.00 limit)")
	if enriched == nil {
		t.Fatal("expected enriched error for budget exceeded")
	}
	if enriched.Title != "Token or cost budget exceeded" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
	if enriched.Severity != "HIGH" {
		t.Errorf("expected HIGH severity, got %s", enriched.Severity)
	}
}

func TestEnrich_NoMatch(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("some completely unknown error xyz123")
	if enriched != nil {
		t.Errorf("expected nil for unknown error, got: %+v", enriched)
	}
}

func TestFormatError(t *testing.T) {
	enriched := &EnrichedError{
		Original:    "nil pointer dereference",
		Title:       "Nil pointer dereference",
		Explanation: "A nil pointer was accessed.",
		Suggestions: []string{"Check if nil before use", "Add nil guard"},
		Examples:    []string{"if obj != nil { obj.Method() }"},
		Severity:    "CRITICAL",
		Recoverable: false,
	}

	result := FormatError(enriched)

	if !strings.Contains(result, "Error: Nil pointer dereference") {
		t.Error("formatted output should contain the title")
	}
	if !strings.Contains(result, "─────────────────────────────────") {
		t.Error("formatted output should contain separator")
	}
	if !strings.Contains(result, "A nil pointer was accessed.") {
		t.Error("formatted output should contain explanation")
	}
	if !strings.Contains(result, "• Check if nil before use") {
		t.Error("formatted output should contain suggestions with bullet")
	}
	if !strings.Contains(result, "• Add nil guard") {
		t.Error("formatted output should contain all suggestions")
	}
	if !strings.Contains(result, "if obj != nil { obj.Method() }") {
		t.Error("formatted output should contain examples")
	}
	if !strings.Contains(result, "Severity: CRITICAL") {
		t.Error("formatted output should contain severity")
	}
	if !strings.Contains(result, "Recoverable: no") {
		t.Error("formatted output should show recoverable as no")
	}
}

func TestFormatError_Nil(t *testing.T) {
	result := FormatError(nil)
	if result != "" {
		t.Errorf("FormatError(nil) should return empty string, got: %s", result)
	}
}

func TestFormatError_Recoverable(t *testing.T) {
	enriched := &EnrichedError{
		Title:       "Test error",
		Explanation: "Test explanation.",
		Severity:    "LOW",
		Recoverable: true,
	}

	result := FormatError(enriched)
	if !strings.Contains(result, "Recoverable: yes") {
		t.Error("formatted output should show recoverable as yes")
	}
}

func TestAddPattern(t *testing.T) {
	ec := NewErrorContext()
	initialCount := len(ec.Patterns)

	err := ec.AddPattern(`custom error: (\w+) failed`, ErrorHelp{
		Title:       "Custom error",
		Explanation: "A custom operation failed.",
		Suggestions: []string{"Retry the operation", "Check logs"},
		Examples:    []string{"// retry with backoff"},
	})
	if err != nil {
		t.Fatalf("AddPattern returned error: %v", err)
	}

	if len(ec.Patterns) != initialCount+1 {
		t.Errorf("expected %d patterns after add, got %d", initialCount+1, len(ec.Patterns))
	}

	enriched := ec.Enrich("custom error: deploy failed")
	if enriched == nil {
		t.Fatal("expected enriched error for custom pattern")
	}
	if enriched.Title != "Custom error" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestAddPattern_InvalidRegex(t *testing.T) {
	ec := NewErrorContext()
	err := ec.AddPattern(`[invalid`, ErrorHelp{
		Title: "Bad pattern",
	})
	if err == nil {
		t.Error("expected error for invalid regex pattern")
	}
}

func TestIsRecoverable(t *testing.T) {
	ec := NewErrorContext()

	tests := []struct {
		err         string
		recoverable bool
	}{
		{"nil pointer dereference", false},
		{"fatal error: all goroutines are asleep - deadlock!", false},
		{"out of memory", false},
		{"no such file or directory", true},
		{"permission denied", true},
		{"old_str not found", true},
		{"Cannot find module 'foo'", true},
		{"IndentationError: unexpected indent", true},
		{"nothing to commit", true},
		{"no space left on device", false},
	}

	for _, tt := range tests {
		t.Run(tt.err, func(t *testing.T) {
			got := ec.IsRecoverable(tt.err)
			if got != tt.recoverable {
				t.Errorf("IsRecoverable(%q) = %v, want %v", tt.err, got, tt.recoverable)
			}
		})
	}
}

func TestSuggestFix(t *testing.T) {
	ec := NewErrorContext()

	// old_str not found has an AutoFix
	fix := ec.SuggestFix("old_str not found in file.go")
	if fix == "" {
		t.Error("expected a fix suggestion for old_str not found")
	}
	if !strings.Contains(fix, "Re-read") {
		t.Errorf("unexpected fix suggestion: %s", fix)
	}

	// file too large has an AutoFix
	fix = ec.SuggestFix("file too large")
	if fix == "" {
		t.Error("expected a fix suggestion for file too large")
	}

	// nil pointer - no AutoFix, falls back to first suggestion
	fix = ec.SuggestFix("nil pointer dereference")
	if fix == "" {
		t.Error("expected a fix suggestion for nil pointer")
	}

	// unknown error - no suggestion
	fix = ec.SuggestFix("completely unknown error xyz")
	if fix != "" {
		t.Errorf("expected empty suggestion for unknown error, got: %s", fix)
	}
}

func TestSuggestFix_FallsBackToFirstSuggestion(t *testing.T) {
	ec := NewErrorContext()

	// Permission denied has no AutoFix but has suggestions
	fix := ec.SuggestFix("permission denied")
	if fix == "" {
		t.Error("expected a suggestion for permission denied")
	}
	if !strings.Contains(fix, "ls -la") {
		t.Errorf("expected first suggestion about ls -la, got: %s", fix)
	}
}

func TestErrorContextConcurrentAccess(t *testing.T) {
	ec := NewErrorContext()
	var wg sync.WaitGroup

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ec.Enrich("nil pointer dereference")
			_ = ec.IsRecoverable("permission denied")
			_ = ec.SuggestFix("old_str not found")
		}()
	}

	// Concurrent writes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = ec.AddPattern(
				strings.Replace("custom_pattern_X", "X", strings.Repeat("a", idx+1), 1),
				ErrorHelp{Title: "concurrent add"},
			)
		}(i)
	}

	wg.Wait()
}

func TestEnrich_GoUnusedImport(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich(`./main.go:3:2: "fmt" imported and not used`)
	if enriched == nil {
		t.Fatal("expected enriched error for unused import")
	}
	if enriched.Title != "Unused import" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
	if enriched.Severity != "LOW" {
		t.Errorf("expected LOW severity for unused import, got %s", enriched.Severity)
	}
}

func TestEnrich_GoUnusedVariable(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("x declared and not used")
	if enriched == nil {
		t.Fatal("expected enriched error for unused variable")
	}
	if enriched.Title != "Unused variable" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_JSONParseError(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("json: cannot unmarshal string into Go value of type int")
	if enriched == nil {
		t.Fatal("expected enriched error for JSON parse error")
	}
	if enriched.Title != "JSON parse error" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_OOM(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("runtime: out of memory")
	if enriched == nil {
		t.Fatal("expected enriched error for OOM")
	}
	if enriched.Title != "Out of memory" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
	if enriched.Severity != "CRITICAL" {
		t.Errorf("expected CRITICAL severity for OOM, got %s", enriched.Severity)
	}
	if enriched.Recoverable {
		t.Error("OOM should not be recoverable")
	}
}

func TestEnrich_Timeout(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("context deadline exceeded")
	if enriched == nil {
		t.Fatal("expected enriched error for timeout")
	}
	if enriched.Title != "Operation timed out" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_InterfaceNotImplemented(t *testing.T) {
	ec := NewErrorContext()
	enriched := ec.Enrich("MyStruct does not implement io.Reader (missing Read method)")
	if enriched == nil {
		t.Fatal("expected enriched error for interface not implemented")
	}
	if enriched.Title != "Interface not satisfied" {
		t.Errorf("unexpected title: %s", enriched.Title)
	}
}

func TestEnrich_OriginalPreserved(t *testing.T) {
	ec := NewErrorContext()
	original := "open /tmp/foo: permission denied"
	enriched := ec.Enrich(original)
	if enriched == nil {
		t.Fatal("expected enriched error")
	}
	if enriched.Original != original {
		t.Errorf("original not preserved: got %q, want %q", enriched.Original, original)
	}
}

func TestSeverityClassification(t *testing.T) {
	tests := []struct {
		err      string
		severity string
	}{
		{"panic: runtime error", "CRITICAL"},
		{"fatal error: stack overflow", "CRITICAL"},
		{"nil pointer dereference", "CRITICAL"},
		{"out of memory", "CRITICAL"},
		{"permission denied", "HIGH"},
		{"import cycle not allowed", "HIGH"},
		{"merge conflict in file.go", "HIGH"},
		{"budget exceeded", "HIGH"},
		{"nothing to commit", "LOW"},
		{"imported and not used", "LOW"},
		{"cannot find module foo", "MEDIUM"},
	}

	for _, tt := range tests {
		t.Run(tt.err, func(t *testing.T) {
			got := classifySeverity(tt.err)
			if got != tt.severity {
				t.Errorf("classifySeverity(%q) = %s, want %s", tt.err, got, tt.severity)
			}
		})
	}
}
