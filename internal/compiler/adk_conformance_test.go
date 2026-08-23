package compiler_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/testing/mockllm"
)

type echoArgs struct {
	Value string `json:"value"`
}

type echoResult struct {
	Value string `json:"value"`
}

func TestADKConformance_DirectSingleTurnRootRejected(t *testing.T) {
	llm := mockllm.New(mockllm.Response{Text: "unused"})

	a, err := llmagent.New(llmagent.Config{
		Name:  "single-turn",
		Model: llm,
		Mode:  llmagent.ModeSingleTurn,
	})
	if err != nil {
		t.Fatalf("llmagent.New: %v", err)
	}

	r, err := runner.New(runner.Config{
		AppName:           "conformance",
		Agent:             a,
		SessionService:    session.InMemoryService(),
		AutoCreateSession: true,
	})
	if err != nil {
		t.Fatalf("runner.New: %v", err)
	}

	var runErr error

	for _, err := range r.Run(t.Context(), "user", "session", genai.NewContentFromText("run", genai.RoleUser), agent.RunConfig{}) {
		if err != nil {
			runErr = err
		}
	}

	if runErr == nil || !strings.Contains(runErr.Error(), "must be a chat LlmAgent") {
		t.Fatalf("run error = %v, want direct single-turn root rejection", runErr)
	}

	if got := llm.CallCount(); got != 0 {
		t.Fatalf("model calls = %d, want 0", got)
	}
}

func TestADKConformance_StructuredOutputWithTypedTool(t *testing.T) {
	const toolName = "echo"

	echo, err := functiontool.New(functiontool.Config{
		Name:        toolName,
		Description: "Echo one bounded value.",
	}, func(_ agent.Context, args echoArgs) (echoResult, error) {
		return echoResult(args), nil
	})
	if err != nil {
		t.Fatalf("functiontool.New: %v", err)
	}

	llm := mockllm.New(
		mockllm.Response{Content: &genai.Content{
			Role: genai.RoleModel,
			Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{
				ID:   "call-1",
				Name: toolName,
				Args: map[string]any{"value": "ok"},
			}}},
		}},
		mockllm.Response{Text: `{"outcome":"completed","answer":"ok"}`},
	)

	outputSchema := &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"outcome": {Type: genai.TypeString},
			"answer":  {Type: genai.TypeString},
		},
		Required: []string{"outcome", "answer"},
	}

	a, err := llmagent.New(llmagent.Config{
		Name:         "structured-step",
		Model:        llm,
		Mode:         llmagent.ModeSingleTurn,
		Tools:        []tool.Tool{echo},
		OutputSchema: outputSchema,
	})
	if err != nil {
		t.Fatalf("llmagent.New: %v", err)
	}

	node, err := workflow.NewAgentNode(a, workflow.NodeConfig{})
	if err != nil {
		t.Fatalf("workflow.NewAgentNode: %v", err)
	}

	wf, err := workflow.New("structured-workflow", []workflow.Edge{{From: workflow.Start, To: node}})
	if err != nil {
		t.Fatalf("workflow.New: %v", err)
	}

	root, err := agent.New(agent.Config{
		Name:      "structured-workflow",
		SubAgents: []agent.Agent{a},
		Run:       wf.Run,
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	r, err := runner.New(runner.Config{
		AppName:           "conformance",
		Agent:             root,
		SessionService:    session.InMemoryService(),
		ArtifactService:   artifact.InMemoryService(),
		AutoCreateSession: true,
	})
	if err != nil {
		t.Fatalf("runner.New: %v", err)
	}

	var finalEvent *session.Event

	for event, err := range r.Run(t.Context(), "user", "session", genai.NewContentFromText("echo ok", genai.RoleUser), agent.RunConfig{}) {
		if err != nil {
			t.Fatalf("runner.Run: %v", err)
		}

		if event != nil && !event.Partial && event.Content != nil && event.Content.Role == genai.RoleModel {
			finalEvent = event
		}
	}

	if finalEvent == nil {
		t.Fatal("runner emitted no final non-partial model event")
	}

	if err := llmagent.ProcessLLMAgentOutput(a, finalEvent); err != nil {
		t.Fatalf("ProcessLLMAgentOutput: %v", err)
	}

	wantOutput := map[string]any{"outcome": "completed", "answer": "ok"}
	gotOutput, ok := finalEvent.Output.(map[string]any)

	if !ok {
		t.Fatalf("typed output = %T, want map[string]any", finalEvent.Output)
	}

	if diff := cmp.Diff(wantOutput, gotOutput); diff != "" {
		t.Fatalf("typed output mismatch (-want +got):\n%s", diff)
	}

	calls := llm.Calls()
	if len(calls) != 2 {
		t.Fatalf("model calls = %d, want 2", len(calls))
	}

	declaration := findDeclaration(calls[0].Config, toolName)

	if declaration == nil {
		t.Fatalf("first model request omitted %q declaration", toolName)
	}

	if declaration.ParametersJsonSchema == nil {
		t.Fatal("typed tool declaration omitted ParametersJsonSchema")
	}

	if declaration.Parameters != nil {
		t.Fatal("typed tool declaration unexpectedly populated legacy Parameters")
	}

	if calls[0].Config.ResponseSchema == nil {
		t.Fatal("structured output schema was not forwarded alongside tools")
	}
}

func findDeclaration(cfg *genai.GenerateContentConfig, name string) *genai.FunctionDeclaration {
	if cfg == nil {
		return nil
	}

	for _, packedTool := range cfg.Tools {
		if packedTool == nil {
			continue
		}

		for _, declaration := range packedTool.FunctionDeclarations {
			if declaration != nil && declaration.Name == name {
				return declaration
			}
		}
	}

	return nil
}
