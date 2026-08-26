package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/plan"
	"github.com/PedroKlein/duto-ai/internal/runtime"
	"github.com/PedroKlein/duto-ai/internal/safeoutput"
	"github.com/PedroKlein/duto-ai/internal/testing/mockllm"
	"github.com/PedroKlein/duto-ai/internal/tool"
	"github.com/PedroKlein/duto-ai/internal/trust"
)

func TestM3Staging_RuntimeWritesAtomicVersionTwoBundle(t *testing.T) {
	t.Parallel()

	compiled, collector, registry, evidenceDir := stagingRuntimeFixture(t, 1<<20)
	llm := mockllm.New(
		mockllm.Response{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "reply-1", Name: "safe-output.conversation-reply", Args: map[string]any{"body": "Please provide the package path."}}},
		}}},
		mockllm.Response{Text: `{"outcome":"completed"}`},
	)

	result, err := runtime.RunWithInputsAndToolsetsAndStaging(t.Context(), compiled, func(context.Context, string) (model.LLM, error) {
		return llm, nil
	}, registry.FilteredToolset, map[string]any{}, collector)
	if err != nil {
		t.Fatalf("RunWithInputsAndToolsetsAndStaging() error = %v, result = %#v", err, result)
	}

	manifestBytes, err := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if err != nil {
		t.Fatalf("ReadFile(manifest) error = %v", err)
	}

	var manifest struct {
		Version      int    `json:"version"`
		BundleKind   string `json:"bundle_kind"`
		OperationSet string `json:"operation_set"`
		Files        []struct {
			Name   string `json:"name"`
			Size   int    `json:"size"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if decodeErr := json.Unmarshal(manifestBytes, &manifest); decodeErr != nil {
		t.Fatalf("Unmarshal(manifest) error = %v", decodeErr)
	}

	if manifest.Version != 2 || manifest.BundleKind != "m3-authoring" || manifest.OperationSet != safeoutput.ConversationReply {
		t.Fatalf("manifest = %#v", manifest)
	}

	names := make([]string, 0, len(manifest.Files))
	for _, entry := range manifest.Files {
		names = append(names, entry.Name)
		if entry.Size <= 0 || len(entry.SHA256) != 64 {
			t.Fatalf("invalid manifest entry = %#v", entry)
		}
	}

	if !slices.IsSorted(names) || slices.Contains(names, "manifest.json") || !slices.Contains(names, "operations/0001-conversation-reply.json") {
		t.Fatalf("manifest files = %v", names)
	}

	operationBytes, err := os.ReadFile(filepath.Join(evidenceDir, "operations/0001-conversation-reply.json"))
	if err != nil {
		t.Fatalf("ReadFile(operation) error = %v", err)
	}

	var operation map[string]json.RawMessage
	if err := json.Unmarshal(operationBytes, &operation); err != nil {
		t.Fatalf("Unmarshal(operation) error = %v", err)
	}

	if len(operation) != 16 {
		t.Fatalf("operation fields = %v", operation)
	}

	for _, name := range []string{"version", "request_id", "correlation_key", "kind", "mode", "run_id", "plan_sha256", "policy_sha256", "control_sha256", "repository", "origin", "base", "source_commit", "depends_on", "preconditions", "payload"} {
		if _, exists := operation[name]; !exists {
			t.Fatalf("operation missing %q", name)
		}
	}
}

func TestM3Staging_FailedRunBundlesRecoveryWithoutOperations(t *testing.T) {
	t.Parallel()

	compiled, collector, registry, evidenceDir := stagingRuntimeFixture(t, 1<<20)
	llm := mockllm.New(mockllm.Response{Text: `{"unexpected":true}`})
	finalizer := func(_ context.Context, succeeded bool) error {
		if succeeded {
			t.Fatal("failed run was finalized as succeeded")
		}

		return collector.SetRecovery([]byte(`{"version":1}`+"\n"), []byte("patch"))
	}

	_, err := runtime.RunWithInputsAndToolsetsAndStaging(t.Context(), compiled, func(context.Context, string) (model.LLM, error) {
		return llm, nil
	}, registry.FilteredToolset, map[string]any{}, collector, finalizer)
	if !errors.Is(err, runtime.ErrExecution) {
		t.Fatalf("error = %v, want ErrExecution", err)
	}

	for _, name := range []string{"recovery/metadata.json", "recovery/changes.patch", "manifest.json"} {
		if _, statErr := os.Stat(filepath.Join(evidenceDir, name)); statErr != nil {
			t.Fatalf("missing %s: %v", name, statErr)
		}
	}

	if matches, globErr := filepath.Glob(filepath.Join(evidenceDir, "operations", "*.json")); globErr != nil || len(matches) != 0 {
		t.Fatalf("failed run operation files = %v, error = %v", matches, globErr)
	}

	manifestBytes, readErr := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}

	if !strings.Contains(string(manifestBytes), `"completion":"failed"`) || !strings.Contains(string(manifestBytes), `"operation_set":"none"`) {
		t.Fatalf("failed manifest = %s", manifestBytes)
	}
}

func TestM3Staging_BundleFailureLeavesNoPartialDirectory(t *testing.T) {
	t.Parallel()

	compiled, collector, registry, evidenceDir := stagingRuntimeFixture(t, 1)
	llm := mockllm.New(
		mockllm.Response{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "reply-1", Name: "safe-output.conversation-reply", Args: map[string]any{"body": "bounded"}}},
		}}},
		mockllm.Response{Text: `{"outcome":"completed"}`},
	)

	_, err := runtime.RunWithInputsAndToolsetsAndStaging(t.Context(), compiled, func(context.Context, string) (model.LLM, error) {
		return llm, nil
	}, registry.FilteredToolset, map[string]any{}, collector)
	if !errors.Is(err, runtime.ErrEvidence) {
		t.Fatalf("error = %v, want ErrEvidence", err)
	}

	if _, statErr := os.Stat(evidenceDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("partial evidence directory exists: %v", statErr)
	}

	matches, globErr := filepath.Glob(filepath.Join(filepath.Dir(evidenceDir), ".duto-evidence-*"))
	if globErr != nil || len(matches) != 0 {
		t.Fatalf("temporary evidence directories = %v, error = %v", matches, globErr)
	}
}

func stagingRuntimeFixture(t *testing.T, maxBundleBytes int) (*plan.Plan, *safeoutput.Collector, *tool.Registry, string) {
	t.Helper()

	root := t.TempDir()
	evidenceDir := filepath.Join(t.TempDir(), "evidence")
	configYAML := strings.ReplaceAll(`version: 1
providers:
  default: {type: custom-provider, config: {}}
models:
  capable: {provider: default, target: example-model}
workspaces:
  source: {root: ROOT, access: write}
tools: [safe-output.conversation-reply]
tool_limits:
  safe-output.conversation-reply: {max_calls: 1, timeout: 5s, max_request_bytes: 33792, max_result_bytes: 4096}
m3:
  admission:
    id: focused-m3
    contexts: [same_repository]
    capabilities: [github.mutate]
  authoring:
    workspace: source
    allowed_paths: [docs/]
    max_changed_files: 2
    max_file_bytes: 1024
    max_total_write_bytes: 2048
    max_commit_message_bytes: 256
    commit_author_name: Example Automation
    commit_author_email: automation@example.invalid
  publication:
    mode: staged
    operation_sets: [conversation-reply]
    branch_prefix: duto/m3/
    max_reply_bytes: 128
    max_pr_title_bytes: 64
    max_pr_body_bytes: 256
    max_bundle_bytes: MAX_BUNDLE
evidence: {directory: EVIDENCE}
`, "ROOT", root)
	configYAML = strings.Replace(configYAML, "EVIDENCE", evidenceDir, 1)
	configYAML = strings.Replace(configYAML, "MAX_BUNDLE", strconv.Itoa(maxBundleBytes), 1)

	cfg, err := config.DecodeConfig("duto.yaml", []byte(configYAML))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}

	workflow, err := config.DecodeWorkflow("workflow.yaml", []byte(`version: 1
name: stage-reply
model: capable
tools: [safe-output.conversation-reply]
limits: {timeout: 1m, max_iterations: 2, max_model_calls: 2, max_tool_calls: 2, max_concurrency: 1, max_parallel_calls: 1, max_artifact_bytes: 16777216}
steps:
  - id: reply
    needs: []
    instruction: {text: Reply.}
    tools: [safe-output.conversation-reply]
    workspaces: []
    input: {type: object, properties: {}, required: []}
    with: {}
    output:
      type: object
      properties: {outcome: {type: string, enum: [completed]}}
      required: [outcome]
result: {step: reply}
`))
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	control := []byte(`{"version":1}`)
	decision := trust.Decision{
		Context: trust.ContextSameRepository, AdmissionID: "focused-m3", CorrelationKey: "issue-42",
		ControlSHA256: strings.Repeat("c", 64), ControlJSON: control, Transport: "staged",
		Repository: trust.Repository{ID: "1001", Owner: "example-owner", Name: "example-repository"},
		Origin:     trust.Origin{Kind: "issue", Number: 42}, CheckoutRef: "refs/heads/main", CheckoutSHA: strings.Repeat("1", 40), Present: true,
	}

	compiled, err := plan.CompileWithTrust(cfg, workflow, decision)
	if err != nil {
		t.Fatalf("CompileWithTrust() error = %v", err)
	}

	spec := compiled.Staging()

	collector, err := safeoutput.New(safeoutput.Policy{
		OperationSet: spec.OperationSet, PlanSHA256: compiled.Digest(), PolicySHA256: spec.PolicySHA256,
		ControlSHA256: spec.ControlSHA256, ControlJSON: spec.ControlJSON, CorrelationKey: spec.CorrelationKey,
		Repository: safeoutput.Repository{ID: spec.Repository.ID, Owner: spec.Repository.Owner, Name: spec.Repository.Name},
		Origin:     safeoutput.Origin{Kind: spec.Origin.Kind, Number: spec.Origin.Number}, Base: safeoutput.Base{Ref: spec.BaseRef, SHA: spec.BaseSHA},
		BranchPrefix: spec.BranchPrefix, MaxReplyBytes: spec.MaxReplyBytes, MaxPRTitleBytes: spec.MaxPRTitleBytes,
		MaxPRBodyBytes: spec.MaxPRBodyBytes, MaxBundleBytes: spec.MaxBundleBytes,
		Limits: map[string]tool.ToolLimit{"safe-output.conversation-reply": {MaxCalls: 1, Timeout: time.Second, MaxRequestBytes: 33792, MaxResultBytes: 4096}},
	})
	if err != nil {
		t.Fatalf("safeoutput.New() error = %v", err)
	}

	registry := tool.NewRegistry()
	if err := safeoutput.RegisterAll(registry, collector); err != nil {
		t.Fatalf("safeoutput.RegisterAll() error = %v", err)
	}

	return compiled, collector, registry, evidenceDir
}
