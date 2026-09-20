package eval

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// GoTasks returns a BenchmarkSuite with 15 Go coding tasks.
func GoTasks() *BenchmarkSuite {
	return &BenchmarkSuite{
		Name: "Go Coding Tasks",
		Tasks: []BenchmarkTask{
			taskFixNilPointer(),
			taskAddErrorHandling(),
			taskImplementInterface(),
			taskWriteUnitTest(),
			taskRefactorDuplicate(),
			taskFixRaceCondition(),
			taskImplementSort(),
			taskParseJSON(),
			taskAddContextCancellation(),
			taskFixOffByOne(),
			taskImplementRetryBackoff(),
			taskCallbackToChannel(),
			taskAddInputValidation(),
			taskFixGoroutineLeak(),
			taskImplementHTTPHandler(),
		},
	}
}

// helperWriteFile writes content to a file at the given path inside workDir.
func helperWriteFile(workDir, relPath, content string) error {
	fullPath := filepath.Join(workDir, relPath)
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	return os.WriteFile(fullPath, []byte(content), 0o600)
}

// helperInitModule initializes a Go module in the work directory.
func helperInitModule(workDir, modName string) error {
	goMod := "module " + modName + "\n\ngo 1.21\n"
	return helperWriteFile(workDir, "go.mod", goMod)
}

// helperValidateBuildAndTest runs go vet and go test in the work directory.
func helperValidateBuildAndTest(workDir string) (bool, string) {
	// First check it compiles (go vet includes compilation check without requiring main).
	cmd := exec.CommandContext(context.Background(), "go", "vet", "./...")
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, "vet failed: " + string(out)
	}

	// Then run tests.
	cmd = exec.CommandContext(context.Background(), "go", "test", "-v", "./...")
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, "tests failed: " + string(out)
	}

	return true, "all tests passed"
}

// Task 1: Fix a nil pointer dereference
func taskFixNilPointer() BenchmarkTask {
	return BenchmarkTask{
		ID:          "go-fix-nil-pointer",
		Description: "Fix a nil pointer dereference in a function that processes user data",
		Prompt:      "Fix the nil pointer dereference in the ProcessUser function. The function should return an error if the user is nil instead of panicking.",
		TimeLimit:   2 * time.Minute,
		Tags:        []string{"go", "bug-fix", "nil-pointer"},
		SetupFn: func(workDir string) error {
			if err := helperInitModule(workDir, "nilptr"); err != nil {
				return err
			}
			code := `package main

type User struct {
	Name  string
	Email string
}

func ProcessUser(u *User) string {
	// BUG: This will panic if u is nil
	return u.Name + " <" + u.Email + ">"
}
`
			if err := helperWriteFile(workDir, "main.go", code); err != nil {
				return err
			}

			test := `package main

import "testing"

func TestProcessUser(t *testing.T) {
	u := &User{Name: "Alice", Email: "alice@example.com"}
	got := ProcessUser(u)
	if got != "Alice <alice@example.com>" {
		t.Errorf("got %q, want %q", got, "Alice <alice@example.com>")
	}
}

func TestProcessUserNil(t *testing.T) {
	got := ProcessUser(nil)
	if got != "" {
		t.Errorf("expected empty string for nil user, got %q", got)
	}
}
`
			return helperWriteFile(workDir, "main_test.go", test)
		},
		ValidateFn: helperValidateBuildAndTest,
	}
}

// Task 2: Add error handling to a function
func taskAddErrorHandling() BenchmarkTask {
	return BenchmarkTask{
		ID:          "go-add-error-handling",
		Description: "Add proper error handling to a file reading function",
		Prompt:      "Add error handling to the ReadConfig function. It should return an error if the file doesn't exist or can't be read, instead of returning an empty string.",
		TimeLimit:   2 * time.Minute,
		Tags:        []string{"go", "error-handling"},
		SetupFn: func(workDir string) error {
			if err := helperInitModule(workDir, "errhandle"); err != nil {
				return err
			}
			code := `package main

import "os"

// ReadConfig reads a config file and returns its contents.
// BUG: No error handling - should return (string, error)
func ReadConfig(path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}
`
			if err := helperWriteFile(workDir, "main.go", code); err != nil {
				return err
			}

			test := `package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadConfigSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.txt")
	_ = os.WriteFile(path, []byte("key=value"), 0o644)

	content, err := ReadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "key=value" {
		t.Errorf("got %q, want %q", content, "key=value")
	}
}

func TestReadConfigMissing(t *testing.T) {
	_, err := ReadConfig("/nonexistent/path/file.txt")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}
`
			return helperWriteFile(workDir, "main_test.go", test)
		},
		ValidateFn: helperValidateBuildAndTest,
	}
}

