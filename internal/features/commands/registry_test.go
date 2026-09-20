package commands

import "testing"

func TestRegistryAliasesAndDeterministicEnumeration(t *testing.T) {
	r := NewRegistry()
	if !r.Register(Spec{Name: "z", Aliases: []string{"last"}}) || !r.Register(Spec{Name: "a"}) {
		t.Fatal("initial registration failed")
	}
	if got, ok := r.Lookup("last"); !ok || got.Name != "z" {
		t.Fatalf("alias lookup = %#v, %v", got, ok)
	}
	if names := r.Names(); len(names) != 2 || names[0] != "a" || names[1] != "z" {
		t.Fatalf("names = %#v", names)
	}
}

func TestRegistryRejectsCollisionsAndCopiesMetadata(t *testing.T) {
	r := NewRegistry()
	aliases := []string{"quit"}
	if !r.Register(Spec{Name: "exit", Aliases: aliases}) {
		t.Fatal("registration failed")
	}
	aliases[0] = "changed"
	if r.Register(Spec{Name: "quit"}) || r.Register(Spec{Name: "other", Aliases: []string{"quit"}}) {
		t.Fatal("collision registration succeeded")
	}
	spec, _ := r.Lookup("exit")
	if len(spec.Aliases) != 1 || spec.Aliases[0] != "quit" {
		t.Fatalf("metadata was not copied: %#v", spec)
	}
}

func TestRegistryRejectsMalformedNamesAndAliasCollisions(t *testing.T) {
	r := NewRegistry()
	for _, spec := range []Spec{
		{Name: ""},
		{Name: "/slash"},
		{Name: "has space"},
		{Name: "main", Aliases: []string{"main"}},
		{Name: "duplicate", Aliases: []string{"alt", "alt"}},
	} {
		if r.Register(spec) {
			t.Errorf("Register(%+v) = true, want false", spec)
		}
	}
	if r.Size() != 0 {
		t.Fatalf("registry accepted malformed entries; size = %d", r.Size())
	}
}
