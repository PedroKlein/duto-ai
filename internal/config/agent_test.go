package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
)

const delegatedWorkflow = `version: 1
name: delegated
inputs:
  objective: {schema: {type: string, max_length: 64}}
model: capable
tools: []
limits: {timeout: 1m, max_iterations: 8, max_model_calls: 8, max_tool_calls: 4, max_concurrency: 1, max_parallel_calls: 2, max_artifact_bytes: 1024}
agents:
  researcher:
    description: Gather evidence.
    mode: single_turn
    model: light
    instruction: {text: Return evidence.}
    tools: []
    workspaces: []
    skills: []
    context:
      mode: snapshot
      include:
        - input: objective
    input:
      type: object
      properties: {question: {type: string, max_length: 64}}
      required: [question]
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}, evidence: {type: string, max_length: 128}}
      required: [outcome, evidence]
  coordinator:
    description: Coordinate research.
    mode: chat
    model: capable
    instruction: {text: "Delegate once, then decide."}
    tools: []
    workspaces: []
    skills: []
    context: {mode: fresh}
    subagents: [researcher]
    input:
      type: object
      properties: {objective: {type: string, max_length: 64}}
      required: [objective]
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

func TestDecodeWorkflow_NamedNativeAgents(t *testing.T) {
	workflow, err := config.DecodeWorkflow("workflow.yaml", []byte(delegatedWorkflow))
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	if len(workflow.AgentOrder) != 2 || workflow.AgentOrder[0] != "researcher" || workflow.AgentOrder[1] != "coordinator" {
		t.Fatalf("agent order = %v", workflow.AgentOrder)
	}

	researcher := workflow.Agents["researcher"]
	if researcher.Mode != "single_turn" || researcher.Context.Mode != "snapshot" || len(researcher.Context.Include) != 1 || researcher.Context.Include[0].Input != "objective" {
		t.Fatalf("researcher = %#v", researcher)
	}

	if workflow.Steps[0].Agent != "coordinator" || workflow.Steps[0].Instruction.Kind != config.InstructionUnknown {
		t.Fatalf("named step = %#v", workflow.Steps[0])
	}
}

func TestDecodeWorkflow_RejectsInvalidNamedAgentGraph(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
		path string
	}{
		{name: "unknown child", old: "subagents: [researcher]", new: "subagents: [missing]", path: "$.agents.coordinator.subagents[0]"},
		{name: "cycle", old: "context:\n      mode: snapshot", new: "subagents: [researcher]\n    context:\n      mode: snapshot", path: "$.agents"},
		{name: "task static step", old: "mode: chat\n    model: capable", new: "mode: task\n    model: capable", path: "$.steps[0].agent"},
		{name: "inline override", old: "    agent: coordinator\n    with:", new: "    agent: coordinator\n    instruction: forbidden\n    with:", path: "$.steps[0].instruction"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := strings.Replace(delegatedWorkflow, test.old, test.new, 1)

			_, err := config.DecodeWorkflow("workflow.yaml", []byte(data))
			if err == nil {
				t.Fatal("DecodeWorkflow() error = nil")
			}

			var diagnostic *config.DiagnosticError
			if !errors.As(err, &diagnostic) || diagnostic.Path != test.path {
				t.Fatalf("error = %v, want diagnostic path %q", err, test.path)
			}
		})
	}
}
