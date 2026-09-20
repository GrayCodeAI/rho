package plugin

import "testing"

func TestRuntimeCommandListIsDeterministicAndDetached(t *testing.T) {
	runtime := NewRuntime()
	runtime.commands["zeta"] = CommandDef{Name: "zeta", Description: "last"}
	runtime.commands["alpha"] = CommandDef{Name: "alpha", Description: "first"}

	commands := runtime.CommandList()
	if len(commands) != 2 || commands[0].Name != "alpha" || commands[1].Name != "zeta" {
		t.Fatalf("CommandList() = %#v, want deterministic name order", commands)
	}

	commands[0].Name = "mutated"
	if got := runtime.CommandList()[0].Name; got != "alpha" {
		t.Fatalf("CommandList() returned shared command state: %q", got)
	}
}

func TestRuntimeRebuildIndexesRemovesStalePluginState(t *testing.T) {
	runtime := NewRuntime()
	if err := runtime.rebuildIndexes([]*Manifest{{
		Name:     "old",
		Version:  "1",
		Commands: []CommandDef{{Name: "old-command"}},
		Hooks:    []HookDef{{Event: "beforeReview", Command: "old"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if !runtime.IsCommand("old-command") || len(runtime.hooks["beforeReview"]) != 1 {
		t.Fatal("initial plugin indexes were not built")
	}

	if err := runtime.rebuildIndexes([]*Manifest{{Name: "new", Version: "1"}}); err != nil {
		t.Fatal(err)
	}
	if runtime.IsCommand("old-command") {
		t.Fatal("stale plugin command survived index rebuild")
	}
	if len(runtime.hooks) != 0 {
		t.Fatalf("stale plugin hooks survived index rebuild: %#v", runtime.hooks)
	}
}

func TestRuntimeRebuildIndexesRejectsInvalidAndDuplicateCommands(t *testing.T) {
	cases := map[string][]*Manifest{
		"invalid name": {{Name: "plugin", Commands: []CommandDef{{Name: "bad name"}}}},
		"duplicate name": {
			{Name: "one", Commands: []CommandDef{{Name: "shared"}}},
			{Name: "two", Commands: []CommandDef{{Name: "shared"}}},
		},
	}
	for name, manifests := range cases {
		t.Run(name, func(t *testing.T) {
			runtime := NewRuntime()
			if err := runtime.rebuildIndexes(manifests); err == nil {
				t.Fatal("rebuildIndexes() unexpectedly accepted invalid command set")
			}
		})
	}
}
