package plan_test

import (
	"errors"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/plan"
)

const routedPlanWorkflow = `version: 1
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
    when: [{step: inspect, outcome_in: [ready]}]
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
    - {when: {step: inspect, outcome: awaiting_input}, step: inspect}
    - {when: {step: report, outcome: completed}, step: report}
`

func TestCompile_TypedAncestorBindingAndExhaustiveRoutes(t *testing.T) {
	cfg, workflow := decodeInputs(t, runtimeConfig, routedPlanWorkflow)

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	snapshot := compiled.Snapshot()

	binding := snapshot.Workflow.Steps[1].Bindings[0]
	if binding.Name != "findings" || binding.Kind != "output" || binding.Step != "inspect" || len(binding.Path) != 1 || binding.Path[0] != "findings" || !binding.Optional {
		t.Fatalf("binding = %#v", binding)
	}

	if len(snapshot.Workflow.Result.Routes) != 2 || snapshot.Workflow.Result.Routes[0].Outcome != "awaiting_input" {
		t.Fatalf("routes = %#v", snapshot.Workflow.Result)
	}
}

func TestCompile_RejectsNonAncestorOptionalRequiredAndUncoveredResult(t *testing.T) {
	tests := []struct {
		name string
		edit func(*config.Workflow)
	}{
		{name: "non ancestor", edit: func(w *config.Workflow) {
			b := w.Steps[1].With["findings"]
			b.Output.Step = "report"
			w.Steps[1].With["findings"] = b
		}},
		{name: "optional required target", edit: func(w *config.Workflow) {
			s := w.Steps[1].Input
			s.Required = append(s.Required, "findings")
			w.Steps[1].Input = s
		}},
		{name: "uncovered result", edit: func(w *config.Workflow) { w.Result.Routes = w.Result.Routes[1:] }},
		{name: "independent ambiguous terminals", edit: func(w *config.Workflow) {
			other := w.Steps[1]
			other.ID = "other"
			other.Needs = nil
			other.When = nil
			other.With = map[string]config.Binding{"label": {Kind: config.BindingLiteral, Literal: "other"}}
			other.WithOrder = []string{"label"}
			w.Steps = append(w.Steps, other)
			w.Result.Routes = append(w.Result.Routes, config.ResultRoute{When: config.ResultWhen{Step: "other", Outcome: "completed"}, Step: "other"})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg, workflow := decodeInputs(t, runtimeConfig, routedPlanWorkflow)
			test.edit(workflow)

			_, err := plan.Compile(cfg, workflow)
			if err == nil || (!errors.Is(err, plan.ErrInvalidBinding) && !errors.Is(err, plan.ErrInvalidResult)) {
				t.Fatalf("Compile() error = %v", err)
			}
		})
	}
}
