package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type SpecScaleTool struct{}

func (SpecScaleTool) Name() string { return "SpecScale" }

// SpecScaleInput is the typed input for SpecScaleTool.
type SpecScaleInput struct {
	ScanDir string `json:"scan_dir"`
}

func (SpecScaleTool) Aliases() []string {
	return []string{"spec_scale", "spec:scale"}
}

func (SpecScaleTool) Description() string {
	return "Determine project complexity and recommend planning depth. Analyzes codebase size, change scope, dependency count, and risk to recommend Quick (bug fix), Standard (feature), or Enterprise (architecture) planning track."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecScaleTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"scan_dir": {Type: "string", Description: "Directory to scan (default: current directory)"},
		},
	}
}

func (SpecScaleTool) Parameters() map[string]interface{} {
	return specScaleSchema.ToJSONSchema()
}

// specScaleSchema is the single source of truth for SpecScale's input schema.
var specScaleSchema = SpecScaleTool{}.Schema()

type ComplexityAssessment struct {
	Level        int      `json:"level"`
	Track        string   `json:"track"`
	Score        float64  `json:"score"`
	Files        int      `json:"files"`
	Modules      int      `json:"modules"`
	Dependencies int      `json:"dependencies"`
	RiskFactors  []string `json:"risk_factors"`
}

func (SpecScaleTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[SpecScaleInput]("SpecScale", input)
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

	assessment := assessComplexity(p.ScanDir, dir)

	var b strings.Builder
	fmt.Fprintf(&b, "## Scale Assessment\n\n")
	fmt.Fprintf(&b, "| Metric | Value |\n")
	fmt.Fprintf(&b, "|--------|-------|\n")
	fmt.Fprintf(&b, "| **Level** | %d (0=minimal, 4=enterprise) |\n", assessment.Level)
	fmt.Fprintf(&b, "| **Track** | %s |\n", assessment.Track)
	fmt.Fprintf(&b, "| **Complexity Score** | %.1f/100 |\n", assessment.Score)
	fmt.Fprintf(&b, "| **Source Files** | %d |\n", assessment.Files)
	fmt.Fprintf(&b, "| **Modules/Packages** | %d |\n", assessment.Modules)
	fmt.Fprintf(&b, "| **External Dependencies** | %d |\n\n", assessment.Dependencies)

	b.WriteString("### Recommended Workflow\n\n")
	switch assessment.Level {
	case 0, 1:
		b.WriteString("**Quick Flow Track**: Tech-spec only, minimal documentation\n")
		b.WriteString("- Write brief spec.md (1-2 pages)\n")
		b.WriteString("- Direct task breakdown\n")
		b.WriteString("- Skip detailed design doc\n")
		b.WriteString("- Focus on implementation + tests\n")
	case 2, 3:
		b.WriteString("**Standard Track**: Full planning with architecture\n")
		b.WriteString("- Complete proposal + spec + design\n")
		b.WriteString("- Detailed task decomposition\n")
		b.WriteString("- S.U.P.E.R health check\n")
		b.WriteString("- Adaptive control enabled\n")
	case 4:
		b.WriteString("**Enterprise Track**: Extended planning\n")
		b.WriteString("- All standard phases +\n")
		b.WriteString("- Security/DevOps/Test planning\n")
		b.WriteString("- Multi-phase delivery batches\n")
		b.WriteString("- Architecture review gate\n")
		b.WriteString("- Full traceability matrix\n")
	}
	b.WriteString("\n")

	if len(assessment.RiskFactors) > 0 {
		b.WriteString("### Risk Factors\n\n")
		for _, r := range assessment.RiskFactors {
			fmt.Fprintf(&b, "- %s\n", r)
		}
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String()), nil
}

func assessComplexity(scanDir, specDir string) ComplexityAssessment {
	assessment := ComplexityAssessment{
		Track: "standard",
	}

	_ = filepath.Walk(scanDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.Contains(path, ".git") || strings.Contains(path, "vendor") || strings.Contains(path, "node_modules") {
			return nil
		}
		if strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".py") {
			assessment.Files++
		}
		return nil
	})

	modSet := make(map[string]bool)
	_ = filepath.Walk(scanDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}
		if strings.Contains(path, ".git") || strings.Contains(path, "vendor") || strings.Contains(path, "node_modules") {
			return nil
		}
		base := filepath.Base(path)
		if base != "." && base != "/" {
			modSet[base] = true
		}
		return nil
	})
	assessment.Modules = len(modSet)

	if data, err := os.ReadFile(filepath.Join(scanDir, "go.mod")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "github.com/") || strings.Contains(line, "golang.org/") {
				assessment.Dependencies++
			}
		}
	}

	score := 0.0
	score += float64(assessment.Files) * 0.1
	score += float64(assessment.Modules) * 2.0
	score += float64(assessment.Dependencies) * 0.5
	assessment.Score = score

	switch {
	case score < 10:
		assessment.Level = 0
		assessment.Track = "quick"
	case score < 25:
		assessment.Level = 1
		assessment.Track = "quick"
	case score < 50:
		assessment.Level = 2
		assessment.Track = "standard"
	case score < 100:
		assessment.Level = 3
		assessment.Track = "standard"
	default:
		assessment.Level = 4
		assessment.Track = "enterprise"
	}

	if assessment.Files > 100 {
		assessment.RiskFactors = append(assessment.RiskFactors, "Large codebase — consider phased delivery")
	}
	if assessment.Dependencies > 20 {
		assessment.RiskFactors = append(assessment.RiskFactors, "High dependency count — verify compatibility")
	}
	if assessment.Modules > 10 {
		assessment.RiskFactors = append(assessment.RiskFactors, "Many modules — ensure clear interfaces")
	}

	return assessment
}

func init() {
	_ = SpecScaleTool{}
}
