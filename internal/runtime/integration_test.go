//go:build integration

package runtime_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/runtime"
	"github.com/PedroKlein/duto-ai/internal/testing/mockllm"
)

func TestIntegration_FullPipeline_ThreeSteps(t *testing.T) {
	mock := mockllm.New(
		mockllm.Response{Text: "gathered: PR adds null check to config parser"},
		mockllm.Response{Text: "analysis: code looks good, minor style nit on line 45"},
		mockllm.Response{Text: "reported: posted review with 1 comment"},
	)

	ctx := context.Background()

	err := runtime.Run(ctx,
		filepath.Join("testdata", "integration_config.yaml"),
		filepath.Join("testdata", "integration_workflow.yaml"),
		runtime.WithLLM(mock),
	)
	if err != nil {
		t.Fatalf("runtime.Run: %v", err)
	}

	if mock.CallCount() != 3 {
		t.Errorf("LLM called %d times, want 3", mock.CallCount())
	}
}

func TestIntegration_OutputPassing(t *testing.T) {
	mock := mockllm.New(
		mockllm.Response{Text: "STEP1_OUTPUT_MARKER_XYZ"},
		mockllm.Response{Text: "received marker and analyzed"},
		mockllm.Response{Text: "done"},
	)

	ctx := context.Background()

	err := runtime.Run(ctx,
		filepath.Join("testdata", "integration_config.yaml"),
		filepath.Join("testdata", "integration_workflow.yaml"),
		runtime.WithLLM(mock),
	)
	if err != nil {
		t.Fatalf("runtime.Run: %v", err)
	}

	// Step 2 should have received step 1's output in its rendered prompt
	calls := mock.Calls()
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 calls, got %d", len(calls))
	}

	// Check that step 2's user message contains step 1's output
	step2Call := calls[1]
	found := false

	for _, content := range step2Call.Contents {
		for _, part := range content.Parts {
			if strings.Contains(part.Text, "STEP1_OUTPUT_MARKER_XYZ") {
				found = true
			}
		}
	}

	if !found {
		t.Error("step 2 prompt should contain step 1's output (STEP1_OUTPUT_MARKER_XYZ)")
	}
}

func TestIntegration_FailFast(t *testing.T) {
	mock := mockllm.New(
		mockllm.Response{Text: "step 1 ok"},
		mockllm.Response{Error: fmt.Errorf("simulated LLM failure")},
		mockllm.Response{Text: "step 3 should never run"},
	)

	ctx := context.Background()

	err := runtime.Run(ctx,
		filepath.Join("testdata", "integration_config.yaml"),
		filepath.Join("testdata", "integration_workflow.yaml"),
		runtime.WithLLM(mock),
	)

	if err == nil {
		t.Fatal("expected error from failed step")
	}

	if !strings.Contains(err.Error(), "analyze") {
		t.Errorf("error should mention failing step 'analyze', got: %v", err)
	}

	// Step 3 should never have been called
	if mock.CallCount() != 2 {
		t.Errorf("LLM called %d times, want 2 (step 3 should not execute)", mock.CallCount())
	}
}
