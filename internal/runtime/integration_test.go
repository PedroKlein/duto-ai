//go:build integration

package runtime_test

import (
	"context"
	"testing"

	"google.golang.org/adk/v2/model"

	"github.com/PedroKlein/duto-ai/internal/compiler"
	"github.com/PedroKlein/duto-ai/internal/runtime"
	"github.com/PedroKlein/duto-ai/internal/testing/mockllm"
)

func TestIntegration_NoToolsTypedRun(t *testing.T) {
	compiled := compilePlan(t, noToolsConfig, noToolsWorkflow)
	llm := mockllm.New(mockllm.Response{Text: `{"outcome":"completed","report":"integration"}`})
	resolver := func(context.Context, string) (model.LLM, error) { return llm, nil }

	result, err := runtime.Run(t.Context(), compiled, compiler.ModelResolver(resolver))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output["report"] != "integration" || result.Status != runtime.StatusSucceeded {
		t.Fatalf("result = %#v", result)
	}
}