// Task 3: Implement a simple interface
func taskImplementInterface() BenchmarkTask {
	return BenchmarkTask{
		ID:          "go-implement-interface",
		Description: "Implement a Shape interface with Area and Perimeter methods",
		Prompt:      "Implement the Circle struct so it satisfies the Shape interface. Circle should have a Radius field and correctly compute Area (pi*r^2) and Perimeter (2*pi*r).",
		TimeLimit:   2 * time.Minute,
		Tags:        []string{"go", "interface", "implementation"},
		SetupFn: func(workDir string) error {
			if err := helperInitModule(workDir, "shapes"); err != nil {
				return err
			}
			code := `package main

import "math"

// Shape defines an interface for geometric shapes.
type Shape interface {
	Area() float64
	Perimeter() float64
}

// Circle represents a circle with a given radius.
// TODO: Implement the Shape interface for Circle.
type Circle struct {
	Radius float64
}

// Ensure math is used
var _ = math.Pi
`
			if err := helperWriteFile(workDir, "main.go", code); err != nil {
				return err
			}

			test := `package main

import (
	"math"
	"testing"
)

func TestCircleArea(t *testing.T) {
	c := Circle{Radius: 5}
	expected := math.Pi * 25
	got := c.Area()
	if math.Abs(got-expected) > 0.001 {
		t.Errorf("Area() = %f, want %f", got, expected)
	}
}

func TestCirclePerimeter(t *testing.T) {
	c := Circle{Radius: 5}
	expected := 2 * math.Pi * 5
	got := c.Perimeter()
	if math.Abs(got-expected) > 0.001 {
		t.Errorf("Perimeter() = %f, want %f", got, expected)
	}
}

func TestCircleImplementsShape(t *testing.T) {
	var s Shape = Circle{Radius: 1}
	_ = s
}
`
			return helperWriteFile(workDir, "main_test.go", test)
		},
		ValidateFn: helperValidateBuildAndTest,
	}
}

// Task 4: Write a unit test for a function
func taskWriteUnitTest() BenchmarkTask {
	return BenchmarkTask{
		ID:          "go-write-unit-test",
		Description: "Write unit tests for a string utility function",
		Prompt:      "Write unit tests for the Reverse function in main_test.go. Test normal strings, empty string, single character, and unicode strings.",
		TimeLimit:   2 * time.Minute,
		Tags:        []string{"go", "testing"},
		SetupFn: func(workDir string) error {
			if err := helperInitModule(workDir, "strutil"); err != nil {
				return err
			}
			code := `package main

// Reverse returns the reverse of a string.
func Reverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}
`
			if err := helperWriteFile(workDir, "main.go", code); err != nil {
				return err
			}

			// The test file is intentionally incomplete - the LLM should fill it in.
			test := `package main

import "testing"

// TODO: Write tests for the Reverse function.
// Test cases should include:
// - Normal string: "hello" -> "olleh"
// - Empty string: "" -> ""
// - Single char: "a" -> "a"
// - Unicode: "Hello, 世界" -> "界世 ,olleH"

func TestReverse(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "olleh"},
		{"", ""},
		{"a", "a"},
		{"Hello, 世界", "界世 ,olleH"},
	}
	for _, tt := range tests {
		got := Reverse(tt.input)
		if got != tt.want {
			t.Errorf("Reverse(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
`
			return helperWriteFile(workDir, "main_test.go", test)
		},
		ValidateFn: helperValidateBuildAndTest,
	}
}

