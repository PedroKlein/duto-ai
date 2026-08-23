package runtime_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/adk/v2/model"

	"github.com/PedroKlein/duto-ai/internal/compiler"
	"github.com/PedroKlein/duto-ai/internal/runtime"
	"github.com/PedroKlein/duto-ai/internal/testing/mockllm"
)

const routedRuntimeWorkflow = `version: 1
name: routed
inputs:
  objective:
    schema: {type: string, max_length: 64}
model: light
tools: []
limits: {timeout: 1m, max_iterations: 4, max_model_calls: 4, max_tool_calls: 0, max_concurrency: 2, max_parallel_calls: 1, max_artifact_bytes: 0}
steps:
  - id: inspect
    needs: []
    instruction:
      template: {text: 'Inspect {{ .Workflow.Inputs.objective }}.', max_output_bytes: 256}
    tools: []
    workspaces: []
    input:
      type: object
      properties: {objective: {type: string, max_length: 64}}
      required: [objective]
    with: {objective: {input: objective}}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [ready, awaiting_input]}
        findings: {type: string, max_length: 64}
      required: [outcome, findings]
  - id: report
    needs: [inspect]
    when: [{step: inspect, outcome_in: [ready]}]
    instruction:
      template: {text: 'Report {{ index .Predecessors "inspect" | json }}.', max_output_bytes: 256}
    tools: []
    workspaces: []
    input:
      type: object
      properties:
        findings: {type: string, max_length: 64}
        label: {type: string, max_length: 16}
      required: [findings, label]
    with:
      findings: {output: {step: inspect, path: [findings]}}
      label: {literal: concise}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        report: {type: string, max_length: 128}
      required: [outcome, report]
result:
  routes:
    - {when: {step: inspect, outcome: awaiting_input}, step: inspect}
    - {when: {step: report, outcome: completed}, step: report}
`

func TestRunWithInputs_TypedChainBindsAncestorAndPromptData(t *testing.T) {
	compiled := compilePlan(t, noToolsConfig, routedRuntimeWorkflow)
	llm := mockllm.New(
		mockllm.Response{Text: `{"outcome":"ready","findings":"bounded"}`},
		mockllm.Response{Text: `{"outcome":"completed","report":"done"}`},
	)
	resolver := func(context.Context, string) (model.LLM, error) { return llm, nil }

	result, err := runtime.RunWithInputs(t.Context(), compiled, compiler.ModelResolver(resolver), map[string]any{"objective": "review"})
	if err != nil {
		t.Fatalf("RunWithInputs() error = %v, result = %#v, calls = %#v", err, result, llm.Calls())
	}

	if result.Outcome != "completed" || result.Output["report"] != "done" {
		t.Fatalf("result = %#v", result)
	}

	calls := llm.Calls()
	if len(calls) != 2 || len(calls[1].Contents) == 0 || !strings.Contains(calls[1].Contents[len(calls[1].Contents)-1].Parts[0].Text, `"findings":"bounded"`) {
		t.Fatalf("calls did not receive bound ancestor output: %#v", calls)
	}
}

func TestRunWithInputs_AwaitingInputIsSuccessfulAndSkipsSuccessor(t *testing.T) {
	compiled := compilePlan(t, noToolsConfig, routedRuntimeWorkflow)
	llm := mockllm.New(mockllm.Response{Text: `{"outcome":"awaiting_input","findings":""}`})
	resolver := func(context.Context, string) (model.LLM, error) { return llm, nil }

	result, err := runtime.RunWithInputs(t.Context(), compiled, compiler.ModelResolver(resolver), map[string]any{"objective": "review"})
	if err != nil {
		t.Fatalf("RunWithInputs() error = %v, result = %#v", err, result)
	}

	if result.Status != runtime.StatusSucceeded || result.Outcome != "awaiting_input" || llm.CallCount() != 1 {
		t.Fatalf("result/calls = %#v/%d", result, llm.CallCount())
	}
}

