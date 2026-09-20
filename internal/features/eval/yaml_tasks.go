package eval

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// YAMLTask is the declarative task definition format.
type YAMLTask struct {
	Task        string            `yaml:"task"`
	Description string            `yaml:"description"`
	Language    string            `yaml:"language"`
	Tags        []string          `yaml:"tags"`
	Timeout     string            `yaml:"timeout"`
	MaxAttempts int               `yaml:"max_attempts"`
	Setup       string            `yaml:"setup"`
	Prompt      string            `yaml:"prompt"`
	Validate    []string          `yaml:"validate"`
	Files       map[string]string `yaml:"files"`
	Filters     []string          `yaml:"filters"`
}

// LoadTasksFromYAML loads task definitions from a directory of YAML files.
func LoadTasksFromYAML(dir string) ([]BenchmarkTask, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var tasks []BenchmarkTask
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml")) {
			continue
		}
		task, err := loadYAMLTask(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", e.Name(), err)
		}
		tasks = append(tasks, *task)
	}
	return tasks, nil
}

// LoadTaskFromYAML loads a single task from a YAML file.
func loadYAMLTask(path string) (*BenchmarkTask, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is a benchmark task file discovered from a local eval task directory
	if err != nil {
		return nil, err
	}
	var yt YAMLTask
	if err := yaml.Unmarshal(data, &yt); err != nil {
		return nil, err
	}

	timeout := 5 * time.Minute
	if yt.Timeout != "" {
		if d, err := time.ParseDuration(yt.Timeout); err == nil {
			timeout = d
		}
	}

	task := &BenchmarkTask{
		ID:          yt.Task,
		Description: yt.Description,
		Prompt:      yt.Prompt,
		Tags:        yt.Tags,
		TimeLimit:   timeout,
		MaxAttempts: yt.MaxAttempts,
		SetupFn:     makeSetupFn(yt),
		ValidateFn:  makeValidateFn(yt),
	}

	// Parse filters
	for _, f := range yt.Filters {
		switch {
		case strings.HasPrefix(f, "extract_code_block:"):
			task.Filters = append(task.Filters, ExtractCodeBlock(strings.TrimPrefix(f, "extract_code_block:")))
		case f == "strip_markdown":
			task.Filters = append(task.Filters, StripMarkdown)
		case f == "trim_explanation":
			task.Filters = append(task.Filters, TrimExplanation)
		}
	}
	// Auto-add language filter if no explicit filters
	if len(task.Filters) == 0 && yt.Language != "" {
		task.Filters = append(task.Filters, ExtractCodeBlock(yt.Language))
	}

	return task, nil
}

func makeSetupFn(yt YAMLTask) func(string) error {
	return func(workDir string) error {
		// Write files from the YAML definition
		for name, content := range yt.Files {
			path := filepath.Join(workDir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				return err
			}
		}
		// Run setup script if provided
		if yt.Setup != "" {
			cmd := exec.CommandContext(context.Background(), "sh", "-c", yt.Setup) // #nosec G204 -- yt.Setup is a shell snippet authored in a local eval task YAML file, not external input
			cmd.Dir = workDir
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("setup: %s: %w", string(out), err)
			}
		}
		return nil
	}
}

func makeValidateFn(yt YAMLTask) func(string) (bool, string) {
	return func(workDir string) (bool, string) {
		for _, v := range yt.Validate {
			cmd := exec.CommandContext(context.Background(), "sh", "-c", v) // #nosec G204 -- v is a validation shell snippet authored in a local eval task YAML file, not external input
			cmd.Dir = workDir
			out, err := cmd.CombinedOutput()
			if err != nil {
				return false, fmt.Sprintf("%s: %s", v, strings.TrimSpace(string(out)))
			}
		}
		return true, ""
	}
}
