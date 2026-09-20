package fingerprint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCountNPMDeps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pkgJSON := `{"dependencies":{"react":"^18","express":"^4"},"devDependencies":{"jest":"^29"}}`
	_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644)
	count := countNPMDeps(filepath.Join(dir, "package.json"))
	if count != 3 {
		t.Errorf("countNPMDeps = %d, want 3", count)
	}
}

func TestCountNPMDeps_Missing(t *testing.T) {
	t.Parallel()
	count := countNPMDeps("/nonexistent/package.json")
	if count != 0 {
		t.Errorf("missing file should return 0, got %d", count)
	}
}

func TestCountCargoDeps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cargo := "[dependencies]\nserde = \"1.0\"\ntokio = \"1\"\n\n[dev-dependencies]\ncriterion = \"0.5\"\n"
	_ = os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(cargo), 0o644)
	count := countCargoDeps(filepath.Join(dir, "Cargo.toml"))
	if count < 2 {
		t.Errorf("countCargoDeps = %d, want >= 2", count)
	}
}

func TestCountLineBasedDeps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reqs := "flask==2.0\nrequests>=2.28\nnumpy\n# comment\n\n"
	_ = os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte(reqs), 0o644)
	count := countLineBasedDeps(filepath.Join(dir, "requirements.txt"))
	if count != 3 {
		t.Errorf("countLineBasedDeps = %d, want 3", count)
	}
}

func TestCountGemfileDeps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gemfile := "source 'https://rubygems.org'\ngem 'rails'\ngem 'puma'\ngem 'sidekiq'\n"
	_ = os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(gemfile), 0o644)
	count := countGemfileDeps(filepath.Join(dir, "Gemfile"))
	if count != 3 {
		t.Errorf("countGemfileDeps = %d, want 3", count)
	}
}
