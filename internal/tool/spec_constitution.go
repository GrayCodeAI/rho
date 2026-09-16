package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConstitutionTool manages the project's constitution — a set of
// non-negotiable rules and constraints that all specs and implementations
// must follow. The constitution is stored in .rho/specs/constitution.md
// and referenced during spec validation.
type ConstitutionTool struct{}

func (ConstitutionTool) Name() string { return "Constitution" }

// ConstitutionInput is the typed input for ConstitutionTool.
type ConstitutionInput struct {
	Action string `json:"action"`
	Rules  string `json:"rules"`
}

func (ConstitutionTool) Aliases() []string {
	return []string{"constitution", "spec_constitution", "spec:constitution"}
}

func (ConstitutionTool) Description() string {
	return "Read or update the project constitution — non-negotiable rules that all specs must follow. Use 'get' to read, 'set' to update, 'init' to create from template, 'validate' to check a spec against constitution rules."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (ConstitutionTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action": {Type: "string", Enum: []interface{}{"get", "set", "init", "validate"}, Description: "Action to perform: get (read), set (update rules), init (create from template), validate (check spec against rules)"},
			"rules":  {Type: "string", Description: "Constitution rules content (required for 'set' action)"},
		},

		Required: []string{"action"},
	}
}

func (ConstitutionTool) Parameters() map[string]interface{} {
	return constitutionSchema.ToJSONSchema()
}

// constitutionSchema is the single source of truth for Constitution's input schema.
var constitutionSchema = ConstitutionTool{}.Schema()

func (ConstitutionTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[ConstitutionInput]("Constitution", input)
	if err != nil {
		return "", err
	}

	dir, err := specDir(ctx)
	if err != nil {
		return "", err
	}

	constitutionPath := filepath.Join(dir, "constitution.md")

	switch p.Action {
	case "get":
		return getConstitution(constitutionPath)

	case "init":
		return initConstitution(constitutionPath)

	case "set":
		if strings.TrimSpace(p.Rules) == "" {
			return "", fmt.Errorf("rules content is required for 'set' action")
		}
		return setConstitution(constitutionPath, p.Rules)

	case "validate":
		return validateAgainstConstitution(ctx, dir, constitutionPath)

	case "gates":
		return getPhaseGates()

	default:
		return "", fmt.Errorf("unknown action %q. Use 'get', 'set', 'init', 'validate', or 'gates'", p.Action)
	}
}

type PhaseGate struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Checks      []string `json:"checks"`
}

func getPhaseGates() (string, error) {
	gates := []PhaseGate{
		{
			Name:        "Simplicity Gate",
			Description: "Keep the implementation minimal and focused",
			Checks: []string{
				"Using ≤3 projects/modules for initial implementation?",
				"No future-proofing or speculative features?",
				"Each component has a single, clear responsibility?",
			},
		},
		{
			Name:        "Anti-Abstraction Gate",
			Description: "Use frameworks directly, avoid premature abstraction",
			Checks: []string{
				"Using framework features directly rather than wrapping them?",
				"Single model representation (no redundant interfaces)?",
				"No abstract base classes unless shared by 3+ implementations?",
			},
		},
		{
			Name:        "Integration-First Gate",
			Description: "Define contracts before implementation",
			Checks: []string{
				"API contracts defined before implementation?",
				"Contract tests written before handler code?",
				"Prefer real dependencies over mocks where feasible?",
			},
		},
		{
			Name:        "Test-First Gate",
			Description: "Tests written before or alongside implementation",
			Checks: []string{
				"Unit tests written for each requirement?",
				"Tests confirmed to fail before implementation (Red phase)?",
				"Integration tests for cross-component behavior?",
			},
		},
	}
	var b strings.Builder
	b.WriteString("## Phase Gates (checked at Plan transition)\n\n")
	b.WriteString("All gates must pass before advancing to Plan. Failures require documented justification.\n\n")
	for _, g := range gates {
		fmt.Fprintf(&b, "### %s\n", g.Name)
		fmt.Fprintf(&b, "%s\n\n", g.Description)
		for _, c := range g.Checks {
			fmt.Fprintf(&b, "- [ ] %s\n", c)
		}
		b.WriteString("\n")
	}
	b.WriteString("### Complexity Tracking\n\n")
	b.WriteString("For any gate that fails, document the justification here:\n\n")
	b.WriteString("| Gate | Justification |\n")
	b.WriteString("|------|---------------|\n")
	b.WriteString("|      |               |\n")
	return strings.TrimSpace(b.String()), nil
}

func getConstitution(path string) (string, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return "No constitution found. Use action='init' to create one from template.", nil
	}
	return fmt.Sprintf("# Project Constitution\n\n%s", string(data)), nil
}

