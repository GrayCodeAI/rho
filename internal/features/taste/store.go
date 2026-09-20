package taste

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GrayCodeAI/rho/internal/storage"
)

const (
	storeVersion = 1
)

// storeEnvelope wraps a profile with versioning metadata for serialization.
type storeEnvelope struct {
	Version    int       `json:"version"`
	ExportedAt time.Time `json:"exported_at"`
	Profile    *Profile  `json:"profile"`
}

// Store manages file-based persistence for taste profiles.
type Store struct {
	baseDir string
}

// NewStore creates a store rooted at the given base directory.
// If baseDir is empty, it defaults to Rho's user state taste directory.
func NewStore(baseDir string) (*Store, error) {
	if baseDir == "" {
		baseDir = storage.TasteDir()
	}

	if err := os.MkdirAll(baseDir, 0o750); err != nil {
		return nil, fmt.Errorf("cannot create taste store directory: %w", err)
	}

	return &Store{baseDir: baseDir}, nil
}

// Save persists a profile for the given project ID.
func (s *Store) Save(projectID string, profile *Profile) error {
	if projectID == "" {
		projectID = "default"
	}

	path := s.profilePath(projectID)
	env := storeEnvelope{
		Version:    storeVersion,
		ExportedAt: time.Now(),
		Profile:    profile,
	}

	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create profile directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write profile: %w", err)
	}

	return nil
}

// Load reads a profile for the given project ID.
func (s *Store) Load(projectID string) (*Profile, error) {
	if projectID == "" {
		projectID = "default"
	}

	path := s.profilePath(projectID)
	data, err := os.ReadFile(path) // #nosec G304 -- path is built from sanitizeProjectID, which strips path separators and other unsafe characters
	if err != nil {
		if os.IsNotExist(err) {
			return NewProfile(projectID), nil
		}
		return nil, fmt.Errorf("read profile: %w", err)
	}

	var env storeEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("unmarshal profile: %w", err)
	}

	if env.Profile == nil {
		return NewProfile(projectID), nil
	}

	return env.Profile, nil
}

// Export serializes the current profile for transfer (push/pull).
func (s *Store) Export(projectID string) ([]byte, error) {
	profile, err := s.Load(projectID)
	if err != nil {
		return nil, err
	}

	env := storeEnvelope{
		Version:    storeVersion,
		ExportedAt: time.Now(),
		Profile:    profile,
	}

	return json.MarshalIndent(env, "", "  ")
}

// Import loads a profile from exported data.
func (s *Store) Import(data []byte) error {
	var env storeEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("invalid taste data: %w", err)
	}

	if env.Profile == nil {
		return fmt.Errorf("empty profile in import data")
	}

	projectID := env.Profile.ProjectID
	if projectID == "" {
		projectID = "default"
	}

	return s.Save(projectID, env.Profile)
}

// List returns all project IDs that have stored profiles.
func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) == ".json" {
			ids = append(ids, strings.TrimSuffix(name, ".json"))
		}
	}
	return ids, nil
}

// Delete removes a stored profile.
func (s *Store) Delete(projectID string) error {
	if projectID == "" {
		projectID = "default"
	}
	path := s.profilePath(projectID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// profilePath returns the file path for a given project ID.
func (s *Store) profilePath(projectID string) string {
	// Sanitize project ID for filesystem.
	safe := sanitizeProjectID(projectID)
	return filepath.Join(s.baseDir, safe+".json")
}

// sanitizeProjectID makes a project ID safe for use as a filename.
func sanitizeProjectID(id string) string {
	var b []byte
	for _, c := range []byte(id) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			b = append(b, c)
		default:
			b = append(b, '_')
		}
	}
	if len(b) == 0 {
		return "default"
	}
	return string(b)
}
