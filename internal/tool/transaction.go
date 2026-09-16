package tool

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FileOperation represents a single file operation within a transaction.
type FileOperation struct {
	Type       string      // "create", "modify", "delete", "rename"
	Path       string      // target path
	OldPath    string      // source path for rename operations
	Content    []byte      // new content (for create/modify)
	OldContent []byte      // original content (captured before modification)
	Mode       os.FileMode // file permissions
}

// Transaction groups multiple file operations into an atomic unit.
// Either all operations succeed or all are rolled back.
type Transaction struct {
	ID          string
	Operations  []FileOperation
	Status      string // "pending", "committed", "rolled_back"
	CreatedAt   time.Time
	CommittedAt *time.Time
	backups     map[string][]byte // original file contents keyed by path
	mu          sync.Mutex
}

// NewTransaction creates a new pending transaction with a unique ID.
func NewTransaction() *Transaction {
	return &Transaction{
		ID:        generateTxID(),
		Status:    "pending",
		CreatedAt: time.Now(),
		backups:   make(map[string][]byte),
	}
}

// Add adds a file operation to the transaction after validating it and capturing
// the current file state for potential rollback.
func (tx *Transaction) Add(op FileOperation) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if tx.Status != "pending" {
		return fmt.Errorf("transaction %s is %s, cannot add operations", tx.ID, tx.Status)
	}

	// Check for duplicate operations on the same path
	for _, existing := range tx.Operations {
		if existing.Path == op.Path {
			return fmt.Errorf("transaction already contains an operation on %s", op.Path)
		}
		if op.Type == "rename" && existing.Path == op.OldPath {
			return fmt.Errorf("transaction already contains an operation on %s", op.OldPath)
		}
	}

	switch op.Type {
	case "create":
		if _, err := os.Stat(op.Path); err == nil {
			return fmt.Errorf("cannot create %s: file already exists", op.Path)
		}
	case "modify":
		info, err := os.Stat(op.Path)
		if err != nil {
			return fmt.Errorf("cannot modify %s: %w", op.Path, err)
		}
		if info.IsDir() {
			return fmt.Errorf("cannot modify %s: is a directory", op.Path)
		}
		data, err := os.ReadFile(op.Path)
		if err != nil {
			return fmt.Errorf("cannot read %s for backup: %w", op.Path, err)
		}
		op.OldContent = data
		if op.Mode == 0 {
			op.Mode = info.Mode()
		}
		tx.backups[op.Path] = data
	case "delete":
		info, err := os.Stat(op.Path)
		if err != nil {
			return fmt.Errorf("cannot delete %s: %w", op.Path, err)
		}
		if info.IsDir() {
			return fmt.Errorf("cannot delete %s: is a directory", op.Path)
		}
		data, err := os.ReadFile(op.Path)
		if err != nil {
			return fmt.Errorf("cannot read %s for backup: %w", op.Path, err)
		}
		op.OldContent = data
		if op.Mode == 0 {
			op.Mode = info.Mode()
		}
		tx.backups[op.Path] = data
	case "rename":
		if op.OldPath == "" {
			return fmt.Errorf("rename operation requires OldPath")
		}
		info, err := os.Stat(op.OldPath)
		if err != nil {
			return fmt.Errorf("cannot rename %s: %w", op.OldPath, err)
		}
		if info.IsDir() {
			return fmt.Errorf("cannot rename %s: is a directory", op.OldPath)
		}
		if _, statErr := os.Stat(op.Path); statErr == nil {
			return fmt.Errorf("cannot rename to %s: file already exists", op.Path)
		}
		data, readErr := os.ReadFile(op.OldPath)
		if readErr != nil {
			return fmt.Errorf("cannot read %s for backup: %w", op.OldPath, readErr)
		}
		op.OldContent = data
		if op.Mode == 0 {
			op.Mode = info.Mode()
		}
		tx.backups[op.OldPath] = data
	default:
		return fmt.Errorf("unknown operation type: %s", op.Type)
	}

	tx.Operations = append(tx.Operations, op)
	return nil
}

// AddCreate adds a file creation operation to the transaction.
func (tx *Transaction) AddCreate(path string, content []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	return tx.Add(FileOperation{
		Type:    "create",
		Path:    path,
		Content: content,
		Mode:    mode,
	})
}

