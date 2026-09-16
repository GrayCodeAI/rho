package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// PRDescription holds all the components of a generated pull request description.
type PRDescription struct {
	Title     string
	Body      string
	Labels    []string
	Reviewers []string
	Type      string // "feat", "fix", "refactor", "docs", "chore"
	Breaking  bool
	TestPlan  string
}

// CommitSummary represents a parsed commit from git log output.
type CommitSummary struct {
	Hash    string
	Message string
	Type    string
	Scope   string
	Files   []string
	Author  string
}

// PRGenerator generates pull request descriptions from commit history and diffs.
type PRGenerator struct {
	ProjectDir string
	mu         sync.Mutex
}

// NewPRGenerator creates a new PRGenerator for the given project directory.
func NewPRGenerator(projectDir string) *PRGenerator {
	return &PRGenerator{
		ProjectDir: projectDir,
	}
}

// Generate produces a PRDescription by analyzing commits since baseBranch.
func (g *PRGenerator) Generate(baseBranch string) (*PRDescription, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Get git log since baseBranch.
	gitLog, err := g.runGit("log", baseBranch+"..HEAD", "--pretty=format:%H|%an|%s", "--no-merges")
	if err != nil {
		return nil, fmt.Errorf("failed to get git log: %w", err)
	}
	if strings.TrimSpace(gitLog) == "" {
		return nil, fmt.Errorf("no commits found between %s and HEAD", baseBranch)
	}

	// Get diff stat.
	diffStat, err := g.runGit("diff", baseBranch+"...HEAD", "--stat")
	if err != nil {
		return nil, fmt.Errorf("failed to get diff stat: %w", err)
	}

	// Get list of changed files.
	filesOutput, err := g.runGit("diff", baseBranch+"...HEAD", "--name-only")
	if err != nil {
		return nil, fmt.Errorf("failed to get changed files: %w", err)
	}

	commits := ParseCommits(gitLog)

	// Attach files to commits by checking each commit individually.
	for i := range commits {
		commitFiles, ferr := g.runGit("diff-tree", "--no-commit-id", "--name-only", "-r", commits[i].Hash)
		if ferr == nil && strings.TrimSpace(commitFiles) != "" {
			commits[i].Files = splitNonEmpty(commitFiles)
		}
	}

	allFiles := splitNonEmpty(filesOutput)

	title := GenerateTitle(commits)
	body := GeneratePRBody(commits, diffStat, allFiles)
	labels := SuggestLabels(commits)
	testPlan := GenerateTestPlan(commits, allFiles)
	prType := detectPRType(commits)
	breaking := detectBreaking(commits)

	// Try to get git blame for reviewer suggestions.
	gitBlame := make(map[string]string)
	for _, f := range allFiles {
		blameOutput, berr := g.runGit("blame", "--porcelain", "-L", "1,5", baseBranch+"--", f)
		if berr == nil {
			for _, line := range strings.Split(blameOutput, "\n") {
				if strings.HasPrefix(line, "author ") {
					author := strings.TrimPrefix(line, "author ")
					if author != "" {
						gitBlame[f] = author
					}
					break
				}
			}
		}
	}
	reviewers := SuggestReviewers(allFiles, gitBlame)

	return &PRDescription{
		Title:     title,
		Body:      body,
		Labels:    labels,
		Reviewers: reviewers,
		Type:      prType,
		Breaking:  breaking,
		TestPlan:  testPlan,
	}, nil
}

// GenerateTitle creates a PR title from the commit summaries.
func GenerateTitle(commits []CommitSummary) string {
	if len(commits) == 0 {
		return "Update"
	}

	if len(commits) == 1 {
		title := commits[0].Message
		if len(title) > 72 {
			// Rune-safe truncation: never split a multibyte UTF-8 sequence.
			if runes := []rune(title); len(runes) > 72 {
				title = string(runes[:69]) + "..."
			}
		}
		return title
	}

	// Check if all commits have the same type.
	firstType := commits[0].Type
	allSameType := true
	scopes := make(map[string]bool)
	for _, c := range commits {
		if c.Type != firstType {
			allSameType = false
		}
		if c.Scope != "" {
			scopes[c.Scope] = true
		}
	}

	if allSameType && firstType != "" {
		scope := ""
		if len(scopes) == 1 {
			for s := range scopes {
				scope = s
			}
		}

		summary := summarizeCommits(commits)
		var title string
		if scope != "" {
			title = fmt.Sprintf("%s(%s): %s", firstType, scope, summary)
		} else {
			title = fmt.Sprintf("%s: %s", firstType, summary)
		}
		if len(title) > 72 {
			// Rune-safe truncation: never split a multibyte UTF-8 sequence.
			if runes := []rune(title); len(runes) > 72 {
				title = string(runes[:69]) + "..."
			}
		}
		return title
	}

	// Mixed types.
	types := make(map[string]bool)
	for _, c := range commits {
		if c.Type != "" {
			types[c.Type] = true
		}
	}

	typeList := make([]string, 0, len(types))
	for t := range types {
		typeList = append(typeList, t)
	}
	sort.Strings(typeList)

	title := "Multiple changes: " + strings.Join(typeList, ", ")
	if len(title) > 72 {
		// Rune-safe truncation: never split a multibyte UTF-8 sequence.
		if runes := []rune(title); len(runes) > 72 {
			title = string(runes[:69]) + "..."
		}
	}
	return title
}

