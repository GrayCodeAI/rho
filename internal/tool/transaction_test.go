package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTransaction_CreateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new_file.go")
	content := []byte("package main\n\nfunc main() {}\n")

	tx := NewTransaction()
	if err := tx.AddCreate(path, content, 0o644); err != nil {
		t.Fatalf("AddCreate: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after commit: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}

	if tx.Status != "committed" {
		t.Errorf("status = %q, want %q", tx.Status, "committed")
	}
	if tx.CommittedAt == nil {
		t.Error("CommittedAt should be set after commit")
	}
}

func TestTransaction_ModifyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.go")
	original := []byte("package main\n\nfunc old() {}\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	newContent := []byte("package main\n\nfunc new() {}\n")
	tx := NewTransaction()
	if err := tx.AddModify(path, newContent); err != nil {
		t.Fatalf("AddModify: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after commit: %v", err)
	}
	if string(got) != string(newContent) {
		t.Errorf("content mismatch: got %q, want %q", got, newContent)
	}
}

func TestTransaction_DeleteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "to_delete.go")
	content := []byte("package old\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	tx := NewTransaction()
	if err := tx.AddDelete(path); err != nil {
		t.Fatalf("AddDelete: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should be deleted, got err: %v", err)
	}
}

func TestTransaction_RenameFile(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old_name.go")
	newPath := filepath.Join(dir, "new_name.go")
	content := []byte("package renamed\n")
	if err := os.WriteFile(oldPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	tx := NewTransaction()
	if err := tx.AddRename(oldPath, newPath); err != nil {
		t.Fatalf("AddRename: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old path should not exist, got err: %v", err)
	}
	got, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("ReadFile new path: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestTransaction_CommitAppliesAllChanges(t *testing.T) {
	dir := t.TempDir()

	// Set up existing files
	existingPath := filepath.Join(dir, "existing.txt")
	toDeletePath := filepath.Join(dir, "delete_me.txt")
	toRenamePath := filepath.Join(dir, "old_name.txt")
	renamedPath := filepath.Join(dir, "new_name.txt")
	createPath := filepath.Join(dir, "subdir", "created.txt")

	os.WriteFile(existingPath, []byte("original"), 0o644)
	os.WriteFile(toDeletePath, []byte("goodbye"), 0o644)
	os.WriteFile(toRenamePath, []byte("moved"), 0o644)

	tx := NewTransaction()
	if err := tx.AddModify(existingPath, []byte("modified")); err != nil {
		t.Fatal(err)
	}
	if err := tx.AddDelete(toDeletePath); err != nil {
		t.Fatal(err)
	}
	if err := tx.AddRename(toRenamePath, renamedPath); err != nil {
		t.Fatal(err)
	}
	if err := tx.AddCreate(createPath, []byte("new file"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Verify all changes
	got, _ := os.ReadFile(existingPath)
	if string(got) != "modified" {
		t.Errorf("existingPath content = %q, want %q", got, "modified")
	}
	if _, err := os.Stat(toDeletePath); !os.IsNotExist(err) {
		t.Error("deleted file still exists")
	}
	if _, err := os.Stat(toRenamePath); !os.IsNotExist(err) {
		t.Error("old renamed file still exists")
	}
	got, _ = os.ReadFile(renamedPath)
	if string(got) != "moved" {
		t.Errorf("renamedPath content = %q, want %q", got, "moved")
	}
	got, _ = os.ReadFile(createPath)
	if string(got) != "new file" {
		t.Errorf("createPath content = %q, want %q", got, "new file")
	}
}

func TestTransaction_RollbackOnFailure(t *testing.T) {
	dir := t.TempDir()

	// Create a file to modify
	existingPath := filepath.Join(dir, "keep.txt")
	os.WriteFile(existingPath, []byte("original content"), 0o644)

	// Create a file that will be "created" successfully
	createPath := filepath.Join(dir, "new.txt")

	// An operation that will fail: try to rename a non-existent file
	// We'll simulate failure by creating a file at the target rename path after adding ops
	failPath := filepath.Join(dir, "nonexistent_dir", "deeply", "nested", "impossible.txt")

	tx := NewTransaction()
	// This will succeed
	if err := tx.AddCreate(createPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// This will succeed
	if err := tx.AddModify(existingPath, []byte("new content")); err != nil {
		t.Fatal(err)
	}

	// Manually add an operation that will fail during commit (write to a read-only dir)
	tx.mu.Lock()
	tx.Operations = append(tx.Operations, FileOperation{
		Type:    "modify",
		Path:    failPath,
		Content: []byte("will fail"),
		Mode:    0o644,
	})
	tx.mu.Unlock()

	err := tx.Commit()
	if err == nil {
		t.Fatal("expected commit to fail")
	}

	if tx.Status != "rolled_back" {
		t.Errorf("status = %q, want %q", tx.Status, "rolled_back")
	}

	// Verify rollback: created file should be removed
	if _, err := os.Stat(createPath); !os.IsNotExist(err) {
		t.Error("created file should have been rolled back (deleted)")
	}

	// Verify rollback: modified file should be restored
	got, _ := os.ReadFile(existingPath)
	if string(got) != "original content" {
		t.Errorf("existing file should be restored, got %q", got)
	}
}

func TestTransaction_PartialFailureRollsBackCleanly(t *testing.T) {
	dir := t.TempDir()

	file1 := filepath.Join(dir, "file1.txt")
	file2 := filepath.Join(dir, "file2.txt")
	file3 := filepath.Join(dir, "file3.txt")

	os.WriteFile(file1, []byte("one"), 0o644)
	os.WriteFile(file2, []byte("two"), 0o644)
	os.WriteFile(file3, []byte("three"), 0o644)

	tx := NewTransaction()
	tx.AddModify(file1, []byte("ONE"))
	tx.AddModify(file2, []byte("TWO"))

	// Add operation that will fail: delete a file that won't exist at commit time
	// Simulate by removing file3 after adding the op
	tx.AddDelete(file3)

	// Remove file3 to make the delete fail (already deleted)
	os.Remove(file3)

	err := tx.Commit()
	if err == nil {
		t.Fatal("expected commit to fail")
	}

	if tx.Status != "rolled_back" {
		t.Errorf("status = %q, want %q", tx.Status, "rolled_back")
	}

	// file1 and file2 should be restored
	got1, _ := os.ReadFile(file1)
	if string(got1) != "one" {
		t.Errorf("file1 should be restored to 'one', got %q", got1)
	}
	got2, _ := os.ReadFile(file2)
	if string(got2) != "two" {
		t.Errorf("file2 should be restored to 'two', got %q", got2)
	}
}

func TestTransaction_DryRunDoesNotModifyFiles(t *testing.T) {
	dir := t.TempDir()

	existingPath := filepath.Join(dir, "existing.txt")
	os.WriteFile(existingPath, []byte("unchanged"), 0o644)
	createPath := filepath.Join(dir, "wont_create.txt")

	tx := NewTransaction()
	tx.AddModify(existingPath, []byte("changed"))
	tx.AddCreate(createPath, []byte("new"), 0o644)

	descriptions := tx.DryRun()
	if len(descriptions) != 2 {
		t.Fatalf("DryRun returned %d descriptions, want 2", len(descriptions))
	}

	// Files should be unmodified
	got, _ := os.ReadFile(existingPath)
	if string(got) != "unchanged" {
		t.Errorf("DryRun modified the file! got %q", got)
	}
	if _, err := os.Stat(createPath); !os.IsNotExist(err) {
		t.Error("DryRun created a file!")
	}

	// Check descriptions contain expected info
	if !strings.Contains(descriptions[0], "MODIFY") {
		t.Errorf("first description should be MODIFY, got %q", descriptions[0])
	}
	if !strings.Contains(descriptions[1], "CREATE") {
		t.Errorf("second description should be CREATE, got %q", descriptions[1])
	}
}

func TestTransaction_ValidateCatchesConflicts(t *testing.T) {
	dir := t.TempDir()

	path := filepath.Join(dir, "file.txt")
	os.WriteFile(path, []byte("data"), 0o644)

	// Create a transaction with operations that reference non-existent directories
	tx := NewTransaction()
	tx.AddModify(path, []byte("new data"))

	// Manually add an operation targeting a non-existent directory
	tx.mu.Lock()
	tx.Operations = append(tx.Operations, FileOperation{
		Type:    "create",
		Path:    filepath.Join(dir, "nonexistent_dir", "file.txt"),
		Content: []byte("content"),
		Mode:    0o644,
	})
	tx.mu.Unlock()

	warnings := tx.Validate()
	if len(warnings) == 0 {
		t.Error("Validate should return warnings for non-existent directory")
	}

	found := false
	for _, w := range warnings {
		if strings.Contains(w, "does not exist") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'does not exist' warning, got: %v", warnings)
	}
}

func TestTransaction_ValidateDuplicateOperations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	os.WriteFile(path, []byte("data"), 0o644)

	tx := NewTransaction()
	if err := tx.AddModify(path, []byte("new")); err != nil {
		t.Fatal(err)
	}

	// Try to add another operation on the same file
	err := tx.AddDelete(path)
	if err == nil {
		t.Fatal("expected error for duplicate operation on same path")
	}
	if !strings.Contains(err.Error(), "already contains an operation") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestTransaction_SummaryFormatting(t *testing.T) {
	dir := t.TempDir()

	existingPath := filepath.Join(dir, "handler.go")
	toDeletePath := filepath.Join(dir, "old.go")
	createPath := filepath.Join(dir, "new.go")

	os.WriteFile(existingPath, []byte("line1\nline2\nline3\n"), 0o644)
	os.WriteFile(toDeletePath, []byte("delete me\n"), 0o644)

	tx := NewTransaction()
	tx.AddModify(existingPath, []byte("line1\nline2\nline3\nline4\n"))
	tx.AddDelete(toDeletePath)
	tx.AddCreate(createPath, []byte("package new\n"), 0o644)

	summary := tx.Summary()

	if !strings.Contains(summary, tx.ID) {
		t.Error("summary should contain transaction ID")
	}
	if !strings.Contains(summary, "pending") {
		t.Error("summary should contain status")
	}
	if !strings.Contains(summary, "MODIFY") {
		t.Error("summary should contain MODIFY")
	}
	if !strings.Contains(summary, "DELETE") {
		t.Error("summary should contain DELETE")
	}
	if !strings.Contains(summary, "CREATE") {
		t.Error("summary should contain CREATE")
	}
	if !strings.Contains(summary, "Total:") {
		t.Error("summary should contain Total line")
	}
}

func TestTransaction_ConcurrentSafety(t *testing.T) {
	dir := t.TempDir()

	// Create multiple files for concurrent transactions
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := filepath.Join(dir, strings.ReplaceAll("file_IDX.txt", "IDX", strings.Repeat("x", idx+1)))
			content := []byte(strings.Repeat("content", idx+1))

			tx := NewTransaction()
			if err := tx.AddCreate(path, content, 0o644); err != nil {
				errors <- err
				return
			}
			if err := tx.Commit(); err != nil {
				errors <- err
				return
			}

			// Verify
			got, err := os.ReadFile(path)
			if err != nil {
				errors <- err
				return
			}
			if string(got) != string(content) {
				errors <- os.ErrInvalid
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Errorf("concurrent transaction error: %v", err)
		}
	}
}

func TestTransaction_MultipleOperationsSameFileRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	os.WriteFile(path, []byte("original"), 0o644)

	tx := NewTransaction()
	if err := tx.AddModify(path, []byte("first edit")); err != nil {
		t.Fatal(err)
	}

	// Second operation on same file should be rejected
	err := tx.AddModify(path, []byte("second edit"))
	if err == nil {
		t.Fatal("expected error for second operation on same file")
	}
	if !strings.Contains(err.Error(), "already contains an operation") {
		t.Errorf("error should mention duplicate: %v", err)
	}

	// Also test create on same path
	tx2 := NewTransaction()
	tx2.AddCreate(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	err = tx2.AddCreate(filepath.Join(dir, "a.txt"), []byte("b"), 0o644)
	if err == nil {
		t.Fatal("expected error for duplicate create on same path")
	}
}

func TestTransaction_DirectoryCreationForNestedPaths(t *testing.T) {
	dir := t.TempDir()
	deepPath := filepath.Join(dir, "a", "b", "c", "deep_file.txt")

	tx := NewTransaction()
	if err := tx.AddCreate(deepPath, []byte("deep content"), 0o644); err != nil {
		t.Fatalf("AddCreate: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got, err := os.ReadFile(deepPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "deep content" {
		t.Errorf("content = %q, want %q", got, "deep content")
	}

	// Verify directory was created
	info, err := os.Stat(filepath.Join(dir, "a", "b", "c"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Error("expected directory to be created")
	}
}

func TestTransaction_RollbackExplicit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	os.WriteFile(path, []byte("original"), 0o644)

	tx := NewTransaction()
	tx.AddModify(path, []byte("modified"))
	tx.Commit()

	// Explicit rollback after commit
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "original" {
		t.Errorf("file should be restored after rollback, got %q", got)
	}
	if tx.Status != "rolled_back" {
		t.Errorf("status = %q, want %q", tx.Status, "rolled_back")
	}
}

func TestTransaction_RollbackPending(t *testing.T) {
	tx := NewTransaction()
	// Rollback a pending transaction (no-op)
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback pending: %v", err)
	}
	if tx.Status != "rolled_back" {
		t.Errorf("status = %q, want %q", tx.Status, "rolled_back")
	}
}

func TestTransaction_CannotAddAfterCommit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	tx := NewTransaction()
	tx.AddCreate(path, []byte("data"), 0o644)
	tx.Commit()

	err := tx.AddCreate(filepath.Join(dir, "another.txt"), []byte("more"), 0o644)
	if err == nil {
		t.Fatal("expected error adding to committed transaction")
	}
	if !strings.Contains(err.Error(), "committed") {
		t.Errorf("error should mention committed state: %v", err)
	}
}

func TestTransaction_CreateFailsIfFileExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	os.WriteFile(path, []byte("exists"), 0o644)

	tx := NewTransaction()
	err := tx.AddCreate(path, []byte("overwrite"), 0o644)
	if err == nil {
		t.Fatal("expected error creating file that already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists': %v", err)
	}
}

func TestTransaction_ModifyFailsIfFileDoesNotExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.txt")

	tx := NewTransaction()
	err := tx.AddModify(path, []byte("content"))
	if err == nil {
		t.Fatal("expected error modifying non-existent file")
	}
}

func TestTransaction_DeleteFailsIfFileDoesNotExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.txt")

	tx := NewTransaction()
	err := tx.AddDelete(path)
	if err == nil {
		t.Fatal("expected error deleting non-existent file")
	}
}

