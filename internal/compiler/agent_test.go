package compiler_test

import (
	"context"
	"testing"

	"google.golang.org/adk/v2/model"

	"github.com/PedroKlein/duto-ai/internal/compiler"
	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/plan"
	"github.com/PedroKlein/duto-ai/internal/testing/mockllm"
)

func TestCompile_NativeSubagentTreeContainsOnlyDeclaredChildren(t *testing.T) {
	cfg, err := config.DecodeConfig("duto.yaml", []byte(nativeAgentConfig))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}

	workflow, err := config.DecodeWorkflow("workflow.yaml", []byte(nativeAgentWorkflow))
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("plan.Compile() error = %v", err)
	}

	llm := mockllm.New(mockllm.Response{Text: `{"outcome":"completed"}`})

	root, err := compiler.Compile(t.Context(), compiled, func(context.Context, string) (model.LLM, error) { return llm, nil })
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	if root.Name() != "coordinator" || len(root.SubAgents()) != 1 || root.SubAgents()[0].Name() != "researcher" {
		t.Fatalf("root/subagents = %q/%#v", root.Name(), root.SubAgents())
	}

	legacyDelegate := "agent" + ".delegate"
	if root.FindSubAgent(legacyDelegate) != nil {
		t.Fatal("aggregate delegation tool was constructed")
	}

	if got := llm.CallCount(); got != 0 {
		t.Fatalf("construction model calls = %d, want 0", got)
	}
}

const nativeAgentConfig = `version: 1
providers:
  default: {type: custom-provider, config: {credential: canary}}
models:
  light: {provider: default, target: example-light}
  capable: {provider: default, target: example-capable}
`

const nativeAgentWorkflow = `version: 1
name: delegated
inputs: {objective: {schema: {type: string, max_length: 64}}}
model: capable
tools: []
limits: {timeout: 1m, max_iterations: 8, max_model_calls: 8, max_tool_calls: 4, max_concurrency: 1, max_parallel_calls: 2, max_artifact_bytes: 1024}
agents:
  researcher:
    description: Gather evidence.
    mode: single_turn
    model: light
    instruction: Return evidence.
    tools: []
    workspaces: []
    skills: []
    context: {mode: snapshot, include: [{input: objective}]}
    input: {type: object, properties: {question: {type: string, max_length: 64}}, required: [question]}
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}, evidence: {type: string, max_length: 128}}
      required: [outcome, evidence]
  coordinator:
    description: Coordinate research.
    mode: chat
    model: capable
    instruction: Delegate once.
    tools: []
    workspaces: []
    skills: []
    context: {mode: fresh}
    subagents: [researcher]
    input: {type: object, properties: {objective: {type: string, max_length: 64}}, required: [objective]}
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}, decision: {type: string, max_length: 128}}
      required: [outcome, decision]
steps:
  - id: coordinate
    needs: []
    agent: coordinator
    with: {objective: {input: objective}}
result: {step: coordinate}
`
