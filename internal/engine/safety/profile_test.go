package safety

import "testing"

func TestProfileFromLevel_Defaults(t *testing.T) {
	p := ProfileFromLevel(AutonomySemi)
	if !p.AutoContinue || !p.AutoApplyEdits || p.AutoExecuteBash || !p.AutoNetwork {
		t.Fatalf("Semi defaults wrong: %#v", p)
	}
	p = ProfileFromLevel(AutonomyFull)
	if !p.AutoExecuteBash {
		t.Fatal("Full should auto-execute bash")
	}
	p = ProfileFromLevel(AutonomySupervised)
	if p.AutoContinue || p.AutoApplyEdits || p.AutoExecuteBash || p.AutoNetwork {
		t.Fatal("Supervised should auto-nothing")
	}
}

func TestProfile_Override(t *testing.T) {
	p := ProfileFromLevel(AutonomyFull)
	// Full auto-executes bash by default; override it off.
	if !p.Override("auto_execute_bash", false) {
		t.Fatal("Override should succeed for known flag")
	}
	if p.AutoExecuteBash {
		t.Fatal("auto_execute_bash should now be false")
	}
	// Unknown flag rejected.
	if p.Override("nonexistent_flag", true) {
		t.Fatal("Override should reject unknown flag")
	}
}

func TestProfile_NeedsPermission(t *testing.T) {
	// Full with bash override off should ask for bash.
	p := ProfileFromLevel(AutonomyFull)
	p.Override("auto_execute_bash", false)
	if !p.NeedsPermission("Bash", true) {
		t.Fatal("Full with bash override off should ask for bash")
	}
	// Safe bash at Full (no override) should not ask.
	p2 := ProfileFromLevel(AutonomyFull)
	if p2.NeedsPermission("Bash", true) {
		t.Fatal("Full should not ask for safe bash")
	}
	// Destructive bash at Full should always ask.
	if !p2.NeedsPermission("Bash", false) {
		t.Fatal("Full should ask for unsafe bash")
	}
	// YOLO never asks.
	yolo := ProfileFromLevel(AutonomyYOLO)
	if yolo.NeedsPermission("Bash", false) {
		t.Fatal("YOLO should never ask")
	}
}

func TestProfile_NetworkOverride(t *testing.T) {
	// Full with network override off should ask for WebFetch.
	p := ProfileFromLevel(AutonomyFull)
	p.Override("auto_network", false)
	if !p.NeedsPermission("WebFetch", true) {
		t.Fatal("Full with network override off should ask for WebFetch")
	}
	// Full default should not ask.
	p2 := ProfileFromLevel(AutonomyFull)
	if p2.NeedsPermission("WebFetch", true) {
		t.Fatal("Full default should not ask for WebFetch")
	}
}

func TestProfile_OverridesRoundtrip(t *testing.T) {
	p := ProfileFromLevel(AutonomySemi)
	p.Override("auto_execute_bash", true)
	p.Override("auto_network", false)
	got := p.Overrides()
	if !got["autoexecutebash"] {
		t.Fatal("auto_execute_bash should be overridden to true")
	}
	if p.AutoNetwork {
		t.Fatal("AutoNetwork should be false after override")
	}
	if !p.IsOverridden("auto_network") {
		t.Fatal("auto_network should be marked overridden")
	}
}

func TestProfile_CloneIsIndependent(t *testing.T) {
	original := ProfileFromLevel(AutonomyFull)
	original.Override("auto_network", false)
	clone := original.Clone()
	if clone == nil || clone.Level != original.Level || clone.AutoNetwork != original.AutoNetwork {
		t.Fatalf("clone did not preserve profile state: original=%#v clone=%#v", original, clone)
	}
	clone.Override("auto_network", true)
	if original.AutoNetwork {
		t.Fatal("mutating a cloned profile changed the live profile")
	}
}
