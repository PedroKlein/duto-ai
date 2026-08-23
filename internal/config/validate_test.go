package config_test

import (
	"errors"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
)

func TestValidateWorkflow_Graph(t *testing.T) {
	tests := []struct {
		name     string
		workflow *config.Workflow
		wantErr  error
	}{
		{name: "nil", workflow: nil, wantErr: config.ErrNilWorkflow},
		{name: "empty", workflow: &config.Workflow{Name: "test", Model: "light"}, wantErr: config.ErrNoSteps},
		{name: "duplicate", workflow: workflowWithSteps(config.Step{ID: "a"}, config.Step{ID: "a"}), wantErr: config.ErrDuplicateStepID},
		{name: "unknown dependency", workflow: workflowWithSteps(config.Step{ID: "a", Needs: []string{"missing"}}), wantErr: config.ErrUnknownDependency},
		{name: "cycle", workflow: workflowWithSteps(config.Step{ID: "a", Needs: []string{"b"}}, config.Step{ID: "b", Needs: []string{"a"}}), wantErr: config.ErrCircularDependency},
		{name: "unknown result", workflow: &config.Workflow{Name: "test", Model: "light", Steps: []config.Step{{ID: "a"}}, Result: config.Result{Step: "missing"}}, wantErr: config.ErrUnknownResultStep},
		{name: "valid", workflow: workflowWithSteps(config.Step{ID: "a"}, config.Step{ID: "b", Needs: []string{"a"}})},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := config.ValidateWorkflow(test.workflow)
			if test.wantErr == nil && err != nil {
				t.Fatalf("ValidateWorkflow() error = %v", err)
			}

			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("ValidateWorkflow() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestTopologicalSort_IsSourceStable(t *testing.T) {
	steps := []config.Step{
		{ID: "third", Needs: []string{"first"}},
		{ID: "first"},
		{ID: "second"},
		{ID: "fourth", Needs: []string{"second"}},
	}

	sorted, err := config.TopologicalSort(steps)
	if err != nil {
		t.Fatalf("TopologicalSort() error = %v", err)
	}

	got := make([]string, 0, len(sorted))
	for _, step := range sorted {
		got = append(got, step.ID)
	}

	want := []string{"first", "second", "third", "fourth"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("TopologicalSort() = %v, want %v", got, want)
		}
	}
}

func workflowWithSteps(steps ...config.Step) *config.Workflow {
	result := "a"
	if len(steps) > 0 {
		result = steps[len(steps)-1].ID
	}

	return &config.Workflow{Name: "test", Model: "light", Steps: steps, Result: config.Result{Step: result}}
}
