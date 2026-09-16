package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"text/template"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// CodeGenerator manages code generation templates and renders them on demand.
type CodeGenerator struct {
	Templates map[string]*CodeTemplate
	Language  string
	mu        sync.RWMutex
}

// CodeTemplate defines a single code generation template.
type CodeTemplate struct {
	Name        string
	Description string
	Language    string
	Template    string
	Variables   []TemplateVar
	Output      string
}

// TemplateVar describes a variable used in a template.
type TemplateVar struct {
	Name        string
	Description string
	Required    bool
	Default     string
}

// NewCodeGenerator creates a CodeGenerator pre-loaded with built-in templates.
func NewCodeGenerator() *CodeGenerator {
	cg := &CodeGenerator{
		Templates: make(map[string]*CodeTemplate),
		Language:  "go",
	}
	cg.registerBuiltins()
	return cg
}

// Generate renders a template with the given variables.
func (cg *CodeGenerator) Generate(templateName string, vars map[string]string) (string, error) {
	cg.mu.RLock()
	tmpl, ok := cg.Templates[templateName]
	cg.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("template %q not found", templateName)
	}

	// Apply defaults for missing non-required variables
	resolved := make(map[string]string)
	for _, v := range tmpl.Variables {
		if val, exists := vars[v.Name]; exists && val != "" {
			resolved[v.Name] = val
		} else if v.Required && v.Default == "" {
			return "", fmt.Errorf("required variable %q not provided for template %q", v.Name, templateName)
		} else if v.Default != "" {
			resolved[v.Name] = v.Default
		} else {
			resolved[v.Name] = ""
		}
	}

	// Also include any extra vars passed in
	for k, v := range vars {
		if _, exists := resolved[k]; !exists {
			resolved[k] = v
		}
	}

	funcMap := template.FuncMap{
		"lower": strings.ToLower,
		"upper": strings.ToUpper,
		"title": cases.Title(language.English).String,
	}

	t, err := template.New(templateName).Funcs(funcMap).Parse(tmpl.Template)
	if err != nil {
		return "", fmt.Errorf("parsing template %q: %w", templateName, err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, resolved); err != nil {
		return "", fmt.Errorf("executing template %q: %w", templateName, err)
	}

	return buf.String(), nil
}

// ListTemplates returns templates filtered by language.
// If language is empty, all templates are returned.
func (cg *CodeGenerator) ListTemplates(language string) []*CodeTemplate {
	cg.mu.RLock()
	defer cg.mu.RUnlock()

	var result []*CodeTemplate
	for _, tmpl := range cg.Templates {
		if language == "" || strings.EqualFold(tmpl.Language, language) {
			result = append(result, tmpl)
		}
	}
	return result
}

// Register adds a custom template to the generator.
func (cg *CodeGenerator) Register(template *CodeTemplate) {
	cg.mu.Lock()
	defer cg.mu.Unlock()
	cg.Templates[template.Name] = template
}

