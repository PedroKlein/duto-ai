package plan_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/plan"
)

const runtimeConfig = `version: 1
providers:
  default:
    type: custom-provider
    config:
      endpoint: https://example.invalid
      credential: canary-credential
models:
  light:
    provider: default
    target: example-small-model
`

const minimalWorkflow = `version: 1
name: minimal
inputs:
  objective:
    schema: {type: string, max_length: 64}
model: light
model_config: {temperature: 0.2, max_output_tokens: 256}
tools: []
limits:
  timeout: 1m
  max_iterations: 2
  max_model_calls: 2
  max_tool_calls: 0
  max_concurrency: 1
  max_parallel_calls: 1
  max_artifact_bytes: 0
steps:
  - id: report
    needs: []
    instruction: {text: Report the objective.}
    tools: []
    workspaces: []
    input:
      type: object
      properties:
        objective: {type: string, max_length: 64}
      required: [objective]
    with:
      objective: {input: objective}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        report: {type: string, max_length: 256}
      required: [outcome, report]
result: {step: report}
`

func TestCompile_MinimalNoTools(t *testing.T) {
	cfg, workflow := decodeInputs(t, runtimeConfig, minimalWorkflow)

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	firstJSON := compiled.JSON()
	secondJSON := compiled.JSON()

	if diff := cmp.Diff(firstJSON, secondJSON); diff != "" {
		t.Fatalf("JSON() is not deterministic (-first +second):\n%s", diff)
	}

	secondConfig, secondWorkflow := decodeInputs(t, runtimeConfig, minimalWorkflow)

	secondPlan, err := plan.Compile(secondConfig, secondWorkflow)
	if err != nil {
		t.Fatalf("second Compile() error = %v", err)
	}

	if diff := cmp.Diff(firstJSON, secondPlan.JSON()); diff != "" {
		t.Fatalf("repeated compilation differs (-first +second):\n%s", diff)
	}

	var jsonProjection plan.Projection
	if decodeErr := json.Unmarshal(firstJSON, &jsonProjection); decodeErr != nil {
		t.Fatalf("json.Unmarshal(JSON()) error = %v", decodeErr)
	}

	var textProjection plan.Projection
	if decodeErr := json.Unmarshal(compiled.Text(), &textProjection); decodeErr != nil {
		t.Fatalf("json.Unmarshal(Text()) error = %v", decodeErr)
	}

	if diff := cmp.Diff(jsonProjection, textProjection); diff != "" {
		t.Fatalf("text and JSON projections differ (-json +text):\n%s", diff)
	}

	if jsonProjection.Version != 1 || jsonProjection.Workflow.Name != "minimal" || jsonProjection.Workflow.Model != "light" {
		t.Fatalf("projection identity = %#v", jsonProjection)
	}

	if len(jsonProjection.Workflow.Steps) != 1 || len(jsonProjection.Workflow.Steps[0].Tools.Names) != 0 {
		t.Fatalf("projection steps/tools = %#v", jsonProjection.Workflow.Steps)
	}

	propertyNames := []string{
		jsonProjection.Workflow.Steps[0].Output.Properties[0].Name,
		jsonProjection.Workflow.Steps[0].Output.Properties[1].Name,
	}
	if diff := cmp.Diff([]string{"outcome", "report"}, propertyNames); diff != "" {
		t.Fatalf("schema property order mismatch (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff([]string{"completed"}, jsonProjection.Workflow.Result.Outcomes); diff != "" {
		t.Fatalf("terminal outcomes mismatch (-want +got):\n%s", diff)
	}

	body := jsonProjection
	body.Digest = ""

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal(projection body) error = %v", err)
	}

	sum := sha256.Sum256(bodyJSON)
	if want := hex.EncodeToString(sum[:]); compiled.Digest() != want || jsonProjection.Digest != want {
		t.Fatalf("digest = (%q, %q), want %q", compiled.Digest(), jsonProjection.Digest, want)
	}

	if strings.Contains(string(firstJSON), "canary-credential") || strings.Contains(string(firstJSON), "example-small-model") || strings.Contains(string(firstJSON), "example.invalid") {
		t.Fatalf("projection leaked trusted provider data: %s", firstJSON)
	}
}

