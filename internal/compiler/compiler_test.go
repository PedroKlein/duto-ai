package compiler_test

import (
	"testing"

	"google.golang.org/adk/v2/agent"

	"github.com/PedroKlein/duto-ai/internal/compiler"
	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/testing/mockllm"
	"github.com/PedroKlein/duto-ai/internal/tool"
)

func TestCompile_LinearDAG(t *testing.T) {
	wf := &config.Workflow{
		Name: "linear",
		Steps: []config.Step{
			{ID: "a", Prompt: "do A", Output: "out_a"},
			{ID: "b", Needs: []string{"a"}, Prompt: "do B", Output: "out_b"},
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
	llm := mockllm.New(mockllm.Response{Text: "ok"})

	result, err := compiler.Compile(wf, cfg, reg, llm, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertAgent(t, result, "linear")

	// Sub-agents should include all 3 step agents
	if got := len(result.SubAgents()); got != 3 {
		t.Errorf("expected 3 sub-agents, got %d", got)
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
	llm := mockllm.New(mockllm.Response{Text: "ok"})

	result, err := compiler.Compile(wf, cfg, reg, llm, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertAgent(t, result, "parallel")

	// 4 step agents registered as sub-agents
	if got := len(result.SubAgents()); got != 4 {
		t.Errorf("expected 4 sub-agents, got %d", got)
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
	llm := mockllm.New(mockllm.Response{Text: "ok"})

	result, err := compiler.Compile(wf, cfg, reg, llm, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertAgent(t, result, "single")
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
	llm := mockllm.New(mockllm.Response{Text: "ok"})

	_, err := compiler.Compile(wf, cfg, reg, llm, nil)
	if err == nil {
		t.Fatal("expected error for circular dependency")
	}
}

func assertAgent(t *testing.T, a agent.Agent, expectedName string) {
	t.Helper()

	if a == nil {
		t.Fatal("expected non-nil agent")
	}

	if got := a.Name(); got != expectedName {
		t.Errorf("expected agent name %q, got %q", expectedName, got)
	}
}