// Preview shows what would be generated without performing full rendering.
// It shows the template with variable placeholders highlighted.
func (cg *CodeGenerator) Preview(templateName string, vars map[string]string) string {
	cg.mu.RLock()
	tmpl, ok := cg.Templates[templateName]
	cg.mu.RUnlock()
	if !ok {
		return fmt.Sprintf("template %q not found", templateName)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Template: %s\n", tmpl.Name))
	sb.WriteString(fmt.Sprintf("Language: %s\n", tmpl.Language))
	sb.WriteString(fmt.Sprintf("Description: %s\n", tmpl.Description))
	sb.WriteString("\nVariables:\n")
	for _, v := range tmpl.Variables {
		value := v.Default
		if val, exists := vars[v.Name]; exists {
			value = val
		}
		required := ""
		if v.Required {
			required = " (required)"
		}
		sb.WriteString(fmt.Sprintf("  %s = %q%s\n", v.Name, value, required))
	}
	sb.WriteString("\n--- Generated Preview ---\n")

	// Attempt to render
	output, err := cg.Generate(templateName, vars)
	if err != nil {
		sb.WriteString(fmt.Sprintf("Error: %v\n", err))
	} else {
		sb.WriteString(output)
	}

	return sb.String()
}

// SuggestTemplate suggests the best template given a natural language description.
func (cg *CodeGenerator) SuggestTemplate(description string) string {
	desc := strings.ToLower(description)

	// Keyword matching rules with weights.
	// More specific (language-prefixed) rules use higher weights so they win ties.
	type rule struct {
		keywords []string
		weights  []int
		template string
	}

	rules := []rule{
		// Python templates (high weight so they beat generic Go matches)
		{keywords: []string{"fastapi", "pydantic", "python api", "python endpoint"}, weights: []int{10, 10, 10, 10}, template: "py-fastapi-endpoint"},
		{keywords: []string{"pytest", "python test"}, weights: []int{10, 10}, template: "py-test-class"},
		{keywords: []string{"dataclass", "python model", "python class"}, weights: []int{10, 10, 10}, template: "py-dataclass"},

		// TypeScript templates (high weight)
		{keywords: []string{"react", "component", "jsx", "tsx"}, weights: []int{10, 5, 10, 10}, template: "ts-react-component"},
		{keywords: []string{"express", "node router", "node api"}, weights: []int{10, 10, 10}, template: "ts-express-router"},
		{keywords: []string{"jest", "vitest", "typescript test", "ts test"}, weights: []int{10, 10, 10, 10}, template: "ts-test-describe"},

		// Go templates (standard weight)
		{keywords: []string{"middleware"}, weights: []int{5}, template: "go-middleware"},
		{keywords: []string{"crud", "create read update delete", "resource operations"}, weights: []int{5, 5, 5}, template: "go-crud"},
		{keywords: []string{"test", "testing", "unit test", "add tests"}, weights: []int{3, 3, 5, 5}, template: "go-test-table"},
		{keywords: []string{"interface", "mock", "abstraction"}, weights: []int{5, 5, 5}, template: "go-interface"},
		{keywords: []string{"error", "custom error", "error type"}, weights: []int{3, 5, 5}, template: "go-errors"},
		{keywords: []string{"config", "configuration", "environment", "env var"}, weights: []int{5, 5, 5, 5}, template: "go-config"},
		{keywords: []string{"handler", "endpoint", "api endpoint", "route", "http"}, weights: []int{3, 3, 5, 3, 3}, template: "go-handler"},
	}

	// Score each rule by summing weights of matched keywords
	bestTemplate := "go-handler"
	bestScore := 0

	for _, r := range rules {
		score := 0
		for i, kw := range r.keywords {
			if strings.Contains(desc, kw) {
				score += r.weights[i]
			}
		}
		if score > bestScore {
			bestScore = score
			bestTemplate = r.template
		}
	}

	return bestTemplate
}

// CodeGenTool implements the Tool interface for code generation.
type CodeGenTool struct {
	generator *CodeGenerator
}

// NewCodeGenTool creates a new CodeGenTool.
func NewCodeGenTool() *CodeGenTool {
	return &CodeGenTool{generator: NewCodeGenerator()}
}

func (t *CodeGenTool) Name() string {
	return "CodeGen"
}

func (t *CodeGenTool) Description() string {
	return "Generate common code patterns from templates. Supports Go, Python, and TypeScript templates for handlers, tests, middleware, CRUD operations, and more."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (t *CodeGenTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action":      {Type: "string", Enum: []interface{}{"generate", "list", "preview", "suggest"}, Description: "Action to perform"},
			"template":    {Type: "string", Description: "Template name (e.g., go-handler, py-fastapi-endpoint)"},
			"variables":   {Type: "object", Description: "Template variables as key-value pairs"},
			"language":    {Type: "string", Description: "Filter templates by language (go, python, typescript)"},
			"description": {Type: "string", Description: "Natural language description for template suggestion"},
		},
		Required: []string{"action"},
	}
}

func (t *CodeGenTool) Parameters() map[string]interface{} {
	return codeGenSchema.ToJSONSchema()
}

// codeGenSchema is the single source of truth for CodeGen's input schema.
var codeGenSchema = (&CodeGenTool{}).Schema()

// CodeGenInput is the typed input for CodeGenTool.
type CodeGenInput struct {
	Action      string            `json:"action"`
	Template    string            `json:"template"`
	Variables   map[string]string `json:"variables"`
	Language    string            `json:"language"`
	Description string            `json:"description"`
}

// codeGenInput is an unexported alias for backward compatibility with tests.
type codeGenInput = CodeGenInput

func (t *CodeGenTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	in, err := DecodeInput[CodeGenInput]("CodeGen", input)
	if err != nil {
		return "", err
	}
	switch in.Action {
	case "generate":
		if in.Template == "" {
			return "", fmt.Errorf("template name is required for generate action")
		}
		code, err := t.generator.Generate(in.Template, in.Variables)
		if err != nil {
			return "", err
		}
		return code, nil

	case "list":
		templates := t.generator.ListTemplates(in.Language)
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Available templates (%d):\n\n", len(templates)))
		for _, tmpl := range templates {
			sb.WriteString(fmt.Sprintf("  %s [%s]\n    %s\n", tmpl.Name, tmpl.Language, tmpl.Description))
			for _, v := range tmpl.Variables {
				req := ""
				if v.Required {
					req = " *"
				}
				def := ""
				if v.Default != "" {
					def = fmt.Sprintf(" (default: %s)", v.Default)
				}
				sb.WriteString(fmt.Sprintf("    - %s: %s%s%s\n", v.Name, v.Description, req, def))
			}
			sb.WriteString("\n")
		}
		return sb.String(), nil

	case "preview":
		if in.Template == "" {
			return "", fmt.Errorf("template name is required for preview action")
		}
		return t.generator.Preview(in.Template, in.Variables), nil

	case "suggest":
		if in.Description == "" {
			return "", fmt.Errorf("description is required for suggest action")
		}
		suggested := t.generator.SuggestTemplate(in.Description)
		return fmt.Sprintf("Suggested template: %s", suggested), nil

	default:
		return "", fmt.Errorf("unknown action: %s (valid: generate, list, preview, suggest)", in.Action)
	}
}

// Generator returns the underlying CodeGenerator for direct use.
func (t *CodeGenTool) Generator() *CodeGenerator {
	return t.generator
}
