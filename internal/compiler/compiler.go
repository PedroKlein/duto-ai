package compiler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/plan"
)

const TerminalNodeName = "duto-terminal-result"

var (
	ErrNilPlan          = errors.New("plan is nil")
	ErrNilResolver      = errors.New("model resolver is nil")
	ErrNilResolvedModel = errors.New("model resolver returned nil")
	ErrUnsupportedPlan  = errors.New("plan is outside the no-tools tracer")
)

type ModelResolver func(context.Context, string) (model.LLM, error)

func Compile(ctx context.Context, compiled *plan.Plan, resolve ModelResolver) (agent.Agent, error) {
	if compiled == nil {
		return nil, ErrNilPlan
	}

	if resolve == nil {
		return nil, ErrNilResolver
	}

	snapshot := compiled.Snapshot()
	if len(snapshot.Workflow.Steps) != 1 || len(snapshot.Workflow.Inputs) != 0 {
		return nil, ErrUnsupportedPlan
	}

	step := snapshot.Workflow.Steps[0]
	if len(step.Needs) != 0 || len(step.Tools) != 0 {
		return nil, ErrUnsupportedPlan
	}

	llm, err := resolve(ctx, step.Model)
	if err != nil {
		return nil, fmt.Errorf("resolving model alias: %w", err)
	}

	if llm == nil {
		return nil, ErrNilResolvedModel
	}

	stepAgent, err := newStepAgent(step, llm)
	if err != nil {
		return nil, err
	}

	timeout, err := time.ParseDuration(step.Limits.Timeout)
	if err != nil {
		return nil, fmt.Errorf("parsing step timeout: %w", err)
	}

	node, err := workflow.NewAgentNodeWithSchemas(stepAgent, toJSONSchema(step.Input), toJSONSchema(step.Output), workflow.NodeConfig{Timeout: timeout})
	if err != nil {
		return nil, fmt.Errorf("creating workflow node: %w", err)
	}

	terminal, err := workflow.NewFunctionNodeWithSchema(TerminalNodeName, func(_ agent.Context, input map[string]any) (map[string]any, error) {
		return input, nil
	}, toJSONSchema(step.Output), toJSONSchema(step.Output), workflow.NodeConfig{})
	if err != nil {
		return nil, fmt.Errorf("creating terminal result node: %w", err)
	}

	edges := []workflow.Edge{{From: workflow.Start, To: node}, {From: node, To: terminal}}

	wf, err := workflow.New(snapshot.Workflow.Name, edges, workflow.WithMaxConcurrency(snapshot.Workflow.Limits.MaxConcurrency))
	if err != nil {
		return nil, fmt.Errorf("creating workflow: %w", err)
	}

	root, err := agent.New(agent.Config{
		Name:        snapshot.Workflow.Name,
		Description: "Execute one admitted typed workflow.",
		SubAgents:   []agent.Agent{stepAgent},
		Run:         wf.Run,
	})
	if err != nil {
		return nil, fmt.Errorf("creating workflow agent: %w", err)
	}

	return root, nil
}

func newStepAgent(step plan.Step, llm model.LLM) (agent.Agent, error) {
	cfg := llmagent.Config{
		Name:         step.ID,
		Description:  "Execute one admitted typed step.",
		Instruction:  step.Instruction,
		Model:        llm,
		Mode:         llmagent.ModeSingleTurn,
		InputSchema:  toGenAISchema(step.Input),
		OutputSchema: toGenAISchema(step.Output),
		BeforeModelCallbacks: []llmagent.BeforeModelCallback{
			NewCallLimiter(step.ID, min(step.Limits.MaxIterations, step.Limits.MaxModelCalls)),
		},
	}

	if step.ModelConfig.Temperature != nil || step.ModelConfig.MaxOutputTokens != nil {
		cfg.GenerateContentConfig = &genai.GenerateContentConfig{}

		if step.ModelConfig.Temperature != nil {
			value := float32(*step.ModelConfig.Temperature)
			cfg.GenerateContentConfig.Temperature = &value
		}

		if step.ModelConfig.MaxOutputTokens != nil {
			cfg.GenerateContentConfig.MaxOutputTokens = int32(*step.ModelConfig.MaxOutputTokens)
		}
	}

	stepAgent, err := llmagent.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating step agent: %w", err)
	}

	return stepAgent, nil
}
