package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
)

func TestLoadConfig_Valid(t *testing.T) {
	cfg, err := config.LoadConfig(filepath.Join("testdata", "valid_config.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Provider.Type != "ai-core" {
		t.Errorf("provider type = %q, want %q", cfg.Provider.Type, "ai-core")
	}

	if cfg.Provider.Config["resource_group"] != "default" {
		t.Errorf("resource_group = %q, want %q", cfg.Provider.Config["resource_group"], "default")
	}

	if cfg.Models["heavy"] != "claude-sonnet-4" {
		t.Errorf("models[heavy] = %q, want %q", cfg.Models["heavy"], "claude-sonnet-4")
	}

	if cfg.Defaults.Model != "medium" {
		t.Errorf("defaults.model = %q, want %q", cfg.Defaults.Model, "medium")
	}

	if len(cfg.Defaults.Tools) != 3 {
		t.Errorf("defaults.tools len = %d, want 3", len(cfg.Defaults.Tools))
	}

	if len(cfg.ContextFiles) != 2 {
		t.Errorf("context_files len = %d, want 2", len(cfg.ContextFiles))
	}
}

func TestLoadConfig_EnvVarExpansion(t *testing.T) {
	t.Setenv("TEST_RESOURCE_GROUP", "my-group")
	t.Setenv("TEST_ENDPOINT", "https://test.example.com")
	t.Setenv("TEST_MODEL", "gpt-4o")

	cfg, err := config.LoadConfig(filepath.Join("testdata", "env_config.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Provider.Config["resource_group"] != "my-group" {
		t.Errorf("resource_group = %q, want %q", cfg.Provider.Config["resource_group"], "my-group")
	}

	if cfg.Provider.Config["endpoint"] != "https://test.example.com" {
		t.Errorf("endpoint = %q, want %q", cfg.Provider.Config["endpoint"], "https://test.example.com")
	}

	if cfg.Models["default"] != "gpt-4o" {
		t.Errorf("models[default] = %q, want %q", cfg.Models["default"], "gpt-4o")
	}
}

func TestLoadConfig_EnvVarUnset(t *testing.T) {
	os.Unsetenv("NONEXISTENT_VAR_12345")

	cfg, err := config.LoadConfig(filepath.Join("testdata", "env_config.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Unset vars resolve to empty string
	if cfg.Provider.Config["resource_group"] != "" {
		t.Errorf("resource_group = %q, want empty", cfg.Provider.Config["resource_group"])
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := config.LoadConfig("nonexistent.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
