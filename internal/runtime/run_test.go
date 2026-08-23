package runtime_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/compiler"
	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/plan"
	"github.com/PedroKlein/duto-ai/internal/runtime"
	"github.com/PedroKlein/duto-ai/internal/testing/mockllm"
)

const noToolsConfig = `version: 1
providers:
  default:
    type: custom-provider
    config:
      credential: canary-credential
models:
  light:
    provider: default
    target: private-model-target
`

const noToolsWorkflow = `version: 1
name: no-tools
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
    instruction: {text: Return the typed report.}
    tools: []
    workspaces: []
    input:
      type: object
      properties: {}
      required: []
    with: {}
    output:
      type: object
      properties:
        outcome: {type: string, enum: [completed]}
        report: {type: string, max_length: 256}
      required: [outcome, report]
result: {step: report}
`

func TestRun_NoToolsReturnsTypedTerminalObject(t *testing.T) {
	compiled := compilePlan(t, noToolsConfig, noToolsWorkflow)
	llm := mockllm.New(mockllm.Response{Emissions: []mockllm.Response{
		{Text: "partial", Partial: true},
		{
			Text: `{"outcome":"completed","report":"ok"}`,
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount:     2,
				CandidatesTokenCount: 1,
			},
		},
	}})
	resolver := func(context.Context, string) (model.LLM, error) { return llm, nil }

	result, err := runtime.Run(t.Context(), compiled, compiler.ModelResolver(resolver))
	if err != nil {
		t.Fatalf("Run() error = %v, result = %#v", err, result)
	}

	want := map[string]any{"outcome": "completed", "report": "ok"}
	if diff := cmp.Diff(want, result.Output); diff != "" {
		t.Fatalf("typed output mismatch (-want +got):\n%s", diff)
	}

	if result.Status != runtime.StatusSucceeded || result.Outcome != "completed" {
		t.Fatalf("status/outcome = %q/%q, want succeeded/completed", result.Status, result.Outcome)
	}

	if result.Usage == nil || result.Usage.InputTokens == nil || *result.Usage.InputTokens != 2 || result.Usage.OutputTokens == nil || *result.Usage.OutputTokens != 1 {
		t.Fatalf("usage = %#v, want reported input/output counts", result.Usage)
	}

	if got := llm.CallCount(); got != 1 {
		t.Fatalf("model calls = %d, want 1", got)
	}

	calls := llm.Calls()
	if calls[0].Config == nil || calls[0].Config.ResponseSchema == nil {
		t.Fatal("model request omitted native structured output schema")
	}

	if len(calls[0].Config.Tools) != 0 {
		t.Fatalf("model tools = %d, want none", len(calls[0].Config.Tools))
	}
}

func TestRun_RejectsInvalidTypedOutput(t *testing.T) {
	compiled := compilePlan(t, noToolsConfig, noToolsWorkflow)
	llm := mockllm.New(mockllm.Response{Text: `{"outcome":"completed"}`})
	resolver := func(context.Context, string) (model.LLM, error) { return llm, nil }

	result, err := runtime.Run(t.Context(), compiled, compiler.ModelResolver(resolver))
	if !errors.Is(err, runtime.ErrExecution) {
		t.Fatalf("Run() error = %v, want ErrExecution", err)
	}

	if result.Status != runtime.StatusFailed || result.Output != nil {
		t.Fatalf("invalid typed result = %#v", result)
	}
}

func TestRun_EvidenceIsAtomicRedactedAndComplete(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "evidence")
	configYAML := noToolsConfig + fmt.Sprintf("evidence:\n  directory: %q\n", directory)
	workflowYAML := strings.Replace(noToolsWorkflow, "Return the typed report.", "canary-prompt-secret", 1)
	compiled := compilePlan(t, configYAML, workflowYAML)
	llm := mockllm.New(mockllm.Response{Content: &genai.Content{
		Role: genai.RoleModel,
		Parts: []*genai.Part{
			{Text: "canary-private-thought", Thought: true},
			{Text: `{"outcome":"completed","report":"public"}`},
		},
	}})
	resolver := func(context.Context, string) (model.LLM, error) { return llm, nil }

	result, err := runtime.Run(t.Context(), compiled, compiler.ModelResolver(resolver))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var all strings.Builder

	for _, name := range []string{"events.jsonl", "result.json", "summary.md", "manifest.json"} {
		content, readErr := os.ReadFile(filepath.Join(directory, name))
		if readErr != nil {
			t.Fatalf("ReadFile(%s) error = %v", name, readErr)
		}

		all.Write(content)
	}

	for _, forbidden := range []string{"canary-prompt-secret", "canary-private-thought", "canary-credential", "private-model-target", "mock-llm"} {
		if strings.Contains(all.String(), forbidden) {
			t.Fatalf("evidence leaked %q", forbidden)
		}
	}

	if strings.Contains(all.String(), `"usage"`) {
		t.Fatal("evidence fabricated unavailable usage")
	}

	verifyManifest(t, directory, result.RunID, compiled.Digest())

	before, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		t.Fatalf("ReadFile(manifest) error = %v", err)
	}

	second := mockllm.New(mockllm.Response{Text: `{"outcome":"completed","report":"second"}`})

	_, err = runtime.Run(t.Context(), compiled, func(context.Context, string) (model.LLM, error) { return second, nil })
	if !errors.Is(err, runtime.ErrEvidence) {
		t.Fatalf("second Run() error = %v, want ErrEvidence", err)
	}

	after, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		t.Fatalf("ReadFile(manifest after rejected publish) error = %v", err)
	}

	if diff := cmp.Diff(before, after); diff != "" {
		t.Fatalf("existing atomic bundle changed (-before +after):\n%s", diff)
	}
}

