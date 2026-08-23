package runtime_test

import (
	"context"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/plan"
	"github.com/PedroKlein/duto-ai/internal/runtime"
	"github.com/PedroKlein/duto-ai/internal/testing/mockllm"
)

func TestRunWithInputs_NativeSubagentSnapshotAndLineage(t *testing.T) {
	evidenceDir := filepath.Join(t.TempDir(), "evidence")

	cfg, err := config.DecodeConfig("duto.yaml", fmt.Appendf(nil, nativeRuntimeConfig, evidenceDir))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}

	workflow, err := config.DecodeWorkflow("workflow.yaml", []byte(nativeRuntimeWorkflow))
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("plan.Compile() error = %v", err)
	}

	coordinator := mockllm.New(
		mockllm.Response{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "delegation-1", Name: "researcher", Args: map[string]any{"question": "bounded question"}}}}}},
		mockllm.Response{Text: `{"outcome":"completed","decision":"accepted"}`},
	)
	researcher := mockllm.New(mockllm.Response{Text: `{"outcome":"completed","evidence":"bounded"}`})
	models := map[string]model.LLM{"capable": coordinator, "light": researcher}

	result, err := runtime.RunWithInputs(t.Context(), compiled, func(_ context.Context, alias string) (model.LLM, error) { return models[alias], nil }, map[string]any{"objective": "snapshot-value"})
	if err != nil {
		events, _ := os.ReadFile(filepath.Join(evidenceDir, "events.jsonl"))
		t.Fatalf("RunWithInputs() error = %v, result = %#v, coordinator calls = %#v, researcher calls = %#v, events = %s", err, result, coordinator.Calls(), researcher.Calls(), events)
	}

	if result.Status != runtime.StatusSucceeded || result.Outcome != "completed" || result.Output["decision"] != "accepted" || result.Steps[0].Status != runtime.StatusSucceeded {
		t.Fatalf("result = %#v", result)
	}

	rootCalls := coordinator.Calls()

	legacyDelegate := "agent" + ".delegate"
	if len(rootCalls) != 2 || findToolDeclaration(rootCalls[0].Config, "researcher") == nil || findToolDeclaration(rootCalls[0].Config, legacyDelegate) != nil {
		t.Fatalf("coordinator declarations/calls = %#v", rootCalls)
	}

	childCalls := researcher.Calls()
	if len(childCalls) != 1 {
		t.Fatalf("researcher calls = %d", len(childCalls))
	}

	if got := allContentText(childCalls[0].Contents); strings.Contains(got, "Delegate once") || strings.Contains(got, "accepted") || !strings.Contains(got, "bounded question") {
		t.Fatalf("fresh child contents = %q", got)
	}

	instruction := allContentText([]*genai.Content{childCalls[0].Config.SystemInstruction})
	if !strings.Contains(instruction, "Snapshot context") || !strings.Contains(instruction, "snapshot-value") || !strings.Contains(instruction, `"digest"`) {
		t.Fatalf("snapshot instruction = %q", instruction)
	}

	events, err := os.ReadFile(filepath.Join(evidenceDir, "events.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile(events.jsonl) error = %v", err)
	}

	if strings.Count(string(events), "delegation-1") != 2 || !strings.Contains(string(events), `"tool":"researcher"`) || !strings.Contains(string(events), `"output_digest"`) || strings.Contains(string(events), "snapshot-value") {
		t.Fatalf("delegation lineage/redaction = %s", events)
	}
}