func TestTransaction_FilesDiff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "code.go")
	os.WriteFile(path, []byte("func old() {}\n"), 0o644)

	tx := NewTransaction()
	tx.AddModify(path, []byte("func new() {}\n"))

	diff := tx.FilesDiff()
	if !strings.Contains(diff, "---") {
		t.Error("diff should contain --- header")
	}
	if !strings.Contains(diff, "+++") {
		t.Error("diff should contain +++ header")
	}
	if !strings.Contains(diff, "-func old()") {
		t.Error("diff should show removed line")
	}
	if !strings.Contains(diff, "+func new()") {
		t.Error("diff should show added line")
	}
}

func TestTransaction_NewTransactionHasUniqueID(t *testing.T) {
	tx1 := NewTransaction()
	tx2 := NewTransaction()
	if tx1.ID == tx2.ID {
		t.Error("two transactions should have different IDs")
	}
	if !strings.HasPrefix(tx1.ID, "tx_") {
		t.Errorf("transaction ID should start with 'tx_', got %q", tx1.ID)
	}
}

func TestTransactionTool_Interface(t *testing.T) {
	var tool Tool = TransactionTool{}
	if tool.Name() != "AtomicMultiEdit" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "AtomicMultiEdit")
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	params := tool.Parameters()
	if params == nil {
		t.Error("Parameters() should not be nil")
	}
}

