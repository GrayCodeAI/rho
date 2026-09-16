package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GrayCodeAI/rho/internal/storage"
	"gopkg.in/yaml.v3"
)

// WorkflowDef defines a scripted workflow.
type WorkflowDef struct {
	Name        string         `yaml:"name" json:"name"`
	Description string         `yaml:"description" json:"description"`
	Steps       []WorkflowStep `yaml:"steps" json:"steps"`
}

// WorkflowStep is a single step in a workflow.
type WorkflowStep struct {
	Name    string `yaml:"name" json:"name"`
	Prompt  string `yaml:"prompt" json:"prompt"`
	Tool    string `yaml:"tool" json:"tool"`
	Input   string `yaml:"input" json:"input"`
	OnError string `yaml:"on_error" json:"on_error"`
}

// WorkflowTool executes scripted workflows.
type WorkflowTool struct{}

// WorkflowInput is the typed input for WorkflowTool.
type WorkflowInput struct {
	Workflow string         `json:"workflow"`
	Args     map[string]any `json:"args"`
}

func (WorkflowTool) Name() string      { return "Workflow" }
func (WorkflowTool) Aliases() []string { return []string{"workflow"} }
func (WorkflowTool) Description() string {
	return "Execute a scripted workflow"
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (WorkflowTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"workflow": {Type: "string", Description: "Name of the workflow to execute"},
			"args":     {Type: "object", Description: "Arguments to pass to the workflow"},
		},
		Required: []string{"workflow"},
	}
}

func (WorkflowTool) Parameters() map[string]interface{} {
	return workflowSchema.ToJSONSchema()
}

// workflowSchema is the single source of truth for Workflow's input schema.
var workflowSchema = WorkflowTool{}.Schema()

func (WorkflowTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[WorkflowInput]("Workflow", input)
	if err != nil {
		return "", err
	}
	if p.Workflow == "" {
		return "", fmt.Errorf("workflow name is required")
	}

	def, err := loadWorkflow(p.Workflow)
	if err != nil {
		return "", err
	}

	var results []map[string]any
	for i, step := range def.Steps {
		prompt := step.Prompt
		// Substitute args into prompt
		for k, v := range p.Args {
			prompt = strings.ReplaceAll(prompt, fmt.Sprintf("{{%s}}", k), fmt.Sprintf("%v", v))
		}
		results = append(results, map[string]any{
			"step":   i + 1,
			"name":   step.Name,
			"prompt": prompt,
			"tool":   step.Tool,
		})
	}

	out, _ := json.Marshal(map[string]any{
		"workflow":    def.Name,
		"description": def.Description,
		"steps":       results,
		"totalSteps":  len(def.Steps),
	})
	return string(out), nil
}

func loadWorkflow(name string) (*WorkflowDef, error) {
	searchPaths := []string{
		filepath.Join(storage.StateDir(), "workflows", name+".yml"),
		filepath.Join(storage.StateDir(), "workflows", name+".yaml"),
		filepath.Join(".agents", "workflows", name+".yml"),
		filepath.Join(".agents", "workflows", name+".yaml"),
	}

	for _, path := range searchPaths {
		data, err := os.ReadFile(path) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
		if err != nil {
			continue
		}
		var def WorkflowDef
		if err := yaml.Unmarshal(data, &def); err != nil {
			return nil, fmt.Errorf("parsing workflow %q: %w", path, err)
		}
		if def.Name == "" {
			def.Name = name
		}
		return &def, nil
	}
	return nil, fmt.Errorf("workflow %q not found", name)
}

// ListWorkflows discovers available workflows.
func ListWorkflows() []WorkflowDef {
	var workflows []WorkflowDef
	searchDirs := []string{filepath.Join(storage.StateDir(), "workflows"), filepath.Join(".agents", "workflows")}

	seen := make(map[string]bool)
	for _, dir := range searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := filepath.Ext(e.Name())
			if ext != ".yml" && ext != ".yaml" {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ext)
			if seen[name] {
				continue
			}
			seen[name] = true
			data, err := os.ReadFile(filepath.Join(dir, e.Name())) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
			if err != nil {
				continue
			}
			var def WorkflowDef
			if err := yaml.Unmarshal(data, &def); err != nil {
				continue
			}
			if def.Name == "" {
				def.Name = name
			}
			workflows = append(workflows, def)
		}
	}
	return workflows
}
