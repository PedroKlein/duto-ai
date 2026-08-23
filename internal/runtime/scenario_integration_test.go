//go:build integration

package runtime_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/compiler"
	"github.com/PedroKlein/duto-ai/internal/plan"
	"github.com/PedroKlein/duto-ai/internal/prompt"
	"github.com/PedroKlein/duto-ai/internal/runtime"
	"github.com/PedroKlein/duto-ai/internal/testing/mockllm"
	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

const scenarioSetDigest = "a9f4458fed938d5332fd75c5331d8dbedbc3c79a9905089cbf920e432afaca0a"

type scenarioCase struct {
	name  string
	owner string
	check func(*testing.T)
}

func TestScenarioSet(t *testing.T) {
	cases := []scenarioCase{
		{name: "agent-skills", owner: "M1", check: checkSkillScenario},
		{name: "context-files", owner: "M1", check: checkFileInstructionScenario},
		{name: "file-exploration", owner: "M1", check: checkToolScenario("files.read", "files.find", "files.grep")},
		{name: "full-pipeline", owner: "M1", check: checkToolScenario("files.read", "git.read.diff", "github.read.pr", "web.fetch", "shell.run")},
		{name: "git-history", owner: "M1", check: checkToolScenario("git.read.log", "git.read.show", "git.read.blame")},
		{name: "iteration-limits", owner: "M1", check: checkIterationScenario},
		{name: "multi-model", owner: "M1", check: checkMultiModelScenario},
		{name: "no-tools", owner: "M1", check: checkNoToolsScenario},
		{name: "output-chain", owner: "M1", check: checkOutputChainScenario},
		{name: "parallel-fan-in", owner: "M1", check: checkParallelFanInScenario},
		{name: "prompt-from-file", owner: "M1", check: checkFileInstructionScenario},
		{name: "retry-transient", owner: "M1", check: checkRetryScenario},
		{name: "shell-exec", owner: "M1", check: checkToolScenario("shell.run")},
		{name: "skills-injection", owner: "M1", check: checkSkillScenario},
		{name: "system-prompt", owner: "M1", check: checkLiteralInstructionScenario},
		{name: "template-prompt-file", owner: "M2", check: checkTemplateFilePrimitive},
		{name: "template-variables", owner: "M2", check: checkTemplatePrimitive},
		{name: "web-fetch", owner: "M1", check: checkToolScenario("web.fetch")},
	}

	names := make([]string, len(cases))
	owners := map[string]int{}
	for i, scenario := range cases {
		names[i] = scenario.name
		owners[scenario.owner]++
		t.Run(scenario.name, scenario.check)
	}
	sort.Strings(names)
	digest := sha256.Sum256([]byte(strings.Join(names, "\n") + "\n"))
	if got := hex.EncodeToString(digest[:]); got != scenarioSetDigest {
		t.Fatalf("scenario digest = %s, want %s", got, scenarioSetDigest)
	}
	if len(cases) != 18 || owners["M1"] != 16 || owners["M2"] != 2 || len(owners) != 2 {
		t.Fatalf("scenario ownership = count %d, owners %#v", len(cases), owners)
	}
}

func checkNoToolsScenario(t *testing.T) {
	compiled := compilePlan(t, noToolsConfig, noToolsWorkflow)
	llm := mockllm.New(mockllm.Response{Text: `{"outcome":"completed","report":"ok"}`})
	result, err := runtime.Run(t.Context(), compiled, func(context.Context, string) (model.LLM, error) { return llm, nil })
	if err != nil || result.Status != runtime.StatusSucceeded || llm.CallCount() != 1 {
		t.Fatalf("no-tools result = %#v, calls = %d, error = %v", result, llm.CallCount(), err)
	}
}

func checkIterationScenario(t *testing.T) {
	source := strings.Replace(noToolsWorkflow, "max_iterations: 2\n  max_model_calls: 2", "max_iterations: 1\n  max_model_calls: 1", 1)
	compiled := compilePlan(t, noToolsConfig, source)
	step := compiled.Snapshot().Workflow.Steps[0]
	if step.Limits.MaxIterations != 1 || step.Limits.MaxModelCalls != 1 || len(step.Tools.Names) != 0 {
		t.Fatalf("iteration plan = %#v", step)
	}
}

