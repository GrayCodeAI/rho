package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type SpecBlastTool struct{}

func (SpecBlastTool) Name() string { return "SpecBlast" }

// SpecBlastInput is the typed input for SpecBlastTool.
type SpecBlastInput struct {
	TargetFile string `json:"target_file"`
}

func (SpecBlastTool) Aliases() []string {
	return []string{"spec_blast", "spec:blast"}
}

func (SpecBlastTool) Description() string {
	return "Blast radius analysis for proposed changes. Estimates which files, functions, and dependencies will be affected by a change before implementation."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecBlastTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"target_file": {Type: "string", Description: "File to analyze for blast radius"},
		},
	}
}

func (SpecBlastTool) Parameters() map[string]interface{} {
	return specBlastSchema.ToJSONSchema()
}

// specBlastSchema is the single source of truth for SpecBlast's input schema.
var specBlastSchema = SpecBlastTool{}.Schema()

type BlastResult struct {
	TargetFile   string   `json:"target_file"`
	DirectImpact []string `json:"direct_impact"`
	Transitive   []string `json:"transitive_impact"`
	RiskAreas    []string `json:"risk_areas"`
	TestTargets  []string `json:"test_targets"`
	Confidence   float64  `json:"confidence"`
}

func (SpecBlastTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[SpecBlastInput]("SpecBlast", input)
	if err != nil {
		return "", err
	}
	if p.TargetFile == "" {
		return "target_file is required", nil
	}

	cwd, _ := os.Getwd()
	result := analyzeBlastRadius(cwd, p.TargetFile)

	var b strings.Builder
	b.WriteString("## Blast Radius Analysis\n\n")
	fmt.Fprintf(&b, "**Target**: `%s`\n", result.TargetFile)
	fmt.Fprintf(&b, "**Confidence**: %.0f%%\n\n", result.Confidence*100)

	if len(result.DirectImpact) > 0 {
		b.WriteString("### Direct Impact\n\n")
		for _, f := range result.DirectImpact {
			fmt.Fprintf(&b, "- `%s`\n", f)
		}
		b.WriteString("\n")
	}

	if len(result.Transitive) > 0 {
		b.WriteString("### Transitive Impact\n\n")
		for _, f := range result.Transitive {
			fmt.Fprintf(&b, "- `%s`\n", f)
		}
		b.WriteString("\n")
	}

	if len(result.RiskAreas) > 0 {
		b.WriteString("### Risk Areas\n\n")
		for _, r := range result.RiskAreas {
			fmt.Fprintf(&b, "- %s\n", r)
		}
		b.WriteString("\n")
	}

	if len(result.TestTargets) > 0 {
		b.WriteString("### Recommended Test Targets\n\n")
		for _, t := range result.TestTargets {
			fmt.Fprintf(&b, "- `%s`\n", t)
		}
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String()), nil
}

func analyzeBlastRadius(root, targetFile string) BlastResult {
	result := BlastResult{TargetFile: targetFile}
	fullPath := filepath.Join(root, targetFile)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, fullPath, nil, parser.ParseComments)
	if err != nil {
		result.Confidence = 0.2
		result.RiskAreas = []string{fmt.Sprintf("Could not parse: %v", err)}
		return result
	}

	result.DirectImpact = append(result.DirectImpact, targetFile)

	imports := make(map[string]bool)
	for _, imp := range file.Imports {
		imports[strings.Trim(imp.Path.Value, `"`)] = true
	}

	calls := make(map[string]bool)
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if ident, ok := sel.X.(*ast.Ident); ok {
				calls[ident.Name+"."+sel.Sel.Name] = true
			}
		}
		return true
	})

	for imp := range imports {
		if strings.Contains(imp, "./") || strings.Contains(imp, "../") {
			result.Transitive = append(result.Transitive, imp)
		}
	}

	for call := range calls {
		result.Transitive = append(result.Transitive, call)
	}

	testFile := strings.TrimSuffix(targetFile, ".go") + "_test.go"
	if _, err := os.Stat(filepath.Join(root, testFile)); err == nil {
		result.TestTargets = append(result.TestTargets, testFile)
	}

	result.Confidence = 0.6
	if len(result.Transitive) > 5 {
		result.RiskAreas = append(result.RiskAreas, "High dependency count — wide impact possible")
	}
	if len(calls) > 10 {
		result.RiskAreas = append(result.RiskAreas, "Many external calls — verify all callers")
	}

	return result
}

func init() {
	_ = SpecBlastTool{}
}
