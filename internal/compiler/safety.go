package compiler

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	model "google.golang.org/adk/v2/model"

	"github.com/PedroKlein/duto-ai/internal/config"
)

// ErrMaxIterations is returned when a step exceeds its configured maximum LLM call limit.
var ErrMaxIterations = errors.New("maximum iterations exceeded")

// NewIterationLimiter returns a BeforeModelCallback that aborts the LLM call
// after maxIterations calls. Each call to the returned callback increments
// the counter; once the limit is reached, it returns ErrMaxIterations.
func NewIterationLimiter(stepID string, maxIterations int) llmagent.BeforeModelCallback {
	var count atomic.Int32

	return func(_ agent.Context, _ *model.LLMRequest) (*model.LLMResponse, error) {
		current := int(count.Add(1))
		if current > maxIterations {
			return nil, fmt.Errorf("step %q: %w (limit: %d)", stepID, ErrMaxIterations, maxIterations)
		}

		return nil, nil //nolint:nilnil // proceed with normal LLM call
	}
}

// ResolveMaxIterations returns the effective max_iterations for a step,
// falling back to the default if not configured.
func ResolveMaxIterations(step config.Step) int {
	if step.MaxIterations > 0 {
		return step.MaxIterations
	}

	return config.DefaultMaxIterations
}

// ResolveTimeout parses the step timeout string and returns the duration,
// falling back to the default if not configured or invalid.
func ResolveTimeout(step config.Step) time.Duration {
	timeout := step.Timeout
	if timeout == "" {
		timeout = config.DefaultTimeout
	}

	d, err := time.ParseDuration(timeout)
	if err != nil {
		// Fallback to default on parse error — validation should catch this earlier.
		d, _ = time.ParseDuration(config.DefaultTimeout)
	}

	return d
}
