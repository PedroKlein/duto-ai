package config

// Step defines a single step in a workflow DAG.
type Step struct {
	ID            string      `yaml:"id"`
	Needs         []string    `yaml:"needs,omitempty"`
	Model         string      `yaml:"model,omitempty"`
	ModelConfig   ModelConfig `yaml:"model_config,omitempty"`
	Tools         *[]string   `yaml:"tools,omitempty"`
	Skills        []string    `yaml:"skills,omitempty"`
	System        string      `yaml:"system,omitempty"`
	Prompt        string      `yaml:"prompt"`
	Output        string      `yaml:"output,omitempty"`
	MaxIterations int         `yaml:"max_iterations,omitempty"`
	Timeout       string      `yaml:"timeout,omitempty"`
}

const (
	DefaultMaxIterations = 25
	DefaultTimeout       = "300s"
)
