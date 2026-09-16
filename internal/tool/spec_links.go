package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/GrayCodeAI/rho/internal/spec"
)

// SpecLinksTool checks and generates bidirectional links between spec
// requirements (REQ-X.Y.Z) and the code/tests that cite them.
type SpecLinksTool struct{}

func (SpecLinksTool) Name() string { return "SpecLinks" }

// SpecLinksInput is the typed input for SpecLinksTool.
type SpecLinksInput struct {
	Action string `json:"action"`
}

func (SpecLinksTool) Aliases() []string {
	return []string{"spec_links", "spec:links"}
}

func (SpecLinksTool) Description() string {
	return "Manage bidirectional links between specs and tests using [@req:XXX] annotations. " +
		"action=check reports which requirements are cited and which are uncovered; " +
		"action=add writes [REQ-X.Y.Z] annotations into the spec's tasks.md checklist."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecLinksTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action": {Type: "string", Enum: []interface{}{"check", "add"}, Description: "Action: check (verify coverage), add (write annotations into tasks.md)"},
		},
	}
}

func (SpecLinksTool) Parameters() map[string]interface{} {
	return specLinksSchema.ToJSONSchema()
}

// specLinksSchema is the single source of truth for SpecLinks's input schema.
var specLinksSchema = SpecLinksTool{}.Schema()

func (SpecLinksTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[SpecLinksInput]("SpecLinks", input)
	if err != nil {
		return "", err
	}
	if p.Action == "" {
		p.Action = "check"
	}

	dir, err := specDir(ctx)
	if err != nil {
		return "", err
	}

	switch p.Action {
	case "check":
		return checkLinks(dir)
	case "add":
		return addLinks(dir)
	default:
		return "", fmt.Errorf("unknown action %q (want check or add)", p.Action)
	}
}

// checkLinks reports each requirement's citation coverage: which files cite it
// and whether any test file cites it.
func checkLinks(dir string) (string, error) {
	specContent := readFileStr(filepath.Join(dir, "spec.md"))
	if specContent == "" {
		return "No spec.md found.", nil
	}

	reqs := spec.ExtractReqIDs(specContent)
	if len(reqs) == 0 {
		return "No REQ IDs found in spec.md.", nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	citations := spec.ScanCodeForReqIDs(cwd)

	// Index citations by requirement ID.
	byReq := make(map[string][]string)
	for path, ids := range citations {
		for _, id := range ids {
			byReq[id] = append(byReq[id], path)
		}
	}

	var b strings.Builder
	b.WriteString("## Spec-Test Links\n\n")
	b.WriteString("| Requirement | Cited in | Test |\n")
	b.WriteString("|---|---|---|\n")

	covered, testCovered := 0, 0
	for _, req := range reqs {
		paths := byReq[req.Raw]
		sort.Strings(paths)
		hasTest := false
		for _, p := range paths {
			if strings.HasSuffix(p, "_test.go") || strings.Contains(p, "test") {
				hasTest = true
				break
			}
		}
		if len(paths) > 0 {
			covered++
		}
		if hasTest {
			testCovered++
		}

		where := "—"
		if len(paths) > 0 {
			rel := make([]string, 0, len(paths))
			for _, p := range paths {
				if r, relErr := filepath.Rel(cwd, p); relErr == nil {
					rel = append(rel, r)
				} else {
					rel = append(rel, p)
				}
			}
			where = strings.Join(rel, ", ")
		}
		testMark := "no"
		if hasTest {
			testMark = "yes"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", req.Raw, where, testMark)
	}

	fmt.Fprintf(&b, "\n%d/%d requirements cited; %d/%d covered by a test.\n",
		covered, len(reqs), testCovered, len(reqs))
	return strings.TrimSpace(b.String()), nil
}

// addLinks writes a traceability checklist into tasks.md, one line per
// requirement, so the implementation phase can tick off citations. Existing
// checklist entries are preserved; only missing requirements are appended.
func addLinks(dir string) (string, error) {
	specContent := readFileStr(filepath.Join(dir, "spec.md"))
	if specContent == "" {
		return "No spec.md found.", nil
	}
	reqs := spec.ExtractReqIDs(specContent)
	if len(reqs) == 0 {
		return "No REQ IDs found in spec.md.", nil
	}

	tasksPath := filepath.Join(dir, "tasks.md")
	existing := readFileStr(tasksPath)

	var missing []string
	for _, req := range reqs {
		if !strings.Contains(existing, req.Raw) {
			missing = append(missing, req.Raw)
		}
	}
	if len(missing) == 0 {
		return fmt.Sprintf("All %d requirement(s) already linked in tasks.md.", len(reqs)), nil
	}

	var b strings.Builder
	b.WriteString(existing)
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		b.WriteString("\n")
	}
	if existing == "" {
		b.WriteString("# Tasks\n\n")
	}
	b.WriteString("\n## Traceability\n\n")
	for _, id := range missing {
		fmt.Fprintf(&b, "- [ ] `%s` — cite in implementation and test\n", id)
	}

	if err := os.WriteFile(tasksPath, []byte(b.String()), 0o644); err != nil { // #nosec G306 -- spec artifacts are user-visible project files
		return "", fmt.Errorf("write tasks.md: %w", err)
	}
	return fmt.Sprintf("Added %d requirement link(s) to tasks.md.", len(missing)), nil
}

func init() {
	_ = SpecLinksTool{}
}