func checkRetryScenario(t *testing.T) {
	source := strings.Replace(noToolsWorkflow, "max_iterations: 2\n  max_model_calls: 2", "max_iterations: 3\n  max_model_calls: 3", 1)
	source = strings.Replace(source, "    output:\n", "    retry: {max_attempts: 2, initial_delay: 1ms, max_delay: 2ms}\n    output:\n", 1)
	compiled := compilePlan(t, noToolsConfig, source)
	llm := mockllm.New(mockllm.Response{Error: context.DeadlineExceeded}, mockllm.Response{Text: `{"outcome":"completed","report":"retried"}`})
	result, err := runtime.Run(t.Context(), compiled, func(context.Context, string) (model.LLM, error) { return llm, nil })
	if err != nil || llm.CallCount() != 2 || result.Output["report"] != "retried" {
		t.Fatalf("retry result = %#v, calls = %d, error = %v", result, llm.CallCount(), err)
	}
}

func checkMultiModelScenario(t *testing.T) {
	source := twoStepWorkflow("multi-model", "light", "capable", false)
	compiled := compilePlan(t, twoModelConfig(), source)
	light := mockllm.New(mockllm.Response{Text: `{"outcome":"completed","value":"first"}`})
	capable := mockllm.New(mockllm.Response{Text: `{"outcome":"completed","report":"second"}`})
	resolver := func(_ context.Context, alias string) (model.LLM, error) {
		if alias == "light" {
			return light, nil
		}
		return capable, nil
	}
	result, err := runtime.Run(t.Context(), compiled, compiler.ModelResolver(resolver))
	if err != nil || result.Output["report"] != "second" || light.CallCount() != 1 || capable.CallCount() != 1 {
		t.Fatalf("multi-model result = %#v, calls = %d/%d, error = %v", result, light.CallCount(), capable.CallCount(), err)
	}
}

func checkOutputChainScenario(t *testing.T) {
	compiled := compilePlan(t, noToolsConfig, twoStepWorkflow("output-chain", "light", "light", false))
	llm := mockllm.New(
		mockllm.Response{Text: `{"outcome":"completed","value":"first"}`},
		mockllm.Response{Text: `{"outcome":"completed","report":"second"}`},
	)
	result, err := runtime.Run(t.Context(), compiled, func(context.Context, string) (model.LLM, error) { return llm, nil })
	if err != nil || result.Output["report"] != "second" || llm.CallCount() != 2 {
		t.Fatalf("output-chain result = %#v, calls = %d, error = %v", result, llm.CallCount(), err)
	}
}

func checkParallelFanInScenario(t *testing.T) {
	compiled := compilePlan(t, noToolsConfig, parallelWorkflow())
	llm := mockllm.New(
		mockllm.Response{Text: `{"outcome":"completed","value":"branch"}`},
		mockllm.Response{Text: `{"outcome":"completed","value":"branch"}`},
		mockllm.Response{Text: `{"outcome":"completed","report":"joined"}`},
	)
	result, err := runtime.Run(t.Context(), compiled, func(context.Context, string) (model.LLM, error) { return llm, nil })
	if err != nil || result.Output["report"] != "joined" || llm.CallCount() != 3 {
		t.Fatalf("parallel result = %#v, calls = %d, error = %v", result, llm.CallCount(), err)
	}
}

func checkToolScenario(names ...string) func(*testing.T) {
	return func(t *testing.T) {
		root := t.TempDir()
		configYAML := scenarioConfig(root, names)
		workflowYAML := scenarioToolWorkflow(names)
		compiled := compilePlan(t, configYAML, workflowYAML)
		got := compiled.Snapshot().Workflow.Steps[0].Tools.Names
		want := append([]string(nil), names...)
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("tools = %v, want %v", got, want)
		}

		registry := dtool.NewRegistry()
		calls := make([]*genai.Part, 0, len(names))
		for i, name := range names {
			current, err := functiontool.New[struct{}, map[string]any](
				functiontool.Config{Name: name, Description: "Return deterministic scenario evidence."},
				func(agent.Context, struct{}) (map[string]any, error) { return map[string]any{"ok": true}, nil },
			)
			if err != nil {
				t.Fatalf("functiontool.New(%s) error = %v", name, err)
			}
			registry.Register(name, current)
			calls = append(calls, &genai.Part{FunctionCall: &genai.FunctionCall{ID: fmt.Sprintf("call-%d", i), Name: name, Args: map[string]any{}}})
		}
		llm := mockllm.New(
			mockllm.Response{Content: &genai.Content{Role: genai.RoleModel, Parts: calls}},
			mockllm.Response{Text: `{"outcome":"completed"}`},
		)
		result, err := runtime.RunWithInputsAndToolsets(t.Context(), compiled, func(context.Context, string) (model.LLM, error) { return llm, nil }, registry.FilteredToolset, map[string]any{})
		if err != nil || result.Status != runtime.StatusSucceeded || llm.CallCount() != 2 {
			t.Fatalf("tool scenario result = %#v, calls = %d, error = %v", result, llm.CallCount(), err)
		}
	}
}