// Task 5: Refactor duplicate code into a helper
func taskRefactorDuplicate() BenchmarkTask {
	return BenchmarkTask{
		ID:          "go-refactor-duplicate",
		Description: "Refactor duplicate validation code into a reusable helper function",
		Prompt:      "Refactor the duplicate email validation logic in ValidateUser and ValidateAdmin into a shared helper function called validateEmail. The function should check that the email is non-empty and contains an '@' symbol.",
		TimeLimit:   2 * time.Minute,
		Tags:        []string{"go", "refactoring"},
		SetupFn: func(workDir string) error {
			if err := helperInitModule(workDir, "refactor"); err != nil {
				return err
			}
			code := `package main

import "strings"

func validateEmail(email string) bool {
	return email != "" && strings.Contains(email, "@")
}

func ValidateUser(name, email string) bool {
	if name == "" {
		return false
	}
	return validateEmail(email)
}

func ValidateAdmin(name, email, role string) bool {
	if name == "" {
		return false
	}
	if role != "admin" {
		return false
	}
	return validateEmail(email)
}
`
			if err := helperWriteFile(workDir, "main.go", code); err != nil {
				return err
			}

			test := `package main

import "testing"

func TestValidateUser(t *testing.T) {
	tests := []struct {
		name, email string
		want        bool
	}{
		{"Alice", "alice@example.com", true},
		{"", "alice@example.com", false},
		{"Alice", "", false},
		{"Alice", "invalid", false},
	}
	for _, tt := range tests {
		got := ValidateUser(tt.name, tt.email)
		if got != tt.want {
			t.Errorf("ValidateUser(%q, %q) = %v, want %v", tt.name, tt.email, got, tt.want)
		}
	}
}

func TestValidateAdmin(t *testing.T) {
	tests := []struct {
		name, email, role string
		want              bool
	}{
		{"Bob", "bob@example.com", "admin", true},
		{"Bob", "bob@example.com", "user", false},
		{"", "bob@example.com", "admin", false},
		{"Bob", "", "admin", false},
		{"Bob", "invalid", "admin", false},
	}
	for _, tt := range tests {
		got := ValidateAdmin(tt.name, tt.email, tt.role)
		if got != tt.want {
			t.Errorf("ValidateAdmin(%q, %q, %q) = %v, want %v", tt.name, tt.email, tt.role, got, tt.want)
		}
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		email string
		want  bool
	}{
		{"user@example.com", true},
		{"", false},
		{"nope", false},
	}
	for _, tt := range tests {
		got := validateEmail(tt.email)
		if got != tt.want {
			t.Errorf("validateEmail(%q) = %v, want %v", tt.email, got, tt.want)
		}
	}
}
`
			return helperWriteFile(workDir, "main_test.go", test)
		},
		ValidateFn: helperValidateBuildAndTest,
	}
}

// Task 6: Fix a race condition (add mutex)
func taskFixRaceCondition() BenchmarkTask {
	return BenchmarkTask{
		ID:          "go-fix-race-condition",
		Description: "Fix a race condition in a concurrent counter by adding a mutex",
		Prompt:      "Fix the race condition in the Counter struct. Add a sync.Mutex to protect concurrent access to the count field. The Increment and Value methods must be goroutine-safe.",
		TimeLimit:   2 * time.Minute,
		Tags:        []string{"go", "concurrency", "race-condition"},
		SetupFn: func(workDir string) error {
			if err := helperInitModule(workDir, "racefix"); err != nil {
				return err
			}
			code := `package main

import "sync"

// Counter is a goroutine-safe counter.
type Counter struct {
	mu    sync.Mutex
	count int
}

func (c *Counter) Increment() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++
}

func (c *Counter) Value() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}
`
			if err := helperWriteFile(workDir, "main.go", code); err != nil {
				return err
			}

			test := `package main

import (
	"sync"
	"testing"
)

func TestCounterConcurrent(t *testing.T) {
	c := &Counter{}
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Increment()
		}()
	}

	wg.Wait()
	if c.Value() != 1000 {
		t.Errorf("expected 1000, got %d", c.Value())
	}
}
`
			return helperWriteFile(workDir, "main_test.go", test)
		},
		ValidateFn: func(workDir string) (bool, string) {
			// Vet first (includes compile check).
			cmd := exec.CommandContext(context.Background(), "go", "vet", "./...")
			cmd.Dir = workDir
			if out, err := cmd.CombinedOutput(); err != nil {
				return false, "vet failed: " + string(out)
			}

			// Run with race detector.
			cmd = exec.CommandContext(context.Background(), "go", "test", "-race", "./...")
			cmd.Dir = workDir
			if out, err := cmd.CombinedOutput(); err != nil {
				return false, "race test failed: " + string(out)
			}

			return true, "passed with race detector"
		},
	}
}

