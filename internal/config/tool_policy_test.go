package config_test

import (
	"errors"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
)

func TestDecodeConfig_ToolPolicyIsStrict(t *testing.T) {
	data := minimalConfig + `tools: [files.*, git.read.*]
tool_profiles:
  source-review: [files.read, git.read.diff]
tool_limits:
  files.read: {max_calls: 4, timeout: 5s, max_request_bytes: 128, max_result_bytes: 256}
`

	value, err := config.DecodeConfig("duto.yaml", []byte(data))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}

	if len(value.Tools) != 2 || len(value.ToolProfiles["source-review"]) != 2 || value.ToolLimits["files.read"].MaxCalls != 4 {
		t.Fatalf("decoded tool policy = %#v", value)
	}

	_, err = config.DecodeConfig("duto.yaml", []byte(data+"  files.grep: {max_calls: 1, unexpected: true}\n"))

	var diagnostic *config.DiagnosticError
	if !errors.As(err, &diagnostic) || diagnostic.Code != config.CodeUnknownField {
		t.Fatalf("strict tool limit error = %v", err)
	}
}

func TestDecodeWorkflow_ToolExpressionsProfilesAndLimits(t *testing.T) {
	data := replaceWorkflow(
		"model: light\nmodel_config:",
		"model: light\ntool_profiles:\n  source-review: [files.read, git.read.diff]\nmodel_config:",
	)
	data = []byte(string(data))
	data = []byte(replaceString(string(data), "tools: []\nlimits:", "tools:\n  from: empty\n  add_profiles: [source-review]\n  add: [files.grep]\n  remove_profiles: []\n  remove: [files.read]\ntool_limits:\n  git.read.diff: {max_calls: 2, timeout: 3s, max_result_bytes: 128}\nlimits:"))

	workflow, err := config.DecodeWorkflow("workflow.yaml", data)
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	if workflow.Tools.From != "empty" || len(workflow.Tools.AddProfiles) != 1 || len(workflow.Tools.Remove) != 1 || workflow.ToolLimits["git.read.diff"].MaxCalls != 2 {
		t.Fatalf("decoded workflow tool policy = %#v/%#v", workflow.Tools, workflow.ToolLimits)
	}
}

func replaceString(value, old, replacement string) string {
	for i := 0; i+len(old) <= len(value); i++ {
		if value[i:i+len(old)] == old {
			return value[:i] + replacement + value[i+len(old):]
		}
	}

	return value
}
