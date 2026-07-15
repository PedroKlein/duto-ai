package compiler_test

import (
	"errors"
	"testing"
	"time"

	"github.com/PedroKlein/duto-ai/internal/compiler"
	"github.com/PedroKlein/duto-ai/internal/config"
)

func TestBuildRetryConfig_NilWhenNoPolicy(t *testing.T) {
	step := config.Step{ID: "s1", Prompt: "test"}

	got := compiler.BuildRetryConfig(step)
	if got != nil {
		t.Errorf("BuildRetryConfig(no retry) = %v, want nil", got)
	}
}

func TestBuildRetryConfig_Defaults(t *testing.T) {
	step := config.Step{
		ID:     "s1",
		Prompt: "test",
		Retry:  &config.RetryPolicy{},
	}

	got := compiler.BuildRetryConfig(step)
	if got == nil {
		t.Fatal("BuildRetryConfig(empty policy) = nil, want non-nil")
	}

	if got.MaxAttempts != config.DefaultRetryAttempts {
		t.Errorf("MaxAttempts = %d, want %d", got.MaxAttempts, config.DefaultRetryAttempts)
	}

	if got.InitialDelay != config.DefaultRetryDelay {
		t.Errorf("InitialDelay = %v, want %v", got.InitialDelay, config.DefaultRetryDelay)
	}
}

func TestBuildRetryConfig_CustomValues(t *testing.T) {
	step := config.Step{
		ID:     "s1",
		Prompt: "test",
		Retry: &config.RetryPolicy{
			MaxAttempts:  5,
			InitialDelay: "5s",
		},
	}

	got := compiler.BuildRetryConfig(step)
	if got == nil {
		t.Fatal("BuildRetryConfig(custom) = nil, want non-nil")
	}

	if got.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %d, want 5", got.MaxAttempts)
	}

	if got.InitialDelay != 5*time.Second {
		t.Errorf("InitialDelay = %v, want 5s", got.InitialDelay)
	}
}

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"429 rate limit", errors.New("status 429: too many requests"), true},
		{"503 unavailable", errors.New("status 503: service unavailable"), true},
		{"500 server error", errors.New("internal server error 500"), true},
		{"timeout", errors.New("context deadline exceeded: timeout"), true},
		{"rate limit text", errors.New("Rate Limit exceeded"), true},
		{"overloaded", errors.New("model is overloaded"), true},
		{"validation error", errors.New("invalid input: missing required field"), false},
		{"permission denied", errors.New("permission denied"), false},
		{"nil error", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := compiler.IsTransientError(tt.err); got != tt.want {
				t.Errorf("IsTransientError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
