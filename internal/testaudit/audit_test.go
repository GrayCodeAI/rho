package testaudit

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// repoRoot returns the rho repo root directory.
// It walks up from the test file's location to find go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot determine working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod)")
		}
		dir = parent
	}
}

// Exempt packages for fmt.Print audit — these use stdout for user-facing output.
var fmtPrintExemptions = map[string]bool{
	"onboarding": true,
	"scaffold":   true,
}

// Exempt packages for os.Getenv audit — these are the abstraction layer,
// standard telemetry, or use standard env vars (HOME, SHELL, GOPATH, etc.)
// that don't benefit from EnvManager indirection.
var getEnvExemptions = map[string]bool{
	"envmanager.go":              true, // the abstraction layer itself
	"oteltrace":                  true, // OTEL standard env vars
	"langfuse":                   true, // Langfuse standard env vars
	"config/catalog_":            true, // config package is the abstraction layer
	"config/deployment":          true, // config package
	"config/deployments_ui":      true, // config package
	"auth/auth.go":               true, // HOME for token file path
	"terminal_context.go":        true, // TMUX, STY, TERM_PROGRAM for terminal detection
	"prompts/loader.go":          true, // SHELL for prompt context
	"health/diagnostics.go":      true, // SHELL, RHO_MODEL for health checks
	"tool/safety.go":             true, // RHO_CONFIG_DIR for security checks
	"tool/treesitter.go":         true, // HOME for grammar dir (uses os.UserHomeDir)
	"tool/web_search_brave.go":   true, // BRAVE_SEARCH_API_KEY
	"tool/web_search_searxng.go": true, // SEARXNG_URL
}

// TestNoRawPanicInInternal verifies that no non-test .go file in internal/
// calls panic() outside of init() functions.
func TestNoRawPanicInInternal(t *testing.T) {
	root := repoRoot(t)
	internalDir := filepath.Join(root, "internal")
	files := parseInternalPackages(t, internalDir)

	for _, pf := range files {
		pf := pf
		rel := relPath(root, pf.Path)

		ast.Inspect(pf.File, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			name := callExprName(call)
			if name != "panic" {
				return true
			}

			// Allow panic inside init() functions
			if isInsideInit(pf.File, call.Pos(), pf.FSet) {
				return true
			}

			// Allow panic in catalogtest (test infrastructure)
			if strings.Contains(pf.Path, "catalogtest") {
				return true
			}

			// Allow panic inside Must* functions (Go convention: MustCompile, MustPut, etc.)
			if isInsideMustFunction(pf.File, call.Pos()) {
				return true
			}

			pos := pf.FSet.Position(call.Pos())
			t.Run(fmt.Sprintf("%s:%d", rel, pos.Line), func(t *testing.T) {
				t.Logf("TECH DEBT: raw panic() at %s:%d — should return error instead", rel, pos.Line)
			})

			return true
		})
	}
}

// TestNoRawFmtPrintInInternal verifies that no non-test .go file in internal/
// uses fmt.Print/Println/Printf (should use logger.Logger instead).
func TestNoRawFmtPrintInInternal(t *testing.T) {
	root := repoRoot(t)
	internalDir := filepath.Join(root, "internal")
	files := parseInternalPackages(t, internalDir)

	for _, pf := range files {
		rel := relPath(root, pf.Path)

		// Skip exempt packages
		if isExemptPackage(pf.Path, fmtPrintExemptions) {
			continue
		}

		ast.Inspect(pf.File, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			name := callExprName(call)
			if name != "fmt.Print" && name != "fmt.Println" && name != "fmt.Printf" {
				return true
			}

			pos := pf.FSet.Position(call.Pos())
			t.Run(fmt.Sprintf("%s:%d", rel, pos.Line), func(t *testing.T) {
				t.Logf("TECH DEBT: %s() at %s:%d — use logger.Logger instead", name, rel, pos.Line)
			})

			return true
		})
	}
}

// TestNoDirectOsGetenvInInternal verifies that no non-test .go file in internal/
// uses os.Getenv directly (should go through config.EnvManager).
// Currently logs violations as tech debt rather than failing CI.
func TestNoDirectOsGetenvInInternal(t *testing.T) {
	root := repoRoot(t)
	internalDir := filepath.Join(root, "internal")
	files := parseInternalPackages(t, internalDir)

	violationCount := 0

	for _, pf := range files {
		rel := relPath(root, pf.Path)

		// Skip exempt packages
		if isExemptPackage(pf.Path, getEnvExemptions) {
			continue
		}

		ast.Inspect(pf.File, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			name := callExprName(call)
			if name != "os.Getenv" {
				return true
			}

			pos := pf.FSet.Position(call.Pos())
			t.Run(fmt.Sprintf("%s:%d", rel, pos.Line), func(t *testing.T) {
				t.Logf("TECH DEBT: os.Getenv() at %s:%d — migrate to config.EnvManager", rel, pos.Line)
			})

			violationCount++
			return true
		})
	}

	t.Logf("Total os.Getenv violations in internal/: %d (logged as tech debt)", violationCount)
}

