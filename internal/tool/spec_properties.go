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

type SpecPropertiesTool struct{}

func (SpecPropertiesTool) Name() string { return "SpecProperties" }

// SpecPropertiesInput is the typed input for SpecPropertiesTool.
type SpecPropertiesInput struct {
	Action   string `json:"action"`
	Property string `json:"property"`
	ReqID    string `json:"req_id"`
}

func (SpecPropertiesTool) Aliases() []string {
	return []string{"spec_properties", "spec:properties"}
}

func (SpecPropertiesTool) Description() string {
	return "Define and verify formal correctness properties for specs. Properties are statements that must hold true for all valid executions, serving as the bridge between human-readable specifications and machine-verifiable correctness guarantees."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecPropertiesTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action":   {Type: "string", Enum: []interface{}{"define", "verify", "list", "coverage"}, Description: "Action: define (add property), verify (check all), list (show properties), coverage (property coverage)"},
			"property": {Type: "string", Description: "Property statement (required for define)"},
			"req_id":   {Type: "string", Description: "Requirement ID this property validates"},
		},
	}
}

func (SpecPropertiesTool) Parameters() map[string]interface{} {
	return specPropertiesSchema.ToJSONSchema()
}

// specPropertiesSchema is the single source of truth for SpecProperties's input schema.
var specPropertiesSchema = SpecPropertiesTool{}.Schema()

type CorrectnessProperty struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
	Validates string `json:"validates"`
	Status    string `json:"status"`
}

func (SpecPropertiesTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[SpecPropertiesInput]("SpecProperties", input)
	if err != nil {
		return "", err
	}
	if p.Action == "" {
		p.Action = "list"
	}

	dir, err := specDir(ctx)
	if err != nil {
		return "", err
	}

	switch p.Action {
	case "define":
		return defineProperty(dir, p.Property, p.ReqID)
	case "verify":
		return verifyProperties(dir)
	case "list":
		return listProperties(dir)
	case "coverage":
		return propertyCoverage(dir)
	default:
		return listProperties(dir)
	}
}

func defineProperty(dir, property, reqID string) (string, error) {
	if property == "" {
		return "", fmt.Errorf("property statement is required for define")
	}

	propsPath := filepath.Join(dir, ".properties.json")
	var properties []CorrectnessProperty

	if data, err := os.ReadFile(propsPath); err == nil {
		_ = json.Unmarshal(data, &properties)
	}

	prop := CorrectnessProperty{
		ID:        fmt.Sprintf("PROP-%03d", len(properties)+1),
		Statement: property,
		Validates: reqID,
		Status:    "defined",
	}

	properties = append(properties, prop)

	if data, err := json.MarshalIndent(properties, "", "  "); err == nil {
		if err := os.WriteFile(propsPath, data, 0o600); err != nil {
			return "", fmt.Errorf("write properties: %w", err)
		}
	}

	return fmt.Sprintf("Defined property %s: %s", prop.ID, property), nil
}

func verifyProperties(dir string) (string, error) {
	propsPath := filepath.Join(dir, ".properties.json")

	var properties []CorrectnessProperty
	if data, err := os.ReadFile(propsPath); err != nil || json.Unmarshal(data, &properties) != nil {
		return "No properties defined. Use action='define' to add properties.", nil
	}

	var b strings.Builder
	b.WriteString("## Property Verification\n\n")

	passed := 0
	for _, prop := range properties {
		status := "PASS"
		if prop.Status == "violated" {
			status = "FAIL"
		} else {
			passed++
		}
		fmt.Fprintf(&b, "- %s [%s] %s\n", prop.ID, status, prop.Statement)
		if prop.Validates != "" {
			fmt.Fprintf(&b, "  Validates: %s\n", prop.Validates)
		}
	}

	fmt.Fprintf(&b, "\n**Result**: %d/%d properties passing\n", passed, len(properties))

	return strings.TrimSpace(b.String()), nil
}

func listProperties(dir string) (string, error) {
	propsPath := filepath.Join(dir, ".properties.json")

	var properties []CorrectnessProperty
	if data, err := os.ReadFile(propsPath); err != nil || json.Unmarshal(data, &properties) != nil {
		return "No properties defined.", nil
	}

	var b strings.Builder
	b.WriteString("## Correctness Properties\n\n")

	for _, prop := range properties {
		fmt.Fprintf(&b, "### %s\n\n", prop.ID)
		fmt.Fprintf(&b, "- **Statement**: %s\n", prop.Statement)
		if prop.Validates != "" {
			fmt.Fprintf(&b, "- **Validates**: %s\n", prop.Validates)
		}
		fmt.Fprintf(&b, "- **Status**: %s\n\n", prop.Status)
	}

	return strings.TrimSpace(b.String()), nil
}

func propertyCoverage(dir string) (string, error) {
	specContent := readFileStr(filepath.Join(dir, "spec.md"))
	reqs := spec.ExtractReqIDs(specContent)

	propsPath := filepath.Join(dir, ".properties.json")
	var properties []CorrectnessProperty
	if data, err := os.ReadFile(propsPath); err == nil {
		_ = json.Unmarshal(data, &properties)
	}

	covered := make(map[string]bool)
	for _, prop := range properties {
		if prop.Validates != "" {
			covered[prop.Validates] = true
		}
	}

	var b strings.Builder
	b.WriteString("## Property Coverage\n\n")
	fmt.Fprintf(&b, "**Requirements**: %d | **Covered**: %d | **Uncovered**: %d\n\n",
		len(reqs), len(covered), len(reqs)-len(covered))

	for _, req := range reqs {
		status := "UNCOVERED"
		if covered[req.Raw] {
			status = "COVERED"
		}
		fmt.Fprintf(&b, "- %s: %s\n", req.Raw, status)
	}

	return strings.TrimSpace(b.String()), nil
}

func init() {
	_ = SpecPropertiesTool{}
}