func checkLiteralInstructionScenario(t *testing.T) {
	compiled := compilePlan(t, noToolsConfig, strings.Replace(noToolsWorkflow, "Return the typed report.", "Act as a bounded reviewer.", 1))
	instruction := compiled.Snapshot().Workflow.Steps[0].Instruction
	if instruction.Source != "Act as a bounded reviewer." || instruction.Kind != prompt.KindText {
		t.Fatalf("literal instruction = %#v", instruction)
	}
}

func checkFileInstructionScenario(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "instruction.md"), []byte("Review the declared input."), 0o600); err != nil {
		t.Fatal(err)
	}
	compiled := compilePromptScenario(t, root, "instruction: {file: {workspace: source, path: instruction.md, max_bytes: 256}}", false)
	instruction := compiled.Snapshot().Workflow.Steps[0].Instruction
	if instruction.Kind != prompt.KindFile || instruction.Source != "Review the declared input." || instruction.Digest == "" {
		t.Fatalf("file instruction = %#v", instruction)
	}
}

func checkSkillScenario(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "skills", "review")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("---\nname: review\ndescription: Review source.\n---\nUse bounded evidence."), 0o600); err != nil {
		t.Fatal(err)
	}
	compiled := compilePromptScenario(t, root, "instruction: Review the declared input.", true)
	if skills := compiled.Snapshot().Workflow.Skills; len(skills) != 1 || skills[0].Name != "review" || len(skills[0].Files) != 1 {
		t.Fatalf("skills = %#v", skills)
	}
}

func checkTemplatePrimitive(t *testing.T) {
	compiled := compilePromptScenario(t, t.TempDir(), `instruction: {template: {text: 'Review {{ quote .Workflow.Inputs.objective }}', max_output_bytes: 256}}`, false)
	assertRenderedScenarioPrompt(t, compiled, `Review "declared"`)
}

func checkTemplateFilePrimitive(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "instruction.tmpl"), []byte(`Review {{ quote .Workflow.Inputs.objective }}`), 0o600); err != nil {
		t.Fatal(err)
	}
	compiled := compilePromptScenario(t, root, "instruction: {template: {file: {workspace: source, path: instruction.tmpl, max_bytes: 256}, max_output_bytes: 256}}", false)
	assertRenderedScenarioPrompt(t, compiled, `Review "declared"`)
}

func assertRenderedScenarioPrompt(t *testing.T, compiled *plan.Plan, want string) {
	t.Helper()
	step := compiled.Snapshot().Workflow.Steps[0]
	got, err := step.Instruction.Render(prompt.Data{Workflow: prompt.WorkflowData{Name: "prompt-scenario", Inputs: map[string]any{"objective": "declared"}}, Step: prompt.StepData{ID: "report", Inputs: map[string]any{"objective": "declared"}}, Predecessors: map[string]any{}})
	if err != nil || got != want {
		t.Fatalf("Render() = %q, %v; want %q", got, err, want)
	}
}

func compilePromptScenario(t *testing.T, root, instruction string, skill bool) *plan.Plan {
	t.Helper()
	configYAML := twoModelConfig() + fmt.Sprintf("workspaces:\n  source: {root: %q, access: read}\n", root)
	skillRoot, skillStep := "", ""
	if skill {
		skillRoot = "skills:\n  review: {workspace: source, path: skills/review}\n"
		skillStep = "    skills: [review]\n"
	}
	workflowYAML := fmt.Sprintf(`version: 1
name: prompt-scenario
model: light
tools: []
%sinputs:
  objective: {schema: {type: string, max_length: 64}}
limits: {timeout: 30s, max_iterations: 2, max_model_calls: 2, max_tool_calls: 0, max_concurrency: 1, max_parallel_calls: 1, max_artifact_bytes: 0}
steps:
  - id: report
    needs: []
    %s
    tools: []
%s    workspaces: [{name: source, access: read}]
    input:
      type: object
      properties: {objective: {type: string, max_length: 64}}
      required: [objective]
    with: {objective: {input: objective}}
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}, report: {type: string, max_length: 128}}
      required: [outcome, report]
result: {step: report}
`, skillRoot, instruction, skillStep)
	return compilePlan(t, configYAML, workflowYAML)
}

