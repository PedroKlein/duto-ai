package config_test

import (
	"path/filepath"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
)

func TestLoadWorkflow_Valid(t *testing.T) {
	wf, err := config.LoadWorkflow(filepath.Join("testdata", "valid_workflow.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if wf.Name != "PR Code Review" {
		t.Errorf("name = %q, want %q", wf.Name, "PR Code Review")
	}

	if len(wf.Steps) != 3 {
		t.Fatalf("steps len = %d, want 3", len(wf.Steps))
	}

	gather := wf.Steps[0]
	if gather.ID != "gather" {
		t.Errorf("step[0].id = %q, want %q", gather.ID, "gather")
	}

	if gather.Model != "light" {
		t.Errorf("step[0].model = %q, want %q", gather.Model, "light")
	}

	if gather.Tools == nil || len(*gather.Tools) != 2 {
		t.Errorf("step[0].tools len unexpected")
	}

	analyze := wf.Steps[1]
	if len(analyze.Needs) != 1 || analyze.Needs[0] != "gather" {
		t.Errorf("step[1].needs = %v, want [gather]", analyze.Needs)
	}

	if analyze.ModelConfig.Temperature == nil || *analyze.ModelConfig.Temperature != 0.1 {
		t.Errorf("step[1].model_config.temperature unexpected")
	}

	if len(analyze.Skills) != 1 || analyze.Skills[0] != "security-analysis" {
		t.Errorf("step[1].skills = %v, want [security-analysis]", analyze.Skills)
	}
}

func TestLoadWorkflow_FileNotFound(t *testing.T) {
	_, err := config.LoadWorkflow("nonexistent.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
