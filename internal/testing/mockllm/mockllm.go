package mockllm

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// ErrNoMoreResponses is returned when the mock runs out of configured responses.
var ErrNoMoreResponses = errors.New("mock: no more responses configured")

// Response defines what the mock returns for a single GenerateContent call.
type Response struct {
	Text          string
	Content       *genai.Content // If set, takes priority over Text.
	Error         error
	UsageMetadata *genai.GenerateContentResponseUsageMetadata
	Partial       bool
	TurnComplete  bool
	Emissions     []Response
}

// MockLLM implements model.LLM for testing.
type MockLLM struct {
	mu        sync.Mutex
	responses []Response
	calls     []RecordedCall
	callIndex int
}

// RecordedCall tracks a call made to the mock.
type RecordedCall struct {
	Contents []*genai.Content
	Tools    map[string]any
	Config   *genai.GenerateContentConfig
}

// New creates a MockLLM with the given sequence of responses.
func New(responses ...Response) *MockLLM {
	return &MockLLM{responses: responses}
}

// Name satisfies model.LLM.
func (m *MockLLM) Name() string {
	return "mock-llm"
}

// GenerateContent satisfies model.LLM.
func (m *MockLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	m.mu.Lock()

	call := RecordedCall{
		Contents: req.Contents,
		Tools:    req.Tools,
		Config:   cloneConfig(req.Config),
	}
	m.calls = append(m.calls, call)

	var resp Response
	if m.callIndex < len(m.responses) {
		resp = m.responses[m.callIndex]
		m.callIndex++
	} else {
		resp = Response{Error: fmt.Errorf("%w (call %d)", ErrNoMoreResponses, m.callIndex)}
	}

	m.mu.Unlock()

	return func(yield func(*model.LLMResponse, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(nil, err)

			return
		}

		emissions := resp.Emissions
		if len(emissions) == 0 {
			emissions = []Response{resp}
		}

		for _, emission := range emissions {
			if err := ctx.Err(); err != nil {
				yield(nil, err)

				return
			}

			if emission.Error != nil {
				yield(nil, emission.Error)

				return
			}

			if !yield(&model.LLMResponse{
				Content:       responseContent(emission),
				UsageMetadata: emission.UsageMetadata,
				Partial:       emission.Partial,
				TurnComplete:  emission.TurnComplete,
			}, nil) {
				return
			}
		}
	}
}

func responseContent(resp Response) *genai.Content {
	if resp.Content != nil {
		return resp.Content
	}

	return genai.NewContentFromText(resp.Text, "model")
}

func cloneConfig(cfg *genai.GenerateContentConfig) *genai.GenerateContentConfig {
	if cfg == nil {
		return nil
	}

	clone := *cfg
	clone.Tools = make([]*genai.Tool, len(cfg.Tools))

	for i, packedTool := range cfg.Tools {
		if packedTool == nil {
			continue
		}

		toolClone := *packedTool
		toolClone.FunctionDeclarations = make([]*genai.FunctionDeclaration, len(packedTool.FunctionDeclarations))

		for j, declaration := range packedTool.FunctionDeclarations {
			if declaration == nil {
				continue
			}

			declarationClone := *declaration
			toolClone.FunctionDeclarations[j] = &declarationClone
		}

		clone.Tools[i] = &toolClone
	}

	return &clone
}

// Calls returns all recorded calls.
func (m *MockLLM) Calls() []RecordedCall {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]RecordedCall, len(m.calls))
	copy(result, m.calls)

	return result
}

// CallCount returns the number of calls made.
func (m *MockLLM) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.calls)
}