func scenarioConfig(root string, names []string) string {
	var limits strings.Builder
	for _, name := range names {
		fmt.Fprintf(&limits, "  %s: {max_calls: 2, timeout: 2s, max_request_bytes: 256, max_result_bytes: 2048}\n", name)
	}
	return fmt.Sprintf(`version: 1
providers:
  default: {type: custom-provider, config: {}}
models:
  light: {provider: default, target: example-model}
workspaces:
  source: {root: %q, access: read}
tool_config:
  files: {workspace: source}
  git: {workspace: source, refs: [HEAD], allow_working_tree: true, max_log_count: 20}
  github: {base_url: https://api.example.test, owner: example-owner, repository: example-repository, subject: 1, ref: example-ref, max_pages: 2, max_results: 20}
  web: {allowed_domains: [example.test], max_redirects: 0}
  shell: {executable: /bin/echo, args: [], workspace: source, environment: {}, max_stdout_bytes: 256, max_stderr_bytes: 256}
tools: [%s]
tool_limits:
%s`, root, strings.Join(names, ", "), limits.String())
}

func scenarioToolWorkflow(names []string) string {
	return fmt.Sprintf(`version: 1
name: tool-scenario
model: light
tools: [%s]
limits: {timeout: 30s, max_iterations: 2, max_model_calls: 2, max_tool_calls: 20, max_concurrency: 1, max_parallel_calls: 1, max_artifact_bytes: 0}
steps:
  - id: inspect
    needs: []
    instruction: Inspect with admitted tools.
    tools: {from: parent}
    workspaces: [{name: source, access: read}]
    input: {type: object, properties: {}, required: []}
    with: {}
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}}
      required: [outcome]
result: {step: inspect}
`, strings.Join(names, ", "))
}

func twoModelConfig() string {
	return `version: 1
providers:
  default: {type: custom-provider, config: {}}
models:
  light: {provider: default, target: example-small-model}
  capable: {provider: default, target: example-capable-model}
`
}

func twoStepWorkflow(name, firstModel, secondModel string, parallel bool) string {
	secondNeeds := "[first]"
	if parallel {
		secondNeeds = "[]"
	}
	return fmt.Sprintf(`version: 1
name: %s
model: light
tools: []
limits: {timeout: 30s, max_iterations: 4, max_model_calls: 4, max_tool_calls: 0, max_concurrency: 2, max_parallel_calls: 1, max_artifact_bytes: 0}
steps:
  - id: first
    needs: []
    instruction: Return the first value.
    model: %s
    tools: []
    workspaces: []
    input: {type: object, properties: {}, required: []}
    with: {}
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}, value: {type: string, max_length: 64}}
      required: [outcome, value]
  - id: second
    needs: %s
    instruction: Return the report.
    model: %s
    tools: []
    workspaces: []
    input:
      type: object
      properties: {value: {type: string, max_length: 64}}
      required: [value]
    with: {value: {output: {step: first, path: [value]}}}
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}, report: {type: string, max_length: 64}}
      required: [outcome, report]
result: {step: second}
`, name, firstModel, secondNeeds, secondModel)
}

func parallelWorkflow() string {
	return `version: 1
name: parallel-fan-in
model: light
tools: []
limits: {timeout: 30s, max_iterations: 4, max_model_calls: 4, max_tool_calls: 0, max_concurrency: 2, max_parallel_calls: 1, max_artifact_bytes: 0}
steps:
  - id: left
    needs: []
    instruction: Return left.
    tools: []
    workspaces: []
    input: {type: object, properties: {}, required: []}
    with: {}
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}, value: {type: string, max_length: 64}}
      required: [outcome, value]
  - id: right
    needs: []
    instruction: Return right.
    tools: []
    workspaces: []
    input: {type: object, properties: {}, required: []}
    with: {}
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}, value: {type: string, max_length: 64}}
      required: [outcome, value]
  - id: join
    needs: [left, right]
    wait: all_succeeded
    instruction: Join values.
    tools: []
    workspaces: []
    input:
      type: object
      properties: {left: {type: string, max_length: 64}, right: {type: string, max_length: 64}}
      required: [left, right]
    with:
      left: {output: {step: left, path: [value]}}
      right: {output: {step: right, path: [value]}}
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}, report: {type: string, max_length: 64}}
      required: [outcome, report]
result: {step: join}
`
}