func TestCompile_IsImmutable(t *testing.T) {
	cfg, workflow := decodeInputs(t, runtimeConfig, minimalWorkflow)

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	want := compiled.JSON()

	cfg.Models["light"] = config.Model{Provider: "changed", Target: "changed"}
	workflow.Name = "changed"
	workflow.Steps[0].Instruction.Text = "changed"
	outcome := workflow.Steps[0].Output.Properties["outcome"]
	outcome.Enum[0] = "changed"
	workflow.Steps[0].Output.Properties["outcome"] = outcome

	returnedJSON := compiled.JSON()
	returnedJSON[0] = '['
	snapshot := compiled.Snapshot()
	snapshot.Workflow.Name = "changed-again"
	snapshot.Workflow.Steps[0].Needs = append(snapshot.Workflow.Steps[0].Needs, "changed")

	if diff := cmp.Diff(want, compiled.JSON()); diff != "" {
		t.Fatalf("plan changed after source/accessor mutation (-want +got):\n%s", diff)
	}
}

func TestCompile_UnknownAliasStopsConstruction(t *testing.T) {
	cfg, workflow := decodeInputs(t, runtimeConfig, minimalWorkflow)
	workflow.Model = "unknown"
	workflow.Tools = config.ToolExpression{Add: []string{"files.read"}}

	constructionCalls := 0

	compiled, err := plan.Compile(cfg, workflow)
	if err == nil || !errors.Is(err, plan.ErrUnknownModel) {
		t.Fatalf("Compile() error = %v, want ErrUnknownModel", err)
	}

	if compiled != nil {
		constructionCalls++
	}

	if constructionCalls != 0 {
		t.Fatalf("construction calls = %d, want 0", constructionCalls)
	}
}

func TestCompile_RejectsInvalidBoundedSchema(t *testing.T) {
	cfg, workflow := decodeInputs(t, runtimeConfig, minimalWorkflow)
	report := workflow.Steps[0].Output.Properties["report"]
	report.MaxLength = 0
	workflow.Steps[0].Output.Properties["report"] = report

	_, err := plan.Compile(cfg, workflow)
	if err == nil || !errors.Is(err, plan.ErrInvalidSchema) {
		t.Fatalf("Compile() error = %v, want ErrInvalidSchema", err)
	}
}

func TestCompile_RejectsInvalidModelLimit(t *testing.T) {
	cfg, workflow := decodeInputs(t, runtimeConfig, minimalWorkflow)
	workflow.ModelConfig.MaxOutputTokens = 0

	_, err := plan.Compile(cfg, workflow)
	if err == nil || !errors.Is(err, plan.ErrInvalidLimits) {
		t.Fatalf("Compile() error = %v, want ErrInvalidLimits", err)
	}
}

func TestCompile_RequiresDirectTerminalResult(t *testing.T) {
	cfg, workflow := decodeInputs(t, runtimeConfig, minimalWorkflow)
	final := workflow.Steps[0]
	final.ID = "final"
	final.Needs = []string{"report"}
	workflow.Steps = append(workflow.Steps, final)

	_, err := plan.Compile(cfg, workflow)
	if err == nil || !errors.Is(err, plan.ErrInvalidResult) {
		t.Fatalf("Compile() error = %v, want ErrInvalidResult", err)
	}
}

func decodeInputs(t *testing.T, configYAML, workflowYAML string) (*config.Config, *config.Workflow) {
	t.Helper()

	cfg, err := config.DecodeConfig("duto.yaml", []byte(configYAML))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}

	workflow, err := config.DecodeWorkflow("workflow.yaml", []byte(workflowYAML))
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	return cfg, workflow
}