// GeneratePRBody creates a detailed PR body with sections.
func GeneratePRBody(commits []CommitSummary, diffStat string, files []string) string {
	var sb strings.Builder

	// Summary section.
	sb.WriteString("## Summary\n")
	sb.WriteString(generateSummary(commits))
	sb.WriteString("\n\n")

	// Changes section.
	sb.WriteString("## Changes\n")
	for _, c := range commits {
		if c.Type != "" && c.Scope != "" {
			sb.WriteString(fmt.Sprintf("- %s(%s): %s\n", c.Type, c.Scope, stripConventionalPrefix(c.Message)))
		} else if c.Type != "" {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", c.Type, stripConventionalPrefix(c.Message)))
		} else {
			sb.WriteString(fmt.Sprintf("- %s\n", c.Message))
		}
	}
	sb.WriteString("\n")

	// Files Changed section.
	sb.WriteString("## Files Changed\n")
	if diffStat != "" {
		// Extract the summary line from diff stat (last line usually).
		statLines := strings.Split(strings.TrimSpace(diffStat), "\n")
		if len(statLines) > 0 {
			sb.WriteString(statLines[len(statLines)-1])
			sb.WriteString("\n")
		}
	}
	if len(files) > 0 {
		sb.WriteString("\nKey files:\n")
		shown := files
		if len(shown) > 10 {
			shown = shown[:10]
		}
		for _, f := range shown {
			desc := describeFile(f)
			sb.WriteString(fmt.Sprintf("- `%s`", f))
			if desc != "" {
				sb.WriteString(fmt.Sprintf(" — %s", desc))
			}
			sb.WriteString("\n")
		}
		if len(files) > 10 {
			sb.WriteString(fmt.Sprintf("- ... and %d more files\n", len(files)-10))
		}
	}
	sb.WriteString("\n")

	// Test Plan section.
	testPlan := GenerateTestPlan(commits, files)
	sb.WriteString("## Test Plan\n")
	sb.WriteString(testPlan)
	sb.WriteString("\n\n")

	// Breaking Changes section.
	sb.WriteString("## Breaking Changes\n")
	if detectBreaking(commits) {
		breakingChanges := collectBreakingChanges(commits)
		for _, bc := range breakingChanges {
			sb.WriteString(fmt.Sprintf("- %s\n", bc))
		}
	} else {
		sb.WriteString("None\n")
	}

	return sb.String()
}

// SuggestLabels suggests PR labels based on commit types.
func SuggestLabels(commits []CommitSummary) []string {
	labels := make(map[string]bool)

	for _, c := range commits {
		switch c.Type {
		case "feat":
			labels["feature"] = true
		case "fix":
			labels["bugfix"] = true
		case "docs":
			labels["documentation"] = true
		case "chore":
			// Check if it's a dependency update.
			if strings.Contains(c.Message, "dep") || strings.Contains(c.Message, "go.mod") ||
				strings.Contains(c.Message, "package.json") {
				labels["dependencies"] = true
			}
		}

		// Check for breaking changes.
		if strings.Contains(c.Message, "BREAKING") || strings.Contains(c.Message, "!:") {
			labels["breaking"] = true
		}

		// Check file paths for dependency indicators.
		for _, f := range c.Files {
			base := filepath.Base(f)
			if base == "go.mod" || base == "go.sum" || base == "package.json" || base == "package-lock.json" {
				labels["dependencies"] = true
			}
		}
	}

	result := make([]string, 0, len(labels))
	for l := range labels {
		result = append(result, l)
	}
	sort.Strings(result)
	return result
}