// AddModify adds a file modification operation to the transaction.
// It reads and stores the original content for potential rollback.
func (tx *Transaction) AddModify(path string, newContent []byte) error {
	return tx.Add(FileOperation{
		Type:    "modify",
		Path:    path,
		Content: newContent,
	})
}

// AddDelete adds a file deletion operation to the transaction.
// It reads and stores the original content for potential rollback.
func (tx *Transaction) AddDelete(path string) error {
	return tx.Add(FileOperation{
		Type: "delete",
		Path: path,
	})
}

// AddRename adds a file rename operation to the transaction.
func (tx *Transaction) AddRename(oldPath, newPath string) error {
	return tx.Add(FileOperation{
		Type:    "rename",
		Path:    newPath,
		OldPath: oldPath,
	})
}

// Commit applies all operations in order. If any operation fails,
// all previously applied operations are automatically rolled back.
func (tx *Transaction) Commit() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if tx.Status != "pending" {
		return fmt.Errorf("transaction %s is %s, cannot commit", tx.ID, tx.Status)
	}

	var applied []int
	for i, op := range tx.Operations {
		if err := applyOperation(op); err != nil {
			// Rollback all previously applied operations
			rollbackErr := rollbackOperations(tx.Operations, applied)
			tx.Status = "rolled_back"
			if rollbackErr != nil {
				return fmt.Errorf("operation %d (%s %s) failed: %w; rollback also encountered errors: %w",
					i, op.Type, op.Path, err, rollbackErr)
			}
			return fmt.Errorf("operation %d (%s %s) failed: %w; all changes rolled back",
				i, op.Type, op.Path, err)
		}
		applied = append(applied, i)
	}

	now := time.Now()
	tx.CommittedAt = &now
	tx.Status = "committed"
	return nil
}

// Rollback reverses all applied operations, restoring original file states.
func (tx *Transaction) Rollback() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if tx.Status == "rolled_back" {
		return fmt.Errorf("transaction %s is already rolled back", tx.ID)
	}

	if tx.Status == "pending" {
		tx.Status = "rolled_back"
		return nil
	}

	// Roll back all operations in reverse order
	indices := make([]int, len(tx.Operations))
	for i := range indices {
		indices[i] = i
	}
	err := rollbackOperations(tx.Operations, indices)
	tx.Status = "rolled_back"
	return err
}

// DryRun returns descriptions of what each operation would do without applying them.
func (tx *Transaction) DryRun() []string {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	var descriptions []string
	for _, op := range tx.Operations {
		switch op.Type {
		case "create":
			descriptions = append(descriptions, fmt.Sprintf("CREATE %s (%d bytes)", op.Path, len(op.Content)))
		case "modify":
			oldLines := countLines(op.OldContent)
			newLines := countLines(op.Content)
			descriptions = append(descriptions, fmt.Sprintf("MODIFY %s (replace %d lines with %d lines)", op.Path, oldLines, newLines))
		case "delete":
			descriptions = append(descriptions, fmt.Sprintf("DELETE %s (%d bytes)", op.Path, len(op.OldContent)))
		case "rename":
			descriptions = append(descriptions, fmt.Sprintf("RENAME %s -> %s", op.OldPath, op.Path))
		}
	}
	return descriptions
}

// Validate pre-checks all operations for potential issues without applying them.
// Returns a list of warnings or an empty slice if everything looks good.
func (tx *Transaction) Validate() []string {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	var warnings []string

	// Check for conflicting operations (create + delete same file)
	pathOps := make(map[string][]string)
	for _, op := range tx.Operations {
		pathOps[op.Path] = append(pathOps[op.Path], op.Type)
		if op.OldPath != "" {
			pathOps[op.OldPath] = append(pathOps[op.OldPath], op.Type+"-source")
		}
	}
	for path, ops := range pathOps {
		if len(ops) > 1 {
			warnings = append(warnings, fmt.Sprintf("conflicting operations on %s: %s", path, strings.Join(ops, ", ")))
		}
	}

	// Check target directories exist
	for _, op := range tx.Operations {
		switch op.Type {
		case "create", "modify":
			dir := filepath.Dir(op.Path)
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				warnings = append(warnings, fmt.Sprintf("directory %s does not exist for %s %s", dir, op.Type, op.Path))
			}
		case "rename":
			dir := filepath.Dir(op.Path)
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				warnings = append(warnings, fmt.Sprintf("directory %s does not exist for rename target %s", dir, op.Path))
			}
		}
	}

	// Check permissions
	for _, op := range tx.Operations {
		switch op.Type {
		case "create":
			dir := filepath.Dir(op.Path)
			if err := checkWritable(dir); err != nil {
				warnings = append(warnings, fmt.Sprintf("directory %s is not writable: %v", dir, err))
			}
		case "modify", "delete":
			if err := checkWritable(filepath.Dir(op.Path)); err != nil {
				warnings = append(warnings, fmt.Sprintf("cannot write to directory of %s: %v", op.Path, err))
			}
		}
	}

	return warnings
}

