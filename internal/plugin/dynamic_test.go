package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginState_Constants(t *testing.T) {
	t.Parallel()
	states := []PluginState{StateDiscovered, StateLoaded, StateActive, StateFailed, StateDisabled}
	expected := []string{"discovered", "loaded", "active", "failed", "disabled"}

	for i, state := range states {
		if string(state) != expected[i] {
			t.Errorf("state %d: expected %q, got %q", i, expected[i], string(state))
		}
	}
}

func TestNewDynamicPluginManager(t *testing.T) {
	t.Parallel()
	dirs := []string{"/tmp/test-plugins"}
	dm := NewDynamicPluginManager(dirs, nil, nil)

	if dm == nil {
		t.Fatal("NewDynamicPluginManager returned nil")
	}
	if len(dm.pluginDirs) != 1 {
		t.Errorf("expected 1 plugin dir, got %d", len(dm.pluginDirs))
	}
	if dm.plugins == nil {
		t.Error("plugins map should be initialized")
	}
	if dm.eventCh == nil {
		t.Error("eventCh should be initialized")
	}
}

func TestDynamicPluginManager_Get_NotFound(t *testing.T) {
	t.Parallel()
	dm := NewDynamicPluginManager(nil, nil, nil)

	dp, ok := dm.Get("nonexistent")
	if ok {
		t.Error("expected Get to return false for nonexistent plugin")
	}
	if dp != nil {
		t.Error("expected nil plugin")
	}
}

func TestDynamicPluginManager_Status_Empty(t *testing.T) {
	t.Parallel()
	dm := NewDynamicPluginManager(nil, nil, nil)

	statuses := dm.Status()
	if len(statuses) != 0 {
		t.Errorf("expected 0 statuses, got %d", len(statuses))
	}
}

func TestDynamicPluginManager_DiscoverAll_NonexistentDir(t *testing.T) {
	t.Parallel()
	dm := NewDynamicPluginManager([]string{"/nonexistent/path"}, nil, nil)

	// Should not error on nonexistent directory
	err := dm.DiscoverAll()
	if err != nil {
		t.Fatalf("DiscoverAll should not error on nonexistent dir: %v", err)
	}
}

func TestDynamicPluginManager_DiscoverAll_WithPlugin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create a plugin directory with a manifest
	pluginDir := filepath.Join(dir, "test-plugin")
	os.MkdirAll(pluginDir, 0o755)

	manifest := ManifestV2{
		Name:        "test-plugin",
		Version:     "1.0.0",
		Description: "A test plugin",
		Tools: []ManifestTool{
			{
				Name:        "echo",
				Description: "Echoes input",
				Command:     "echo",
			},
		},
	}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644)

	dm := NewDynamicPluginManager([]string{dir}, nil, nil)
	err := dm.DiscoverAll()
	if err != nil {
		t.Fatalf("DiscoverAll failed: %v", err)
	}

	dp, ok := dm.Get("test-plugin")
	if !ok {
		t.Fatal("expected to find test-plugin")
	}
	if dp.State != StateDiscovered {
		t.Errorf("expected state 'discovered', got %q", dp.State)
	}
	if dp.Plugin.Name != "test-plugin" {
		t.Errorf("expected plugin name 'test-plugin', got %q", dp.Plugin.Name)
	}
}

func TestDynamicPluginManager_Activate_NotFound(t *testing.T) {
	t.Parallel()
	dm := NewDynamicPluginManager(nil, nil, nil)

	err := dm.Activate("nonexistent")
	if err == nil {
		t.Error("expected error activating nonexistent plugin")
	}
}

func TestDynamicPluginManager_Deactivate_NotFound(t *testing.T) {
	t.Parallel()
	dm := NewDynamicPluginManager(nil, nil, nil)

	err := dm.Deactivate("nonexistent")
	if err == nil {
		t.Error("expected error deactivating nonexistent plugin")
	}
}

func TestDynamicPluginManager_Events(t *testing.T) {
	t.Parallel()
	dm := NewDynamicPluginManager(nil, nil, nil)

	ch := dm.Events()
	if ch == nil {
		t.Error("Events should return non-nil channel")
	}
}

