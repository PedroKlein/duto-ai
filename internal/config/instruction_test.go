package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
)

func TestDecodeWorkflow_InstructionSourcesAndSkills(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		instruction string
		kind        config.InstructionKind
	}{
		{name: "literal scalar", instruction: `Review carefully.`, kind: config.InstructionText},
		{name: "literal object", instruction: `{text: Review carefully.}`, kind: config.InstructionText},
		{name: "file", instruction: `{file: {workspace: source, path: prompts/review.md, max_bytes: 64}}`, kind: config.InstructionFile},
		{name: "template text", instruction: `{template: {text: 'Review {{ .Step.ID }}', max_output_bytes: 128}}`, kind: config.InstructionTemplate},
		{name: "template file", instruction: `{template: {file: {workspace: source, path: prompts/review.tmpl, max_bytes: 64}, max_output_bytes: 128}}`, kind: config.InstructionTemplateFile},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			data := strings.Replace(minimalWorkflow, `instruction: {text: "Report ${WORKFLOW_SECRET} literally."}`, "instruction: "+test.instruction, 1)
			data = strings.Replace(data, "tools: []\nlimits:", "tools: []\nskills:\n  go-review: {workspace: source, path: .agents/skills/go-review}\nlimits:", 1)
			data = strings.Replace(data, "    tools: []\n    workspaces:", "    tools: []\n    skills: [go-review]\n    workspaces:", 1)

			workflow, err := config.DecodeWorkflow("workflow.yaml", []byte(data))
			if err != nil {
				t.Fatalf("DecodeWorkflow() error = %v", err)
			}

			if workflow.Steps[0].Instruction.Kind != test.kind {
				t.Fatalf("instruction kind = %v, want %v", workflow.Steps[0].Instruction.Kind, test.kind)
			}

			if workflow.Skills["go-review"].Workspace != "source" || workflow.Steps[0].Skills[0] != "go-review" {
				t.Fatalf("skills = %#v / %#v", workflow.Skills, workflow.Steps[0].Skills)
			}
		})
	}
}

func TestDecodeWorkflow_InstructionUnionRejectsInvalidForms(t *testing.T) {
	t.Parallel()

	for _, instruction := range []string{
		`{text: literal, file: {workspace: source, path: prompt.md, max_bytes: 64}}`,
		`{file: {workspace: source, path: prompt.md}}`,
		`{template: {text: '{{ .Step.ID }}'}}`,
		`{template: {text: x, file: {workspace: source, path: prompt.md, max_bytes: 64}, max_output_bytes: 64}}`,
		`{template: {text: x, max_output_bytes: 64, unknown: true}}`,
	} {
		data := strings.Replace(minimalWorkflow, `instruction: {text: "Report ${WORKFLOW_SECRET} literally."}`, "instruction: "+instruction, 1)

		_, err := config.DecodeWorkflow("workflow.yaml", []byte(data))
		if err == nil {
			t.Fatalf("DecodeWorkflow(%s) error = nil", instruction)
		}

		var diagnostic *config.DiagnosticError
		if !errors.As(err, &diagnostic) {
			t.Fatalf("error type = %T", err)
		}
	}
}

func TestDecodeConfig_WorkspaceRootsExpandOnlyAfterStrictDecode(t *testing.T) {
	t.Setenv("DUTO_TEST_ROOT", "/trusted/root")

	data := minimalConfig + "workspaces:\n  source: {root: \"${DUTO_TEST_ROOT}\", access: read}\n"

	decoded, err := config.DecodeConfig("duto.yaml", []byte(data))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}

	if decoded.Workspaces["source"].Root != "/trusted/root" || decoded.Workspaces["source"].Access != "read" {
		t.Fatalf("workspace = %#v", decoded.Workspaces["source"])
	}

	invalid := strings.Replace(data, "access: read}", "access: read, unknown: true}", 1)
	_, err = config.DecodeConfig("duto.yaml", []byte(invalid))

	var diagnostic *config.DiagnosticError
	if !errors.As(err, &diagnostic) || diagnostic.Code != config.CodeUnknownField {
		t.Fatalf("strict workspace error = %v", err)
	}
}
