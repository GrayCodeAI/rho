package commands

import "testing"

func TestBuiltInCatalogIncludesCoreCommands(t *testing.T) {
	names := BuiltInNames()
	want := map[string]bool{"/help": false, "/resume": false, "/review": false, "/status": false}
	for _, name := range names {
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("BuiltInNames missing %q", name)
		}
	}
}

func TestBuiltInCatalogHasAliasAndDescription(t *testing.T) {
	if got := BuiltInAliases()["/themes"]; got != "/theme" {
		t.Fatalf("/themes alias = %q", got)
	}
	if got := BuiltInDescriptions()["/help"]; got == "" {
		t.Fatal("/help description is empty")
	}
}

func TestBuiltInCatalogIsConsistent(t *testing.T) {
	if err := ValidateBuiltIns(); err != nil {
		t.Fatal(err)
	}
}

func TestBuiltInCategoriesHaveStableFallback(t *testing.T) {
	if got := Category("/help"); got != "Core" {
		t.Fatalf("Category(/help) = %q, want Core", got)
	}
	if got := Category("/plugin-command"); got != "Other" {
		t.Fatalf("Category(plugin command) = %q, want Other", got)
	}
	if got := Category("/themes"); got != "Settings" {
		t.Fatalf("Category(/themes) = %q, want Settings", got)
	}
}