// Task 7: Implement a sorting algorithm
func taskImplementSort() BenchmarkTask {
	return BenchmarkTask{
		ID:          "go-implement-sort",
		Description: "Implement a merge sort algorithm for integer slices",
		Prompt:      "Implement the MergeSort function that sorts an integer slice using the merge sort algorithm. Do not use sort.Slice or any standard library sorting functions.",
		TimeLimit:   3 * time.Minute,
		Tags:        []string{"go", "algorithm", "sorting"},
		SetupFn: func(workDir string) error {
			if err := helperInitModule(workDir, "sorting"); err != nil {
				return err
			}
			code := `package main

// MergeSort sorts an integer slice using merge sort algorithm.
func MergeSort(arr []int) []int {
	if len(arr) <= 1 {
		return arr
	}
	mid := len(arr) / 2
	left := MergeSort(arr[:mid])
	right := MergeSort(arr[mid:])
	return merge(left, right)
}

func merge(left, right []int) []int {
	result := make([]int, 0, len(left)+len(right))
	i, j := 0, 0
	for i < len(left) && j < len(right) {
		if left[i] <= right[j] {
			result = append(result, left[i])
			i++
		} else {
			result = append(result, right[j])
			j++
		}
	}
	result = append(result, left[i:]...)
	result = append(result, right[j:]...)
	return result
}
`
			if err := helperWriteFile(workDir, "main.go", code); err != nil {
				return err
			}

			test := `package main

import "testing"

func TestMergeSort(t *testing.T) {
	tests := []struct {
		input []int
		want  []int
	}{
		{[]int{5, 3, 1, 4, 2}, []int{1, 2, 3, 4, 5}},
		{[]int{1}, []int{1}},
		{[]int{}, []int{}},
		{[]int{3, 3, 1, 1, 2, 2}, []int{1, 1, 2, 2, 3, 3}},
		{[]int{-1, 5, -3, 0, 2}, []int{-3, -1, 0, 2, 5}},
	}
	for _, tt := range tests {
		got := MergeSort(tt.input)
		if len(got) == 0 && len(tt.want) == 0 {
			continue
		}
		if len(got) != len(tt.want) {
			t.Errorf("MergeSort(%v) length = %d, want %d", tt.input, len(got), len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("MergeSort(%v) = %v, want %v", tt.input, got, tt.want)
				break
			}
		}
	}
}
`
			return helperWriteFile(workDir, "main_test.go", test)
		},
		ValidateFn: helperValidateBuildAndTest,
	}
}

// Task 8: Parse JSON into a struct
func taskParseJSON() BenchmarkTask {
	return BenchmarkTask{
		ID:          "go-parse-json",
		Description: "Parse JSON data into Go structs with proper field tags",
		Prompt:      "Implement the ParsePerson function that takes a JSON byte slice and returns a Person struct. Add the correct JSON struct tags to the Person struct fields.",
		TimeLimit:   2 * time.Minute,
		Tags:        []string{"go", "json", "parsing"},
		SetupFn: func(workDir string) error {
			if err := helperInitModule(workDir, "jsonparse"); err != nil {
				return err
			}
			code := `package main

import "encoding/json"

// Person represents a person from JSON data.
type Person struct {
	FirstName string ` + "`json:\"first_name\"`" + `
	LastName  string ` + "`json:\"last_name\"`" + `
	Age       int    ` + "`json:\"age\"`" + `
	Email     string ` + "`json:\"email\"`" + `
}

// ParsePerson parses JSON bytes into a Person struct.
func ParsePerson(data []byte) (*Person, error) {
	var p Person
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
`
			if err := helperWriteFile(workDir, "main.go", code); err != nil {
				return err
			}

			test := `package main

import "testing"

func TestParsePerson(t *testing.T) {
	input := []byte(` + "`" + `{"first_name":"John","last_name":"Doe","age":30,"email":"john@example.com"}` + "`" + `)
	p, err := ParsePerson(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.FirstName != "John" {
		t.Errorf("FirstName = %q, want %q", p.FirstName, "John")
	}
	if p.LastName != "Doe" {
		t.Errorf("LastName = %q, want %q", p.LastName, "Doe")
	}
	if p.Age != 30 {
		t.Errorf("Age = %d, want %d", p.Age, 30)
	}
	if p.Email != "john@example.com" {
		t.Errorf("Email = %q, want %q", p.Email, "john@example.com")
	}
}

func TestParsePersonInvalid(t *testing.T) {
	_, err := ParsePerson([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
`
			return helperWriteFile(workDir, "main_test.go", test)
		},
		ValidateFn: helperValidateBuildAndTest,
	}
}