func TestManifestV2ToPlugin(t *testing.T) {
	t.Parallel()
	m := &ManifestV2{
		Name:        "my-plugin",
		Version:     "2.0.0",
		Description: "A plugin",
		Author:      "test",
		Tools: []ManifestTool{
			{
				Name:           "tool1",
				Description:    "First tool",
				Command:        "echo",
				TimeoutSeconds: 10,
			},
			{
				Name:        "tool2",
				Description: "Second tool",
				Command:     "cat",
				Args:        []string{"-n"},
			},
		},
		Permissions: []string{"read", "write"},
	}

	p := manifestV2ToPlugin(m, "/path/to/plugin")

	if p.Name != "my-plugin" {
		t.Errorf("expected name 'my-plugin', got %q", p.Name)
	}
	if p.Version != "2.0.0" {
		t.Errorf("expected version '2.0.0', got %q", p.Version)
	}
	if p.Path != "/path/to/plugin" {
		t.Errorf("expected path '/path/to/plugin', got %q", p.Path)
	}
	if len(p.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(p.Tools))
	}

	// tool1
	if p.Tools[0].Name != "tool1" {
		t.Errorf("expected tool name 'tool1', got %q", p.Tools[0].Name)
	}
	if p.Tools[0].Timeout != 10*1000000000 { // 10 seconds in nanoseconds
		t.Errorf("expected timeout 10s, got %v", p.Tools[0].Timeout)
	}

	// tool2 - command should include args
	if p.Tools[1].Command != "cat -n" {
		t.Errorf("expected command 'cat -n', got %q", p.Tools[1].Command)
	}
}

func TestIsFullURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected bool
	}{
		{"https://github.com/user/repo.git", true},
		{"http://example.com/repo.git", true},
		{"user/repo", false},
		{"git@github.com:user/repo.git", false},
		{"", false},
	}

	for _, tt := range tests {
		got := isFullURL(tt.input)
		if got != tt.expected {
			t.Errorf("isFullURL(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestSplitCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected []string
	}{
		{"echo hello", []string{"echo", "hello"}},
		{
			"python -c print(1)", []string{"python", "-c", "print(1)"},
		},
		{"echo 'hello world'", []string{"echo", "hello world"}},
		{"python -c \"print('x y')\"", []string{"python", "-c", "print('x y')"}},
		{"echo hello\\ world", []string{"echo", "hello world"}},
		{"single", []string{"single"}},
		{"", nil},
		{"  spaced  out  ", []string{"spaced", "out"}},
	}

	for _, tt := range tests {
		got := splitCommand(tt.input)
		if len(got) != len(tt.expected) {
			t.Errorf("splitCommand(%q) = %v (len %d), want %v (len %d)", tt.input, got, len(got), tt.expected, len(tt.expected))
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("splitCommand(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestBytesReaderAt(t *testing.T) {
	t.Parallel()
	data := []byte("hello world")
	reader := &bytesReaderAt{data: data}

	buf := make([]byte, 5)
	n, err := reader.ReadAt(buf, 0)
	if err != nil {
		t.Fatalf("ReadAt failed: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5 bytes, got %d", n)
	}
	if string(buf) != "hello" {
		t.Errorf("expected 'hello', got %q", string(buf))
	}

	// Read at offset
	buf2 := make([]byte, 5)
	n2, err := reader.ReadAt(buf2, 6)
	if err != nil {
		t.Fatalf("ReadAt at offset failed: %v", err)
	}
	if n2 != 5 {
		t.Errorf("expected 5 bytes, got %d", n2)
	}
	if string(buf2) != "world" {
		t.Errorf("expected 'world', got %q", string(buf2))
	}

	// Read past end
	buf3 := make([]byte, 5)
	_, err = reader.ReadAt(buf3, 100)
	if err == nil {
		t.Error("expected EOF error when reading past end")
	}
}

func TestDynamicPluginManager_DiscoverAll_IgnoresFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create a file (not a directory) - should be ignored
	manifest := ManifestV2{Name: "file-plugin", Version: "1.0.0"}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(dir, "not-a-dir.json"), data, 0o644)

	dm := NewDynamicPluginManager([]string{dir}, nil, nil)
	err := dm.DiscoverAll()
	if err != nil {
		t.Fatalf("DiscoverAll failed: %v", err)
	}

	if len(dm.plugins) != 0 {
		t.Errorf("expected 0 plugins (files should be ignored), got %d", len(dm.plugins))
	}
}

func TestDynamicPluginManager_DiscoverAll_SkipsInvalidManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create a directory without a valid manifest
	pluginDir := filepath.Join(dir, "bad-plugin")
	os.MkdirAll(pluginDir, 0o755)
	os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte("invalid json"), 0o644)

	dm := NewDynamicPluginManager([]string{dir}, nil, nil)
	err := dm.DiscoverAll()
	if err != nil {
		t.Fatalf("DiscoverAll should not error on invalid manifest: %v", err)
	}

	if len(dm.plugins) != 0 {
		t.Errorf("expected 0 plugins (invalid manifest should be skipped), got %d", len(dm.plugins))
	}
}

