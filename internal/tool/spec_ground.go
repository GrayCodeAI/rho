package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/GrayCodeAI/rho/internal/spec"
)

type SpecGroundTool struct{}

func (SpecGroundTool) Name() string { return "SpecGround" }

// SpecGroundInput is the typed input for SpecGroundTool.
type SpecGroundInput struct {
	Stage string `json:"stage"`
	Query string `json:"query"`
}

func (SpecGroundTool) Aliases() []string {
	return []string{"spec_ground", "spec:ground"}
}

func (SpecGroundTool) Description() string {
	return "Gather repository context to ground the current spec stage. Probes the codebase for relevant files, patterns, dependencies, and API contracts. Use before Specify, Design, or Plan to ensure the LLM has full context."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecGroundTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"stage": {Type: "string", Enum: []interface{}{"specify", "design", "plan", "tasks", "implement"}, Description: "Which stage to gather context for: specify, design, plan, tasks, implement"},
			"query": {Type: "string", Description: "Optional focus area to narrow the search (e.g., 'auth', 'database', 'API')"},
		},

		Required: []string{"stage"},
	}
}

func (SpecGroundTool) Parameters() map[string]interface{} {
	return specGroundSchema.ToJSONSchema()
}

// specGroundSchema is the single source of truth for SpecGround's input schema.
var specGroundSchema = SpecGroundTool{}.Schema()