// Summary returns a human-readable transaction summary.
func (tx *Transaction) Summary() string {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Transaction %s (%s):\n", tx.ID, tx.Status))

	var created, modified, deleted, renamed int
	for _, op := range tx.Operations {
		switch op.Type {
		case "create":
			sb.WriteString(fmt.Sprintf("  CREATE %s (+%d bytes)\n", op.Path, len(op.Content)))
			created++
		case "modify":
			added, removed := diffLineCount(op.OldContent, op.Content)
			sb.WriteString(fmt.Sprintf("  MODIFY %s (+%d, -%d)\n", op.Path, added, removed))
			modified++
		case "delete":
			sb.WriteString(fmt.Sprintf("  DELETE %s (-%d bytes)\n", op.Path, len(op.OldContent)))
			deleted++
		case "rename":
			sb.WriteString(fmt.Sprintf("  RENAME %s -> %s\n", op.OldPath, op.Path))
			renamed++
		}
	}

	parts := []string{}
	if modified > 0 {
		parts = append(parts, fmt.Sprintf("%d files modified", modified))
	}
	if created > 0 {
		parts = append(parts, fmt.Sprintf("%d created", created))
	}
	if deleted > 0 {
		parts = append(parts, fmt.Sprintf("%d deleted", deleted))
	}
	if renamed > 0 {
		parts = append(parts, fmt.Sprintf("%d renamed", renamed))
	}
	if len(parts) > 0 {
		sb.WriteString(fmt.Sprintf("Total: %s\n", strings.Join(parts, ", ")))
	}

	return sb.String()
}

// FilesDiff returns a unified diff representation of all modifications in the transaction.
func (tx *Transaction) FilesDiff() string {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	var sb strings.Builder
	for _, op := range tx.Operations {
		switch op.Type {
		case "create":
			sb.WriteString(fmt.Sprintf("--- /dev/null\n+++ b/%s\n", op.Path))
			lines := strings.Split(string(op.Content), "\n")
			for _, line := range lines {
				sb.WriteString(fmt.Sprintf("+%s\n", line))
			}
			sb.WriteString("\n")
		case "modify":
			sb.WriteString(fmt.Sprintf("--- a/%s\n+++ b/%s\n", op.Path, op.Path))
			sb.WriteString(unifiedDiff(string(op.OldContent), string(op.Content)))
			sb.WriteString("\n")
		case "delete":
			sb.WriteString(fmt.Sprintf("--- a/%s\n+++ /dev/null\n", op.Path))
			lines := strings.Split(string(op.OldContent), "\n")
			for _, line := range lines {
				sb.WriteString(fmt.Sprintf("-%s\n", line))
			}
			sb.WriteString("\n")
		case "rename":
			sb.WriteString(fmt.Sprintf("rename from %s\nrename to %s\n\n", op.OldPath, op.Path))
		}
	}
	return sb.String()
}

// TransactionTool implements the Tool interface for atomic multi-file edits.
type TransactionTool struct{}

func (TransactionTool) Name() string      { return "AtomicMultiEdit" }
func (TransactionTool) RiskLevel() string { return "high" }
func (TransactionTool) Aliases() []string {
	return []string{"atomic_multi_edit", "transaction_edit"}
}

