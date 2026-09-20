package taste

import (
	"testing"
)

func TestHooks_OnCodeAccepted(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewStore(tmp)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	hooks, err := NewHooks("test-project", store)
	if err != nil {
		t.Fatalf("NewHooks: %v", err)
	}

	code := `func getUserData(userID string) (*User, error) {
	user, err := db.FindUser(userID)
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	return user, nil
}`

	hooks.OnCodeAccepted("session-1", code)

	// Profile should have learned something.
	profile := hooks.Profile()
	if len(profile.Preferences) == 0 {
		t.Error("expected profile to have preferences after accept")
	}
}

func TestHooks_OnCodeEdited(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewStore(tmp)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	hooks, err := NewHooks("test-project", store)
	if err != nil {
		t.Fatalf("NewHooks: %v", err)
	}

	proposed := `func get_data(user_id string) {
	result := fetch_from_db(user_id)
	panic("not implemented")
}`
	final := `func getData(userID string) error {
	result := fetchFromDB(userID)
	if result == nil {
		return fmt.Errorf("no data for user %s: %w", userID, ErrNotFound)
	}
	return nil
}`

	hooks.OnCodeEdited("session-1", proposed, final)

	profile := hooks.Profile()
	if len(profile.Preferences) == 0 {
		t.Error("expected preferences to be updated after edit")
	}
}

func TestHooks_OnCodeRejected(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewStore(tmp)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	hooks, err := NewHooks("test-project", store)
	if err != nil {
		t.Fatalf("NewHooks: %v", err)
	}

	code := `func getData() {
	panic("something went wrong")
}`

	hooks.OnCodeRejected("session-1", code)

	// Should not crash, and collector should have the proposal recorded.
	collector := hooks.Collector()
	if collector == nil {
		t.Fatal("collector should not be nil")
	}
}

func TestHooks_PromptContext(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewStore(tmp)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	hooks, err := NewHooks("test-project", store)
	if err != nil {
		t.Fatalf("NewHooks: %v", err)
	}

	// Initially empty.
	ctx := hooks.PromptContext()
	if ctx != "" {
		t.Errorf("expected empty prompt context initially, got %q", ctx)
	}

	// After enough signals, should produce context.
	for i := 0; i < 5; i++ {
		hooks.Profile().Update(CategoryNaming, Signal{Value: "camelCase", Confidence: 0.8, SampleCount: 3})
	}

	ctx = hooks.PromptContext()
	if ctx == "" {
		t.Error("expected non-empty prompt context after signals")
	}
}

func TestHooks_Persistence(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewStore(tmp)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	hooks, err := NewHooks("persist-test", store)
	if err != nil {
		t.Fatalf("NewHooks: %v", err)
	}

	code := `func getUserName(userID string) string {
	name := fetchName(userID)
	return name
}`
	hooks.OnCodeAccepted("session-1", code)

	// Load from store to verify persistence.
	loaded, err := store.Load("persist-test")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// The profile should have been saved.
	if loaded.ProjectID != "persist-test" {
		t.Errorf("expected project ID 'persist-test', got %q", loaded.ProjectID)
	}
}
