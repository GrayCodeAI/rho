package taste

import (
	"testing"
)

func TestDetectNamingStyle_SnakeCase(t *testing.T) {
	code := `
func get_user_data(user_id string) {
	first_name := fetch_first_name(user_id)
	last_name := fetch_last_name(user_id)
	full_name := first_name + " " + last_name
}
`
	style := DetectNamingStyle(code)
	if style != NamingSnakeCase {
		t.Errorf("expected snake_case, got %q", style)
	}
}

func TestDetectNamingStyle_CamelCase(t *testing.T) {
	code := `
func getUserData(userId string) {
	firstName := fetchFirstName(userId)
	lastName := fetchLastName(userId)
	fullName := firstName + " " + lastName
}
`
	style := DetectNamingStyle(code)
	if style != NamingCamelCase {
		t.Errorf("expected camelCase, got %q", style)
	}
}

func TestDetectNamingStyle_PascalCase(t *testing.T) {
	// PascalCase identifiers: start with uppercase, contain at least one more uppercase.
	code := `
DataStore := CreateNewStore()
UserProfile := LoadProfile()
HttpClient := BuildHttpClient()
TaskRunner := NewTaskRunner()
EventBus := InitEventBus()
`
	style := DetectNamingStyle(code)
	if style != NamingPascalCase {
		t.Errorf("expected PascalCase, got %q", style)
	}
}

func TestDetectNamingStyle_Empty(t *testing.T) {
	style := DetectNamingStyle("")
	if style != "unknown" {
		t.Errorf("expected unknown for empty code, got %q", style)
	}
}

func TestDetectCommentDensity_NoComments(t *testing.T) {
	code := `func main() {
	x := 1
	y := 2
	fmt.Println(x + y)
}`
	density := DetectCommentDensity(code)
	if density > 0.01 {
		t.Errorf("expected near-zero density, got %f", density)
	}
}

func TestDetectCommentDensity_HeavyComments(t *testing.T) {
	code := `// Package main is the entry point.
// It does important things.
func main() {
	// Initialize x
	x := 1
	// Initialize y
	y := 2
	// Print the sum
	fmt.Println(x + y)
}`
	density := DetectCommentDensity(code)
	if density < 0.3 {
		t.Errorf("expected high density for heavily commented code, got %f", density)
	}
}

func TestDetectErrorPattern_Wrapped(t *testing.T) {
	code := `
func fetchUser(id string) (*User, error) {
	user, err := db.Find(id)
	if err != nil {
		return nil, fmt.Errorf("fetch user %s: %w", id, err)
	}
	data, err := user.LoadProfile()
	if err != nil {
		return nil, fmt.Errorf("load profile: %w", err)
	}
	return user, nil
}
`
	pattern := DetectErrorPattern(code)
	if pattern != ErrorWrapped {
		t.Errorf("expected wrapped, got %q", pattern)
	}
}

func TestDetectErrorPattern_Panic(t *testing.T) {
	code := `
func mustConnect() *DB {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		panic(err)
	}
	if err := db.Ping(); err != nil {
		panic(fmt.Sprintf("db ping: %v", err))
	}
	return &DB{db: db}
}
`
	pattern := DetectErrorPattern(code)
	if pattern != ErrorPanic {
		t.Errorf("expected panic, got %q", pattern)
	}
}

func TestDetectErrorPattern_Sentinel(t *testing.T) {
	code := `
var ErrNotFound = errors.New("not found")
var ErrForbidden = errors.New("forbidden")
var ErrTimeout = fmt.Errorf("operation timed out")

func findUser(id string) error {
	if id == "" {
		return ErrNotFound
	}
	return nil
}
`
	pattern := DetectErrorPattern(code)
	if pattern != ErrorSentinel {
		t.Errorf("expected sentinel, got %q", pattern)
	}
}

func TestDetectErrorPattern_Empty(t *testing.T) {
	pattern := DetectErrorPattern("")
	if pattern != "unknown" {
		t.Errorf("expected unknown for empty code, got %q", pattern)
	}
}

func TestDetectAbstractionLevel_Extracted(t *testing.T) {
	code := `
func main() {
	initDB()
	startServer()
}

func initDB() {
	connectDB()
	runMigrations()
}

func connectDB() {}
func runMigrations() {}
func startServer() {}
func handleRequest() {}
func parseBody() {}
func validateInput() {}
func sendResponse() {}
`
	level := DetectAbstractionLevel(code)
	if level != AbstractionExtracted {
		t.Errorf("expected extracted, got %q", level)
	}
}

func TestDetectAbstractionLevel_Empty(t *testing.T) {
	level := DetectAbstractionLevel("")
	if level != "unknown" {
		t.Errorf("expected unknown for empty code, got %q", level)
	}
}

func TestDetectTestStyle_TableDriven(t *testing.T) {
	code := `
func TestAdd(t *testing.T) {
	tests := []struct{
		a, b, want int
	}{
		{1, 2, 3},
		{0, 0, 0},
		{-1, 1, 0},
	}
	for _, tt := range tests {
		got := Add(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("Add(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
`
	style := DetectTestStyle(code)
	if style != TestTableDriven {
		t.Errorf("expected table_driven, got %q", style)
	}
}

func TestDetectTestStyle_Subtests(t *testing.T) {
	code := `
func TestMath(t *testing.T) {
	t.Run("addition", func(t *testing.T) {
		if Add(1, 2) != 3 {
			t.Error("wrong")
		}
	})
	t.Run("subtraction", func(t *testing.T) {
		if Sub(3, 1) != 2 {
			t.Error("wrong")
		}
	})
}
`
	style := DetectTestStyle(code)
	if style != TestSubtests {
		t.Errorf("expected subtests, got %q", style)
	}
}

func TestDetectTestStyle_AssertLib(t *testing.T) {
	code := `
func TestUser(t *testing.T) {
	user := NewUser("test")
	assert.Equal(t, "test", user.Name)
	assert.NotNil(t, user.ID)
	require.NoError(t, user.Validate())
}
`
	style := DetectTestStyle(code)
	if style != TestAssertLib {
		t.Errorf("expected assert_lib, got %q", style)
	}
}

func TestDetectLanguage(t *testing.T) {
	goCode := `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`
	if lang := DetectLanguage(goCode); lang != "go" {
		t.Errorf("expected go, got %q", lang)
	}

	pyCode := `import os
def main():
    print("hello")
`
	if lang := DetectLanguage(pyCode); lang != "python" {
		t.Errorf("expected python, got %q", lang)
	}
}

func TestAnalyzeCode(t *testing.T) {
	code := `
// Package users handles user management.
func getUserByID(userID string) (*User, error) {
	result, err := db.Query(userID)
	if err != nil {
		return nil, fmt.Errorf("query user: %w", err)
	}
	return result, nil
}
`
	results := AnalyzeCode(code)
	if len(results) == 0 {
		t.Error("expected at least one detected signal")
	}

	if _, ok := results[CategoryComments]; !ok {
		t.Error("expected comments category to be detected")
	}
}