func TestRunWithInputs_SubagentSnapshotUsesFrozenFile(t *testing.T) {
	root := t.TempDir()

	path := filepath.Join(root, "context.txt")
	if err := os.WriteFile(path, []byte("frozen-file"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfgYAML := fmt.Sprintf(`version: 1
providers:
  default: {type: custom-provider, config: {credential: canary}}
models:
  light: {provider: default, target: example-light}
  capable: {provider: default, target: example-capable}
workspaces:
  source: {root: %q, access: read}
`, root)
	workflowYAML := strings.Replace(nativeRuntimeWorkflow, "    workspaces: []\n    skills: []\n    context: {mode: snapshot, include: [{input: objective}]}", "    workspaces: [{name: source, access: read}]\n    skills: []\n    context: {mode: snapshot, include: [{input: objective}, {file: {workspace: source, path: context.txt, max_bytes: 64}}]}", 1)
	workflowYAML = strings.Replace(workflowYAML, "    workspaces: []\n    skills: []\n    context: {mode: fresh}", "    workspaces: [{name: source, access: read}]\n    skills: []\n    context: {mode: fresh}", 1)

	cfg, err := config.DecodeConfig("duto.yaml", []byte(cfgYAML))
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

	if writeErr := os.WriteFile(path, []byte("changed-file"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	coordinator := mockllm.New(
		mockllm.Response{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "delegation-1", Name: "researcher", Args: map[string]any{"question": "bounded"}}}}}},
		mockllm.Response{Text: `{"outcome":"completed","decision":"accepted"}`},
	)
	researcher := mockllm.New(mockllm.Response{Text: `{"outcome":"completed","evidence":"bounded"}`})
	models := map[string]model.LLM{"capable": coordinator, "light": researcher}

	_, err = runtime.RunWithInputs(t.Context(), compiled, func(_ context.Context, alias string) (model.LLM, error) { return models[alias], nil }, map[string]any{"objective": "snapshot"})
	if err != nil {
		t.Fatalf("RunWithInputs() error = %v", err)
	}

	calls := researcher.Calls()
	if len(calls) != 1 {
		t.Fatalf("researcher calls = %d", len(calls))
	}

	instruction := allContentText([]*genai.Content{calls[0].Config.SystemInstruction})
	if !strings.Contains(instruction, "frozen-file") || strings.Contains(instruction, "changed-file") {
		t.Fatalf("snapshot instruction = %q", instruction)
	}
}

func TestRunWithInputs_NativeTaskSubagentReturnsFinishTaskOutput(t *testing.T) {
	workflowYAML := strings.Replace(nativeRuntimeWorkflow, "mode: single_turn", "mode: task", 1)
	workflowYAML = strings.Replace(workflowYAML, "max_parallel_calls: 2", "max_parallel_calls: 1", 1)

	cfg, err := config.DecodeConfig("duto.yaml", fmt.Appendf(nil, nativeRuntimeConfig, ""))
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

	coordinator := mockllm.New(
		mockllm.Response{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "task-1", Name: "researcher", Args: map[string]any{"question": "bounded question"}}}}}},
		mockllm.Response{Text: `{"outcome":"completed","decision":"accepted"}`},
	)
	researcher := mockllm.New(mockllm.Response{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "finish-1", Name: "finish_task", Args: map[string]any{"outcome": "completed", "evidence": "bounded"}}}}}})
	models := map[string]model.LLM{"capable": coordinator, "light": researcher}

	result, err := runtime.RunWithInputs(t.Context(), compiled, func(_ context.Context, alias string) (model.LLM, error) { return models[alias], nil }, map[string]any{"objective": "snapshot-value"})
	if err != nil {
		t.Fatalf("RunWithInputs() error = %v, result = %#v", err, result)
	}

	if result.Outcome != "completed" || result.Output["decision"] != "accepted" {
		t.Fatalf("result = %#v", result)
	}

	calls := researcher.Calls()
	if len(calls) != 1 || findToolDeclaration(calls[0].Config, "finish_task") == nil {
		t.Fatalf("task child declarations = %#v", calls)
	}
}

func TestRunWithInputs_InvalidSubagentOutputFailsWorkflow(t *testing.T) {
	cfg, err := config.DecodeConfig("duto.yaml", fmt.Appendf(nil, nativeRuntimeConfig, ""))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}

	workflow, err := config.DecodeWorkflow("workflow.yaml", []byte(nativeRuntimeWorkflow))
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("plan.Compile() error = %v", err)
	}

	coordinator := mockllm.New(mockllm.Response{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "delegation-1", Name: "researcher", Args: map[string]any{"question": "bounded"}}}}}})
	researcher := mockllm.New(mockllm.Response{Text: `{"outcome":"completed"}`})
	models := map[string]model.LLM{"capable": coordinator, "light": researcher}

	result, err := runtime.RunWithInputs(t.Context(), compiled, func(_ context.Context, alias string) (model.LLM, error) { return models[alias], nil }, map[string]any{"objective": "review"})
	if err == nil || result.Status != runtime.StatusFailed {
		t.Fatalf("result/error = %#v/%v", result, err)
	}
}

