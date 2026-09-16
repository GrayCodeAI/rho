package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/GrayCodeAI/rho/internal/spec"
)

type SpecDriftTool struct{}

func (SpecDriftTool) Name() string { return "SpecDrift" }

// SpecDriftInput is the typed input for SpecDriftTool.
type SpecDriftInput struct {
	ScanDir string `json:"scan_dir"`
}

func (SpecDriftTool) Aliases() []string {
	return []string{"spec_drift", "spec:drift"}
}

func (SpecDriftTool) Description() string {
	return "Detect drift between specs and implementation. Compares requirements in spec.md against actual code coverage."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecDriftTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"scan_dir": {Type: "string", Description: "Directory to scan (default: current directory)"},
		},
	}
}

func (SpecDriftTool) Parameters() map[string]interface{} {
	return specDriftSchema.ToJSONSchema()
}

// specDriftSchema is the single source of truth for SpecDrift's input schema.
var specDriftSchema = SpecDriftTool{}.Schema()

func (SpecDriftTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[SpecDriftInput]("SpecDrift", input)
	if err != nil {
		return "", err
	}
	if p.ScanDir == "" {
		p.ScanDir, _ = os.Getwd()
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

	codeFiles := spec.ScanCodeForReqIDs(p.ScanDir)
	citedReqs := make(map[string][]string)
	for file, ids := range codeFiles {
		for _, id := range ids {
			citedReqs[id] = append(citedReqs[id], file)
		}
	}

	testFiles := findTestFiles(p.ScanDir)
	testedReqs := findTestedReqs(testFiles)

	var findings []string
	covered := 0
	drift := 0

	for _, req := range reqs {
		codeRefs := citedReqs[req.Raw]
		testRefs := testedReqs[req.Raw]
		if len(codeRefs) == 0 && len(testRefs) == 0 {
			findings = append(findings, fmt.Sprintf("CRITICAL: %s has no implementation or test coverage", req.Raw))
			drift++
		} else if len(codeRefs) > 0 && len(testRefs) > 0 {
			covered++
		} else if len(codeRefs) > 0 {
			findings = append(findings, fmt.Sprintf("WARN: %s implemented but has no test", req.Raw))
		} else {
			findings = append(findings, fmt.Sprintf("WARN: %s has test but no implementation citation", req.Raw))
		}
	}

	totalReqs := len(reqs)
	coverage := 0.0
	if totalReqs > 0 {
		coverage = float64(covered) / float64(totalReqs) * 100
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## Spec Drift Report\n\n")
	fmt.Fprintf(&b, "**Coverage**: %.0f%% (%d/%d fully covered)\n\n", coverage, covered, totalReqs)

	if len(findings) > 0 {
		b.WriteString("### Findings\n\n")
		for _, f := range findings {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteString("\n")
	}

	if coverage >= 100 {
		b.WriteString("Status: ALIGNED - All requirements covered\n")
	} else if coverage >= 80 {
		b.WriteString("Status: MINOR DRIFT - Most requirements covered\n")
	} else if coverage >= 50 {
		b.WriteString("Status: MODERATE DRIFT - Some requirements lack coverage\n")
	} else {
		b.WriteString("Status: SIGNIFICANT DRIFT - Spec and implementation out of sync\n")
	}

	return strings.TrimSpace(b.String()), nil
}

func findTestFiles(root string) []string {
	var tests []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.Contains(path, ".git") || strings.Contains(path, "vendor") {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, ".test.ts") || strings.HasSuffix(base, ".test.js") {
			tests = append(tests, path)
		}
		return nil
	})
	return tests
}

func findTestedReqs(testFiles []string) map[string][]string {
	result := make(map[string][]string)
	reReq := regexp.MustCompile(`REQ-(\d+)(?:\.(\d+))?(?:\.(\d+))?`)
	for _, file := range testFiles {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for _, match := range reReq.FindAllString(string(data), -1) {
			result[match] = append(result[match], file)
		}
	}
	return result
}

func init() {
	_ = SpecDriftTool{}
}
