package compiler_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/adk/v2/model"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/plan"
	"github.com/PedroKlein/duto-ai/internal/runtime"
	"github.com/PedroKlein/duto-ai/internal/testing/mockllm"
)

func TestCompile_UsesFrozenTemplateAndRestrictedNativeSkills(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	skillDirectory := filepath.Join(root, "skills", "go-review")
	if err := os.MkdirAll(skillDirectory, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "instruction.tmpl"), []byte(`{{ .Workflow.Name }}/{{ .Step.ID }}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDirectory, "SKILL.md"), []byte("---\nname: go-review\ndescription: Review Go code.\nallowed-tools: [shell.run]\n---\nReview safely."), 0o600); err != nil {
		t.Fatal(err)
	}

	configYAML := fmt.Sprintf(`version: 1
providers:
  default: {type: custom-provider, config: {endpoint: "https://example.invalid", credential: placeholder}}
models:
  light: {provider: default, target: example-small-model}
workspaces:
  source: {root: %q, access: read}
`, root)
	workflowYAML := `version: 1
name: prompt-check
model: light
tools: []
skills:
  go-review: {workspace: source, path: skills/go-review}
limits: {timeout: 1m, max_iterations: 2, max_model_calls: 2, max_tool_calls: 0, max_concurrency: 1, max_parallel_calls: 1, max_artifact_bytes: 0}
steps:
  - id: report
    needs: []
    instruction: {template: {file: {workspace: source, path: instruction.tmpl, max_bytes: 256}, max_output_bytes: 256}}
    tools: []
    skills: [go-review]
    workspaces: [{name: source, access: read}]
    input: {type: object, properties: {}, required: []}
    with: {}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        report: {type: string, max_length: 64}
      required: [outcome, report]
result: {step: report}
`

	cfg, err := config.DecodeConfig("duto.yaml", []byte(configYAML))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}

	workflow, err := config.DecodeWorkflow("workflow.yaml", []byte(workflowYAML))
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("plan.Compile() error = %v", err)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "instruction.tmpl"), []byte("changed"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	llm := mockllm.New(mockllm.Response{Text: `{"outcome":"completed","report":"ok"}`})

	result, err := runtime.Run(t.Context(), compiled, func(_ context.Context, _ string) (model.LLM, error) { return llm, nil })
	if err != nil {
		t.Fatalf("runtime.Run() error = %v, result = %#v", err, result)
	}

	calls := llm.Calls()
	if len(calls) != 1 {
		t.Fatalf("model calls = %d, want 1", len(calls))
	}

	var instruction strings.Builder
	for _, part := range calls[0].Config.SystemInstruction.Parts {
		instruction.WriteString(part.Text)
	}

	if !strings.Contains(instruction.String(), "prompt-check/report") || strings.Contains(instruction.String(), "changed") {
		t.Fatalf("system instruction = %q", instruction.String())
	}

	if findDeclaration(calls[0].Config, "load_skill") == nil || findDeclaration(calls[0].Config, "shell.run") != nil {
		t.Fatalf("skill declarations widened authority: %#v", calls[0].Config.Tools)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "instruction.tmpl"), []byte(`{{ .Step.Inputs.missing }}`), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	missingPlan, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("plan.Compile(missing runtime value) error = %v", err)
	}

	unreached := mockllm.New(mockllm.Response{Text: `{"outcome":"completed","report":"unreached"}`})

	if _, err := runtime.Run(t.Context(), missingPlan, func(_ context.Context, _ string) (model.LLM, error) { return unreached, nil }); err == nil {
		t.Fatal("runtime.Run() error = nil for missing template value")
	}

	if got := unreached.CallCount(); got != 0 {
		t.Fatalf("model calls after render failure = %d, want 0", got)
	}
}
