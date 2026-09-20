package eval

import "time"

// This file holds Go benchmark tasks 9-15. Tasks 1-8, the GoTasks() suite
// builder, and the shared helpers live in tasks_go.go.

// Task 9: Add context cancellation
func taskAddContextCancellation() BenchmarkTask {
	return BenchmarkTask{
		ID:          "go-add-context-cancellation",
		Description: "Add context cancellation support to a long-running operation",
		Prompt:      "Add context.Context support to the Process function so it can be cancelled. The function should check for context cancellation between iterations and return ctx.Err() if cancelled.",
		TimeLimit:   2 * time.Minute,
		Tags:        []string{"go", "context", "cancellation"},
		SetupFn: func(workDir string) error {
			if err := helperInitModule(workDir, "ctxcancel"); err != nil {
				return err
			}
			code := `package main

import "context"

// Process performs work in a loop, respecting context cancellation.
func Process(ctx context.Context, items []string) ([]string, error) {
	results := make([]string, 0, len(items))
	for _, item := range items {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}
		results = append(results, "processed: "+item)
	}
	return results, nil
}
`
			if err := helperWriteFile(workDir, "main.go", code); err != nil {
				return err
			}

			test := `package main

import (
	"context"
	"testing"
)

func TestProcessSuccess(t *testing.T) {
	ctx := context.Background()
	items := []string{"a", "b", "c"}
	results, err := Process(ctx, items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0] != "processed: a" {
		t.Errorf("results[0] = %q, want %q", results[0], "processed: a")
	}
}

func TestProcessCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	items := []string{"a", "b", "c"}
	_, err := Process(ctx, items)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
`
			return helperWriteFile(workDir, "main_test.go", test)
		},
		ValidateFn: helperValidateBuildAndTest,
	}
}

// Task 10: Fix an off-by-one error
func taskFixOffByOne() BenchmarkTask {
	return BenchmarkTask{
		ID:          "go-fix-off-by-one",
		Description: "Fix an off-by-one error in a pagination function",
		Prompt:      "Fix the off-by-one error in the Paginate function. It should return the correct slice of items for the given page number (1-based) and page size.",
		TimeLimit:   2 * time.Minute,
		Tags:        []string{"go", "bug-fix", "off-by-one"},
		SetupFn: func(workDir string) error {
			if err := helperInitModule(workDir, "paginate"); err != nil {
				return err
			}
			code := `package main

// Paginate returns a page of items. Page is 1-based.
func Paginate(items []int, page, pageSize int) []int {
	if page < 1 || pageSize < 1 {
		return nil
	}
	start := (page - 1) * pageSize
	if start >= len(items) {
		return nil
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}
`
			if err := helperWriteFile(workDir, "main.go", code); err != nil {
				return err
			}

			test := `package main

import "testing"

func TestPaginate(t *testing.T) {
	items := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	tests := []struct {
		page, size int
		want       []int
	}{
		{1, 3, []int{1, 2, 3}},
		{2, 3, []int{4, 5, 6}},
		{3, 3, []int{7, 8, 9}},
		{4, 3, []int{10}},
		{5, 3, nil},
		{1, 10, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
		{0, 3, nil},
		{1, 0, nil},
	}

	for _, tt := range tests {
		got := Paginate(items, tt.page, tt.size)
		if !intSliceEqual(got, tt.want) {
			t.Errorf("Paginate(items, %d, %d) = %v, want %v", tt.page, tt.size, got, tt.want)
		}
	}
}

func intSliceEqual(a, b []int) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
`
			return helperWriteFile(workDir, "main_test.go", test)
		},
		ValidateFn: helperValidateBuildAndTest,
	}
}

