package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Workflow represents a parsed workflow YAML definition.
type Workflow struct {
	Name   string `yaml:"name"`
	Config string `yaml:"config,omitempty"`
	Steps  []Step `yaml:"steps"`
}

// LoadWorkflow reads and parses a workflow YAML file.
func LoadWorkflow(path string) (*Workflow, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is user-provided workflow file
	if err != nil {
		return nil, fmt.Errorf("reading workflow %s: %w", path, err)
	}

	var wf Workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("parsing workflow %s: %w", path, err)
	}

	return &wf, nil
}
