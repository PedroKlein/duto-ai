package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
)

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
    instruction: {text: "Report ${WORKFLOW_SECRET} literally."}
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

func TestDecodeWorkflow_MinimalNoTools(t *testing.T) {
	t.Setenv("WORKFLOW_SECRET", "must-not-expand")

	workflow, err := config.DecodeWorkflow("workflow.yaml", []byte(minimalWorkflow))
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	if workflow.Version != 1 || workflow.Name != "minimal" || workflow.Model != "light" {
		t.Fatalf("DecodeWorkflow() identity = (%d, %q, %q)", workflow.Version, workflow.Name, workflow.Model)
	}

	if len(workflow.Tools) != 0 || len(workflow.Steps) != 1 || len(workflow.Steps[0].Tools) != 0 {
		t.Fatalf("DecodeWorkflow() tools/steps = (%v, %d, %v)", workflow.Tools, len(workflow.Steps), workflow.Steps[0].Tools)
	}

	if !strings.Contains(workflow.Steps[0].Instruction.Text, "${WORKFLOW_SECRET}") || strings.Contains(workflow.Steps[0].Instruction.Text, "must-not-expand") {
		t.Fatalf("portable workflow environment value was expanded: %q", workflow.Steps[0].Instruction.Text)
	}

	if workflow.Result.Step != "report" || workflow.Steps[0].Output.Properties["outcome"].Enum[0] != "completed" {
		t.Fatalf("DecodeWorkflow() terminal result was not preserved")
	}
}

func TestDecodeWorkflow_StrictRejections(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		code string
		path string
	}{
		{name: "unknown root field", data: replaceWorkflow("result: {step: report}", "result: {step: report}\nlegacy: true"), code: config.CodeUnknownField, path: "$.legacy"},
		{name: "unknown nested field", data: replaceWorkflow("    needs: []", "    needs: []\n    prompt: legacy"), code: config.CodeUnknownField, path: "$.steps[0].prompt"},
		{name: "duplicate key", data: replaceWorkflow("name: minimal", "name: minimal\nname: duplicate"), code: config.CodeDuplicateKey, path: "$.name"},
		{name: "anchor", data: replaceWorkflow("name: minimal", "name: &workflow-name minimal"), code: config.CodeAnchor, path: "$.name"},
		{name: "alias", data: replaceWorkflow("name: minimal", "name: &workflow-name minimal\ndescription: *workflow-name"), code: config.CodeAnchor, path: "$.name"},
		{name: "merge", data: replaceWorkflow("model: light", "<<: {}\nmodel: light"), code: config.CodeMerge, path: "$.<<"},
		{name: "null", data: replaceWorkflow("model: light", "model: null"), code: config.CodeNull, path: "$.model"},
		{name: "unsupported tag", data: replaceWorkflow("model: light", "model: !private light"), code: config.CodeUnsupportedTag, path: "$.model"},
		{name: "multiple documents", data: append([]byte(minimalWorkflow), []byte("---\nversion: 1\n")...), code: config.CodeMultipleDocs, path: "$"},
		{name: "invalid utf8", data: []byte{0xff, 0xfe}, code: config.CodeInvalidUTF8, path: "$"},
		{name: "scalar coercion", data: replaceWorkflow("name: minimal", "name: 123"), code: config.CodeInvalidType, path: "$.name"},
		{name: "invalid declared name", data: replaceWorkflow("  objective:\n    schema:", "  Bad_Name:\n    schema:"), code: config.CodeInvalidValue, path: "$.inputs.Bad_Name"},
		{name: "integer overflow", data: replaceWorkflow("  max_model_calls: 2", "  max_model_calls: 999999999999999999999999999999"), code: config.CodeInvalidValue, path: "$.limits.max_model_calls"},
		{name: "non-finite float", data: replaceWorkflow("model_config: {temperature: 0.2, max_output_tokens: 256}", "model_config: {temperature: .inf, max_output_tokens: 256}"), code: config.CodeInvalidValue, path: "$.model_config.temperature"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := config.DecodeWorkflow("workflow.yaml", test.data)
			if err == nil {
				t.Fatal("DecodeWorkflow() error = nil")
			}

			var diagnostic *config.DiagnosticError
			if !errors.As(err, &diagnostic) {
				t.Fatalf("DecodeWorkflow() error type = %T, want *DiagnosticError", err)
			}

			if diagnostic.Code != test.code || diagnostic.Path != test.path {
				t.Fatalf("diagnostic = (%q, %q), want (%q, %q)", diagnostic.Code, diagnostic.Path, test.code, test.path)
			}

			if diagnostic.File != "workflow.yaml" || diagnostic.Line < 1 || diagnostic.Column < 1 {
				t.Fatalf("diagnostic source = (%q, %d, %d)", diagnostic.File, diagnostic.Line, diagnostic.Column)
			}

			if strings.Contains(err.Error(), "must-not-expand") {
				t.Fatalf("diagnostic leaked scalar content: %q", err)
			}
		})
	}
}

func TestLoadWorkflow_UsesStrictDecoder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workflow.yaml")
	if err := os.WriteFile(path, []byte(minimalWorkflow), 0o600); err != nil {
		t.Fatal(err)
	}

	workflow, err := config.LoadWorkflow(path)
	if err != nil {
		t.Fatalf("LoadWorkflow() error = %v", err)
	}

	if workflow.Name != "minimal" {
		t.Fatalf("LoadWorkflow().Name = %q", workflow.Name)
	}
}

func TestLoadWorkflow_FileNotFound(t *testing.T) {
	_, err := config.LoadWorkflow("nonexistent.yaml")
	if err == nil {
		t.Fatal("LoadWorkflow() error = nil")
	}
}

func replaceWorkflow(old, replacement string) []byte {
	return []byte(strings.Replace(minimalWorkflow, old, replacement, 1))
}