// SuggestReviewers suggests reviewers based on git blame data for changed files.
func SuggestReviewers(files []string, gitBlame map[string]string) []string {
	if len(gitBlame) == 0 {
		return nil
	}

	// Count how many files each author owns.
	authorCount := make(map[string]int)
	for _, f := range files {
		if author, ok := gitBlame[f]; ok {
			authorCount[author]++
		}
	}

	if len(authorCount) == 0 {
		return nil
	}

	// Sort by count descending.
	type authorEntry struct {
		name  string
		count int
	}
	entries := make([]authorEntry, 0, len(authorCount))
	for name, count := range authorCount {
		entries = append(entries, authorEntry{name, count})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].count > entries[j].count
	})

	// Return top 3 reviewers.
	var reviewers []string
	for i, e := range entries {
		if i >= 3 {
			break
		}
		reviewers = append(reviewers, e.name)
	}
	return reviewers
}

// ParseCommits parses git log output into CommitSummary slices.
// Expected format per line: hash|author|message
func ParseCommits(gitLog string) []CommitSummary {
	lines := splitNonEmpty(gitLog)
	commits := make([]CommitSummary, 0, len(lines))

	for _, line := range lines {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}

		hash := parts[0]
		author := parts[1]
		message := parts[2]

		commitType, scope := parseConventionalCommit(message)

		commits = append(commits, CommitSummary{
			Hash:    hash,
			Author:  author,
			Message: message,
			Type:    commitType,
			Scope:   scope,
		})
	}

	return commits
}

// GenerateTestPlan creates a test plan based on the commits and files changed.
func GenerateTestPlan(commits []CommitSummary, files []string) string {
	var items []string

	// Check if there are Go files — suggest running tests.
	hasGo := false
	hasJS := false
	hasPython := false
	hasAPI := false
	hasConfig := false
	hasDB := false

	for _, f := range files {
		ext := filepath.Ext(f)
		switch ext {
		case ".go":
			hasGo = true
		case ".js", ".ts", ".jsx", ".tsx":
			hasJS = true
		case ".py":
			hasPython = true
		}

		lower := strings.ToLower(f)
		if strings.Contains(lower, "api") || strings.Contains(lower, "handler") ||
			strings.Contains(lower, "endpoint") || strings.Contains(lower, "route") {
			hasAPI = true
		}
		if strings.Contains(lower, "config") || strings.Contains(lower, "setting") {
			hasConfig = true
		}
		if strings.Contains(lower, "db") || strings.Contains(lower, "migration") ||
			strings.Contains(lower, "schema") || strings.Contains(lower, "model") {
			hasDB = true
		}
	}

	if hasGo {
		items = append(items, "- [ ] Unit tests pass (`go test ./...`)")
	}
	if hasJS {
		items = append(items, "- [ ] Unit tests pass (`npm test`)")
	}
	if hasPython {
		items = append(items, "- [ ] Unit tests pass (`pytest`)")
	}

	// Add specific test suggestions based on change types.
	for _, c := range commits {
		switch c.Type {
		case "feat":
			items = append(items, fmt.Sprintf("- [ ] Manual test: %s", stripConventionalPrefix(c.Message)))
		case "fix":
			items = append(items, fmt.Sprintf("- [ ] Verify fix: %s", stripConventionalPrefix(c.Message)))
		}
	}

	if hasAPI {
		items = append(items, "- [ ] API endpoints respond correctly")
	}
	if hasConfig {
		items = append(items, "- [ ] Configuration changes work as expected")
	}
	if hasDB {
		items = append(items, "- [ ] Database migrations run successfully")
	}

	// Check for breaking changes.
	if detectBreaking(commits) {
		items = append(items, "- [ ] Verify backward compatibility or document migration path")
	} else {
		items = append(items, "- [ ] No breaking changes to public API")
	}

	// De-duplicate items.
	seen := make(map[string]bool)
	var unique []string
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			unique = append(unique, item)
		}
	}

	return strings.Join(unique, "\n")
}

// FormatForGitHub formats the PRDescription into a string ready for gh pr create.
func FormatForGitHub(pr *PRDescription) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("gh pr create --title %q", pr.Title))
	sb.WriteString(fmt.Sprintf(" --body %q", pr.Body))

	if len(pr.Labels) > 0 {
		sb.WriteString(fmt.Sprintf(" --label %q", strings.Join(pr.Labels, ",")))
	}

	if len(pr.Reviewers) > 0 {
		sb.WriteString(fmt.Sprintf(" --reviewer %q", strings.Join(pr.Reviewers, ",")))
	}

	return sb.String()
}

// PRGeneratorTool implements the Tool interface for PR description generation.
type PRGeneratorTool struct {
	ProjectDir string
}

// Name returns the tool name.
func (t *PRGeneratorTool) Name() string {
	return "pr_generate"
}

