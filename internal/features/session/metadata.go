package session

import (
	"fmt"
	"os"
	"path/filepath"

	store "github.com/GrayCodeAI/rho/internal/session"
	"github.com/GrayCodeAI/rho/internal/storage"
)

// Rename changes the durable filename for a session after validating the new
// identifier.
func Rename(oldID, newID string) error {
	return RenameIn(storage.SessionsDir(), oldID, newID)
}

// RenameIn is the injectable-directory form used by tests and alternate
// storage layouts.
func RenameIn(dir, oldID, newID string) error {
	if err := store.ValidateID(newID); err != nil {
		return fmt.Errorf("invalid session name: %w", err)
	}
	oldPath := filepath.Join(dir, filepath.Base(oldID)+".jsonl")
	newPath := filepath.Join(dir, newID+".jsonl")
	return os.Rename(oldPath, newPath)
}

// AddTag appends a label to a session's durable tag file.
func AddTag(id, label string) error {
	return AddTagIn(storage.SessionsDir(), id, label)
}

// AddTagIn is the injectable-directory form used by tests and alternate
// storage layouts.
func AddTagIn(dir, id, label string) (retErr error) {
	tagPath := filepath.Join(dir, filepath.Base(id)+".tags")
	f, err := os.OpenFile(tagPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- path is constrained to the session directory
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := f.Close(); retErr == nil {
			retErr = closeErr
		}
	}()
	_, retErr = f.WriteString(label + "\n")
	return retErr
}