// Task 11: Implement retry with backoff
func taskImplementRetryBackoff() BenchmarkTask {
	return BenchmarkTask{
		ID:          "go-implement-retry-backoff",
		Description: "Implement a retry function with exponential backoff",
		Prompt:      "Implement the Retry function that retries a fallible operation with exponential backoff. It should retry up to maxRetries times, doubling the wait between each attempt starting from initialDelay.",
		TimeLimit:   3 * time.Minute,
		Tags:        []string{"go", "retry", "backoff"},
		SetupFn: func(workDir string) error {
			if err := helperInitModule(workDir, "retrybackoff"); err != nil {
				return err
			}
			code := `package main

import "time"

// Retry retries fn up to maxRetries times with exponential backoff.
// initialDelay is doubled after each failed attempt.
func Retry(fn func() error, maxRetries int, initialDelay time.Duration) error {
	var err error
	delay := initialDelay
	for i := 0; i <= maxRetries; i++ {
		err = fn()
		if err == nil {
			return nil
		}
		if i < maxRetries {
			time.Sleep(delay)
			delay *= 2
		}
	}
	return err
}
`
			if err := helperWriteFile(workDir, "main.go", code); err != nil {
				return err
			}

			test := `package main

import (
	"errors"
	"testing"
	"time"
)

func TestRetrySuccess(t *testing.T) {
	calls := 0
	fn := func() error {
		calls++
		if calls < 3 {
			return errors.New("not yet")
		}
		return nil
	}

	err := Retry(fn, 5, 1*time.Millisecond)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetryExhausted(t *testing.T) {
	fn := func() error {
		return errors.New("always fails")
	}

	err := Retry(fn, 3, 1*time.Millisecond)
	if err == nil {
		t.Error("expected error after retries exhausted")
	}
}

func TestRetryImmediateSuccess(t *testing.T) {
	calls := 0
	fn := func() error {
		calls++
		return nil
	}

	err := Retry(fn, 3, 1*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}
`
			return helperWriteFile(workDir, "main_test.go", test)
		},
		ValidateFn: helperValidateBuildAndTest,
	}
}

// Task 12: Convert callback to channel pattern
func taskCallbackToChannel() BenchmarkTask {
	return BenchmarkTask{
		ID:          "go-callback-to-channel",
		Description: "Convert a callback-based API to use channels",
		Prompt:      "Implement the StreamResults function that converts the callback-based FetchWithCallback into a channel-based API. It should return a channel that receives results as they arrive.",
		TimeLimit:   2 * time.Minute,
		Tags:        []string{"go", "channels", "concurrency"},
		SetupFn: func(workDir string) error {
			if err := helperInitModule(workDir, "chanpattern"); err != nil {
				return err
			}
			code := `package main

// FetchWithCallback calls the callback for each result.
func FetchWithCallback(items []string, cb func(string)) {
	for _, item := range items {
		cb("result: " + item)
	}
}

// StreamResults converts callback-based FetchWithCallback into channel-based API.
func StreamResults(items []string) <-chan string {
	ch := make(chan string)
	go func() {
		defer close(ch)
		FetchWithCallback(items, func(result string) {
			ch <- result
		})
	}()
	return ch
}
`
			if err := helperWriteFile(workDir, "main.go", code); err != nil {
				return err
			}

			test := `package main

import "testing"

func TestStreamResults(t *testing.T) {
	items := []string{"a", "b", "c"}
	ch := StreamResults(items)

	var results []string
	for r := range ch {
		results = append(results, r)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	expected := []string{"result: a", "result: b", "result: c"}
	for i, r := range results {
		if r != expected[i] {
			t.Errorf("results[%d] = %q, want %q", i, r, expected[i])
		}
	}
}

func TestStreamResultsEmpty(t *testing.T) {
	ch := StreamResults(nil)
	var results []string
	for r := range ch {
		results = append(results, r)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}
`
			return helperWriteFile(workDir, "main_test.go", test)
		},
		ValidateFn: helperValidateBuildAndTest,
	}
}

// Task 13: Add input validation
func taskAddInputValidation() BenchmarkTask {
	return BenchmarkTask{
		ID:          "go-add-input-validation",
		Description: "Add input validation to a user registration function",
		Prompt:      "Add input validation to the Register function. Validate that: name is non-empty (max 100 chars), email contains '@' and '.', age is between 0 and 150, password is at least 8 chars.",
		TimeLimit:   2 * time.Minute,
		Tags:        []string{"go", "validation"},
		SetupFn: func(workDir string) error {
			if err := helperInitModule(workDir, "validation"); err != nil {
				return err
			}
			code := `package main

import (
	"errors"
	"strings"
)

// Register validates and registers a user.
func Register(name, email string, age int, password string) error {
	if name == "" || len(name) > 100 {
		return errors.New("invalid name: must be 1-100 characters")
	}
	if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
		return errors.New("invalid email: must contain @ and .")
	}
	if age < 0 || age > 150 {
		return errors.New("invalid age: must be 0-150")
	}
	if len(password) < 8 {
		return errors.New("invalid password: must be at least 8 characters")
	}
	return nil
}
`
			if err := helperWriteFile(workDir, "main.go", code); err != nil {
				return err
			}

			test := `package main

import (
	"strings"
	"testing"
)

func TestRegisterValid(t *testing.T) {
	err := Register("Alice", "alice@example.com", 25, "securepass")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRegisterInvalidName(t *testing.T) {
	if err := Register("", "a@b.c", 25, "securepass"); err == nil {
		t.Error("expected error for empty name")
	}
	longName := strings.Repeat("a", 101)
	if err := Register(longName, "a@b.c", 25, "securepass"); err == nil {
		t.Error("expected error for name > 100 chars")
	}
}

func TestRegisterInvalidEmail(t *testing.T) {
	if err := Register("Alice", "invalid", 25, "securepass"); err == nil {
		t.Error("expected error for email without @")
	}
	if err := Register("Alice", "no@dot", 25, "securepass"); err == nil {
		t.Error("expected error for email without .")
	}
}

func TestRegisterInvalidAge(t *testing.T) {
	if err := Register("Alice", "a@b.c", -1, "securepass"); err == nil {
		t.Error("expected error for negative age")
	}
	if err := Register("Alice", "a@b.c", 151, "securepass"); err == nil {
		t.Error("expected error for age > 150")
	}
}

func TestRegisterInvalidPassword(t *testing.T) {
	if err := Register("Alice", "a@b.c", 25, "short"); err == nil {
		t.Error("expected error for password < 8 chars")
	}
}
`
			return helperWriteFile(workDir, "main_test.go", test)
		},
		ValidateFn: helperValidateBuildAndTest,
	}
}