// Description returns the tool description.
func (t *PRGeneratorTool) Description() string {
	return "Generate a comprehensive pull request description from commit history and diffs. Analyzes commits since a base branch to create a structured PR with title, body, labels, and reviewer suggestions."
}

// Parameters returns the JSON schema for the tool's input.
// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (t *PRGeneratorTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"base_branch": {Type: "string", Default: "main", Description: "The base branch to compare against (e.g., 'main', 'develop')"},
			"project_dir": {Type: "string", Description: "The project directory (defaults to current directory)"},
		},
		Required: []string{},
	}
}

func (t *PRGeneratorTool) Parameters() map[string]interface{} {
	return prGeneratorSchema.ToJSONSchema()
}

// prGeneratorSchema is the single source of truth for PRGenerator's input schema.
var prGeneratorSchema = (&PRGeneratorTool{}).Schema()

// PRGeneratorInput is the typed input for PRGeneratorTool.
type PRGeneratorInput struct {
	BaseBranch string `json:"base_branch"`
	ProjectDir string `json:"project_dir"`
}

// Execute runs the PR generator tool.
func (t *PRGeneratorTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	params, err := DecodeInput[PRGeneratorInput]("PRGenerator", input)
	if err != nil {
		return "", err
	}

	if params.BaseBranch == "" {
		params.BaseBranch = "main"
	}

	projectDir := t.ProjectDir
	if params.ProjectDir != "" {
		projectDir = params.ProjectDir
	}
	if projectDir == "" {
		projectDir = "."
	}

	gen := NewPRGenerator(projectDir)
	pr, err := gen.Generate(params.BaseBranch)
	if err != nil {
		return "", err
	}

	// Build output.
	var sb strings.Builder
	sb.WriteString("# Generated PR Description\n\n")
	sb.WriteString(fmt.Sprintf("**Title:** %s\n\n", pr.Title))
	sb.WriteString(fmt.Sprintf("**Type:** %s\n", pr.Type))
	sb.WriteString(fmt.Sprintf("**Breaking:** %v\n", pr.Breaking))

	if len(pr.Labels) > 0 {
		sb.WriteString(fmt.Sprintf("**Labels:** %s\n", strings.Join(pr.Labels, ", ")))
	}
	if len(pr.Reviewers) > 0 {
		sb.WriteString(fmt.Sprintf("**Reviewers:** %s\n", strings.Join(pr.Reviewers, ", ")))
	}

	sb.WriteString("\n---\n\n")
	sb.WriteString(pr.Body)

	sb.WriteString("\n---\n\n")
	sb.WriteString("## GitHub CLI Command\n\n```\n")
	sb.WriteString(FormatForGitHub(pr))
	sb.WriteString("\n```\n")

	return sb.String(), nil
}

// --- Internal helpers ---

