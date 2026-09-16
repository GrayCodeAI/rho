package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/GrayCodeAI/rho/internal/spec"
)

type SpecPlanVariationsTool struct{}

func (SpecPlanVariationsTool) Name() string { return "SpecPlanVariations" }

// SpecPlanVariationsInput is the typed input for SpecPlanVariationsTool.
type SpecPlanVariationsInput struct {
	Count    int `json:"count"`
	Selected int `json:"selected"`
}

func (SpecPlanVariationsTool) Aliases() []string {
	return []string{"spec_plan_variations", "spec:plan_variations"}
}

func (SpecPlanVariationsTool) Description() string {
	return "Generate multiple implementation plan variations for the active spec. Each variation emphasizes different tradeoffs: performance, simplicity, maintainability, or speed. Outputs a comparison matrix so the agent can choose the best approach."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecPlanVariationsTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"count":    {Type: "integer", Description: "Number of variations to generate (2-4, default 3)"},
			"selected": {Type: "integer", Description: "Select a variation by number (1-based) to write as plan.md"},
		},
	}
}

func (SpecPlanVariationsTool) Parameters() map[string]interface{} {
	return specPlanVariationsSchema.ToJSONSchema()
}

// specPlanVariationsSchema is the single source of truth for SpecPlanVariations's input schema.
var specPlanVariationsSchema = SpecPlanVariationsTool{}.Schema()

func (SpecPlanVariationsTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var p SpecPlanVariationsInput
	if len(input) > 0 && string(input) != "null" {
		decoded, err := DecodeInput[SpecPlanVariationsInput]("SpecPlanVariations", input)
		if err != nil {
			return "", err
		}
		p = decoded
	}
	if p.Count < 2 || p.Count > 4 {
		p.Count = 3
	}

	dir, err := specDir(ctx)
	if err != nil {
		return "", err
	}

	specContent := readFileStr(filepath.Join(dir, "spec.md"))
	reqs := spec.ExtractReqIDs(specContent)

	var b strings.Builder
	b.WriteString("## Plan Variations\n\n")
	fmt.Fprintf(&b, "Generated %d implementation approaches for %d requirements:\n\n", p.Count, len(reqs))

	variations := []struct {
		name    string
		focus   string
		pros    string
		cons    string
		risk    string
		complex string
	}{
		{
			name:    "Performance-First",
			focus:   "Optimize for throughput and latency",
			pros:    "Fastest execution, best resource utilization",
			cons:    "Higher complexity, harder to maintain",
			risk:    "Medium - performance targets may not be met",
			complex: "High",
		},
		{
			name:    "Simplicity-First",
			focus:   "Minimize complexity and maximize readability",
			pros:    "Easy to understand, maintain, and extend",
			cons:    "May not meet aggressive performance targets",
			risk:    "Low - straightforward implementation",
			complex: "Low",
		},
		{
			name:    "Maintainability-First",
			focus:   "Strong interfaces, comprehensive tests, clear documentation",
			pros:    "Long-term sustainability, easy onboarding",
			cons:    "Slower initial delivery",
			risk:    "Low - investment in quality",
			complex: "Medium",
		},
		{
			name:    "Speed-First",
			focus:   "Fastest time to working implementation",
			pros:    "Quickest delivery, early feedback",
			cons:    "May accrue technical debt",
			risk:    "High - may need rework",
			complex: "Low",
		},
	}

	for i, v := range variations {
		if i >= p.Count {
			break
		}
		fmt.Fprintf(&b, "### Variation %d: %s\n\n", i+1, v.name)
		fmt.Fprintf(&b, "- **Focus**: %s\n", v.focus)
		fmt.Fprintf(&b, "- **Pros**: %s\n", v.pros)
		fmt.Fprintf(&b, "- **Cons**: %s\n", v.cons)
		fmt.Fprintf(&b, "- **Risk**: %s\n", v.risk)
		fmt.Fprintf(&b, "- **Complexity**: %s\n\n", v.complex)
	}

	b.WriteString("### Comparison Matrix\n\n")
	b.WriteString("| Criteria |")
	for i := range variations {
		if i >= p.Count {
			break
		}
		fmt.Fprintf(&b, " Var %d |", i+1)
	}
	b.WriteString("\n|----------|")
	for i := 0; i < p.Count; i++ {
		b.WriteString("-------|")
	}
	b.WriteString("\n| Performance |")
	for i := 0; i < p.Count; i++ {
		switch variations[i].name {
		case "Performance-First":
			b.WriteString(" ***** |")
		case "Simplicity-First":
			b.WriteString(" ***   |")
		case "Maintainability-First":
			b.WriteString(" ****  |")
		case "Speed-First":
			b.WriteString(" **    |")
		}
	}
	b.WriteString("\n| Simplicity |")
	for i := 0; i < p.Count; i++ {
		switch variations[i].name {
		case "Performance-First":
			b.WriteString(" **    |")
		case "Simplicity-First":
			b.WriteString(" ***** |")
		case "Maintainability-First":
			b.WriteString(" ****  |")
		case "Speed-First":
			b.WriteString(" ****  |")
		}
	}
	b.WriteString("\n| Maintainability |")
	for i := 0; i < p.Count; i++ {
		switch variations[i].name {
		case "Performance-First":
			b.WriteString(" **    |")
		case "Simplicity-First":
			b.WriteString(" ****  |")
		case "Maintainability-First":
			b.WriteString(" ***** |")
		case "Speed-First":
			b.WriteString(" **    |")
		}
	}
	b.WriteString("\n| Speed to Deliver |")
	for i := 0; i < p.Count; i++ {
		switch variations[i].name {
		case "Performance-First":
			b.WriteString(" **    |")
		case "Simplicity-First":
			b.WriteString(" ***   |")
		case "Maintainability-First":
			b.WriteString(" **    |")
		case "Speed-First":
			b.WriteString(" ***** |")
		}
	}
	b.WriteString("\n\n")

	if p.Selected > 0 && p.Selected <= p.Count {
		selected := variations[p.Selected-1]
		b.WriteString(fmt.Sprintf("### Selected: Variation %d (%s)\n\n", p.Selected, selected.name))
		b.WriteString("Use the Plan tool to write the detailed implementation plan for this variation.\n")
	}

	return strings.TrimSpace(b.String()), nil
}

func init() {
	_ = SpecPlanVariationsTool{}
}
