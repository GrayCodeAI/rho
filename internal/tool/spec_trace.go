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

type SpecTraceTool struct{}

func (SpecTraceTool) Name() string { return "SpecTrace" }

// SpecTraceInput is the typed input for SpecTraceTool.
type SpecTraceInput struct {
	Action  string `json:"action"`
	ScanDir string `json:"scan_dir"`
}

func (SpecTraceTool) Aliases() []string {
	return []string{"spec_trace", "spec:trace"}
}

func (SpecTraceTool) Description() string {
	return "Requirements Traceability Matrix (RTM). Maps requirements to design decisions, implementation files, and tests. Supports forward (req->code), backward (test->req), and bidirectional traceability. Flags gaps and orphans."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecTraceTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action":   {Type: "string", Enum: []interface{}{"matrix", "forward", "backward", "gaps"}, Description: "Action: matrix (full RTM), forward (req->code), backward (test->req), gaps (only gaps)"},
			"scan_dir": {Type: "string", Description: "Directory to scan (default: current directory)"},
		},
	}
}

func (SpecTraceTool) Parameters() map[string]interface{} {
	return specTraceSchema.ToJSONSchema()
}

// specTraceSchema is the single source of truth for SpecTrace's input schema.
var specTraceSchema = SpecTraceTool{}.Schema()

type TraceLink struct {
	ReqID     string   `json:"req_id"`
	DesignRef string   `json:"design_ref,omitempty"`
	ImplFiles []string `json:"impl_files"`
	TestFiles []string `json:"test_files"`
	Status    string   `json:"status"`
}

type TraceMatrix struct {
	Links       []TraceLink `json:"links"`
	TotalReqs   int         `json:"total_reqs"`
	CoveredReqs int         `json:"covered_reqs"`
	Gaps        int         `json:"gaps"`
	Orphans     int         `json:"orphans"`
}

func (SpecTraceTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[SpecTraceInput]("SpecTrace", input)
	if err != nil {
		return "", err
	}
	if p.Action == "" {
		p.Action = "matrix"
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

	designContent := readFileStr(filepath.Join(dir, "design.md"))
	_ = designContent

	reqs := spec.ExtractReqIDs(specContent)
	codeFiles := spec.ScanCodeForReqIDs(p.ScanDir)
	testFiles := findTestFilesForTrace(p.ScanDir)

	matrix := buildTraceMatrix(reqs, codeFiles, testFiles)

	switch p.Action {
	case "matrix":
		return formatMatrix(matrix), nil
	case "forward":
		return formatForward(matrix), nil
	case "backward":
		return formatBackward(matrix), nil
	case "gaps":
		return formatGaps(matrix), nil
	default:
		return formatMatrix(matrix), nil
	}
}

func findTestFilesForTrace(root string) []string {
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

func buildTraceMatrix(reqs []spec.ReqID, codeFiles map[string][]string, testFiles []string) TraceMatrix {
	matrix := TraceMatrix{}

	for _, req := range reqs {
		link := TraceLink{
			ReqID: req.Raw,
		}

		if files, ok := codeFiles[req.Raw]; ok {
			link.ImplFiles = files
		}

		for _, tf := range testFiles {
			data, err := os.ReadFile(tf)
			if err != nil {
				continue
			}
			if strings.Contains(string(data), req.Raw) {
				link.TestFiles = append(link.TestFiles, tf)
			}
		}

		if len(link.ImplFiles) > 0 && len(link.TestFiles) > 0 {
			link.Status = "covered"
			matrix.CoveredReqs++
		} else if len(link.ImplFiles) > 0 {
			link.Status = "partial (no tests)"
			matrix.Gaps++
		} else if len(link.TestFiles) > 0 {
			link.Status = "partial (no impl)"
			matrix.Gaps++
		} else {
			link.Status = "uncovered"
			matrix.Gaps++
		}

		matrix.Links = append(matrix.Links, link)
	}

	for id := range codeFiles {
		if !reqExistsTrace(reqs, id) {
			matrix.Orphans++
		}
	}

	matrix.TotalReqs = len(reqs)
	return matrix
}

func reqExistsTrace(reqs []spec.ReqID, id string) bool {
	for _, r := range reqs {
		if r.Raw == id {
			return true
		}
	}
	return false
}

func formatMatrix(matrix TraceMatrix) string {
	var b strings.Builder
	b.WriteString("## Requirements Traceability Matrix\n\n")
	fmt.Fprintf(&b, "**Total**: %d | **Covered**: %d | **Gaps**: %d | **Orphans**: %d\n\n",
		matrix.TotalReqs, matrix.CoveredReqs, matrix.Gaps, matrix.Orphans)

	b.WriteString("| REQ | Design | Implementation | Tests | Status |\n")
	b.WriteString("|-----|--------|----------------|-------|--------|\n")

	for _, link := range matrix.Links {
		design := link.DesignRef
		if design == "" {
			design = "-"
		}
		impl := strings.Join(link.ImplFiles, ", ")
		if impl == "" {
			impl = "-"
		}
		tests := strings.Join(link.TestFiles, ", ")
		if tests == "" {
			tests = "-"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", link.ReqID, design, impl, tests, link.Status)
	}

	return strings.TrimSpace(b.String())
}

func formatForward(matrix TraceMatrix) string {
	var b strings.Builder
	b.WriteString("## Forward Traceability (REQ -> Code -> Tests)\n\n")

	for _, link := range matrix.Links {
		if link.Status == "uncovered" {
			continue
		}
		fmt.Fprintf(&b, "### %s\n", link.ReqID)
		if len(link.ImplFiles) > 0 {
			b.WriteString("- Implementation:\n")
			for _, f := range link.ImplFiles {
				fmt.Fprintf(&b, "  - `%s`\n", f)
			}
		}
		if len(link.TestFiles) > 0 {
			b.WriteString("- Tests:\n")
			for _, f := range link.TestFiles {
				fmt.Fprintf(&b, "  - `%s`\n", f)
			}
		}
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
}

func formatBackward(matrix TraceMatrix) string {
	var b strings.Builder
	b.WriteString("## Backward Traceability (Tests -> Code -> REQ)\n\n")

	reqByTest := make(map[string][]string)
	for _, link := range matrix.Links {
		for _, tf := range link.TestFiles {
			reqByTest[tf] = append(reqByTest[tf], link.ReqID)
		}
	}

	for test, reqs := range reqByTest {
		fmt.Fprintf(&b, "- `%s` -> %s\n", test, strings.Join(reqs, ", "))
	}

	return strings.TrimSpace(b.String())
}

func formatGaps(matrix TraceMatrix) string {
	var b strings.Builder
	b.WriteString("## Traceability Gaps\n\n")

	for _, link := range matrix.Links {
		if link.Status == "covered" {
			continue
		}
		fmt.Fprintf(&b, "- **%s**: %s\n", link.ReqID, link.Status)
	}

	if matrix.Orphans > 0 {
		fmt.Fprintf(&b, "\n**%d orphan code references** (code cites REQ not in spec)\n", matrix.Orphans)
	}

	return strings.TrimSpace(b.String())
}

func init() {
	_ = SpecTraceTool{}
}