const fanInConfig = `version: 1
providers:
  default: {type: custom-provider, config: {credential: canary}}
models:
  first: {provider: default, target: example-first}
  second: {provider: default, target: example-second}
  summary: {provider: default, target: example-summary}
`

const fanInWorkflow = `version: 1
name: fan-in
inputs:
  objective: {schema: {type: string, max_length: 64}}
model: summary
tools: []
limits: {timeout: 1m, max_iterations: 4, max_model_calls: 4, max_tool_calls: 0, max_concurrency: 2, max_parallel_calls: 1, max_artifact_bytes: 0}
steps:
  - id: first
    needs: []
    instruction: First.
    model: first
    tools: []
    workspaces: []
    input: {type: object, properties: {objective: {type: string, max_length: 64}}, required: [objective]}
    with: {objective: {input: objective}}
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}, value: {type: string, max_length: 32}}
      required: [outcome, value]
  - id: second
    needs: []
    instruction: Second.
    model: second
    tools: []
    workspaces: []
    input: {type: object, properties: {objective: {type: string, max_length: 64}}, required: [objective]}
    with: {objective: {input: objective}}
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}, value: {type: string, max_length: 32}}
      required: [outcome, value]
  - id: summarize
    needs: [second, first]
    wait: all_succeeded
    instruction: Summarize.
    model: summary
    tools: []
    workspaces: []
    input:
      type: object
      properties: {left: {type: string, max_length: 32}, right: {type: string, max_length: 32}}
      required: [left, right]
    with:
      left: {output: {step: second, path: [value]}}
      right: {output: {step: first, path: [value]}}
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}, report: {type: string, max_length: 64}}
      required: [outcome, report]
result: {step: summarize}
`

func TestRunWithInputs_InvalidClosedWorkflowInputMakesZeroModelCalls(t *testing.T) {
	compiled := compilePlan(t, noToolsConfig, routedRuntimeWorkflow)
	llm := mockllm.New(mockllm.Response{Text: `{"outcome":"ready","findings":"unexpected"}`})
	resolverCalls := 0
	resolver := func(context.Context, string) (model.LLM, error) {
		resolverCalls++
		return llm, nil
	}

	result, err := runtime.RunWithInputs(t.Context(), compiled, compiler.ModelResolver(resolver), map[string]any{"unexpected": "value"})
	if err == nil || result.Status != runtime.StatusFailed || resolverCalls != 0 || llm.CallCount() != 0 {
		t.Fatalf("result/error/resolver/model calls = %#v/%v/%d/%d", result, err, resolverCalls, llm.CallCount())
	}
}

func TestRunWithInputs_ParallelFanInUsesDeclaredBindings(t *testing.T) {
	compiled := compilePlan(t, fanInConfig, fanInWorkflow)
	models := map[string]*mockllm.MockLLM{
		"first":   mockllm.New(mockllm.Response{Text: `{"outcome":"completed","value":"one"}`}),
		"second":  mockllm.New(mockllm.Response{Text: `{"outcome":"completed","value":"two"}`}),
		"summary": mockllm.New(mockllm.Response{Text: `{"outcome":"completed","report":"both"}`}),
	}
	resolver := func(_ context.Context, alias string) (model.LLM, error) { return models[alias], nil }

	result, err := runtime.RunWithInputs(t.Context(), compiled, compiler.ModelResolver(resolver), map[string]any{"objective": "review"})
	if err != nil {
		t.Fatalf("RunWithInputs() error = %v, result = %#v", err, result)
	}

	calls := models["summary"].Calls()
	if len(calls) != 1 {
		t.Fatalf("summary calls = %d", len(calls))
	}

	input := calls[0].Contents[len(calls[0].Contents)-1].Parts[0].Text
	if !strings.Contains(input, `"left":"two"`) || !strings.Contains(input, `"right":"one"`) {
		t.Fatalf("fan-in input = %s", input)
	}
}