func TestTransactionTool_Execute(t *testing.T) {
	dir := t.TempDir()
	existingPath := filepath.Join(dir, "existing.txt")
	os.WriteFile(existingPath, []byte("original"), 0o644)

	createPath := filepath.Join(dir, "new_file.txt")

	input := TransactionInput{
		Operations: []struct {
			Type       string `json:"type"`
			Path       string `json:"path"`
			OldPath    string `json:"old_path,omitempty"`
			Content    string `json:"content,omitempty"`
			NewContent string `json:"new_content,omitempty"`
			Mode       int    `json:"mode,omitempty"`
		}{
			{Type: "create", Path: createPath, Content: "created content"},
			{Type: "modify", Path: existingPath, Content: "modified content"},
		},
	}

	data, _ := json.Marshal(input)
	ctx := testCtx()

	tool := TransactionTool{}
	result, err := tool.Execute(ctx, data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "committed successfully") {
		t.Errorf("result should contain success message, got: %s", result)
	}

	// Verify files
	got, _ := os.ReadFile(createPath)
	if string(got) != "created content" {
		t.Errorf("created file content = %q, want %q", got, "created content")
	}
	got, _ = os.ReadFile(existingPath)
	if string(got) != "modified content" {
		t.Errorf("modified file content = %q, want %q", got, "modified content")
	}
}

