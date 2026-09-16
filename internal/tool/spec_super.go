package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type SpecSuperTool struct{}

func (SpecSuperTool) Name() string { return "SpecSuper" }

// SpecSuperInput is the typed input for SpecSuperTool.
type SpecSuperInput struct {
	ScanDir  string `json:"scan_dir"`
	Language string `json:"language"`
}

func (SpecSuperTool) Aliases() []string {
	return []string{"spec_super", "spec:super"}
}

func (SpecSuperTool) Description() string {
	return "Evaluate codebase against S.U.P.E.R architectural principles: Single Purpose, Unidirectional Flow, Ports over Implementation, Environment-Agnostic, Replaceable Parts. Returns per-dimension scores and actionable recommendations."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecSuperTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"scan_dir": {Type: "string", Description: "Directory to scan (default: current directory)"},
			"language": {Type: "string", Description: "Language to analyze: go, ts, py (default: auto-detect)"},
		},
	}
}

func (SpecSuperTool) Parameters() map[string]interface{} {
	return specSuperSchema.ToJSONSchema()
}

// specSuperSchema is the single source of truth for SpecSuper's input schema.
var specSuperSchema = SpecSuperTool{}.Schema()

type SuperScore struct {
	SinglePurpose       float64 `json:"single_purpose"`
	Unidirectional      float64 `json:"unidirectional"`
	PortsOverImpl       float64 `json:"ports_over_implementation"`
	EnvironmentAgnostic float64 `json:"environment_agnostic"`
	Replaceable         float64 `json:"replaceable"`
	Overall             float64 `json:"overall"`
}

type SuperFinding struct {
	Principle string `json:"principle"`
	Severity  string `json:"severity"`
	File      string `json:"file"`
	Message   string `json:"message"`
}

func (SpecSuperTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var p SpecSuperInput
	if len(input) > 0 && string(input) != "null" {
		decoded, err := DecodeInput[SpecSuperInput]("SpecSuper", input)
		if err != nil {
			return "", err
		}
		p = decoded
	}
	if p.ScanDir == "" {
		p.ScanDir, _ = os.Getwd()
	}
	if p.Language == "" {
		p.Language = detectSuperLanguage(p.ScanDir)
	}

	var score SuperScore
	var findings []SuperFinding

	switch p.Language {
	case "go":
		score, findings = analyzeGoCodebase(p.ScanDir)
	default:
		score, findings = analyzeGenericCodebase(p.ScanDir)
	}

	score.Overall = (score.SinglePurpose + score.Unidirectional + score.PortsOverImpl + score.EnvironmentAgnostic + score.Replaceable) / 5.0

	var b strings.Builder
	fmt.Fprintf(&b, "## S.U.P.E.R Architecture Health\n\n")
	fmt.Fprintf(&b, "| Principle | Score | Status |\n")
	fmt.Fprintf(&b, "|-----------|-------|--------|\n")
	fmt.Fprintf(&b, "| Single Purpose (S) | %.0f%% | %s |\n", score.SinglePurpose*100, statusEmoji(score.SinglePurpose))
	fmt.Fprintf(&b, "| Unidirectional Flow (U) | %.0f%% | %s |\n", score.Unidirectional*100, statusEmoji(score.Unidirectional))
	fmt.Fprintf(&b, "| Ports over Implementation (P) | %.0f%% | %s |\n", score.PortsOverImpl*100, statusEmoji(score.PortsOverImpl))
	fmt.Fprintf(&b, "| Environment-Agnostic (E) | %.0f%% | %s |\n", score.EnvironmentAgnostic*100, statusEmoji(score.EnvironmentAgnostic))
	fmt.Fprintf(&b, "| Replaceable Parts (R) | %.0f%% | %s |\n", score.Replaceable*100, statusEmoji(score.Replaceable))
	fmt.Fprintf(&b, "| **Overall** | **%.0f%%** | **%s** |\n\n", score.Overall*100, statusEmoji(score.Overall))

	if len(findings) > 0 {
		fmt.Fprintf(&b, "### Findings (%d)\n\n", len(findings))
		for _, f := range findings {
			fmt.Fprintf(&b, "- %s [%s] `%s`: %s\n", severityEmoji(f.Severity), f.Principle, f.File, f.Message)
		}
		b.WriteString("\n")
	}

	b.WriteString("### Recommendations\n\n")
	recommendations := generateRecommendations(score, findings)
	for _, r := range recommendations {
		fmt.Fprintf(&b, "- %s\n", r)
	}

	return strings.TrimSpace(b.String()), nil
}

func statusEmoji(score float64) string {
	switch {
	case score >= 0.8:
		return "STRONG"
	case score >= 0.6:
		return "GOOD"
	case score >= 0.4:
		return "FAIR"
	default:
		return "WEAK"
	}
}

func severityEmoji(severity string) string {
	switch severity {
	case "critical":
		return "CRITICAL"
	case "warning":
		return "WARNING"
	default:
		return "INFO"
	}
}

func detectSuperLanguage(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return "go"
	}
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
		return "ts"
	}
	if _, err := os.Stat(filepath.Join(dir, "requirements.txt")); err == nil {
		return "py"
	}
	return "unknown"
}