func initConstitution(path string) (string, error) {
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("constitution already exists at %s — use 'set' to update", path)
	}

	template := "## Constitution\n\n" +
		"### Article I: Library-First\n" +
		"Every feature starts as a standalone library. Prefer well-maintained libraries over custom implementations. " +
		"Justify any custom implementation in Complexity Tracking.\n\n" +
		"### Article II: CLI Interface\n" +
		"Every library must expose a command-line interface for observability and scriptability.\n\n" +
		"### Article III: Test-First Imperative\n" +
		"NON-NEGOTIABLE: All implementation follows strict Test-Driven Development. " +
		"No implementation code before: (1) unit tests written, (2) tests confirmed to FAIL (Red phase).\n\n" +
		"### Article IV: Explicit Over Implicit\n" +
		"Use SHALL/MUST for normative requirements. No vague language. " +
		"Ambiguity is marked with [NEEDS CLARIFICATION] instead of guessing.\n\n" +
		"### Article V: Security First\n" +
		"Never expose secrets, never trust user input, always validate. " +
		"API keys in OS keychain, not config files.\n\n" +
		"### Article VI: Observability Over Opacity\n" +
		"All functionality must be inspectable through CLI interfaces. Structured logging, not fmt.Print.\n\n" +
		"### Article VII: Simplicity\n" +
		"Maximum 3 projects for initial implementation. The simplest solution that meets requirements wins. " +
		"No future-proofing. No speculative features.\n\n" +
		"### Article VIII: Anti-Abstraction\n" +
		"Use framework features directly. No premature abstraction. " +
		"Abstract only when shared by 3+ implementations. Document any exception.\n\n" +
		"### Article IX: Integration-First Testing\n" +
		"Tests in realistic environments. Prefer real databases over mocks. " +
		"Prefer actual service instances over stubs. Contract tests mandatory before implementation.\n\n" +
		"## Review Requirements\n\n" +
		"- All PRs must have at least one approval\n" +
		"- Security-sensitive changes require security review\n" +
		"- Performance changes require benchmarks\n" +
		"- Breaking changes require migration plan\n"

	if err := os.WriteFile(path, []byte(template), 0o600); err != nil {
		return "", fmt.Errorf("write constitution: %w", err)
	}
	return fmt.Sprintf("Created constitution at %s\n\n%s", path, template), nil
}

func setConstitution(path, rules string) (string, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}

	if err := os.WriteFile(path, []byte(rules), 0o600); err != nil {
		return "", fmt.Errorf("write constitution: %w", err)
	}
	return fmt.Sprintf("Updated constitution at %s", path), nil
}

func validateAgainstConstitution(ctx context.Context, specDir, constitutionPath string) (string, error) {
	// Load constitution
	constData, err := os.ReadFile(constitutionPath) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return "No constitution found — skipping validation. Use action='init' to create one.", nil
	}

	// Load spec
	specData, err := os.ReadFile(filepath.Join(specDir, "spec.md")) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return "", fmt.Errorf("no spec.md found — write a spec first")
	}

	specContent := string(specData)
	constContent := string(constData)
	var issues []string

	// Check for security rules
	if strings.Contains(constContent, "Security First") || strings.Contains(constContent, "security") {
		if strings.Contains(specContent, "password") || strings.Contains(specContent, "secret") || strings.Contains(specContent, "api key") {
			issues = append(issues, "Security: Spec mentions sensitive data — ensure proper handling is specified")
		}
	}

	// Check for SHALL/MUST requirements
	if strings.Contains(constContent, "SHALL/MUST") || strings.Contains(constContent, "normative") {
		reqs := extractRequirementsFromContent(specContent)
		if len(reqs) > 0 {
			// Check if the spec content as a whole uses SHALL/MUST
			// (requirement headers don't contain SHALL/MUST — it's in the body)
			hasShallMust := strings.Contains(specContent, "SHALL") || strings.Contains(specContent, "MUST")
			if !hasShallMust {
				issues = append(issues, fmt.Sprintf("Normative: %d requirement(s) lack SHALL/MUST keywords", len(reqs)))
			}
		}
	}

	// Check for testability
	if strings.Contains(constContent, "Testable Everything") || strings.Contains(constContent, "test scenario") {
		scenarios := extractScenariosFromContent(specContent)
		reqs := extractRequirementsFromContent(specContent)
		if len(reqs) > 0 && len(scenarios) == 0 {
			issues = append(issues, "Testability: No test scenarios found — every requirement needs at least one scenario")
		}
	}

	if len(issues) == 0 {
		return fmt.Sprintf("+ Spec passes constitution validation (%d rules checked)", countConstitutionRules(constContent)), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Constitution validation found %d issue(s):\n\n", len(issues))
	for i, iss := range issues {
		fmt.Fprintf(&b, "%d. %s\n", i+1, iss)
	}

	return strings.TrimSpace(b.String()), nil
}

func extractRequirementsFromContent(content string) []string {
	var reqs []string
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### Requirement:") || strings.HasPrefix(trimmed, "## Requirement:") {
			reqs = append(reqs, trimmed)
		}
	}
	return reqs
}

func extractScenariosFromContent(content string) []string {
	var scenarios []string
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#### Scenario:") || strings.HasPrefix(trimmed, "### Scenario:") {
			scenarios = append(scenarios, trimmed)
		}
	}
	return scenarios
}

func countConstitutionRules(content string) int {
	count := 0
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 2 && (strings.HasPrefix(trimmed, "1.") || strings.HasPrefix(trimmed, "2.") ||
			strings.HasPrefix(trimmed, "3.") || strings.HasPrefix(trimmed, "4.") || strings.HasPrefix(trimmed, "5.") ||
			strings.HasPrefix(trimmed, "6.") || strings.HasPrefix(trimmed, "7.") || strings.HasPrefix(trimmed, "8.") ||
			strings.HasPrefix(trimmed, "9.")) {
			count++
		}
	}
	return count
}
