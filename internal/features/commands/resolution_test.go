package commands

import "testing"

func TestResolveAppliesAliasAndCanonicalName(t *testing.T) {
	resolution := Resolve(Parse("/themes"), map[string]string{"/themes": "/theme"})
	if resolution.Command != "/theme" || resolution.Name != "theme" || !resolution.IsSlash || resolution.Namespaced {
		t.Fatalf("unexpected resolution: %#v", resolution)
	}
}

func TestResolveDetectsNamespacedCommands(t *testing.T) {
	resolution := Resolve(Parse("/vendor:review arg"), nil)
	if resolution.Name != "vendor:review" || !resolution.Namespaced {
		t.Fatalf("unexpected namespaced resolution: %#v", resolution)
	}
}

func TestResolveLeavesNaturalLanguageUnchanged(t *testing.T) {
	resolution := Resolve(Parse("build this"), nil)
	if resolution.Command != "build" || resolution.IsSlash || resolution.Namespaced {
		t.Fatalf("unexpected natural-language resolution: %#v", resolution)
	}
}
