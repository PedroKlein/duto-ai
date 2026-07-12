package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Provider holds the LLM provider configuration.
type Provider struct {
	Type   string            `yaml:"type"`
	Config map[string]string `yaml:"config"`
}

// ModelConfig holds per-step or default model parameters.
type ModelConfig struct {
	Temperature *float64       `yaml:"temperature,omitempty"`
	MaxTokens   *int           `yaml:"max_tokens,omitempty"`
	Extra       map[string]any `yaml:"extra,omitempty"`
}

// Defaults holds default settings applied to all steps.
type Defaults struct {
	Model       string      `yaml:"model"`
	ModelConfig ModelConfig `yaml:"model_config"`
	Tools       []string    `yaml:"tools"`
}

// Config is the global configuration loaded from config.yaml.
type Config struct {
	Provider     Provider          `yaml:"provider"`
	Models       map[string]string `yaml:"models"`
	Defaults     Defaults          `yaml:"defaults"`
	ContextFiles []string          `yaml:"context_files"`
}

// LoadConfig reads and parses a config YAML file, expanding env vars.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is user-provided config file
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	expanded := expandEnvVars(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	return &cfg, nil
}

var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

func expandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		key := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")

		return os.Getenv(key)
	})
}
