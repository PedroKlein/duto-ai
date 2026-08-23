package runtime_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/adk/v2/model"

	"github.com/PedroKlein/duto-ai/internal/compiler"
	"github.com/PedroKlein/duto-ai/internal/runtime"
	"github.com/PedroKlein/duto-ai/internal/testing/mockllm"
)

func TestRun_RetriesTransientTimeoutWithNativeNodePolicy(t *testing.T) {
	workflow := strings.Replace(noToolsWorkflow, "max_iterations: 2\n  max_model_calls: 2", "max_iterations: 3\n  max_model_calls: 3", 1)
	workflow = strings.Replace(workflow, "    output:\n", "    retry: {max_attempts: 2, initial_delay: 1ms, max_delay: 2ms}\n    output:\n", 1)
	compiled := compilePlan(t, noToolsConfig, workflow)
	llm := mockllm.New(
		mockllm.Response{Error: context.DeadlineExceeded},
		mockllm.Response{Text: `{"outcome":"completed","report":"retried"}`},
	)
	resolver := func(context.Context, string) (model.LLM, error) { return llm, nil }

	result, err := runtime.Run(t.Context(), compiled, compiler.ModelResolver(resolver))
	if err != nil {
		t.Fatalf("Run() error = %v, result = %#v", err, result)
	}

	if llm.CallCount() != 2 || result.Output["report"] != "retried" {
		t.Fatalf("retry result = %#v, calls = %d", result, llm.CallCount())
	}
}
