package config_test

import (
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
)

const routedWorkflow = `version: 1
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
    instruction: Inspect.
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
      required: [outcome]
  - id: report
    needs: [inspect]
    wait: all_succeeded
    when:
      - step: inspect
        outcome_in: [ready]
    instruction: Report.
    tools: []
    workspaces: []
    input:
      type: object
      properties:
        findings: {type: string, max_length: 64}
        label: {type: string, max_length: 16}
      required: [label]
    with:
      findings: {output: {step: inspect, path: [findings]}, optional: true}
      label: {literal: concise}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        report: {type: string, max_length: 128}
      required: [outcome, report]
result:
  routes:
    - when: {step: inspect, outcome: awaiting_input}
      step: inspect
    - when: {step: report, outcome: completed}
      step: report
`

func TestDecodeWorkflow_TypedGraphSourcesRoutesAndConditions(t *testing.T) {
	workflow, err := config.DecodeWorkflow("workflow.yaml", []byte(routedWorkflow))
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	binding := workflow.Steps[1].With["findings"]
	if binding.Kind != config.BindingOutput || binding.Output.Step != "inspect" || len(binding.Output.Path) != 1 || binding.Output.Path[0] != "findings" || !binding.Optional {
		t.Fatalf("output binding = %#v", binding)
	}

	if workflow.Steps[1].Wait != "all_succeeded" || len(workflow.Steps[1].When) != 1 || workflow.Steps[1].When[0].Outcomes[0] != "ready" {
		t.Fatalf("step conditions = %#v", workflow.Steps[1])
	}

	if len(workflow.Result.Routes) != 2 || workflow.Result.Routes[0].When.Outcome != "awaiting_input" {
		t.Fatalf("result routes = %#v", workflow.Result)
	}
}
