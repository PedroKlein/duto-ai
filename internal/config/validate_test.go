package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
)

func TestValidateWorkflow_Valid(t *testing.T) {
	wf, err := config.LoadWorkflow(filepath.Join("testdata", "valid_workflow.yaml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if err := config.ValidateWorkflow(wf); err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestValidateWorkflow_DuplicateID(t *testing.T) {
	wf := &config.Workflow{
		Name: "test",
		Steps: []config.Step{
			{ID: "a", Prompt: "x"},
			{ID: "a", Prompt: "y"},
		},
	}

	err := config.ValidateWorkflow(wf)
	if err == nil {
		t.Fatal("expected error for duplicate ID")
	}

	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error = %q, want to contain 'duplicate'", err.Error())
	}
}

func TestValidateWorkflow_MissingNeedsRef(t *testing.T) {
	wf := &config.Workflow{
		Name: "test",
		Steps: []config.Step{
			{ID: "a", Needs: []string{"nonexistent"}, Prompt: "x"},
		},
	}

	err := config.ValidateWorkflow(wf)
	if err == nil {
		t.Fatal("expected error for missing needs reference")
	}

	if !strings.Contains(err.Error(), "unknown dependency") {
		t.Errorf("error = %q, want to contain 'unknown dependency'", err.Error())
	}
}

func TestValidateWorkflow_CircularDeps(t *testing.T) {
	wf, err := config.LoadWorkflow(filepath.Join("testdata", "circular_workflow.yaml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	err = config.ValidateWorkflow(wf)
	if err == nil {
		t.Fatal("expected error for circular dependency")
	}

	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("error = %q, want to contain 'circular'", err.Error())
	}
}

func TestValidateWorkflow_NilWorkflow(t *testing.T) {
	err := config.ValidateWorkflow(nil)
	if err == nil {
		t.Fatal("expected error for nil workflow")
	}
}

func TestValidateWorkflow_EmptySteps(t *testing.T) {
	wf := &config.Workflow{Name: "test", Steps: []config.Step{}}

	err := config.ValidateWorkflow(wf)
	if err == nil {
		t.Fatal("expected error for empty steps")
	}
}

func TestTopologicalSort_Linear(t *testing.T) {
	steps := []config.Step{
		{ID: "c", Needs: []string{"b"}, Prompt: "x"},
		{ID: "a", Prompt: "x"},
		{ID: "b", Needs: []string{"a"}, Prompt: "x"},
	}

	sorted, err := config.TopologicalSort(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sorted) != 3 {
		t.Fatalf("len = %d, want 3", len(sorted))
	}

	// a must come before b, b before c
	indexOf := make(map[string]int, len(sorted))
	for i, s := range sorted {
		indexOf[s.ID] = i
	}

	if indexOf["a"] >= indexOf["b"] {
		t.Error("a should come before b")
	}

	if indexOf["b"] >= indexOf["c"] {
		t.Error("b should come before c")
	}
}

func TestTopologicalSort_Circular(t *testing.T) {
	steps := []config.Step{
		{ID: "a", Needs: []string{"b"}, Prompt: "x"},
		{ID: "b", Needs: []string{"a"}, Prompt: "x"},
	}

	_, err := config.TopologicalSort(steps)
	if err == nil {
		t.Fatal("expected error for circular dependency")
	}
}
