package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GrayCodeAI/rho/internal/spec"
)

type SpecBddTool struct{}

func (SpecBddTool) Name() string { return "SpecBdd" }

// SpecBddInput is the typed input for SpecBddTool.
type SpecBddInput struct {
	Action string `json:"action"`
	Format string `json:"format"`
}

func (SpecBddTool) Aliases() []string {
	return []string{"spec_bdd", "spec:bdd"}
}

func (SpecBddTool) Description() string {
	return "Generate Gherkin/BDD scenarios from requirements. Converts EARS-format requirements into Given/When/Then scenarios for behavior-driven testing. Links scenarios back to requirements for traceability."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecBddTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action": {Type: "string", Enum: []interface{}{"generate", "validate", "export"}, Description: "Action: generate (from spec), validate (check coverage), export (to feature files)"},
			"format": {Type: "string", Enum: []interface{}{"gherkin", "cucumber", "pytest-bdd"}, Description: "Output format: gherkin (default), cucumber, pytest-bdd"},
		},
	}
}

func (SpecBddTool) Parameters() map[string]interface{} {
	return specBddSchema.ToJSONSchema()
}

// specBddSchema is the single source of truth for SpecBdd's input schema.
var specBddSchema = SpecBddTool{}.Schema()

type BddScenario struct {
	Feature  string   `json:"feature"`
	Scenario string   `json:"scenario"`
	Steps    []string `json:"steps"`
	ReqID    string   `json:"req_id"`
}

func (SpecBddTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[SpecBddInput]("SpecBdd", input)
	if err != nil {
		return "", err
	}
	if p.Action == "" {
		p.Action = "generate"
	}
	if p.Format == "" {
		p.Format = "gherkin"
	}

	dir, err := specDir(ctx)
	if err != nil {
		return "", err
	}

	switch p.Action {
	case "generate":
		return generateBdd(dir, p.Format)
	case "validate":
		return validateBdd(dir)
	case "export":
		return exportBdd(dir, p.Format)
	default:
		return "", fmt.Errorf("unknown action %q", p.Action)
	}
}

func generateBdd(dir, format string) (string, error) {
	specContent := readFileStr(filepath.Join(dir, "spec.md"))
	if specContent == "" {
		return "No spec.md found.", nil
	}

	reqs := spec.ExtractReqIDs(specContent)
	if len(reqs) == 0 {
		return "No REQ IDs found in spec.md.", nil
	}

	var b strings.Builder
	b.WriteString("## BDD Scenarios\n\n")

	for _, req := range reqs {
		fmt.Fprintf(&b, "### %s\n\n", req.Raw)
		b.WriteString("```gherkin\n")
		fmt.Fprintf(&b, "Feature: %s\n", req.Raw)
		fmt.Fprintf(&b, "  Scenario: %s - happy path\n", req.Raw)
		b.WriteString("    Given the system is in a valid state\n")
		fmt.Fprintf(&b, "    When the user triggers %s\n", req.Raw)
		b.WriteString("    Then the expected outcome occurs\n")
		b.WriteString("```\n\n")
	}

	return strings.TrimSpace(b.String()), nil
}

func validateBdd(dir string) (string, error) {
	specContent := readFileStr(filepath.Join(dir, "spec.md"))
	if specContent == "" {
		return "No spec.md found.", nil
	}

	reqs := spec.ExtractReqIDs(specContent)
	if len(reqs) == 0 {
		return "No REQ IDs found.", nil
	}

	var b strings.Builder
	b.WriteString("## BDD Coverage\n\n")
	fmt.Fprintf(&b, "**%d requirements** need BDD scenarios\n\n", len(reqs))

	for _, req := range reqs {
		fmt.Fprintf(&b, "- [ ] `%s`\n", req.Raw)
	}

	return strings.TrimSpace(b.String()), nil
}

func exportBdd(dir, format string) (string, error) {
	featuresDir := filepath.Join(dir, "features")
	_ = os.MkdirAll(featuresDir, 0o700)

	specContent := readFileStr(filepath.Join(dir, "spec.md"))
	reqs := spec.ExtractReqIDs(specContent)

	written := 0
	for _, req := range reqs {
		filename := strings.ToLower(strings.ReplaceAll(req.Raw, ".", "_")) + ".feature"
		path := filepath.Join(featuresDir, filename)

		var content strings.Builder
		fmt.Fprintf(&content, "Feature: %s\n\n", req.Raw)
		fmt.Fprintf(&content, "  Scenario: %s - happy path\n", req.Raw)
		content.WriteString("    Given the system is in a valid state\n")
		fmt.Fprintf(&content, "    When the user triggers %s\n", req.Raw)
		content.WriteString("    Then the expected outcome occurs\n")

		if err := os.WriteFile(path, []byte(content.String()), 0o600); err == nil {
			written++
		}
	}

	return fmt.Sprintf("Exported %d feature files to %s", written, featuresDir), nil
}

func init() {
	_ = SpecBddTool{}
}
