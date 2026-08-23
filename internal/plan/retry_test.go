package plan_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/plan"
)

func TestCompile_RetryUsesNativeBoundedPolicy(t *testing.T) {
	source := strings.Replace(minimalWorkflow, "    output:\n", "    retry: {max_attempts: 3, initial_delay: 1ms, max_delay: 4ms}\n    output:\n", 1)
	cfg, workflow := decodeInputs(t, runtimeConfig, source)

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	retry := compiled.Snapshot().Workflow.Steps[0].Retry
	if retry.MaxAttempts != 3 || retry.InitialDelay != "1ms" || retry.MaxDelay != "4ms" {
		t.Fatalf("retry = %#v", retry)
	}
}

func TestCompile_RejectsRetryForProcessAuthority(t *testing.T) {
	configYAML := runtimeConfig + `workspaces:
  source: {root: ., access: read}
tool_config:
  shell: {executable: /bin/echo, args: [], workspace: source, environment: {}, max_stdout_bytes: 64, max_stderr_bytes: 64}
tools: [shell.run]
tool_limits:
  shell.run: {max_calls: 2, timeout: 2s, max_request_bytes: 64, max_result_bytes: 256}
`
	source := strings.Replace(minimalWorkflow, "tools: []\nlimits:", "tools: [shell.run]\ntool_limits:\n  shell.run: {max_calls: 2, timeout: 2s}\nlimits:", 1)
	source = strings.Replace(source, "    tools: []\n", "    tools: {from: parent}\n", 1)
	source = strings.Replace(source, "    output:\n", "    retry: {max_attempts: 2, initial_delay: 1ms, max_delay: 2ms}\n    output:\n", 1)
	cfg, workflow := decodeInputs(t, configYAML, source)

	if _, err := plan.Compile(cfg, workflow); !errors.Is(err, plan.ErrInvalidLimits) {
		t.Fatalf("Compile() error = %v, want ErrInvalidLimits for retry with process authority", err)
	}
}