// TestNoDirectLowerFluxImports verifies production Rho code uses only
// Flux's stable engine facade. Tests may import lower packages for fixtures.
func TestNoDirectLowerFluxImports(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "internal"),
		filepath.Join(root, "cmd"),
	}

	for _, dir := range paths {
		files := parseGoFiles(t, dir)
		for _, pf := range files {
			rel := relPath(root, pf.Path)
			if strings.HasSuffix(rel, "_test.go") {
				continue
			}
			for _, imp := range pf.File.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if !strings.HasPrefix(path, "github.com/GrayCodeAI/flux/") ||
					path == "github.com/GrayCodeAI/flux/engine" ||
					strings.HasPrefix(path, "github.com/GrayCodeAI/flux/engine/") {
					continue
				}
				// Rho uses the full vendored Flux API surface for provider,
				// graph, and tooling contracts that the engine facade does not
				// re-export.
				switch path {
				case "github.com/GrayCodeAI/flux/llm",
					"github.com/GrayCodeAI/flux/graph",
					"github.com/GrayCodeAI/flux/tools":
					continue
				}
				pos := pf.FSet.Position(imp.Pos())
				t.Fatalf("forbidden lower-level Flux import %q at %s:%d; use github.com/GrayCodeAI/flux/engine", path, rel, pos.Line)
			}
		}
	}
}

// TestNoLazyProviderConstructionInRho verifies Rho does not construct lazy
// provider transports directly. Provider/model transport resolution belongs in Flux.
func TestNoLazyProviderConstructionInRho(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "internal"),
		filepath.Join(root, "cmd"),
	}

	for _, dir := range paths {
		files := parseGoFiles(t, dir)
		for _, pf := range files {
			rel := relPath(root, pf.Path)
			if strings.HasSuffix(rel, "_test.go") {
				continue
			}
			ast.Inspect(pf.File, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "NewLazyProvider" {
					return true
				}
				pos := pf.FSet.Position(call.Pos())
				t.Fatalf("forbidden flux lazy provider construction at %s:%d; use the flux/engine facade", rel, pos.Line)
				return true
			})
		}
	}
}

// TestAllExportedTypesHaveDocComments verifies that all exported type
// declarations in non-test .go files have doc comments.
func TestAllExportedTypesHaveDocComments(t *testing.T) {
	root := repoRoot(t)
	internalDir := filepath.Join(root, "internal")
	files := parseInternalPackages(t, internalDir)

	for _, pf := range files {
		rel := relPath(root, pf.Path)

		for _, decl := range pf.File.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}

			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}

				// Skip unexported types
				if !typeSpec.Name.IsExported() {
					continue
				}

				// Check for doc comment on the GenDecl (covers the type)
				if genDecl.Doc != nil && genDecl.Doc.Text() != "" {
					continue
				}

				pos := pf.FSet.Position(typeSpec.Pos())
				t.Run(fmt.Sprintf("%s/%s", rel, typeSpec.Name.Name), func(t *testing.T) {
					t.Logf("TECH DEBT: exported type %s at %s:%d has no doc comment",
						typeSpec.Name.Name, rel, pos.Line)
				})
			}
		}
	}
}

// legacySessionFields is the canonical list of Session fields marked
// Deprecated in internal/engine/session.go. The H6 god-object
// decomposition extracted these into 6 sub-services (LLM, Perms,
// Life, Memory, Persistence, Tools). New code should go through
// the sub-services; this audit tracks the migration progress.
//
// The list is hand-curated from the Deprecated: comments on the
// Session struct. If a field is added or removed there, update this
// list.
var legacySessionFields = []string{
	"Permissions", "Classifier", "BypassKill", "Mode",
	"MaxTurns", "MaxBudgetUSD", "AllowedDirs", "PermissionFn",
	"Memory", "HarrierBridge", "EnhancedMemory",
	"Cascade", "Lifecycle", "Reflector", "CostTracker",
	"Autonomy", "Plan", "Beliefs", "Critic", "Backtrack",
	"Limits", "Trajectory", "Shadow", "Snapshots", "ConversationGraph",
	"Sleeptime", "Activity", "SkillDistiller", "Tracer",
	"LintLoop", "TestLoop", "FileMentions", "ResponseCache",
	"Pipeline", "Files", "Steering", "RateLimiter", "AgentsAccum",
	"FewShotStore", "AdaptivePrompt", "OutputSchema", "Approval",
	"SettingsGet", "SettingsSet", "AgentSpawnFn", "AskUserFn",
	"Verbose", "GLMThinkingEnabled", "PinnedMessages",
	"AutoCompactThresholdPct", "ContextWindowCached",
	"AutoCompactor", "persistID", "lastPromptTokens",
	"lastCompletionTokens", "checkpointMgr", "OnCompaction",
	"Router", "provider", "model", "system",
	"Cost",
	"DeploymentRouting",
}

