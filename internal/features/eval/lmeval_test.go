package eval

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResultStore_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	store := &ResultStore{Dir: dir}

	result := &SuiteResult{
		Suite:         "test-suite",
		TotalTasks:    2,
		Passed:        1,
		Failed:        1,
		PassRate:      0.5,
		TotalDuration: 10 * time.Second,
		TotalTokens:   100,
		TotalCostUSD:  0.01,
		Results: []TaskResult{
			{TaskID: "t1", Passed: true, Duration: 5 * time.Second, TokensUsed: 50},
			{TaskID: "t2", Passed: false, Duration: 5 * time.Second, Error: "failed"},
		},
	}

	path, err := store.Save(result, "gpt-4o", "openai", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("file not created: %v", statErr)
	}

	loaded, err := store.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Suite != "test-suite" {
		t.Errorf("suite = %q, want test-suite", loaded.Suite)
	}
	if loaded.Summary.PassRate != 0.5 {
		t.Errorf("pass_rate = %f, want 0.5", loaded.Summary.PassRate)
	}
	if len(loaded.Tasks) != 2 {
		t.Errorf("tasks = %d, want 2", len(loaded.Tasks))
	}
}

func TestResultStore_List(t *testing.T) {
	dir := t.TempDir()
	store := &ResultStore{Dir: dir}

	// Empty dir
	files, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}

	// Write a file
	os.WriteFile(filepath.Join(dir, "test.json"), []byte("{}"), 0o644)
	files, err = store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 file, got %d", len(files))
	}
}

func TestCache_PutGet(t *testing.T) {
	dir := t.TempDir()
	cache := &Cache{Dir: dir}

	// Miss
	if entry := cache.Get("model", "prompt"); entry != nil {
		t.Error("expected nil on cache miss")
	}

	// Put
	if err := cache.Put("model", "prompt", "response", 50, 0.01); err != nil {
		t.Fatal(err)
	}

	// Hit
	entry := cache.Get("model", "prompt")
	if entry == nil {
		t.Fatal("expected cache hit")
	}
	if entry.Response != "response" {
		t.Errorf("response = %q, want 'response'", entry.Response)
	}
	if entry.Tokens != 50 {
		t.Errorf("tokens = %d, want 50", entry.Tokens)
	}
}

func TestCache_Clear(t *testing.T) {
	dir := t.TempDir()
	cache := &Cache{Dir: dir}
	_ = cache.Put("m", "p", "r", 1, 0)
	if err := cache.Clear(); err != nil {
		t.Fatal(err)
	}
	if entry := cache.Get("m", "p"); entry != nil {
		t.Error("expected nil after clear")
	}
}

func TestComputeHash(t *testing.T) {
	tasks := []BenchmarkTask{
		{ID: "t1", Prompt: "fix this"},
		{ID: "t2", Prompt: "implement that"},
	}
	h := ComputeHash(tasks)
	if h.TasksHash == "" {
		t.Error("expected non-empty tasks hash")
	}
	if h.PromptHash == "" {
		t.Error("expected non-empty prompt hash")
	}
	if h.GoVersion == "" {
		t.Error("expected non-empty go version")
	}
	if h.OS == "" {
		t.Error("expected non-empty OS")
	}
}

func TestExtractCodeBlock(t *testing.T) {
	input := "Here's the fix:\n```go\npackage main\n\nfunc main() {}\n```\nDone!"
	filter := ExtractCodeBlock("go")
	got := filter(input)
	want := "package main\n\nfunc main() {}"
	if got != want {
		t.Errorf("ExtractCodeBlock = %q, want %q", got, want)
	}
}

func TestExtractCodeBlock_NoMatch(t *testing.T) {
	input := "just plain text"
	filter := ExtractCodeBlock("go")
	if got := filter(input); got != input {
		t.Errorf("expected original string on no match, got %q", got)
	}
}

func TestApplyFilters(t *testing.T) {
	input := "```go\nfmt.Println(\"hi\")\n```"
	got := ApplyFilters(input, ExtractCodeBlock("go"))
	if got != `fmt.Println("hi")` {
		t.Errorf("ApplyFilters = %q", got)
	}
}

func TestGroupTasks(t *testing.T) {
	tasks := []BenchmarkTask{
		{ID: "t1", Tags: []string{"bug-fix"}},
		{ID: "t2", Tags: []string{"implementation"}},
		{ID: "t3", Tags: []string{"bug-fix", "concurrency"}},
	}
	groups := GroupTasks(tasks, DefaultGroups())
	var bugGroup *TaskGroup
	for i := range groups {
		if groups[i].Name == "bug-fixing" {
			bugGroup = &groups[i]
			break
		}
	}
	if bugGroup == nil {
		t.Fatal("bug-fixing group not found")
	}
	if len(bugGroup.Tasks) != 2 {
		t.Errorf("bug-fixing tasks = %d, want 2", len(bugGroup.Tasks))
	}
}

func TestAggregateGroupResults(t *testing.T) {
	tasks := []BenchmarkTask{
		{ID: "t1", Tags: []string{"bug-fix"}},
		{ID: "t2", Tags: []string{"bug-fix"}},
	}
	groups := []TaskGroup{{Name: "bugs", Tags: []string{"bug-fix"}, Tasks: tasks}}
	results := []TaskResult{
		{TaskID: "t1", Passed: true},
		{TaskID: "t2", Passed: false},
	}
	gr := AggregateGroupResults(groups, results)
	if len(gr) != 1 {
		t.Fatal("expected 1 group result")
	}
	if gr[0].PassRate != 0.5 {
		t.Errorf("pass rate = %f, want 0.5", gr[0].PassRate)
	}
}

func TestLoadTasksFromYAML(t *testing.T) {
	dir := t.TempDir()
	yaml := `task: test_task
description: A test task
language: go
tags: [test]
timeout: 1m
prompt: "Fix the code"
validate:
  - "true"
files:
  main.go: |
    package main
    func main() {}
`
	os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(yaml), 0o644)

	tasks, err := LoadTasksFromYAML(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].ID != "test_task" {
		t.Errorf("task ID = %q, want test_task", tasks[0].ID)
	}
	if tasks[0].Prompt != "Fix the code" {
		t.Errorf("prompt = %q", tasks[0].Prompt)
	}

	// Test setup creates files
	workDir := t.TempDir()
	if err := tasks[0].SetupFn(workDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "main.go")); err != nil {
		t.Error("setup didn't create main.go")
	}

	// Test validate
	passed, _ := tasks[0].ValidateFn(workDir)
	if !passed {
		t.Error("expected validation to pass (command is 'true')")
	}
}
