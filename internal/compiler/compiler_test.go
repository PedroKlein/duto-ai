package compiler_test

import (
	"testing"

	"github.com/PedroKlein/duto-ai/internal/compiler"
	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/tool"
)

func TestCompile_LinearDAG(t *testing.T) {
	wf := &config.Workflow{
		Name: "linear",
		Steps: []config.Step{
			{ID: "a", Prompt: "do A"},
			{ID: "b", Needs: []string{"a"}, Prompt: "do B"},
			{ID: "c", Needs: []string{"b"}, Prompt: "do C"},
		},
	}

	cfg := &config.Config{
		Defaults: config.Defaults{
			Model: "gpt-4",
			Tools: []string{},
		},
	}

	reg := tool.NewRegistry()

	result, err := compiler.Compile(wf, cfg, reg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil workflow")
	}
}

func TestCompile_ParallelDAG(t *testing.T) {
	wf := &config.Workflow{
		Name: "parallel",
		Steps: []config.Step{
			{ID: "a", Prompt: "do A"},
			{ID: "b", Needs: []string{"a"}, Prompt: "do B"},
			{ID: "c", Needs: []string{"a"}, Prompt: "do C"},
			{ID: "d", Needs: []string{"b", "c"}, Prompt: "do D"},
		},
	}

	cfg := &config.Config{
		Defaults: config.Defaults{
			Model: "gpt-4",
			Tools: []string{},
		},
	}

	reg := tool.NewRegistry()

	result, err := compiler.Compile(wf, cfg, reg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil workflow")
	}
}

func TestCompile_SingleStep(t *testing.T) {
	wf := &config.Workflow{
		Name: "single",
		Steps: []config.Step{
			{ID: "only", Prompt: "do it"},
		},
	}

	cfg := &config.Config{
		Defaults: config.Defaults{
			Model: "gpt-4",
			Tools: []string{},
		},
	}

	reg := tool.NewRegistry()

	result, err := compiler.Compile(wf, cfg, reg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil workflow")
	}
}

func TestCompile_CircularDependency(t *testing.T) {
	wf := &config.Workflow{
		Name: "circular",
		Steps: []config.Step{
			{ID: "a", Needs: []string{"b"}, Prompt: "x"},
			{ID: "b", Needs: []string{"a"}, Prompt: "y"},
		},
	}

	cfg := &config.Config{
		Defaults: config.Defaults{Model: "gpt-4"},
	}

	reg := tool.NewRegistry()

	_, err := compiler.Compile(wf, cfg, reg, nil)
	if err == nil {
		t.Fatal("expected error for circular dependency")
	}
}

func TestDetectParallel(t *testing.T) {
	steps := []config.Step{
		{ID: "a", Prompt: "x"},
		{ID: "b", Needs: []string{"a"}, Prompt: "y"},
		{ID: "c", Needs: []string{"a"}, Prompt: "z"},
		{ID: "d", Needs: []string{"b", "c"}, Prompt: "w"},
	}

	groups := compiler.DetectParallel(steps)

	// b and c share the same dependency (a), so they form a parallel group
	found := false

	for _, group := range groups {
		if len(group) == 2 {
			found = true
		}
	}

	if !found {
		t.Errorf("expected a parallel group of 2, got groups: %v", groups)
	}
}
