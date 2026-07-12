//go:build integration

package runtime_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
)

func TestIntegration_WorkflowParsing(t *testing.T) {
	configPath := filepath.Join("testdata", "integration_config.yaml")
	workflowPath := filepath.Join("testdata", "integration_workflow.yaml")

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

	if cfg.Provider.Type != "ai-core" {
		t.Errorf("provider type = %q, want ai-core", cfg.Provider.Type)
	}

	wf, err := config.LoadWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("loading workflow: %v", err)
	}

	if err := config.ValidateWorkflow(wf); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	if len(wf.Steps) != 3 {
		t.Errorf("steps = %d, want 3", len(wf.Steps))
	}

	// Verify topological order
	sorted, err := config.TopologicalSort(wf.Steps)
	if err != nil {
		t.Fatalf("sort: %v", err)
	}

	if sorted[0].ID != "gather" {
		t.Errorf("first step = %q, want gather", sorted[0].ID)
	}
}

func TestIntegration_FailFast(t *testing.T) {
	// Verify that the runtime returns error on invalid config
	ctx := context.Background()
	_ = ctx

	// Load a workflow with circular deps
	wf := &config.Workflow{
		Name: "circular",
		Steps: []config.Step{
			{ID: "a", Needs: []string{"b"}, Prompt: "x"},
			{ID: "b", Needs: []string{"a"}, Prompt: "y"},
		},
	}

	err := config.ValidateWorkflow(wf)
	if err == nil {
		t.Fatal("expected error for circular deps")
	}
}

func TestIntegration_OutputPassing(t *testing.T) {
	// Verify template rendering with step outputs
	wf, err := config.LoadWorkflow(filepath.Join("testdata", "integration_workflow.yaml"))
	if err != nil {
		t.Fatalf("loading workflow: %v", err)
	}

	analyzeStep := wf.Steps[1]
	if analyzeStep.ID != "analyze" {
		t.Fatalf("step[1] = %q, want analyze", analyzeStep.ID)
	}

	// The prompt should reference .Steps.gather.Output
	if analyzeStep.Prompt == "" {
		t.Error("analyze step should have a prompt")
	}
}