func TestRunWithInputs_NativeSubagentFanoutUsesTaskRunnerCap(t *testing.T) {
	cfg, err := config.DecodeConfig("duto.yaml", []byte(twoChildConfig))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}

	workflow, err := config.DecodeWorkflow("workflow.yaml", []byte(twoChildWorkflow))
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("plan.Compile() error = %v", err)
	}

	coordinator := mockllm.New(
		mockllm.Response{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "child-a", Name: "researcher-a", Args: map[string]any{"question": "a"}}},
			{FunctionCall: &genai.FunctionCall{ID: "child-b", Name: "researcher-b", Args: map[string]any{"question": "b"}}},
		}}},
		mockllm.Response{Text: `{"outcome":"completed","decision":"both"}`},
	)
	children := &boundedModel{}
	models := map[string]model.LLM{"capable": coordinator, "light": children}

	result, err := runtime.RunWithInputs(t.Context(), compiled, func(_ context.Context, alias string) (model.LLM, error) { return models[alias], nil }, map[string]any{"objective": "review"})
	if err != nil {
		t.Fatalf("RunWithInputs() error = %v, result = %#v", err, result)
	}

	if got := children.maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent child model calls = %d, want 1", got)
	}

	if got := children.calls.Load(); got != 2 {
		t.Fatalf("child model calls = %d, want 2", got)
	}

	children.mu.Lock()
	contents := append([]string(nil), children.contents...)
	children.mu.Unlock()

	if len(contents) != 2 || strings.Contains(contents[0], `"question":"b"`) || strings.Contains(contents[1], `"question":"a"`) {
		t.Fatalf("sibling child contents leaked = %#v", contents)
	}
}

func TestRunWithInputs_SubagentCancellationPropagates(t *testing.T) {
	cfg, err := config.DecodeConfig("duto.yaml", fmt.Appendf(nil, nativeRuntimeConfig, ""))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}

	workflow, err := config.DecodeWorkflow("workflow.yaml", []byte(nativeRuntimeWorkflow))
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("plan.Compile() error = %v", err)
	}

	coordinator := mockllm.New(mockllm.Response{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "delegation-1", Name: "researcher", Args: map[string]any{"question": "bounded"}}}}}})
	child := &cancelModel{}
	models := map[string]model.LLM{"capable": coordinator, "light": child}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	result, err := runtime.RunWithInputs(ctx, compiled, func(_ context.Context, alias string) (model.LLM, error) { return models[alias], nil }, map[string]any{"objective": "review"})
	if err == nil || result.Status != runtime.StatusCancelled || child.calls.Load() != 1 {
		t.Fatalf("result/error/calls = %#v/%v/%d", result, err, child.calls.Load())
	}
}

type cancelModel struct{ calls atomic.Int64 }

func (m *cancelModel) Name() string { return "cancel-model" }

func (m *cancelModel) GenerateContent(ctx context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		m.calls.Add(1)
		<-ctx.Done()
		yield(nil, ctx.Err())
	}
}

func TestRunWithInputs_SubagentCallLimitDebitsBeforeChildModel(t *testing.T) {
	cfg, err := config.DecodeConfig("duto.yaml", []byte(twoChildConfig))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}

	workflowYAML := strings.Replace(twoChildWorkflow, "max_tool_calls: 4", "max_tool_calls: 1", 1)

	workflow, err := config.DecodeWorkflow("workflow.yaml", []byte(workflowYAML))
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		t.Fatalf("plan.Compile() error = %v", err)
	}

	coordinator := mockllm.New(
		mockllm.Response{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "child-a", Name: "researcher-a", Args: map[string]any{"question": "a"}}},
			{FunctionCall: &genai.FunctionCall{ID: "child-b", Name: "researcher-b", Args: map[string]any{"question": "b"}}},
		}}},
		mockllm.Response{Text: `{"outcome":"completed","decision":"bounded"}`},
	)
	children := &boundedModel{}
	models := map[string]model.LLM{"capable": coordinator, "light": children}

	result, err := runtime.RunWithInputs(t.Context(), compiled, func(_ context.Context, alias string) (model.LLM, error) { return models[alias], nil }, map[string]any{"objective": "review"})
	if err != nil {
		t.Fatalf("RunWithInputs() error = %v, result = %#v", err, result)
	}

	if got := children.calls.Load(); got != 1 {
		t.Fatalf("child model calls = %d, want 1", got)
	}
}

type boundedModel struct {
	active   atomic.Int64
	maximum  atomic.Int64
	calls    atomic.Int64
	mu       sync.Mutex
	contents []string
}

func (m *boundedModel) Name() string { return "bounded-model" }

