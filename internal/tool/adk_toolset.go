package tool

import (
	"context"
	"fmt"
	"slices"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	adktool "google.golang.org/adk/v2/tool"
	"google.golang.org/genai"
)

type dutoToolset struct {
	tools []adktool.Tool
}

func NewToolset(tools []adktool.Tool) adktool.Toolset {
	return &dutoToolset{tools: slices.Clone(tools)}
}

func (ts *dutoToolset) Name() string {
	return "duto"
}

func (ts *dutoToolset) Tools(_ agent.ReadonlyContext) ([]adktool.Tool, error) {
	return slices.Clone(ts.tools), nil
}

type guardedToolset struct {
	toolset adktool.Toolset
	guard   *Guard
}

func GuardToolset(toolset adktool.Toolset, guard *Guard) adktool.Toolset {
	return &guardedToolset{toolset: toolset, guard: guard}
}

func (ts *guardedToolset) Name() string {
	return ts.toolset.Name()
}

func (ts *guardedToolset) Tools(ctx agent.ReadonlyContext) ([]adktool.Tool, error) {
	tools, err := ts.toolset.Tools(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing guarded tools: %w", err)
	}

	guarded := make([]adktool.Tool, len(tools))
	for i, current := range tools {
		guarded[i] = &guardedTool{Tool: current, guard: ts.guard}
	}

	return guarded, nil
}

type guardedTool struct {
	adktool.Tool
	guard *Guard
}

func (t *guardedTool) Declaration() *genai.FunctionDeclaration {
	declarer, ok := t.Tool.(interface {
		Declaration() *genai.FunctionDeclaration
	})
	if !ok {
		return nil
	}

	return declarer.Declaration()
}

func (t *guardedTool) ProcessRequest(ctx agent.Context, request *model.LLMRequest) error {
	processor, ok := t.Tool.(interface {
		ProcessRequest(agent.Context, *model.LLMRequest) error
	})
	if !ok {
		return nil
	}

	if err := processor.ProcessRequest(ctx, request); err != nil {
		return fmt.Errorf("processing guarded tool request: %w", err)
	}

	if request.Tools != nil {
		request.Tools[t.Name()] = t
	}

	return nil
}

func (t *guardedTool) Run(ctx agent.Context, args any) (map[string]any, error) {
	runnable, ok := t.Tool.(interface {
		Run(agent.Context, any) (map[string]any, error)
	})
	if !ok {
		return nil, ErrToolUnavailable
	}

	bounded, cancel, err := t.guard.Context(ctx, t.Name())
	if err != nil {
		return nil, err
	}
	defer cancel()

	result, err := runnable.Run(boundedContext{Context: ctx, bounded: bounded}, args)
	if err != nil {
		return nil, fmt.Errorf("running guarded tool: %w", err)
	}

	return result, nil
}

type boundedContext struct {
	agent.Context
	bounded context.Context
}

func (c boundedContext) Deadline() (time.Time, bool) {
	return c.bounded.Deadline()
}

func (c boundedContext) Done() <-chan struct{} {
	return c.bounded.Done()
}

func (c boundedContext) Err() error {
	return c.bounded.Err() //nolint:wrapcheck // context.Context requires sentinel identity.
}

func (c boundedContext) Value(key any) any {
	return c.bounded.Value(key)
}
