package mockllm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/testing/mockllm"
)

func TestMockLLM_RecordsStableRequestAndResponseMetadata(t *testing.T) {
	usage := &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:     3,
		CandidatesTokenCount: 2,
		TotalTokenCount:      5,
	}
	llm := mockllm.New(mockllm.Response{
		Text:          "partial",
		Partial:       true,
		TurnComplete:  true,
		UsageMetadata: usage,
	})
	req := &model.LLMRequest{
		Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name:                 "inspect",
				ParametersJsonSchema: map[string]any{"type": "object"},
			}},
		}}},
	}

	var got *model.LLMResponse

	for response, err := range llm.GenerateContent(t.Context(), req, true) {
		if err != nil {
			t.Fatalf("GenerateContent: %v", err)
		}

		got = response
	}

	if got == nil {
		t.Fatal("GenerateContent returned no response")
	}

	if !got.Partial || !got.TurnComplete {
		t.Fatalf("stream flags = partial:%t complete:%t, want both true", got.Partial, got.TurnComplete)
	}

	if diff := cmp.Diff(usage, got.UsageMetadata); diff != "" {
		t.Fatalf("usage mismatch (-want +got):\n%s", diff)
	}

	req.Config.Tools[0].FunctionDeclarations[0].Name = "mutated"

	calls := llm.Calls()

	if got := calls[0].Config.Tools[0].FunctionDeclarations[0].Name; got != "inspect" {
		t.Fatalf("recorded declaration = %q, want immutable snapshot", got)
	}
}

func TestMockLLM_PropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	llm := mockllm.New(mockllm.Response{Text: "must not be emitted"})

	var gotErr error

	for _, err := range llm.GenerateContent(ctx, &model.LLMRequest{}, false) {
		gotErr = err
	}

	if !errors.Is(gotErr, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", gotErr)
	}
}