func TestDynamicPluginManager_Activate_AlreadyActive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	pluginDir := filepath.Join(dir, "active-plugin")
	os.MkdirAll(pluginDir, 0o755)
	manifest := ManifestV2{Name: "active-plugin", Version: "1.0.0"}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644)

	dm := NewDynamicPluginManager([]string{dir}, nil, nil)
	dm.DiscoverAll()

	// Manually set to active
	dp, _ := dm.Get("active-plugin")
	dp.State = StateActive

	// Activating again should be a no-op
	err := dm.Activate("active-plugin")
	if err != nil {
		t.Fatalf("Activate on already-active plugin should not error: %v", err)
	}
}

func TestDynamicPluginManager_Deactivate_DisabledPlugin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	pluginDir := filepath.Join(dir, "disabled-plugin")
	os.MkdirAll(pluginDir, 0o755)
	manifest := ManifestV2{Name: "disabled-plugin", Version: "1.0.0"}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644)

	dm := NewDynamicPluginManager([]string{dir}, nil, nil)
	dm.DiscoverAll()

	dp, _ := dm.Get("disabled-plugin")
	dp.State = StateDisabled

	// Deactivating a disabled plugin should be a no-op
	err := dm.Deactivate("disabled-plugin")
	if err != nil {
		t.Fatalf("Deactivate on disabled plugin should not error: %v", err)
	}
}

func TestDynamicPluginManager_ExecuteTool_NotFound(t *testing.T) {
	t.Parallel()
	dm := NewDynamicPluginManager(nil, nil, nil)

	_, err := dm.ExecuteTool(context.TODO(), "nonexistent", "tool", nil)
	if err == nil {
		t.Error("expected error executing tool on nonexistent plugin")
	}
}

func TestDynamicPluginManager_ExecuteTool_NotActive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	pluginDir := filepath.Join(dir, "inactive-plugin")
	os.MkdirAll(pluginDir, 0o755)
	manifest := ManifestV2{
		Name:    "inactive-plugin",
		Version: "1.0.0",
		Tools:   []ManifestTool{{Name: "echo", Command: "echo"}},
	}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644)

	dm := NewDynamicPluginManager([]string{dir}, nil, nil)
	dm.DiscoverAll()

	// Plugin is in "discovered" state, not active
	_, err := dm.ExecuteTool(context.TODO(), "inactive-plugin", "echo", nil)
	if err == nil {
		t.Error("expected error executing tool on inactive plugin")
	}
}

func TestPluginToolAdapter(t *testing.T) {
	t.Parallel()
	dp := &DynamicPlugin{
		Plugin: Plugin{
			Name: "test-plugin",
		},
	}
	tool := PluginTool{
		Name:        "echo",
		Description: "Echo input",
		PluginName:  "test-plugin",
	}

	adapter := &PluginToolAdapter{
		plugin: dp,
		tool:   tool,
	}

	if adapter.Name() != "plugin__test-plugin__echo" {
		t.Errorf("expected name 'plugin__test-plugin__echo', got %q", adapter.Name())
	}
	if !adapter.Untrusted() {
		t.Fatal("plugin tools must be marked untrusted for permission evaluation")
	}
}

func TestPluginStatus(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	pluginDir := filepath.Join(dir, "status-plugin")
	os.MkdirAll(pluginDir, 0o755)
	manifest := ManifestV2{
		Name:    "status-plugin",
		Version: "2.0.0",
		Tools:   []ManifestTool{{Name: "tool1", Command: "echo"}},
		Hooks:   []ManifestHook{{Event: "pre_tool", Command: "hook-cmd"}},
	}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644)

	dm := NewDynamicPluginManager([]string{dir}, nil, nil)
	dm.DiscoverAll()

	statuses := dm.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}

	s := statuses[0]
	if s.Name != "status-plugin" {
		t.Errorf("expected name 'status-plugin', got %q", s.Name)
	}
	if s.Version != "2.0.0" {
		t.Errorf("expected version '2.0.0', got %q", s.Version)
	}
	if s.State != StateDiscovered {
		t.Errorf("expected state 'discovered', got %q", s.State)
	}
	if s.ToolCount != 1 {
		t.Errorf("expected 1 tool, got %d", s.ToolCount)
	}
	if s.HookCount != 1 {
		t.Errorf("expected 1 hook, got %d", s.HookCount)
	}
}