func (TransactionTool) Description() string {
	return "Apply multiple file operations (create, modify, delete, rename) as an atomic transaction. " +
		"Either all operations succeed or all are rolled back."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (TransactionTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"operations": {
				Type:        "array",
				Description: "List of file operations to apply atomically",
				Items: &SchemaProperty{
					Type: "object",
					Properties: map[string]SchemaProperty{
						"type":        {Type: "string", Enum: []interface{}{"create", "modify", "delete", "rename"}, Description: "Operation type"},
						"path":        {Type: "string", Description: "Target file path"},
						"old_path":    {Type: "string", Description: "Source path (for rename)"},
						"content":     {Type: "string", Description: "File content (for create/modify)"},
						"mode":        {Type: "integer", Description: "File mode/permissions (optional, default 0644)"},
						"new_content": {Type: "string", Description: "New content (alias for content, for modify)"},
					},
					Required: []string{"type", "path"},
				},
			},
			"dry_run": {Type: "boolean", Description: "If true, validate and describe operations without applying them"},
		},
		Required: []string{"operations"},
	}
}

func (TransactionTool) Parameters() map[string]interface{} {
	return transactionSchema.ToJSONSchema()
}

// transactionSchema is the single source of truth for Transaction's input schema.
var transactionSchema = TransactionTool{}.Schema()

// TransactionInput is the typed input for TransactionTool.
type TransactionInput struct {
	Operations []struct {
		Type       string `json:"type"`
		Path       string `json:"path"`
		OldPath    string `json:"old_path,omitempty"`
		Content    string `json:"content,omitempty"`
		NewContent string `json:"new_content,omitempty"`
		Mode       int    `json:"mode,omitempty"`
	} `json:"operations"`
	DryRun bool `json:"dry_run,omitempty"`
}

func (TransactionTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[TransactionInput]("Transaction", input)
	if err != nil {
		return "", err
	}
	if len(p.Operations) == 0 {
		return "", fmt.Errorf("at least one operation is required")
	}

	// Validate paths
	for _, op := range p.Operations {
		if err := validatePathAllowed(ctx, op.Path); err != nil {
			return "", err
		}
		if reason := IsSensitivePath(op.Path); reason != "" {
			return "", fmt.Errorf("blocked: %s (%s)", reason, op.Path)
		}
		if tc := GetToolContext(ctx); tc != nil && tc.Protected != nil && tc.Protected.IsProtected(op.Path) {
			return "", fmt.Errorf("path %s is protected (read-only)", op.Path)
		}
		if op.OldPath != "" {
			if err := validatePathAllowed(ctx, op.OldPath); err != nil {
				return "", err
			}
		}
	}

	tx := NewTransaction()

	for _, op := range p.Operations {
		content := op.Content
		if content == "" && op.NewContent != "" {
			content = op.NewContent
		}
		// Scan new file content for credentials, mirroring the FileWrite /
		// FileEdit guard. Without this, AtomicMultiEdit is a bypass for the
		// anti-exfil check: an LLM could persist a stolen API key to disk.
		if op.Type == "create" || op.Type == "modify" {
			if cred := DetectCredentials(content); cred != "" {
				return "", fmt.Errorf("content for %s contains a credential (%s) — refusing to write", op.Path, cred)
			}
		}
		mode := os.FileMode(0o644)
		if op.Mode != 0 {
			if op.Mode < 0 || op.Mode > 0o777 {
				return "", fmt.Errorf("invalid mode %d for %s: must be between 0 and 0777", op.Mode, op.Path)
			}
			mode = os.FileMode(op.Mode) // #nosec G115 -- op.Mode bounds-checked above (0-0777)
		}

		var err error
		switch op.Type {
		case "create":
			err = tx.AddCreate(op.Path, []byte(content), mode)
		case "modify":
			err = tx.AddModify(op.Path, []byte(content))
		case "delete":
			err = tx.AddDelete(op.Path)
		case "rename":
			err = tx.AddRename(op.OldPath, op.Path)
		default:
			return "", fmt.Errorf("unknown operation type: %s", op.Type)
		}
		if err != nil {
			return "", fmt.Errorf("failed to add operation: %w", err)
		}
	}

	if p.DryRun {
		warnings := tx.Validate()
		dryRun := tx.DryRun()
		var sb strings.Builder
		sb.WriteString("Dry run results:\n")
		for _, d := range dryRun {
			sb.WriteString("  " + d + "\n")
		}
		if len(warnings) > 0 {
			sb.WriteString("\nWarnings:\n")
			for _, w := range warnings {
				sb.WriteString("  " + w + "\n")
			}
		}
		return sb.String(), nil
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	return fmt.Sprintf("Transaction %s committed successfully.\n%s", tx.ID, tx.Summary()), nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func generateTxID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("tx_%d", time.Now().UnixNano())
	}
	return "tx_" + hex.EncodeToString(b)
}

