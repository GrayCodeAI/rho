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

type SpecTestGenTool struct{}

func (SpecTestGenTool) Name() string { return "SpecTestGen" }

// SpecTestGenInput is the typed input for SpecTestGenTool.
type SpecTestGenInput struct {
	ScanDir  string `json:"scan_dir"`
	Language string `json:"language"`
}

func (SpecTestGenTool) Aliases() []string {
	return []string{"spec_testgen", "spec:testgen"}
}

func (SpecTestGenTool) Description() string {
	return "Generate test stubs from requirements in spec.md. Creates test functions for each REQ-XXX.Y.Z requirement."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecTestGenTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"scan_dir": {Type: "string", Description: "Directory to scan (default: current directory)"},
			"language": {Type: "string", Enum: []interface{}{"go", "ts", "py"}, Description: "Language: go, ts, py (default: auto-detect)"},
		},
	}
}

func (SpecTestGenTool) Parameters() map[string]interface{} {
	return specTestGenSchema.ToJSONSchema()
}

// specTestGenSchema is the single source of truth for SpecTestGen's input schema.
var specTestGenSchema = SpecTestGenTool{}.Schema()

func (SpecTestGenTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[SpecTestGenInput]("SpecTestGen", input)
	if err != nil {
		return "", err
	}
	if p.ScanDir == "" {
		p.ScanDir, _ = os.Getwd()
	}
	if p.Language == "" {
		p.Language = detectLanguageForTests(p.ScanDir)
	}

	dir, err := specDir(ctx)
	if err != nil {
		return "", err
	}

	specContent := readFileStr(filepath.Join(dir, "spec.md"))
	if specContent == "" {
		return "No spec.md found.", nil
	}

	reqs := spec.ExtractReqIDs(specContent)
	if len(reqs) == 0 {
		return "No REQ IDs found in spec.md.", nil
	}

	var b strings.Builder
	b.WriteString("## Test Generation from Specs\n\n")

	for _, req := range reqs {
		fmt.Fprintf(&b, "### %s\n\n", req.Raw)

		switch p.Language {
		case "go":
			fmt.Fprintf(&b, "```go\nfunc Test_%s_HappyPath(t *testing.T) {\n", goTestName(req.Raw))
			fmt.Fprintf(&b, "    // TODO: Implement happy path for %s\n", req.Raw)
			b.WriteString("}\n\n")
			fmt.Fprintf(&b, "func Test_%s_ErrorCases(t *testing.T) {\n", goTestName(req.Raw))
			fmt.Fprintf(&b, "    // TODO: Implement error cases for %s\n", req.Raw)
			b.WriteString("}\n")
			b.WriteString("```\n\n")
		case "ts":
			fmt.Fprintf(&b, "```typescript\ndescribe('%s', () => {\n", req.Raw)
			fmt.Fprintf(&b, "  it('should handle happy path', () => {\n")
			fmt.Fprintf(&b, "    // TODO: Implement for %s\n", req.Raw)
			b.WriteString("  });\n")
			fmt.Fprintf(&b, "  it('should handle error cases', () => {\n")
			b.WriteString("    // TODO: Implement\n")
			b.WriteString("  });\n")
			b.WriteString("});\n")
			b.WriteString("```\n\n")
		case "py":
			fmt.Fprintf(&b, "```python\nclass Test%s(TestCase):\n", goTestName(req.Raw))
			fmt.Fprintf(&b, "    def test_happy_path(self):\n")
			fmt.Fprintf(&b, "        # TODO: Implement for %s\n", req.Raw)
			b.WriteString("        pass\n\n")
			fmt.Fprintf(&b, "    def test_error_cases(self):\n")
			b.WriteString("        # TODO: Implement\n")
			b.WriteString("        pass\n")
			b.WriteString("```\n\n")
		}
	}

	return strings.TrimSpace(b.String()), nil
}

func goTestName(reqID string) string {
	name := strings.ReplaceAll(reqID, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")
	return titleCaser.String(name)
}

func detectLanguageForTests(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return "go"
	}
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
		return "ts"
	}
	if _, err := os.Stat(filepath.Join(dir, "requirements.txt")); err == nil {
		return "py"
	}
	return "go"
}

func init() {
	_ = SpecTestGenTool{}
}
