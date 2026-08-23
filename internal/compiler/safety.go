package compiler

import (
	"errors"
	"fmt"
	"sync/atomic"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
)

var ErrModelCallLimit = errors.New("model call limit exceeded")

func NewCallLimiter(stepID string, limit int) llmagent.BeforeModelCallback {
	var calls atomic.Int64

	return func(_ agent.Context, _ *model.LLMRequest) (*model.LLMResponse, error) {
		if calls.Add(1) > int64(limit) {
			return nil, fmt.Errorf("step %q: %w", stepID, ErrModelCallLimit)
		}

		return nil, nil //nolint:nilnil // nil continues the native ADK model flow
	}
}