// TestSessionLegacyFieldAccessAudit counts how many call sites still
// access the legacy Session fields directly (e.g. `s.Memory`,
// `s.Permissions`). These are the migration targets for the H6
// god-object decomposition: new code should go through
// `s.SubServices().X().Y()` instead.
//
// The rule is currently soft-fail (tech-debt log) so that
// in-progress migrations don't break CI. To hard-fail once
// migration is complete, change t.Logf to t.Errorf in this test.
//
// Audited directories: cmd/, internal/daemon/, internal/engine/,
// internal/multiagent/, internal/session/, internal/snapshot/.
// The audit is per-file: each access counts as one (no de-dup of
// the same field in the same file).
func TestSessionLegacyFieldAccessAudit(t *testing.T) {
	root := repoRoot(t)
	dirs := []string{
		filepath.Join(root, "cmd"),
		filepath.Join(root, "internal", "daemon"),
		filepath.Join(root, "internal", "engine"),
		filepath.Join(root, "internal", "multiagent"),
		filepath.Join(root, "internal", "session"),
		filepath.Join(root, "internal", "snapshot"),
	}

	// Build a single regex matching any of the legacy fields as
	// a `s.<Field>` access. Whitespace between the dot and the
	// field name is allowed (rare but legal). We deliberately
	// exclude `s.SubServices()` and `s.Services()` so the new
	// access paths don't get counted.
	quoted := make([]string, len(legacySessionFields))
	for i, f := range legacySessionFields {
		quoted[i] = regexp.QuoteMeta(f)
	}
	// Match `s.Field`, `sess.Field`, or `m.session.Field` as a bare
	// token. We then post-filter to exclude method calls (Field
	// followed by `(`), which are not legacy access — they're the
	// proper way to interact with the field via its getter/setter
	// methods.
	fieldPattern := regexp.MustCompile(`\b(?:s|sess|m\.session)\.\s*(?:` + strings.Join(quoted, "|") + `)\b`)

	total := 0
	perFile := map[string]int{}
	for _, dir := range dirs {
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			// Skip the deprecation source file itself.
			if strings.HasSuffix(path, "session.go") && strings.Contains(path, "internal/engine/session.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			text := string(data)
			matches := fieldPattern.FindAllString(text, -1)
			filtered := matches[:0]
			rest := text
			for range matches {
				loc := fieldPattern.FindStringIndex(rest)
				if loc == nil {
					break
				}
				// Check what follows the match; if `(`, it's a
				// method call, skip.
				after := rest[loc[1]:]
				trimmed := strings.TrimLeft(after, " \t")
				if !strings.HasPrefix(trimmed, "(") {
					filtered = append(filtered, "x")
				}
				rest = rest[loc[1]:]
			}
			if len(filtered) > 0 {
				perFile[rel] = len(filtered)
				total += len(filtered)
			}
			return nil
		})
	}

	if total == 0 {
		t.Log("H6 MIGRATION COMPLETE: no legacy Session field access in cmd/ or internal/.")
		return
	}

	// Hard-fail threshold for cmd/ — once the cmd/ sub-PR is done,
	// any new legacy access in cmd/ is a regression. The internal/
	// sub-PRs are still in progress (largest backlog is in
	// internal/engine/stream.go with ~120 sites), so internal/
	// remains soft-fail until those sub-PRs land.
	//
	// M1 (2026-06-17): audit now matches `m.session.X` (was `s.X` /
	// `sess.X` only — a blind spot that missed all chatModel access).
	// Threshold lowered to 0; remaining false-positives are
	// method calls (Field followed by `(`) that the post-filter
	// already strips out.
	const cmdHardFailThreshold = 0
	var cmdLegacy int
	for f, n := range perFile {
		if strings.HasPrefix(f, "cmd/") {
			cmdLegacy += n
		}
	}
	if cmdLegacy > cmdHardFailThreshold {
		t.Errorf("H6 cmd/ REGRESSION: %d legacy Session field accesses in cmd/ (was %d, should not increase). The cmd/ sub-PR is done; do not add new legacy access in cmd/.", cmdLegacy, cmdHardFailThreshold)
	}

	// Sort for deterministic output.
	sorted := make([]string, 0, len(perFile))
	for f := range perFile {
		sorted = append(sorted, f)
	}
	sort.Strings(sorted)

	t.Logf("H6 MIGRATION (soft-fail): %d legacy Session field accesses across %d files. New code should use s.SubServices().X().Y() instead.",
		total, len(perFile))
	for _, f := range sorted {
		t.Logf("  %s: %d", f, perFile[f])
	}
}
