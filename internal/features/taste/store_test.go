package taste

import (
	"testing"
)

func TestStore_SaveAndLoad(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewStore(tmp)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	profile := NewProfile("test-project")
	profile.Update(CategoryNaming, Signal{Value: "camelCase", Confidence: 0.8})
	profile.Update(CategoryComments, Signal{Value: "minimal", Confidence: 0.6})

	if saveErr := store.Save("test-project", profile); saveErr != nil {
		t.Fatalf("Save: %v", saveErr)
	}

	loaded, err := store.Load("test-project")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.ProjectID != "test-project" {
		t.Errorf("expected project ID 'test-project', got %q", loaded.ProjectID)
	}

	naming := loaded.Get(CategoryNaming)
	if naming.Value != "camelCase" {
		t.Errorf("expected naming 'camelCase', got %q", naming.Value)
	}

	comments := loaded.Get(CategoryComments)
	if comments.Value != "minimal" {
		t.Errorf("expected comments 'minimal', got %q", comments.Value)
	}
}

func TestStore_LoadNonexistent(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewStore(tmp)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	profile, err := store.Load("nonexistent")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Should return a fresh profile.
	if profile.ProjectID != "nonexistent" {
		t.Errorf("expected project ID 'nonexistent', got %q", profile.ProjectID)
	}
	if len(profile.Preferences) != 0 {
		t.Errorf("expected empty preferences, got %d", len(profile.Preferences))
	}
}

func TestStore_ExportAndImport(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewStore(tmp)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	profile := NewProfile("export-test")
	profile.Update(CategoryNaming, Signal{Value: "snake_case", Confidence: 0.9})

	if saveErr := store.Save("export-test", profile); saveErr != nil {
		t.Fatalf("Save: %v", saveErr)
	}

	data, err := store.Export("export-test")
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Import into a new store.
	tmp2 := t.TempDir()
	store2, err := NewStore(tmp2)
	if err != nil {
		t.Fatalf("NewStore2: %v", err)
	}

	if importErr := store2.Import(data); importErr != nil {
		t.Fatalf("Import: %v", importErr)
	}

	loaded, err := store2.Load("export-test")
	if err != nil {
		t.Fatalf("Load after import: %v", err)
	}

	naming := loaded.Get(CategoryNaming)
	if naming.Value != "snake_case" {
		t.Errorf("expected snake_case after import, got %q", naming.Value)
	}
}

func TestStore_List(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewStore(tmp)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	store.Save("project-a", NewProfile("project-a"))
	store.Save("project-b", NewProfile("project-b"))

	ids, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(ids) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(ids))
	}
}

func TestStore_Delete(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewStore(tmp)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	store.Save("delete-me", NewProfile("delete-me"))

	if deleteErr := store.Delete("delete-me"); deleteErr != nil {
		t.Fatalf("Delete: %v", deleteErr)
	}

	// Load should return a fresh profile after deletion.
	profile, err := store.Load("delete-me")
	if err != nil {
		t.Fatalf("Load after delete: %v", err)
	}
	if len(profile.Preferences) != 0 {
		t.Error("expected empty profile after deletion")
	}
}

func TestStore_Import_InvalidData(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewStore(tmp)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	err = store.Import([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSanitizeProjectID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my-project", "my-project"},
		{"my/project", "my_project"},
		{"path\\to\\project", "path_to_project"},
		{"", "default"},
		{"hello world", "hello_world"},
	}

	for _, tt := range tests {
		got := sanitizeProjectID(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeProjectID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
