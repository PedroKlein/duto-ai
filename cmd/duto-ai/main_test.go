package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/adk/v2/model"

	"github.com/PedroKlein/duto-ai/internal/compiler"
	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/plan"
	"github.com/PedroKlein/duto-ai/internal/runtime"
	"github.com/PedroKlein/duto-ai/internal/testing/mockllm"
)

const testConfig = `version: 1
providers:
  default:
    type: custom-provider
    config:
      endpoint: https://example.invalid
      credential: test-credential
models:
  light:
    provider: default
    target: example-small-model
`

const testWorkflow = `version: 1
name: cli-test
inputs:
  objective:
    schema: {type: string, max_length: 64}
model: light
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

const testNoInputWorkflow = `version: 1
name: cli-run
model: light
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
    instruction: {text: Return a typed report.}
    tools: []
    workspaces: []
    input: {type: object, properties: {}, required: []}
    with: {}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        report: {type: string, max_length: 256}
      required: [outcome, report]
result: {step: report}
`

func TestCLIValidateFileText(t *testing.T) {
	configPath, workflowPath := writeInputs(t)

	code, stdout, stderr := executeForTest(t, []string{"validate", "--config", configPath, workflowPath}, bytes.NewReader(nil), nil)

	assertCommandResult(t, code, stdout, stderr, exitSuccess, "valid\n", "")
}

func TestCLIValidateStdinJSON(t *testing.T) {
	configPath, _ := writeInputs(t)

	code, stdout, stderr := executeForTest(t, []string{"validate", "--format", "json", "--config", configPath, "-"}, bytes.NewBufferString(testWorkflow), nil)

	assertCommandResult(t, code, stdout, stderr, exitSuccess, "{\"version\":1,\"valid\":true}\n", "")
}

func TestCLIPlanFormats(t *testing.T) {
	configPath, workflowPath := writeInputs(t)
	cfg, workflow := decodeInputs(t)

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("plan.Compile() error = %v", err)
	}

	tests := []struct {
		name   string
		format string
		want   string
	}{
		{name: "text", format: "text", want: string(compiled.Text()) + "\n"},
		{name: "json", format: "json", want: string(compiled.JSON()) + "\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, stdout, stderr := executeForTest(t, []string{"plan", "--format", test.format, "--config", configPath, workflowPath}, bytes.NewReader(nil), nil)
			assertCommandResult(t, code, stdout, stderr, exitSuccess, test.want, "")
		})
	}
}

func TestCLIRunUsesAdmittedPlan(t *testing.T) {
	configPath, workflowPath := writeInputs(t)
	calls := 0
	run := func(_ context.Context, cfg *config.Config, compiled *plan.Plan, format outputFormat) ([]byte, error) {
		calls++

		if cfg.Version != 1 || compiled.Snapshot().Workflow.Name != "cli-test" || compiled.Digest() == "" || format != formatJSON {
			return nil, errors.New("invalid admitted inputs")
		}

		return []byte(`{"version":1,"status":"succeeded"}`), nil
	}

	code, stdout, stderr := executeForTest(t, []string{"run", "--format", "json", "--config", configPath, workflowPath}, bytes.NewReader(nil), run)

	assertCommandResult(t, code, stdout, stderr, exitSuccess, "{\"version\":1,\"status\":\"succeeded\"}\n", "")

	if calls != 1 {
		t.Fatalf("run calls = %d, want 1", calls)
	}
}

func TestRunAdmittedWorkflow_RejectsRequiredInputsBeforeProviderConstruction(t *testing.T) {
	cfg, workflow := decodeInputs(t)

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("plan.Compile() error = %v", err)
	}

	_, err = runAdmittedWorkflow(t.Context(), cfg, compiled, formatJSON)

	var commandErr *commandError
	if !errors.As(err, &commandErr) || commandErr.code != exitExecution || !errors.Is(err, errWorkflowInputs) {
		t.Fatalf("runAdmittedWorkflow() error = %v", err)
	}
}

func TestCLIRunFakeModelJSON(t *testing.T) {
	configPath, workflowPath := writeNoInputInputs(t)
	llm := mockllm.New(mockllm.Response{Text: `{"outcome":"completed","report":"cli"}`})
	run := func(ctx context.Context, _ *config.Config, compiled *plan.Plan, format outputFormat) ([]byte, error) {
		resolver := func(context.Context, string) (model.LLM, error) { return llm, nil }

		result, err := runtime.Run(ctx, compiled, compiler.ModelResolver(resolver))
		if err != nil {
			return nil, err
		}

		if format != formatJSON {
			return nil, errors.New("unexpected output format")
		}

		return result.JSON()
	}

	code, stdout, stderr := executeForTest(t, []string{"run", "--format", "json", "--config", configPath, workflowPath}, bytes.NewReader(nil), run)
	if code != exitSuccess || stderr != "" {
		t.Fatalf("exit/stderr = %d/%q, want 0/empty", code, stderr)
	}

	var result runtime.Result
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout is not one JSON result payload: %v\n%s", err, stdout)
	}

	if result.Output["report"] != "cli" || result.Status != runtime.StatusSucceeded || stdout[len(stdout)-1] != '\n' {
		t.Fatalf("result/stdout = %#v/%q", result, stdout)
	}
}

func TestCLIRejectsV02FlagsBeforeRun(t *testing.T) {
	configPath, workflowPath := writeInputs(t)
	legacyFlags := []string{"--repo", "--pr", "--event", "--dry-run", "--verbose", "--output-format", "--output-file", "--log-level"}

	for _, legacyFlag := range legacyFlags {
		t.Run(legacyFlag, func(t *testing.T) {
			calls := 0
			run := func(context.Context, *config.Config, *plan.Plan, outputFormat) ([]byte, error) {
				calls++
				return nil, nil
			}

			args := []string{"run", "--config", configPath, legacyFlag, workflowPath}
			code, stdout, stderr := executeForTest(t, args, bytes.NewReader(nil), run)
			wantStderr := fmt.Sprintf("error: unknown flag: %s\n", legacyFlag)
			assertCommandResult(t, code, stdout, stderr, exitUsage, "", wantStderr)

			if calls != 0 {
				t.Fatalf("run calls = %d, want 0", calls)
			}
		})
	}
}

func TestCLIUsageErrors(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{name: "missing command", args: nil, wantStderr: "error: command is required\n"},
		{name: "unknown command", args: []string{"unknown"}, wantStderr: "error: unknown command \"unknown\" for \"duto-ai\"\n"},
		{name: "missing workflow", args: []string{"validate"}, wantStderr: "error: workflow is required\n"},
		{name: "extra workflow", args: []string{"validate", "one.yaml", "two.yaml"}, wantStderr: "error: accepts 1 arg(s), received 2\n"},
		{name: "invalid format", args: []string{"validate", "--format", "markdown", "workflow.yaml"}, wantStderr: "error: invalid format \"markdown\"\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, stdout, stderr := executeForTest(t, test.args, bytes.NewReader(nil), nil)
			assertCommandResult(t, code, stdout, stderr, exitUsage, "", test.wantStderr)
		})
	}
}

func TestCLIAdmissionError(t *testing.T) {
	configPath, workflowPath := writeInputs(t)
	if err := os.WriteFile(workflowPath, []byte("version: 1\nunknown: true\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	code, stdout, stderr := executeForTest(t, []string{"validate", "--config", configPath, workflowPath}, bytes.NewReader(nil), nil)

	wantStderr := fmt.Sprintf("error: loading workflow: %s:2:1: $.unknown: unknown_field\n", workflowPath)
	assertCommandResult(t, code, stdout, stderr, exitAdmission, "", wantStderr)
}

func TestCLIRunExitClasses(t *testing.T) {
	configPath, workflowPath := writeInputs(t)

	tests := []struct {
		name       string
		runErr     error
		wantCode   int
		wantStderr string
	}{
		{name: "execution", runErr: executionError(errors.New("step stopped")), wantCode: exitExecution, wantStderr: "error: step stopped\n"},
		{name: "cancellation", runErr: context.Canceled, wantCode: exitCancelled, wantStderr: "error: context canceled\n"},
		{name: "internal", runErr: errors.New("broken invariant"), wantCode: exitInternal, wantStderr: "error: broken invariant\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			run := func(context.Context, *config.Config, *plan.Plan, outputFormat) ([]byte, error) {
				return nil, test.runErr
			}

			code, stdout, stderr := executeForTest(t, []string{"run", "--config", configPath, workflowPath}, bytes.NewReader(nil), run)
			assertCommandResult(t, code, stdout, stderr, test.wantCode, "", test.wantStderr)
		})
	}
}

func TestBundledModelResolver_RedactsTrustedBindingFailure(t *testing.T) {
	cfg, _ := decodeInputs(t)

	_, err := bundledModelResolver(cfg)(t.Context(), "light")
	if !errors.Is(err, errBundledProvider) {
		t.Fatalf("resolver error = %v, want errBundledProvider", err)
	}

	for _, forbidden := range []string{"custom-provider", "example-small-model", "test-credential", "example.invalid"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("resolver error leaked %q: %v", forbidden, err)
		}
	}
}

func TestCLIVersion(t *testing.T) {
	code, stdout, stderr := executeForTest(t, []string{"version"}, bytes.NewReader(nil), nil)
	assertCommandResult(t, code, stdout, stderr, exitSuccess, "duto-ai dev (none)\n", "")
}

func TestCLIProcessContract(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "duto-ai")

	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build error = %v\n%s", err, output)
	}

	configPath, workflowPath := writeInputs(t)
	cfg, workflow := decodeInputs(t)

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("plan.Compile() error = %v", err)
	}

	tests := []struct {
		name       string
		args       []string
		stdin      string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "validate file text",
			args:       []string{"validate", "--config", configPath, workflowPath},
			wantCode:   exitSuccess,
			wantStdout: "valid\n",
		},
		{
			name:       "validate stdin json",
			args:       []string{"validate", "--format", "json", "--config", configPath, "-"},
			stdin:      testWorkflow,
			wantCode:   exitSuccess,
			wantStdout: "{\"version\":1,\"valid\":true}\n",
		},
		{
			name:       "plan file json",
			args:       []string{"plan", "--format", "json", "--config", configPath, workflowPath},
			wantCode:   exitSuccess,
			wantStdout: string(compiled.JSON()) + "\n",
		},
		{
			name:       "plan stdin text",
			args:       []string{"plan", "--config", configPath, "-"},
			stdin:      testWorkflow,
			wantCode:   exitSuccess,
			wantStdout: string(compiled.Text()) + "\n",
		},
		{
			name:       "run rejects v0.2 dry run",
			args:       []string{"run", "--dry-run", "--config", configPath, workflowPath},
			wantCode:   exitUsage,
			wantStderr: "error: unknown flag: --dry-run\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command(binary, test.args...)
			command.Stdin = bytes.NewBufferString(test.stdin)

			var (
				stdout bytes.Buffer
				stderr bytes.Buffer
			)

			command.Stdout = &stdout
			command.Stderr = &stderr

			code := exitSuccess

			if err := command.Run(); err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatalf("command.Run() error = %v", err)
				}

				code = exitErr.ExitCode()
			}

			assertCommandResult(t, code, stdout.String(), stderr.String(), test.wantCode, test.wantStdout, test.wantStderr)
		})
	}
}

func executeForTest(t *testing.T, args []string, stdin io.Reader, run runWorkflow) (int, string, string) {
	t.Helper()

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	dependencies := commandDependencies{stdin: stdin, stdout: &stdout, stderr: &stderr, run: run}

	return execute(context.Background(), args, dependencies), stdout.String(), stderr.String()
}

func writeInputs(t *testing.T) (string, string) {
	t.Helper()

	directory := t.TempDir()
	configPath := filepath.Join(directory, "duto.yaml")
	workflowPath := filepath.Join(directory, "workflow.yaml")

	if err := os.WriteFile(configPath, []byte(testConfig), 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	if err := os.WriteFile(workflowPath, []byte(testWorkflow), 0o600); err != nil {
		t.Fatalf("WriteFile(workflow) error = %v", err)
	}

	return configPath, workflowPath
}

func writeNoInputInputs(t *testing.T) (string, string) {
	t.Helper()

	directory := t.TempDir()
	configPath := filepath.Join(directory, "duto.yaml")
	workflowPath := filepath.Join(directory, "workflow.yaml")

	if err := os.WriteFile(configPath, []byte(testConfig), 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	if err := os.WriteFile(workflowPath, []byte(testNoInputWorkflow), 0o600); err != nil {
		t.Fatalf("WriteFile(workflow) error = %v", err)
	}

	return configPath, workflowPath
}

func decodeInputs(t *testing.T) (*config.Config, *config.Workflow) {
	t.Helper()

	cfg, err := config.DecodeConfig("duto.yaml", []byte(testConfig))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}

	workflow, err := config.DecodeWorkflow("workflow.yaml", []byte(testWorkflow))
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	return cfg, workflow
}

func assertCommandResult(t *testing.T, gotCode int, gotStdout, gotStderr string, wantCode int, wantStdout, wantStderr string) {
	t.Helper()

	if gotCode != wantCode {
		t.Errorf("exit code = %d, want %d", gotCode, wantCode)
	}

	if gotStdout != wantStdout {
		t.Errorf("stdout = %q, want %q", gotStdout, wantStdout)
	}

	if gotStderr != wantStderr {
		t.Errorf("stderr = %q, want %q", gotStderr, wantStderr)
	}
}