func TestTransactionTool_ExecuteDryRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	os.WriteFile(path, []byte("original"), 0o644)

	input := TransactionInput{
		Operations: []struct {
			Type       string `json:"type"`
			Path       string `json:"path"`
			OldPath    string `json:"old_path,omitempty"`
			Content    string `json:"content,omitempty"`
			NewContent string `json:"new_content,omitempty"`
			Mode       int    `json:"mode,omitempty"`
		}{
			{Type: "modify", Path: path, Content: "changed"},
		},
		DryRun: true,
	}

	data, _ := json.Marshal(input)
	ctx := testCtx()

	tool := TransactionTool{}
	result, err := tool.Execute(ctx, data)
	if err != nil {
		t.Fatalf("Execute dry run: %v", err)
	}
	if !strings.Contains(result, "Dry run") {
		t.Errorf("result should mention dry run, got: %s", result)
	}

	// File should not be modified
	got, _ := os.ReadFile(path)
	if string(got) != "original" {
		t.Errorf("dry run should not modify files, got %q", got)
	}
}

func TestTransactionTool_ExecuteEmptyOperations(t *testing.T) {
	input := TransactionInput{}
	data, _ := json.Marshal(input)
	ctx := testCtx()

	tool := TransactionTool{}
	_, err := tool.Execute(ctx, data)
	if err == nil {
		t.Fatal("expected error for empty operations")
	}
	if !strings.Contains(err.Error(), "at least one operation") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTransactionTool_RejectsCredentialContent(t *testing.T) {
	dir := t.TempDir()
	createPath := filepath.Join(dir, "notes.txt")

	input := TransactionInput{
		Operations: []struct {
			Type       string `json:"type"`
			Path       string `json:"path"`
			OldPath    string `json:"old_path,omitempty"`
			Content    string `json:"content,omitempty"`
			NewContent string `json:"new_content,omitempty"`
			Mode       int    `json:"mode,omitempty"`
		}{
			{Type: "create", Path: createPath, Content: "token=sk-abcdefghijklmnopqrstuvwxyz"},
		},
	}

	data, _ := json.Marshal(input)
	_, err := (TransactionTool{}).Execute(testCtx(), data)
	if err == nil || !strings.Contains(err.Error(), "contains a credential") {
		t.Fatalf("expected credential rejection, got %v", err)
	}
	if _, statErr := os.Stat(createPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected file not to be created, stat err = %v", statErr)
	}
}

func TestTransaction_RenameRequiresOldPath(t *testing.T) {
	dir := t.TempDir()
	tx := NewTransaction()
	err := tx.Add(FileOperation{
		Type: "rename",
		Path: filepath.Join(dir, "new.txt"),
		// OldPath is empty
	})
	if err == nil {
		t.Fatal("expected error for rename without OldPath")
	}
	if !strings.Contains(err.Error(), "requires OldPath") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTransaction_UnknownOperationType(t *testing.T) {
	tx := NewTransaction()
	err := tx.Add(FileOperation{
		Type: "invalid",
		Path: "/tmp/file.txt",
	})
	if err == nil {
		t.Fatal("expected error for unknown operation type")
	}
	if !strings.Contains(err.Error(), "unknown operation type") {
		t.Errorf("unexpected error: %v", err)
	}
}