func (SpecGroundTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[SpecGroundInput]("SpecGround", input)
	if err != nil {
		return "", err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir, err := specDir(ctx)
	if err != nil {
		dir = filepath.Join(cwd, ".rho", "specs")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## Context Grounding: %s Stage\n\n", titleCaser.String(p.Stage))

	switch p.Stage {
	case "specify":
		groundForSpecify(cwd, p.Query, &b)
	case "design":
		groundForDesign(dir, cwd, p.Query, &b)
	case "plan":
		groundForPlan(dir, cwd, p.Query, &b)
	case "tasks":
		groundForTasks(dir, cwd, &b)
	case "implement":
		groundForImplement(dir, cwd, &b)
	default:
		return "", fmt.Errorf("unknown stage %q", p.Stage)
	}

	return strings.TrimSpace(b.String()), nil
}

func groundForSpecify(cwd, query string, b *strings.Builder) {
	b.WriteString("### Codebase Structure\n\n")

	if out, err := runCmd(cwd, "find", ".", "-type", "f", "-name", "*.go", "-not", "-path", "./vendor/*", "-not", "-path", "./.git/*"); err == nil {
		files := strings.Split(strings.TrimSpace(out), "\n")
		if len(files) > 30 {
			fmt.Fprintf(b, "**%d Go files found** (showing first 30):\n\n", len(files))
			files = files[:30]
		} else {
			fmt.Fprintf(b, "**%d Go files found**:\n\n", len(files))
		}
		for _, f := range files {
			if f != "" {
				fmt.Fprintf(b, "- `%s`\n", f)
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("### Key Directories\n\n")
	if out, err := runCmd(cwd, "find", ".", "-maxdepth", "2", "-type", "d", "-not", "-path", "./.git/*", "-not", "-path", "./vendor/*"); err == nil {
		dirs := strings.Split(strings.TrimSpace(out), "\n")
		for _, d := range dirs {
			if d != "" && d != "." {
				fmt.Fprintf(b, "- `%s`\n", d)
			}
		}
		b.WriteString("\n")
	}

	if query != "" {
		b.WriteString(fmt.Sprintf("### Search Results for %q\n\n", query))
		if out, err := runCmd(cwd, "grep", "-r", "-l", "--include=*.go", query, ".", "--exclude-dir=vendor", "--exclude-dir=.git"); err == nil {
			matches := strings.Split(strings.TrimSpace(out), "\n")
			if len(matches) > 0 && matches[0] != "" {
				fmt.Fprintf(b, "Found in %d files:\n\n", len(matches))
				for _, m := range matches {
					if m != "" {
						fmt.Fprintf(b, "- `%s`\n", m)
					}
				}
				b.WriteString("\n")
			}
		}
	}

	b.WriteString("### Instructions for Specify Stage\n\n")
	b.WriteString("- Review the codebase structure above\n")
	b.WriteString("- Identify affected packages and files\n")
	b.WriteString("- Use `[NEEDS CLARIFICATION: ...]` for any ambiguity\n")
	b.WriteString("- Write requirements using EARS notation (The system shall...)\n")
}

func groundForDesign(dir, cwd, query string, b *strings.Builder) {
	b.WriteString("### Existing Patterns\n\n")

	rePattern := regexp.MustCompile(`(?m)^(type|func|interface)\s+\w+`)
	if entries, err := os.ReadDir(cwd); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if strings.HasSuffix(entry.Name(), ".go") {
				path := filepath.Join(cwd, entry.Name())
				data, err := os.ReadFile(path)
				if err != nil {
					continue
				}
				matches := rePattern.FindAllString(string(data), -1)
				if len(matches) > 0 {
					fmt.Fprintf(b, "**%s**: %d definitions\n", entry.Name(), len(matches))
				}
			}
		}
		b.WriteString("\n")
	}

	specContent := readFileStr(filepath.Join(dir, "spec.md"))
	if specContent != "" {
		b.WriteString("### Requirements from Spec\n\n")
		for _, req := range spec.ExtractReqIDs(specContent) {
			fmt.Fprintf(b, "- `%s`\n", req.Raw)
		}
		b.WriteString("\n")
	}

	if query != "" {
		b.WriteString(fmt.Sprintf("### Related Code for %q\n\n", query))
		if out, err := runCmd(cwd, "grep", "-r", "-n", "--include=*.go", query, ".", "--exclude-dir=vendor", "--exclude-dir=.git"); err == nil {
			lines := strings.Split(strings.TrimSpace(out), "\n")
			if len(lines) > 20 {
				lines = lines[:20]
				fmt.Fprintf(b, "Found %d matches (showing first 20):\n\n", len(lines))
			}
			for _, l := range lines {
				if l != "" {
					fmt.Fprintf(b, "- `%s`\n", l)
				}
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("### Instructions for Design Stage\n\n")
	b.WriteString("- Follow existing patterns and conventions\n")
	b.WriteString("- Reuse existing interfaces where possible\n")
	b.WriteString("- Document key decisions with rationale\n")
	b.WriteString("- Consider the Simplicity and Anti-Abstraction gates\n")
}

func groundForPlan(dir, cwd, query string, b *strings.Builder) {
	specContent := readFileStr(filepath.Join(dir, "spec.md"))
	designContent := readFileStr(filepath.Join(dir, "design.md"))

	if specContent != "" {
		b.WriteString("### Requirements Coverage\n\n")
		reqs := spec.ExtractReqIDs(specContent)
		fmt.Fprintf(b, "%d requirements identified\n\n", len(reqs))
	}

	if designContent != "" {
		b.WriteString("### Design Decisions\n\n")
		reDecision := regexp.MustCompile(`(?m)^#{2,4}\s+.+`)
		decisions := reDecision.FindAllString(designContent, -1)
		for _, d := range decisions {
			fmt.Fprintf(b, "- %s\n", strings.TrimSpace(d))
		}
		b.WriteString("\n")
	}

	b.WriteString("### Test Coverage\n\n")
	if out, err := runCmd(cwd, "find", ".", "-name", "*_test.go", "-not", "-path", "./vendor/*"); err == nil {
		tests := strings.Split(strings.TrimSpace(out), "\n")
		fmt.Fprintf(b, "%d test files found\n\n", len(tests))
	}

	b.WriteString("### Instructions for Plan Stage\n\n")
	b.WriteString("- Map requirements to implementation steps\n")
	b.WriteString("- Define API contracts before implementation\n")
	b.WriteString("- Include phase gates (Simplicity, Anti-Abstraction, Integration-First)\n")
	b.WriteString("- Add Complexity Tracking table for any exceptions\n")
}

func groundForTasks(dir, cwd string, b *strings.Builder) {
	specContent := readFileStr(filepath.Join(dir, "spec.md"))

	b.WriteString("### Requirements to Implement\n\n")
	if specContent != "" {
		for _, req := range spec.ExtractReqIDs(specContent) {
			fmt.Fprintf(b, "- [ ] `%s`\n", req.Raw)
		}
		b.WriteString("\n")
	}

	b.WriteString("### Recommended Task Order\n\n")
	b.WriteString("1. Setup and scaffolding\n")
	b.WriteString("2. Core data structures\n")
	b.WriteString("3. API contracts and interfaces\n")
	b.WriteString("4. Business logic implementation\n")
	b.WriteString("5. Integration and wiring\n")
	b.WriteString("6. Error handling and edge cases\n")
	b.WriteString("7. Tests and validation\n\n")

	b.WriteString("### Instructions for Tasks Stage\n\n")
	b.WriteString("- Each task should be completable in one session\n")
	b.WriteString("- Order by dependency (foundational first)\n")
	b.WriteString("- Reference REQ-XXX.Y.Z IDs for traceability\n")
	b.WriteString("- Use `- [ ]` checkbox format\n")
}

func groundForImplement(dir, cwd string, b *strings.Builder) {
	tasksContent := readFileStr(filepath.Join(dir, "tasks.md"))
	if tasksContent != "" {
		tasks := spec.ParseTasks(tasksContent)
		total := len(tasks)
		complete := 0
		for _, t := range tasks {
			if strings.Contains(t.Description, "[x]") {
				complete++
			}
		}
		fmt.Fprintf(b, "**Progress**: %d/%d tasks complete\n\n", complete, total)
	}

	b.WriteString("### REQ Coverage\n\n")
	specContent := readFileStr(filepath.Join(dir, "spec.md"))
	if specContent != "" {
		codeFiles := spec.ScanCodeForReqIDs(cwd)
		citedReqs := make(map[string]bool)
		for _, ids := range codeFiles {
			for _, id := range ids {
				citedReqs[id] = true
			}
		}
		for _, req := range spec.ExtractReqIDs(specContent) {
			status := "MISS"
			if citedReqs[req.Raw] {
				status = "DONE"
			}
			fmt.Fprintf(b, "- %s `%s`\n", status, req.Raw)
		}
		b.WriteString("\n")
	}

	b.WriteString("### Instructions for Implementation Stage\n")
	b.WriteString("- Add `// [REQ-XXX.Y.Z]` comments to code\n")
	b.WriteString("- Run tests after each change\n")
	b.WriteString("- Use SpecProgress to track completion\n")
}

func runCmd(dir string, name string, args ...string) (string, error) {
	// Bound find/grep so a pathological tree cannot block the tool forever.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- executable and args are fixed by the caller
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func init() {
	_ = SpecGroundTool{}
}