func TestRun_UsesFreshRunScopedServices(t *testing.T) {
	firstDirectory := filepath.Join(t.TempDir(), "first-evidence")
	secondDirectory := filepath.Join(t.TempDir(), "second-evidence")
	firstPlan := compilePlan(t, noToolsConfig+fmt.Sprintf("evidence:\n  directory: %q\n", firstDirectory), noToolsWorkflow)
	secondPlan := compilePlan(t, noToolsConfig+fmt.Sprintf("evidence:\n  directory: %q\n", secondDirectory), noToolsWorkflow)
	llm := mockllm.New(
		mockllm.Response{Text: `{"outcome":"completed","report":"first"}`},
		mockllm.Response{Text: `{"outcome":"completed","report":"second"}`},
	)
	resolver := func(context.Context, string) (model.LLM, error) { return llm, nil }

	first, err := runtime.Run(t.Context(), firstPlan, compiler.ModelResolver(resolver))
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	second, err := runtime.Run(t.Context(), secondPlan, compiler.ModelResolver(resolver))
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	if first.RunID == second.RunID {
		t.Fatalf("run IDs are equal: %q", first.RunID)
	}

	calls := llm.Calls()
	if len(calls) != 2 {
		t.Fatalf("model calls = %d, want 2", len(calls))
	}

	for i, call := range calls {
		if len(call.Contents) != 1 || len(call.Contents[0].Parts) != 1 || call.Contents[0].Parts[0].Text != "{}" {
			t.Fatalf("call %d contents leaked prior state: %#v", i, call.Contents)
		}
	}

	firstEvents, err := os.ReadFile(filepath.Join(firstDirectory, "events.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile(first events) error = %v", err)
	}

	secondEvents, err := os.ReadFile(filepath.Join(secondDirectory, "events.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile(second events) error = %v", err)
	}

	if strings.Contains(string(firstEvents), second.RunID) || strings.Contains(string(secondEvents), first.RunID) {
		t.Fatal("run-scoped plugin buffers leaked IDs across evidence bundles")
	}
}

func verifyManifest(t *testing.T, directory, runID, planDigest string) {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		t.Fatalf("ReadFile(manifest) error = %v", err)
	}

	var document struct {
		Version    int    `json:"version"`
		RunID      string `json:"run_id"`
		PlanDigest string `json:"plan_digest"`
		Completion string `json:"completion"`
		Files      []struct {
			Name   string `json:"name"`
			Size   int    `json:"size"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}

	unmarshalErr := json.Unmarshal(content, &document)
	if unmarshalErr != nil {
		t.Fatalf("Unmarshal(manifest) error = %v", unmarshalErr)
	}

	if document.Version != 1 || document.RunID != runID || document.PlanDigest != planDigest || document.Completion != "succeeded" {
		t.Fatalf("manifest identity = %#v", document)
	}

	if len(document.Files) != 3 {
		t.Fatalf("manifest files = %d, want 3", len(document.Files))
	}

	for _, file := range document.Files {
		body, readErr := os.ReadFile(filepath.Join(directory, file.Name))
		if readErr != nil {
			t.Fatalf("ReadFile(%s) error = %v", file.Name, readErr)
		}

		sum := sha256.Sum256(body)
		if len(body) != file.Size || hex.EncodeToString(sum[:]) != file.SHA256 {
			t.Fatalf("manifest entry %q does not match bytes", file.Name)
		}
	}

	events, err := os.ReadFile(filepath.Join(directory, "events.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile(events) error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(events)), "\n")

	var last map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatalf("Unmarshal(last event) error = %v", err)
	}

	if last["kind"] != "run.finish" || last["status"] != "succeeded" {
		t.Fatalf("last evidence record = %#v", last)
	}
}

func compilePlan(t *testing.T, configYAML, workflowYAML string) *plan.Plan {
	t.Helper()

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

	return compiled
}
