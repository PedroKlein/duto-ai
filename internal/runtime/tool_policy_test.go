package runtime_test

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/plan"
	"github.com/PedroKlein/duto-ai/internal/runtime"
	"github.com/PedroKlein/duto-ai/internal/testing/mockllm"
	dtool "github.com/PedroKlein/duto-ai/internal/tool"
	"github.com/PedroKlein/duto-ai/internal/tool/shell"
)

type boundedReadArgs struct {
	Path string `json:"path"`
}

type boundedReadResult struct {
	Content string `json:"content"`
}

const guardedToolConfig = `version: 1
providers:
  default: {type: custom-provider, config: {credential: canary}}
models:
  light: {provider: default, target: example-model}
workspaces:
  source: {root: ., access: read}
tool_config:
  files: {workspace: source}
tools: [files.read]
tool_limits:
  files.read: {max_calls: 2, timeout: 5s, max_request_bytes: 128, max_result_bytes: 256}
`

const guardedToolWorkflow = `version: 1
name: guarded-tool
model: light
tools: [files.read]
tool_limits:
  files.read: {max_calls: 2, timeout: 2s}
limits: {timeout: 30s, max_iterations: 4, max_model_calls: 4, max_tool_calls: 2, max_concurrency: 1, max_parallel_calls: 1, max_artifact_bytes: 0}
steps:
  - id: inspect
    needs: []
    instruction: Inspect two files.
    tools: {from: parent}
    tool_limits: {files.read: {max_calls: 2, timeout: 1s}}
    workspaces: []
    input: {type: object, properties: {}, required: []}
    with: {}
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}, report: {type: string, max_length: 64}}
      required: [outcome, report]
result: {step: inspect}
`

func TestRunWithInputsAndToolsets_ExposesExactToolsAndBoundsParallelCalls(t *testing.T) {
	cfg, err := config.DecodeConfig("duto.yaml", []byte(guardedToolConfig))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}

	workflow, err := config.DecodeWorkflow("workflow.yaml", []byte(guardedToolWorkflow))
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("plan.Compile() error = %v", err)
	}

	var (
		active          atomic.Int64
		maximum         atomic.Int64
		boundedDeadline atomic.Bool
	)
	boundedDeadline.Store(true)

	readTool, err := functiontool.New(functiontool.Config{Name: "files.read", Description: "Read one admitted file."}, func(ctx agent.Context, args boundedReadArgs) (boundedReadResult, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 1100*time.Millisecond {
			boundedDeadline.Store(false)
		}

		current := active.Add(1)
		defer active.Add(-1)

		for {
			prior := maximum.Load()
			if current <= prior || maximum.CompareAndSwap(prior, current) {
				break
			}
		}

		time.Sleep(20 * time.Millisecond)

		return boundedReadResult{Content: args.Path}, nil
	})
	if err != nil {
		t.Fatalf("functiontool.New() error = %v", err)
	}

	registry := dtool.NewRegistry()
	registry.Register("files.read", readTool)

	llm := mockllm.New(
		mockllm.Response{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "read-1", Name: "files.read", Args: map[string]any{"path": "one"}}},
			{FunctionCall: &genai.FunctionCall{ID: "read-2", Name: "files.read", Args: map[string]any{"path": "two"}}},
		}}},
		mockllm.Response{Text: `{"outcome":"completed","report":"done"}`},
	)

	result, err := runtime.RunWithInputsAndToolsets(
		t.Context(),
		compiled,
		func(context.Context, string) (model.LLM, error) { return llm, nil },
		registry.FilteredToolset,
		map[string]any{},
	)
	if err != nil {
		t.Fatalf("RunWithInputsAndToolsets() error = %v, result = %#v", err, result)
	}

	if maximum.Load() != 1 {
		t.Fatalf("maximum parallel tool calls = %d, want 1", maximum.Load())
	}

	if !boundedDeadline.Load() {
		t.Fatal("tool handler did not receive earliest policy deadline")
	}

	calls := llm.Calls()
	if len(calls) != 2 || findToolDeclaration(calls[0].Config, "files.read") == nil || findToolDeclaration(calls[0].Config, "web.fetch") != nil {
		t.Fatalf("model tool declarations = %#v", calls)
	}
}

func TestRunWithInputsAndToolsets_DebitsShellAttemptBeforeProcessStart(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("trusted shell fixture requires /bin/sh")
	}

	cfg, err := config.DecodeConfig("duto.yaml", []byte(`version: 1
providers:
  default: {type: custom-provider, config: {credential: canary}}
models:
  light: {provider: default, target: example-model}
workspaces:
  source: {root: ., access: read}
tool_config:
  shell: {executable: /bin/echo, args: [], workspace: source, environment: {}, max_stdout_bytes: 64, max_stderr_bytes: 64}
tools: [shell.run]
tool_limits:
  shell.run: {max_calls: 1, timeout: 5s, max_request_bytes: 64, max_result_bytes: 256}
`))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}

	workflow, err := config.DecodeWorkflow("workflow.yaml", []byte(`version: 1
name: bounded-shell
model: light
tools: [shell.run]
tool_limits: {shell.run: {max_calls: 1, timeout: 2s}}
limits: {timeout: 30s, max_iterations: 4, max_model_calls: 4, max_tool_calls: 1, max_concurrency: 1, max_parallel_calls: 1, max_artifact_bytes: 0}
steps:
  - id: execute
    needs: []
    instruction: Run the trusted command twice.
    tools: {from: parent}
    tool_limits: {shell.run: {max_calls: 1, timeout: 1s}}
    workspaces: []
    input: {type: object, properties: {}, required: []}
    with: {}
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}, report: {type: string, max_length: 64}}
      required: [outcome, report]
result: {step: execute}
`))
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("plan.Compile() error = %v", err)
	}

	marker := filepath.Join(t.TempDir(), "starts")

	registry := dtool.NewRegistry()

	registrationErr := shell.RegisterAll(registry, shell.Policy{
		Executable:     "/bin/sh",
		Args:           []string{"-c", `printf x >> "$1"`, "shell.run", marker},
		Workspace:      t.TempDir(),
		Environment:    map[string]string{"LC_ALL": "C"},
		MaxStdoutBytes: 64,
		MaxStderrBytes: 64,
		Limit:          dtool.ToolLimit{MaxCalls: 1, Timeout: time.Second, MaxRequestBytes: 64, MaxResultBytes: 256},
	})
	if registrationErr != nil {
		t.Fatalf("shell.RegisterAll() error = %v", registrationErr)
	}

	llm := mockllm.New(mockllm.Response{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{
		{FunctionCall: &genai.FunctionCall{ID: "run-1", Name: "shell.run", Args: map[string]any{}}},
		{FunctionCall: &genai.FunctionCall{ID: "run-2", Name: "shell.run", Args: map[string]any{}}},
	}}})

	_, _ = runtime.RunWithInputsAndToolsets(
		t.Context(),
		compiled,
		func(context.Context, string) (model.LLM, error) { return llm, nil },
		registry.FilteredToolset,
		map[string]any{},
	)

	started, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("reading process marker: %v", err)
	}

	if string(started) != "x" {
		t.Fatalf("process starts = %q, want exactly one", started)
	}
}

func findToolDeclaration(cfg *genai.GenerateContentConfig, name string) *genai.FunctionDeclaration {
	if cfg == nil {
		return nil
	}

	for _, packed := range cfg.Tools {
		for _, declaration := range packed.FunctionDeclarations {
			if declaration.Name == name {
				return declaration
			}
		}
	}

	return nil
}