func (g *PRGenerator) runGit(args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", args...) // #nosec G204 -- executable is fixed to git; args are generated by this tool
	cmd.Dir = g.ProjectDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func parseConventionalCommit(message string) (commitType, scope string) {
	// Parse "type(scope): message" or "type: message" or "type!: message"
	colonIdx := strings.Index(message, ":")
	if colonIdx < 0 {
		return "", ""
	}

	prefix := message[:colonIdx]

	// Strip "!" for breaking change indicator.
	prefix = strings.TrimSuffix(prefix, "!")

	// Check for scope: "type(scope)"
	if parenOpen := strings.Index(prefix, "("); parenOpen > 0 {
		if parenClose := strings.Index(prefix, ")"); parenClose > parenOpen {
			commitType = prefix[:parenOpen]
			scope = prefix[parenOpen+1 : parenClose]
		}
	} else {
		commitType = prefix
	}

	// Validate the type.
	commitType = strings.TrimSpace(commitType)
	if !isValidPRCommitType(commitType) {
		return "", ""
	}

	return commitType, scope
}

func isValidPRCommitType(t string) bool {
	valid := []string{"feat", "fix", "refactor", "test", "docs", "style", "chore", "perf", "ci", "build"}
	for _, v := range valid {
		if t == v {
			return true
		}
	}
	return false
}

func stripConventionalPrefix(message string) string {
	colonIdx := strings.Index(message, ":")
	if colonIdx < 0 {
		return message
	}
	// Verify it looks like a conventional commit prefix.
	prefix := message[:colonIdx]
	prefix = strings.TrimSuffix(prefix, "!")
	if parenOpen := strings.Index(prefix, "("); parenOpen > 0 {
		prefix = prefix[:parenOpen]
	}
	prefix = strings.TrimSpace(prefix)
	if isValidPRCommitType(prefix) {
		return strings.TrimSpace(message[colonIdx+1:])
	}
	return message
}

func detectPRType(commits []CommitSummary) string {
	if len(commits) == 0 {
		return "chore"
	}

	typeCounts := make(map[string]int)
	for _, c := range commits {
		if c.Type != "" {
			typeCounts[c.Type]++
		}
	}

	// Return the most common type.
	maxCount := 0
	maxType := "chore"
	for t, count := range typeCounts {
		if count > maxCount {
			maxCount = count
			maxType = t
		}
	}
	return maxType
}

func detectBreaking(commits []CommitSummary) bool {
	for _, c := range commits {
		if strings.Contains(c.Message, "BREAKING CHANGE") ||
			strings.Contains(c.Message, "BREAKING-CHANGE") {
			return true
		}
		// Check for "type!:" pattern.
		colonIdx := strings.Index(c.Message, ":")
		if colonIdx > 0 && strings.HasSuffix(c.Message[:colonIdx], "!") {
			return true
		}
	}
	return false
}

func collectBreakingChanges(commits []CommitSummary) []string {
	var changes []string
	for _, c := range commits {
		if strings.Contains(c.Message, "BREAKING CHANGE") ||
			strings.Contains(c.Message, "BREAKING-CHANGE") ||
			(strings.Index(c.Message, ":") > 0 && strings.HasSuffix(c.Message[:strings.Index(c.Message, ":")], "!")) {
			changes = append(changes, c.Message)
		}
	}
	return changes
}

func generateSummary(commits []CommitSummary) string {
	if len(commits) == 0 {
		return "No changes."
	}

	if len(commits) == 1 {
		return stripConventionalPrefix(commits[0].Message)
	}

	types := make(map[string]int)
	for _, c := range commits {
		if c.Type != "" {
			types[c.Type]++
		}
	}

	var parts []string
	if n, ok := types["feat"]; ok {
		parts = append(parts, fmt.Sprintf("%d new feature(s)", n))
	}
	if n, ok := types["fix"]; ok {
		parts = append(parts, fmt.Sprintf("%d bug fix(es)", n))
	}
	if n, ok := types["refactor"]; ok {
		parts = append(parts, fmt.Sprintf("%d refactoring(s)", n))
	}
	if n, ok := types["docs"]; ok {
		parts = append(parts, fmt.Sprintf("%d documentation update(s)", n))
	}
	if n, ok := types["chore"]; ok {
		parts = append(parts, fmt.Sprintf("%d maintenance task(s)", n))
	}
	if n, ok := types["test"]; ok {
		parts = append(parts, fmt.Sprintf("%d test update(s)", n))
	}
	if n, ok := types["perf"]; ok {
		parts = append(parts, fmt.Sprintf("%d performance improvement(s)", n))
	}

	if len(parts) == 0 {
		return fmt.Sprintf("This PR includes %d commit(s).", len(commits))
	}

	return fmt.Sprintf("This PR includes %s.", strings.Join(parts, ", "))
}

func summarizeCommits(commits []CommitSummary) string {
	if len(commits) == 0 {
		return "updates"
	}
	if len(commits) <= 3 {
		var subjects []string
		for _, c := range commits {
			subject := stripConventionalPrefix(c.Message)
			if len(subject) > 30 {
				// Rune-safe truncation: never split a multibyte UTF-8 sequence.
				if runes := []rune(subject); len(runes) > 30 {
					subject = string(runes[:27]) + "..."
				}
			}
			subjects = append(subjects, subject)
		}
		return strings.Join(subjects, ", ")
	}
	return fmt.Sprintf("%d changes", len(commits))
}

func describeFile(path string) string {
	ext := filepath.Ext(path)
	base := filepath.Base(path)
	dir := filepath.Dir(path)

	if strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, ".test.js") ||
		strings.HasSuffix(base, ".test.ts") || strings.HasSuffix(base, "_test.ts") {
		return "Tests"
	}

	if base == "go.mod" || base == "go.sum" || base == "package.json" || base == "package-lock.json" {
		return "Dependencies"
	}

	if ext == ".md" {
		return "Documentation"
	}

	if strings.Contains(dir, "config") || strings.Contains(dir, "setting") {
		return "Configuration"
	}

	if strings.Contains(dir, "handler") || strings.Contains(dir, "api") ||
		strings.Contains(dir, "route") {
		return "API layer"
	}

	if strings.Contains(dir, "model") || strings.Contains(dir, "schema") {
		return "Data model"
	}

	return ""
}

func splitNonEmpty(s string) []string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
