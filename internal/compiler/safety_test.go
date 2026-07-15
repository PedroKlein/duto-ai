package compiler_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PedroKlein/duto-ai/internal/compiler"
	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/testing/mockllm"
	"github.com/PedroKlein/duto-ai/internal/tool"

	"google.golang.org/adk/v2/model"
)

func TestCompile_MaxIterations_WiredIntoAgent(t *testing.T) {
	wf := &config.Workflow{
		Name: "safety-test",
		Steps: []config.Step{
			{
				ID:            "limited",
				Prompt:        "do something",
				MaxIterations: 5,
			},
		},
	}

	cfg := &config.Config{
		Defaults: config.Defaults{
			Model: "test-model",
			Tools: []string{},
		},
	}

	reg := tool.NewRegistry()
	mock := mockllm.New(mockllm.Response{Text: "done"})
	resolve := func(_ string) (model.LLM, error) { return mock, nil }

	_, err := compiler.Compile(wf, cfg, reg, resolve, nil)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}
}

func TestCompile_DefaultMaxIterations_WhenNotSet(t *testing.T) {
	wf := &config.Workflow{
		Name: "default-limits",
		Steps: []config.Step{
			{
				ID:     "step1",
				Prompt: "do work",
			},
		},
	}

	cfg := &config.Config{
		Defaults: config.Defaults{
			Model: "test-model",
			Tools: []string{},
		},
	}

	reg := tool.NewRegistry()
	mock := mockllm.New(mockllm.Response{Text: "done"})
	resolve := func(_ string) (model.LLM, error) { return mock, nil }

	_, err := compiler.Compile(wf, cfg, reg, resolve, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompile_Timeout_WiredFromStep(t *testing.T) {
	wf := &config.Workflow{
		Name: "timeout-test",
		Steps: []config.Step{
			{
				ID:      "fast",
				Prompt:  "quick task",
				Timeout: "10s",
			},
		},
	}

	cfg := &config.Config{
		Defaults: config.Defaults{
			Model: "test-model",
			Tools: []string{},
		},
	}

	reg := tool.NewRegistry()
	mock := mockllm.New(mockllm.Response{Text: "done"})
	resolve := func(_ string) (model.LLM, error) { return mock, nil }

	_, err := compiler.Compile(wf, cfg, reg, resolve, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompile_Timeout_InvalidFallsBackToDefault(t *testing.T) {
	wf := &config.Workflow{
		Name: "bad-timeout",
		Steps: []config.Step{
			{
				ID:      "bad",
				Prompt:  "task with bad timeout",
				Timeout: "not-a-duration",
			},
		},
	}

	cfg := &config.Config{
		Defaults: config.Defaults{
			Model: "test-model",
			Tools: []string{},
		},
	}

	reg := tool.NewRegistry()
	mock := mockllm.New(mockllm.Response{Text: "done"})
	resolve := func(_ string) (model.LLM, error) { return mock, nil }

	_, err := compiler.Compile(wf, cfg, reg, resolve, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveMaxIterations(t *testing.T) {
	tests := []struct {
		name string
		step config.Step
		want int
	}{
		{
			name: "explicit value",
			step: config.Step{MaxIterations: 10},
			want: 10,
		},
		{
			name: "zero falls back to default",
			step: config.Step{MaxIterations: 0},
			want: config.DefaultMaxIterations,
		},
		{
			name: "negative falls back to default",
			step: config.Step{MaxIterations: -1},
			want: config.DefaultMaxIterations,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := compiler.ResolveMaxIterations(tc.step)
			if got != tc.want {
				t.Errorf("ResolveMaxIterations() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestResolveTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		want    time.Duration
	}{
		{
			name:    "explicit seconds",
			timeout: "60s",
			want:    60 * time.Second,
		},
		{
			name:    "explicit minutes",
			timeout: "5m",
			want:    5 * time.Minute,
		},
		{
			name:    "empty falls back to default",
			timeout: "",
			want:    300 * time.Second,
		},
		{
			name:    "invalid falls back to default",
			timeout: "banana",
			want:    300 * time.Second,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			step := config.Step{Timeout: tc.timeout}
			got := compiler.ResolveTimeout(step)

			if got != tc.want {
				t.Errorf("ResolveTimeout(%q) = %v, want %v", tc.timeout, got, tc.want)
			}
		})
	}
}

func TestIterationLimiter_AbortsAfterMax(t *testing.T) {
	limiter := compiler.NewIterationLimiter("test-step", 3)

	// First 3 calls should succeed.
	for i := range 3 {
		resp, err := limiter(nil, nil)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i+1, err)
		}

		if resp != nil {
			t.Fatalf("call %d: expected nil response", i+1)
		}
	}

	// 4th call should fail.
	_, err := limiter(nil, nil)
	if err == nil {
		t.Fatal("expected error on 4th call")
	}

	if !errors.Is(err, compiler.ErrMaxIterations) {
		t.Errorf("expected ErrMaxIterations, got: %v", err)
	}
}

func TestIterationLimiter_ErrorContainsStepID(t *testing.T) {
	limiter := compiler.NewIterationLimiter("my-step", 1)

	// First call succeeds.
	_, _ = limiter(nil, nil)

	// Second call exceeds limit.
	_, err := limiter(nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "my-step") {
		t.Errorf("error should contain step ID, got: %s", err.Error())
	}
}