func (m *boundedModel) GenerateContent(ctx context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		m.calls.Add(1)
		m.mu.Lock()
		m.contents = append(m.contents, allContentText(req.Contents))
		m.mu.Unlock()

		current := m.active.Add(1)
		defer m.active.Add(-1)

		for {
			prior := m.maximum.Load()
			if current <= prior || m.maximum.CompareAndSwap(prior, current) {
				break
			}
		}

		timer := time.NewTimer(20 * time.Millisecond)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			yield(nil, ctx.Err())
		case <-timer.C:
			yield(&model.LLMResponse{Content: genai.NewContentFromText(`{"outcome":"completed","evidence":"bounded"}`, genai.RoleModel)}, nil)
		}
	}
}

func allContentText(contents []*genai.Content) string {
	var result strings.Builder

	for _, content := range contents {
		if content == nil {
			continue
		}

		for _, part := range content.Parts {
			if part != nil && !part.Thought {
				result.WriteString(part.Text)
			}
		}
	}

	return result.String()
}

const twoChildConfig = `version: 1
providers:
  default: {type: custom-provider, config: {credential: canary}}
models:
  light: {provider: default, target: example-light}
  capable: {provider: default, target: example-capable}
`

const twoChildWorkflow = `version: 1
name: delegated-pair
inputs: {objective: {schema: {type: string, max_length: 64}}}
model: capable
tools: []
limits: {timeout: 1m, max_iterations: 8, max_model_calls: 8, max_tool_calls: 4, max_concurrency: 1, max_parallel_calls: 1, max_artifact_bytes: 0}
agents:
  researcher-a:
    description: Research A.
    mode: single_turn
    model: light
    instruction: Research A.
    tools: []
    workspaces: []
    skills: []
    context: {mode: fresh}
    input: {type: object, properties: {question: {type: string, max_length: 64}}, required: [question]}
    output: {type: object, properties: {outcome: {type: string, enum: [completed]}, evidence: {type: string, max_length: 128}}, required: [outcome, evidence]}
  researcher-b:
    description: Research B.
    mode: single_turn
    model: light
    instruction: Research B.
    tools: []
    workspaces: []
    skills: []
    context: {mode: fresh}
    input: {type: object, properties: {question: {type: string, max_length: 64}}, required: [question]}
    output: {type: object, properties: {outcome: {type: string, enum: [completed]}, evidence: {type: string, max_length: 128}}, required: [outcome, evidence]}
  coordinator:
    description: Coordinate both.
    mode: chat
    model: capable
    instruction: Delegate both.
    tools: []
    workspaces: []
    skills: []
    context: {mode: fresh}
    subagents: [researcher-a, researcher-b]
    input: {type: object, properties: {objective: {type: string, max_length: 64}}, required: [objective]}
    output: {type: object, properties: {outcome: {type: string, enum: [completed]}, decision: {type: string, max_length: 128}}, required: [outcome, decision]}
steps:
  - id: coordinate
    needs: []
    agent: coordinator
    with: {objective: {input: objective}}
result: {step: coordinate}
`

const nativeRuntimeConfig = `version: 1
providers:
  default: {type: custom-provider, config: {credential: canary}}
models:
  light: {provider: default, target: example-light}
  capable: {provider: default, target: example-capable}
evidence: {directory: %q}
`

const nativeRuntimeWorkflow = `version: 1
name: delegated
inputs: {objective: {schema: {type: string, max_length: 64}}}
model: capable
tools: []
limits: {timeout: 1m, max_iterations: 8, max_model_calls: 8, max_tool_calls: 4, max_concurrency: 1, max_parallel_calls: 2, max_artifact_bytes: 1024}
agents:
  researcher:
    description: Gather evidence.
    mode: single_turn
    model: light
    instruction: Return evidence.
    tools: []
    workspaces: []
    skills: []
    context: {mode: snapshot, include: [{input: objective}]}
    input: {type: object, properties: {question: {type: string, max_length: 64}}, required: [question]}
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}, evidence: {type: string, max_length: 128}}
      required: [outcome, evidence]
  coordinator:
    description: Coordinate research.
    mode: chat
    model: capable
    instruction: Delegate once.
    tools: []
    workspaces: []
    skills: []
    context: {mode: fresh}
    subagents: [researcher]
    input: {type: object, properties: {objective: {type: string, max_length: 64}}, required: [objective]}
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
