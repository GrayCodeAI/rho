package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/GrayCodeAI/rho/internal/gitcmd"
)

// GitHistoryTool provides git history mining for developer workflow insights.
// It shows co-change patterns, commit history for files, and code ownership.
type GitHistoryTool struct{}

func (GitHistoryTool) Name() string      { return "GitHistory" }
func (GitHistoryTool) RiskLevel() string { return "low" }
func (GitHistoryTool) Aliases() []string { return []string{"git-history", "cochange"} }
func (GitHistoryTool) Description() string {
	return "Mine git history for co-change patterns, file history, and code ownership insights."
}

// GitHistoryInput is the typed input for GitHistoryTool.
type GitHistoryInput struct {
	Action string `json:"action"`
	File   string `json:"file"`
	Limit  int    `json:"limit"`
	Root   string `json:"root"`
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (GitHistoryTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action": {Type: "string", Description: "Action: history (commit log), cochange (frequently changed together), owners (top contributors), blame (line-level ownership)", Enum: []interface{}{"history", "cochange", "owners", "blame"}},
			"file":   {Type: "string", Description: "File to analyze"},
			"limit":  {Type: "integer", Description: "Max results (default: 20)"},
			"root":   {Type: "string", Description: "Project root directory (default: current dir)"},
		},
		Required: []string{"action"},
	}
}

func (GitHistoryTool) Parameters() map[string]interface{} {
	return gitHistorySchema.ToJSONSchema()
}

// gitHistorySchema is the single source of truth for GitHistory's input schema.
var gitHistorySchema = GitHistoryTool{}.Schema()

func (GitHistoryTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[GitHistoryInput]("GitHistory", input)
	if err != nil {
		return "", err
	}

	root := p.Root
	if root == "" {
		root = "."
	}
	if err := validatePathAllowed(ctx, root); err != nil {
		return "", err
	}
	if p.Limit <= 0 {
		p.Limit = 20
	}

	switch p.Action {
	case "history":
		return gitFileHistory(ctx, root, p.File, p.Limit)
	case "cochange":
		return gitCoChange(ctx, root, p.File, p.Limit)
	case "owners":
		return gitFileOwners(ctx, root, p.File, p.Limit)
	case "blame":
		return gitBlame(ctx, root, p.File)
	default:
		return "", fmt.Errorf("unknown action: %s (use: history, cochange, owners, blame)", p.Action)
	}
}

func gitFileHistory(ctx context.Context, root, file string, limit int) (string, error) {
	if file == "" {
		return "", fmt.Errorf("file is required for history action")
	}

	cmd := gitcmd.Command(ctx, "log", "--oneline", "--follow", fmt.Sprintf("-%d", limit), "--", file) // #nosec G204 -- git subcommand invocation with fixed subcommand and internally-derived args
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return fmt.Sprintf("No git history found for %s", file), nil
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return fmt.Sprintf("No git history found for %s", file), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Git History for `%s` (%d commits)\n\n", file, len(lines)))
	for _, line := range lines {
		b.WriteString(fmt.Sprintf("- %s\n", line))
	}
	return b.String(), nil
}

func gitCoChange(ctx context.Context, root, file string, limit int) (string, error) {
	if file == "" {
		return "", fmt.Errorf("file is required for cochange action")
	}

	// Get commits that touched this file
	cmd := gitcmd.Command(ctx, "log", "--format=%H", "--follow", fmt.Sprintf("-%d", limit*5), "--", file) // #nosec G204 -- git subcommand invocation with fixed subcommand and internally-derived args
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return fmt.Sprintf("No co-change data found for %s", file), nil
	}

	commits := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(commits) == 0 || (len(commits) == 1 && commits[0] == "") {
		return fmt.Sprintf("No co-change data found for %s", file), nil
	}

	// For each commit, find other files changed
	coChangeCount := make(map[string]int)
	for _, commit := range commits {
		if commit == "" {
			continue
		}
		cmd := gitcmd.Command(ctx, "diff-tree", "--no-commit-id", "--name-only", "-r", commit) // #nosec G204 -- git subcommand invocation with fixed subcommand and internally-derived args
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && line != file {
				coChangeCount[line]++
			}
		}
	}

	// Sort by count
	type scored struct {
		file  string
		count int
	}
	var results []scored
	for f, c := range coChangeCount {
		if c >= 2 { // at least 2 co-occurrences
			results = append(results, scored{f, c})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].count > results[j].count
	})

	limitResults := limit
	if limitResults > len(results) {
		limitResults = len(results)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Co-Change Analysis for `%s`\n\n", file))
	b.WriteString(fmt.Sprintf("Files that frequently change together with `%s`:\n\n", file))
	for i := 0; i < limitResults; i++ {
		b.WriteString(fmt.Sprintf("- `%s` (%d co-occurrences)\n", results[i].file, results[i].count))
	}
	if limitResults == 0 {
		b.WriteString("No co-change patterns found.\n")
	}
	return b.String(), nil
}

func gitFileOwners(ctx context.Context, root, file string, limit int) (string, error) {
	args := []string{"log", "--format=%an", "--follow", fmt.Sprintf("-%d", limit*10)}
	if file != "" {
		args = append(args, "--", file)
	}

	cmd := gitcmd.Command(ctx, args...) // #nosec G204 -- git subcommand invocation with fixed subcommand and internally-derived args
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "No ownership data found.", nil
	}

	// Count commits per author
	authorCount := make(map[string]int)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			authorCount[line]++
		}
	}

	type scored struct {
		author string
		count  int
	}
	var results []scored
	for author, count := range authorCount {
		results = append(results, scored{author, count})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].count > results[j].count
	})

	limitResults := limit
	if limitResults > len(results) {
		limitResults = len(results)
	}

	var b strings.Builder
	if file != "" {
		b.WriteString(fmt.Sprintf("## Code Owners for `%s`\n\n", file))
	} else {
		b.WriteString("## Top Contributors\n\n")
	}
	totalCommits := 0
	for _, r := range results {
		totalCommits += r.count
	}
	for i := 0; i < limitResults; i++ {
		pct := float64(results[i].count) / float64(totalCommits) * 100
		b.WriteString(fmt.Sprintf("- %s: %d commits (%.0f%%)\n", results[i].author, results[i].count, pct))
	}
	if limitResults == 0 {
		b.WriteString("No ownership data found.\n")
	}
	return b.String(), nil
}

func gitBlame(ctx context.Context, root, file string) (string, error) {
	if file == "" {
		return "", fmt.Errorf("file is required for blame action")
	}

	cmd := gitcmd.Command(ctx, "blame", "--line-porcelain", file) // #nosec G204 -- fixed git subcommand and separate file argument
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return fmt.Sprintf("Could not blame %s: %v", file, err), nil
	}

	// Parse blame output to get author counts per line
	authorLines := make(map[string]int)
	totalLines := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "author ") {
			author := strings.TrimPrefix(line, "author ")
			authorLines[author]++
			totalLines++
		}
	}

	type scored struct {
		author string
		lines  int
	}
	var results []scored
	for author, lines := range authorLines {
		results = append(results, scored{author, lines})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].lines > results[j].lines
	})

	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Blame Summary for `%s` (%d lines)\n\n", file, totalLines))
	for _, r := range results {
		pct := float64(r.lines) / float64(totalLines) * 100
		b.WriteString(fmt.Sprintf("- %s: %d lines (%.0f%%)\n", r.author, r.lines, pct))
	}
	return b.String(), nil
}