func analyzeGoCodebase(dir string) (SuperScore, []SuperFinding) {
	var score SuperScore
	var findings []SuperFinding

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(info os.FileInfo) bool {
		return !strings.Contains(info.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		return score, findings
	}

	totalFiles := 0
	exportedFuncs := 0
	imports := make(map[string]int)
	interfaceCount := 0
	hardcodedValues := 0

	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			totalFiles++

			for _, decl := range f.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok {
					if fn.Name.IsExported() {
						exportedFuncs++
					}
					if fn.Recv != nil && len(fn.Type.Params.List) > 5 {
						findings = append(findings, SuperFinding{
							Principle: "S",
							Severity:  "warning",
							File:      fset.Position(fn.Pos()).Filename,
							Message:   fmt.Sprintf("Function %s has %d parameters — consider reducing", fn.Name.Name, len(fn.Type.Params.List)),
						})
					}
				}
			}

			for _, imp := range f.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				imports[path]++
			}

			for _, decl := range f.Decls {
				if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.TYPE {
					for _, spec := range gen.Specs {
						if ts, ok := spec.(*ast.TypeSpec); ok {
							if _, ok := ts.Type.(*ast.InterfaceType); ok {
								interfaceCount++
							}
						}
					}
				}
			}

			for _, cg := range f.Comments {
				for _, c := range cg.List {
					text := c.Text
					if strings.Contains(text, "http://") || strings.Contains(text, "localhost") {
						hardcodedValues++
					}
				}
			}
		}
	}

	if totalFiles > 0 {
		avgFuncs := float64(exportedFuncs) / float64(totalFiles)
		if avgFuncs <= 5 {
			score.SinglePurpose = 0.9
		} else if avgFuncs <= 10 {
			score.SinglePurpose = 0.7
		} else {
			score.SinglePurpose = 0.4
			findings = append(findings, SuperFinding{
				Principle: "S",
				Severity:  "warning",
				File:      dir,
				Message:   fmt.Sprintf("High average functions per file (%.1f) — consider splitting", avgFuncs),
			})
		}
	} else {
		score.SinglePurpose = 0.5
	}

	if len(imports) > 0 {
		maxImports := 0
		for _, count := range imports {
			if count > maxImports {
				maxImports = count
			}
		}
		if maxImports > 10 {
			score.Unidirectional = 0.5
			findings = append(findings, SuperFinding{
				Principle: "U",
				Severity:  "warning",
				File:      dir,
				Message:   fmt.Sprintf("Most imported package used %d times — possible circular dependency", maxImports),
			})
		} else {
			score.Unidirectional = 0.85
		}
	} else {
		score.Unidirectional = 0.5
	}

	if exportedFuncs > 0 {
		interfaceRatio := float64(interfaceCount) / float64(exportedFuncs)
		score.PortsOverImpl = math.Min(interfaceRatio*3, 0.95)
		if score.PortsOverImpl < 0.5 {
			findings = append(findings, SuperFinding{
				Principle: "P",
				Severity:  "warning",
				File:      dir,
				Message:   fmt.Sprintf("Low interface ratio (%.0f%%) — define contracts before implementations", interfaceRatio*100),
			})
		}
	} else {
		score.PortsOverImpl = 0.5
	}

	if hardcodedValues == 0 {
		score.EnvironmentAgnostic = 0.9
	} else {
		score.EnvironmentAgnostic = math.Max(0.3, 0.9-float64(hardcodedValues)*0.1)
		findings = append(findings, SuperFinding{
			Principle: "E",
			Severity:  "warning",
			File:      dir,
			Message:   fmt.Sprintf("%d hardcoded URL/endpoint found — use environment variables", hardcodedValues),
		})
	}

	if interfaceCount > 0 {
		score.Replaceable = math.Min(0.5+float64(interfaceCount)*0.1, 0.95)
	} else {
		score.Replaceable = 0.4
		findings = append(findings, SuperFinding{
			Principle: "R",
			Severity:  "warning",
			File:      dir,
			Message:   "No interfaces found — components may be hard to replace",
		})
	}

	return score, findings
}

func analyzeGenericCodebase(dir string) (SuperScore, []SuperFinding) {
	var score SuperScore
	score.SinglePurpose = 0.5
	score.Unidirectional = 0.5
	score.PortsOverImpl = 0.5
	score.EnvironmentAgnostic = 0.5
	score.Replaceable = 0.5

	reHardcoded := regexp.MustCompile(`(http://|localhost|127\.0\.0\.1|password|secret|api[_-]?key)`)

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.Contains(path, ".git") || strings.Contains(path, "vendor") {
			return nil
		}
		if !strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".js") && !strings.HasSuffix(path, ".py") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if reHardcoded.Match(data) {
			score.EnvironmentAgnostic -= 0.05
		}
		return nil
	})

	score.EnvironmentAgnostic = math.Max(0.2, score.EnvironmentAgnostic)
	return score, nil
}

func generateRecommendations(score SuperScore, findings []SuperFinding) []string {
	var recs []string

	if score.SinglePurpose < 0.7 {
		recs = append(recs, "S: Split multi-responsibility modules into focused single-purpose units")
	}
	if score.Unidirectional < 0.7 {
		recs = append(recs, "U: Break circular dependencies — ensure data flows one direction")
	}
	if score.PortsOverImpl < 0.7 {
		recs = append(recs, "P: Define interfaces/contracts before implementing concrete types")
	}
	if score.EnvironmentAgnostic < 0.7 {
		recs = append(recs, "E: Move hardcoded values to environment variables or config files")
	}
	if score.Replaceable < 0.7 {
		recs = append(recs, "R: Introduce interfaces to make components swappable without cascading changes")
	}

	if len(recs) == 0 {
		recs = append(recs, "Architecture health is strong — maintain current practices")
	}

	return recs
}

func init() {
	_ = SpecSuperTool{}
}