func applyOperation(op FileOperation) error {
	switch op.Type {
	case "create":
		dir := filepath.Dir(op.Path)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
		return os.WriteFile(op.Path, op.Content, op.Mode)
	case "modify":
		return os.WriteFile(op.Path, op.Content, op.Mode)
	case "delete":
		return os.Remove(op.Path)
	case "rename":
		dir := filepath.Dir(op.Path)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
		return os.Rename(op.OldPath, op.Path)
	default:
		return fmt.Errorf("unknown operation type: %s", op.Type)
	}
}

func rollbackOperations(ops []FileOperation, applied []int) error {
	var errs []string
	// Rollback in reverse order
	for i := len(applied) - 1; i >= 0; i-- {
		idx := applied[i]
		op := ops[idx]
		if err := reverseOperation(op); err != nil {
			errs = append(errs, fmt.Sprintf("rollback op %d (%s %s): %v", idx, op.Type, op.Path, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func reverseOperation(op FileOperation) error {
	switch op.Type {
	case "create":
		// Reverse of create: delete the file
		return os.Remove(op.Path)
	case "modify":
		// Reverse of modify: restore old content
		return os.WriteFile(op.Path, op.OldContent, op.Mode)
	case "delete":
		// Reverse of delete: re-create the file
		dir := filepath.Dir(op.Path)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
		return os.WriteFile(op.Path, op.OldContent, op.Mode)
	case "rename":
		// Reverse of rename: move it back
		return os.Rename(op.Path, op.OldPath)
	default:
		return fmt.Errorf("unknown operation type: %s", op.Type)
	}
}

func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	n := 1
	for _, b := range data {
		if b == '\n' {
			n++
		}
	}
	return n
}

func diffLineCount(old, new []byte) (added, removed int) {
	oldLines := strings.Split(string(old), "\n")
	newLines := strings.Split(string(new), "\n")

	oldSet := make(map[string]int)
	for _, line := range oldLines {
		oldSet[line]++
	}
	newSet := make(map[string]int)
	for _, line := range newLines {
		newSet[line]++
	}

	for line, count := range newSet {
		if oldCount, exists := oldSet[line]; exists {
			if count > oldCount {
				added += count - oldCount
			}
		} else {
			added += count
		}
	}
	for line, count := range oldSet {
		if newCount, exists := newSet[line]; exists {
			if count > newCount {
				removed += count - newCount
			}
		} else {
			removed += count
		}
	}
	return added, removed
}

func unifiedDiff(old, new string) string {
	oldLines := strings.Split(old, "\n")
	newLines := strings.Split(new, "\n")

	var sb strings.Builder

	// Simple LCS-based diff
	lcs := lcsTable(oldLines, newLines)
	i, j := len(oldLines), len(newLines)

	type diffLine struct {
		prefix string
		text   string
	}
	var result []diffLine

	for i > 0 || j > 0 {
		if i > 0 && j > 0 && oldLines[i-1] == newLines[j-1] {
			result = append(result, diffLine{" ", oldLines[i-1]})
			i--
			j--
		} else if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
			result = append(result, diffLine{"+", newLines[j-1]})
			j--
		} else if i > 0 {
			result = append(result, diffLine{"-", oldLines[i-1]})
			i--
		}
	}

	// Reverse the result
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}

	for _, dl := range result {
		sb.WriteString(dl.prefix + dl.text + "\n")
	}
	return sb.String()
}

func lcsTable(a, b []string) [][]int {
	m, n := len(a), len(b)
	table := make([][]int, m+1)
	for i := range table {
		table[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				table[i][j] = table[i-1][j-1] + 1
			} else if table[i-1][j] >= table[i][j-1] {
				table[i][j] = table[i-1][j]
			} else {
				table[i][j] = table[i][j-1]
			}
		}
	}
	return table
}

func checkWritable(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	// Try to create a temp file to check writability
	f, err := os.CreateTemp(dir, ".rho_tx_check_*")
	if err != nil {
		return fmt.Errorf("not writable: %w", err)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}