// Task 14: Fix a goroutine leak
func taskFixGoroutineLeak() BenchmarkTask {
	return BenchmarkTask{
		ID:          "go-fix-goroutine-leak",
		Description: "Fix a goroutine leak in a producer function",
		Prompt:      "Fix the goroutine leak in the Produce function. The goroutine should stop when the done channel is closed, and the returned channel should be properly closed when the goroutine exits.",
		TimeLimit:   2 * time.Minute,
		Tags:        []string{"go", "concurrency", "goroutine-leak"},
		SetupFn: func(workDir string) error {
			if err := helperInitModule(workDir, "leakfix"); err != nil {
				return err
			}
			code := `package main

// Produce generates sequential numbers until done is closed.
func Produce(done <-chan struct{}) <-chan int {
	ch := make(chan int)
	go func() {
		defer close(ch)
		i := 0
		for {
			select {
			case <-done:
				return
			case ch <- i:
				i++
			}
		}
	}()
	return ch
}
`
			if err := helperWriteFile(workDir, "main.go", code); err != nil {
				return err
			}

			test := `package main

import "testing"

func TestProduceAndStop(t *testing.T) {
	done := make(chan struct{})
	ch := Produce(done)

	// Read a few values.
	for i := 0; i < 5; i++ {
		val := <-ch
		if val != i {
			t.Errorf("expected %d, got %d", i, val)
		}
	}

	// Signal done.
	close(done)

	// Channel should eventually be closed.
	// Drain any remaining buffered values.
	drained := false
	for range ch {
		drained = true
		_ = drained
	}
	// If we get here, the channel was closed properly.
}

func TestProduceImmediateStop(t *testing.T) {
	done := make(chan struct{})
	close(done)
	ch := Produce(done)

	// Channel should be closed without producing values.
	count := 0
	for range ch {
		count++
	}
	// Might get 0 or a very small number.
	if count > 1 {
		t.Errorf("expected at most 1 value after immediate close, got %d", count)
	}
}
`
			return helperWriteFile(workDir, "main_test.go", test)
		},
		ValidateFn: helperValidateBuildAndTest,
	}
}

// Task 15: Implement a simple HTTP handler
func taskImplementHTTPHandler() BenchmarkTask {
	return BenchmarkTask{
		ID:          "go-implement-http-handler",
		Description: "Implement a simple HTTP handler that returns JSON responses",
		Prompt:      "Implement the HealthHandler function that returns a JSON response with status 200 and body {\"status\":\"ok\",\"service\":\"rho\"}. Also implement NotFoundHandler that returns 404 with {\"error\":\"not found\"}.",
		TimeLimit:   2 * time.Minute,
		Tags:        []string{"go", "http", "handler"},
		SetupFn: func(workDir string) error {
			if err := helperInitModule(workDir, "httphandler"); err != nil {
				return err
			}
			code := `package main

import (
	"encoding/json"
	"net/http"
)

// HealthHandler returns a JSON health check response.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"service": "rho",
	})
}

// NotFoundHandler returns a JSON 404 response.
func NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{
		"error": "not found",
	})
}
`
			if err := helperWriteFile(workDir, "main.go", code); err != nil {
				return err
			}

			test := `package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	HealthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want %q", body["status"], "ok")
	}
	if body["service"] != "rho" {
		t.Errorf("service = %q, want %q", body["service"], "rho")
	}
}

func TestNotFoundHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	w := httptest.NewRecorder()

	NotFoundHandler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if body["error"] != "not found" {
		t.Errorf("error = %q, want %q", body["error"], "not found")
	}
}
`
			return helperWriteFile(workDir, "main_test.go", test)
		},
		ValidateFn: helperValidateBuildAndTest,
	}
}
