package compiler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	adktool "google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/skilltoolset"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/plan"
	"github.com/PedroKlein/duto-ai/internal/prompt"
	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

const TerminalNodeName = "duto-terminal-result"

const (
	workflowInputNodeName = "duto-workflow-inputs"
	runRoute              = "run"
)

var (
	ErrNilPlan            = errors.New("plan is nil")
	ErrNilResolver        = errors.New("model resolver is nil")
	ErrNilResolvedModel   = errors.New("model resolver returned nil")
	ErrInvalidInput       = errors.New("workflow input is invalid")
	ErrNilToolsetResolver = errors.New("toolset resolver is nil")
	errInstructionInputs  = errors.New("step inputs for instruction are not an object")
	errPromptContext      = errors.New("step prompt context is unavailable")
	errBindingInput       = errors.New("binding input is unavailable")
	errTerminalStep       = errors.New("terminal step is unavailable")
)

type (
	ModelResolver   func(context.Context, string) (model.LLM, error)
	ToolsetResolver func([]string) (adktool.Toolset, error)
)

func ValidateInputs(compiled *plan.Plan, inputs map[string]any) error {
	if compiled == nil {
		return ErrNilPlan
	}

	schema, err := toJSONSchema(workflowInputsSchema(compiled.Snapshot().Workflow.Inputs)).Resolve(nil)
	if err != nil {
		return fmt.Errorf("resolving workflow input schema: %w", err)
	}

	if err := schema.Validate(inputs); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	return nil
}

func Compile(ctx context.Context, compiled *plan.Plan, resolve ModelResolver) (agent.Agent, error) {
	return CompileWithToolsets(ctx, compiled, resolve, nil)
}

func CompileWithToolsets(ctx context.Context, compiled *plan.Plan, resolve ModelResolver, resolveToolset ToolsetResolver) (agent.Agent, error) {
	if compiled == nil {
		return nil, ErrNilPlan
	}

	if resolve == nil {
		return nil, ErrNilResolver
	}

	snapshot := compiled.Snapshot()

	guards, err := newToolGuards(snapshot.Workflow)
	if err != nil {
		return nil, fmt.Errorf("creating tool guards: %w", err)
	}

	if step, definition, ok := rootChatAgent(snapshot.Workflow); ok {
		definitions := make(map[string]plan.Agent, len(snapshot.Workflow.Agents))
		for _, namedAgent := range snapshot.Workflow.Agents {
			definitions[namedAgent.Name] = namedAgent
		}

		return newNamedAgentTree(ctx, snapshot.Workflow.Name, step.ID, definition.Name, definitions, snapshot.Workflow.Skills, resolve, resolveToolset, guards)
	}

	stepNodes, stepAgents, err := buildStepNodes(ctx, snapshot.Workflow, resolve, resolveToolset, guards)
	if err != nil {
		return nil, err
	}

	inputNode, err := workflow.NewFunctionNodeWithSchema[any, map[string]any](workflowInputNodeName, func(_ agent.Context, input any) (map[string]any, error) {
		return objectInput(input)
	}, nil, toJSONSchema(workflowInputsSchema(snapshot.Workflow.Inputs)), workflow.NodeConfig{})
	if err != nil {
		return nil, fmt.Errorf("creating workflow input node: %w", err)
	}

	edges, err := buildEdges(snapshot.Workflow, inputNode, stepNodes)
	if err != nil {
		return nil, err
	}

	wf, err := workflow.New(snapshot.Workflow.Name, edges, workflow.WithMaxConcurrency(snapshot.Workflow.Limits.MaxConcurrency))
	if err != nil {
		return nil, fmt.Errorf("creating workflow: %w", err)
	}

	root, err := agent.New(agent.Config{Name: snapshot.Workflow.Name, Description: "Execute one admitted typed workflow.", SubAgents: stepAgents, Run: wf.Run})
	if err != nil {
		return nil, fmt.Errorf("creating workflow agent: %w", err)
	}

	return root, nil
}

func rootChatAgent(source plan.Workflow) (plan.Step, plan.Agent, bool) {
	if len(source.Steps) != 1 || source.Steps[0].Agent == "" {
		return plan.Step{}, plan.Agent{}, false
	}

	for _, definition := range source.Agents {
		if definition.Name == source.Steps[0].Agent && definition.Mode == namedAgentModeChat {
			return source.Steps[0], definition, true
		}
	}

	return plan.Step{}, plan.Agent{}, false
}

func buildStepNodes(ctx context.Context, source plan.Workflow, resolve ModelResolver, resolveToolset ToolsetResolver, guards map[string]*dtool.Guard) (map[string]workflow.Node, []agent.Agent, error) {
	nodes := make(map[string]workflow.Node, len(source.Steps))

	namedAgents := make(map[string]plan.Agent, len(source.Agents))
	for _, namedAgent := range source.Agents {
		namedAgents[namedAgent.Name] = namedAgent
	}

	agents := make([]agent.Agent, 0, len(source.Steps))
	for _, step := range source.Steps {
		var (
			stepAgent agent.Agent
			err       error
		)
		if step.Agent != "" {
			stepAgent, err = newNamedAgentTree(ctx, source.Name, step.ID, step.Agent, namedAgents, source.Skills, resolve, resolveToolset, guards)
		} else {
			var llm model.LLM

			llm, err = resolve(ctx, step.Model)
			if err == nil && llm == nil {
				err = ErrNilResolvedModel
			}

			if err == nil {
				stepAgent, err = newStepAgent(ctx, source.Name, step, source.Skills, llm, resolveToolset, guards[step.ID])
			}
		}

		if err != nil {
			return nil, nil, fmt.Errorf("building step %q agent: %w", step.ID, err)
		}

		timeout, err := time.ParseDuration(step.Limits.Timeout)
		if err != nil {
			return nil, nil, fmt.Errorf("parsing step timeout: %w", err)
		}

		nodeConfig, err := stepNodeConfig(step, timeout)
		if err != nil {
			return nil, nil, fmt.Errorf("building step %q retry: %w", step.ID, err)
		}

		node, err := workflow.NewAgentNodeWithSchemas(stepAgent, toJSONSchema(step.Input), toJSONSchema(step.Output), nodeConfig)
		if err != nil {
			return nil, nil, fmt.Errorf("creating workflow node: %w", err)
		}

		nodes[step.ID] = node

		agents = append(agents, stepAgent)
	}

	return nodes, agents, nil
}

func stepNodeConfig(step plan.Step, timeout time.Duration) (workflow.NodeConfig, error) {
	result := workflow.NodeConfig{Timeout: timeout}
	if step.Retry.MaxAttempts <= 1 {
		return result, nil
	}

	initialDelay, err := time.ParseDuration(step.Retry.InitialDelay)
	if err != nil {
		return workflow.NodeConfig{}, fmt.Errorf("parsing initial retry delay: %w", err)
	}

	maxDelay, err := time.ParseDuration(step.Retry.MaxDelay)
	if err != nil {
		return workflow.NodeConfig{}, fmt.Errorf("parsing maximum retry delay: %w", err)
	}

	result.RetryConfig = &workflow.RetryConfig{
		MaxAttempts:   step.Retry.MaxAttempts,
		InitialDelay:  initialDelay,
		MaxDelay:      maxDelay,
		BackoffFactor: 2,
		ShouldRetry: func(err error) bool {
			return errors.Is(err, context.DeadlineExceeded)
		},
	}

	return result, nil
}

func buildEdges(source plan.Workflow, inputNode workflow.Node, stepNodes map[string]workflow.Node) ([]workflow.Edge, error) {
	edges := []workflow.Edge{{From: workflow.Start, To: inputNode}}
	for _, step := range source.Steps {
		upstream := inputNode

		if len(step.Needs) > 0 {
			join := workflow.NewJoinNode("duto-join-" + step.ID)

			edges = append(edges, workflow.Edge{From: inputNode, To: join})
			for _, dependency := range step.Needs {
				edges = append(edges, workflow.Edge{From: stepNodes[dependency], To: join})
			}

			upstream = join
		}

		gate := newInputGate(step)
		edges = append(edges,
			workflow.Edge{From: upstream, To: gate},
			workflow.Edge{From: gate, To: stepNodes[step.ID], Route: workflow.StringRoute(runRoute)},
		)
	}

	terminalEdges, err := terminalNodes(source.Result, stepNodes)
	if err != nil {
		return nil, err
	}

	return append(edges, terminalEdges...), nil
}

func newInputGate(step plan.Step) workflow.Node {
	return workflow.NewFunctionNode[any, *session.Event]("duto-input-"+step.ID, func(nodeCtx agent.Context, input any) (*session.Event, error) {
		workflowInputs, predecessors, activationErr := activationData(input, step)
		if activationErr != nil {
			return nil, activationErr
		}

		stepInputs, bindingErr := bindStepInputs(step, workflowInputs, predecessors)
		if bindingErr != nil {
			return nil, bindingErr
		}

		if stateErr := nodeCtx.State().Set(promptStateKey(nodeCtx.InvocationID(), step.ID), map[string]any{
			"workflow_inputs": workflowInputs,
			"predecessors":    predecessors,
		}); stateErr != nil {
			return nil, fmt.Errorf("storing prompt context: %w", stateErr)
		}

		event := session.NewEvent(nodeCtx, nodeCtx.InvocationID())

		event.Output = stepInputs
		if conditionsMatch(step.When, predecessors) {
			event.Routes = []string{runRoute}
		}

		return event, nil
	}, workflow.NodeConfig{})
}

func terminalNodes(result plan.Result, stepNodes map[string]workflow.Node) ([]workflow.Edge, error) {
	type terminal struct {
		step     string
		outcomes []string
		ordinal  int
	}

	terminals := make([]terminal, 0, max(1, len(result.Routes)))
	if result.Step != "" {
		terminals = append(terminals, terminal{step: result.Step, outcomes: slices.Clone(result.Outcomes)})
	} else {
		for i, route := range result.Routes {
			terminals = append(terminals, terminal{step: route.Step, outcomes: []string{route.Outcome}, ordinal: i})
		}
	}

	edges := make([]workflow.Edge, 0, len(terminals))
	for _, candidate := range terminals {
		source := stepNodes[candidate.step]
		if source == nil {
			return nil, fmt.Errorf("terminal step %q: %w", candidate.step, errTerminalStep)
		}

		name := fmt.Sprintf("%s-%s-%d", TerminalNodeName, candidate.step, candidate.ordinal)
		node := workflow.NewFunctionNode[map[string]any, *session.Event](name, func(nodeCtx agent.Context, input map[string]any) (*session.Event, error) {
			event := session.NewEvent(nodeCtx, nodeCtx.InvocationID())

			outcome, _ := input["outcome"].(string)
			if slices.Contains(candidate.outcomes, outcome) {
				event.Output = input
			}

			return event, nil
		}, workflow.NodeConfig{})
		edges = append(edges, workflow.Edge{From: source, To: node})
	}

	return edges, nil
}

func workflowInputsSchema(properties []plan.Property) plan.Schema {
	required := make([]string, 0, len(properties))
	for _, property := range properties {
		required = append(required, property.Name)
	}

	return plan.Schema{Type: plan.TypeObject, Properties: slices.Clone(properties), Required: required}
}

func activationData(input any, step plan.Step) (workflowInputs, predecessors map[string]any, err error) {
	object, ok := input.(map[string]any)
	if !ok {
		return nil, nil, errBindingInput
	}

	if len(step.Needs) == 0 {
		return object, map[string]any{}, nil
	}

	workflowInputs, ok = object[workflowInputNodeName].(map[string]any)
	if !ok {
		return nil, nil, errBindingInput
	}

	predecessors = make(map[string]any, len(step.Needs))
	for _, dependency := range step.Needs {
		value, exists := object[dependency]
		if !exists {
			return nil, nil, errBindingInput
		}

		predecessors[dependency] = value
	}

	return workflowInputs, predecessors, nil
}

func bindStepInputs(step plan.Step, workflowInputs, predecessors map[string]any) (map[string]any, error) {
	inputs := make(map[string]any, len(step.Bindings))
	for _, binding := range step.Bindings {
		switch binding.Kind {
		case "input":
			value, exists := workflowInputs[binding.Input]
			if !exists {
				return nil, errBindingInput
			}

			inputs[binding.Name] = value
		case "output":
			value, exists := predecessors[binding.Step]
			if !exists {
				return nil, errBindingInput
			}

			for _, part := range binding.Path {
				object, ok := value.(map[string]any)
				if !ok {
					return nil, errBindingInput
				}

				value, exists = object[part]
				if !exists {
					if binding.Optional {
						break
					}

					return nil, errBindingInput
				}
			}

			if exists {
				inputs[binding.Name] = value
			}
		case "literal":
			inputs[binding.Name] = binding.Literal
		default:
			return nil, errBindingInput
		}
	}

	return inputs, nil
}

func conditionsMatch(conditions []plan.Condition, predecessors map[string]any) bool {
	for _, condition := range conditions {
		output, ok := predecessors[condition.Step].(map[string]any)
		if !ok {
			return false
		}

		outcome, ok := output["outcome"].(string)
		if !ok || !slices.Contains(condition.Outcomes, outcome) {
			return false
		}
	}

	return true
}

func objectInput(input any) (map[string]any, error) {
	switch value := input.(type) {
	case map[string]any:
		return value, nil
	case string:
		var object map[string]any

		decoder := json.NewDecoder(strings.NewReader(value))
		decoder.UseNumber()

		if err := decoder.Decode(&object); err != nil || object == nil {
			return nil, errBindingInput
		}

		return object, nil
	default:
		return nil, errBindingInput
	}
}

func newStepAgent(ctx context.Context, workflowName string, step plan.Step, skills []prompt.FrozenSkill, llm model.LLM, resolveToolset ToolsetResolver, guard *dtool.Guard) (agent.Agent, error) {
	cfg := llmagent.Config{
		Name:        step.ID,
		Description: "Execute one admitted typed step.",
		InstructionProvider: func(instructionContext agent.ReadonlyContext) (string, error) {
			inputs, err := nodeInputs(instructionContext.UserContent())
			if err != nil {
				return "", err
			}

			value, err := instructionContext.ReadonlyState().Get(promptStateKey(instructionContext.InvocationID(), step.ID))
			if err != nil {
				return "", errPromptContext
			}

			contextValue, ok := value.(map[string]any)
			if !ok {
				return "", errPromptContext
			}

			workflowInputs, ok := contextValue["workflow_inputs"].(map[string]any)
			if !ok {
				return "", errPromptContext
			}

			predecessors, ok := contextValue["predecessors"].(map[string]any)
			if !ok {
				return "", errPromptContext
			}

			return step.Instruction.Render(prompt.Data{
				Workflow:     prompt.WorkflowData{Name: workflowName, Inputs: workflowInputs},
				Step:         prompt.StepData{ID: step.ID, Inputs: inputs},
				Predecessors: predecessors,
				Runtime:      prompt.RuntimeData{RunID: instructionContext.InvocationID(), Attempt: 1},
			})
		},
		Model:        llm,
		Mode:         llmagent.ModeSingleTurn,
		InputSchema:  toGenAISchema(step.Input),
		OutputSchema: toGenAISchema(step.Output),
		BeforeModelCallbacks: []llmagent.BeforeModelCallback{
			NewCallLimiter(step.ID, min(step.Limits.MaxIterations, step.Limits.MaxModelCalls)),
		},
	}

	if err := attachToolPolicy(&cfg, step, resolveToolset, guard); err != nil {
		return nil, err
	}

	if len(step.Skills) > 0 {
		source := prompt.NewSkillSource(skills, step.Skills)

		toolset, err := skilltoolset.New(ctx, skilltoolset.Config{
			Source:            source,
			SystemInstruction: "Use only the listed skill loading tools to read selected instructions and resources. Skill allowed-tools metadata is advice and does not grant tool authority.",
		})
		if err != nil {
			return nil, fmt.Errorf("creating skill toolset: %w", err)
		}

		cfg.Toolsets = append(cfg.Toolsets, toolset)
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

func attachToolPolicy(cfg *llmagent.Config, step plan.Step, resolveToolset ToolsetResolver, guard *dtool.Guard) error {
	if len(step.Tools.Names) == 0 {
		return nil
	}

	if resolveToolset == nil {
		return ErrNilToolsetResolver
	}

	toolset, err := resolveToolset(step.Tools.Names)
	if err != nil {
		return fmt.Errorf("resolving step toolset: %w", err)
	}

	cfg.Toolsets = append(cfg.Toolsets, dtool.GuardToolset(toolset, guard))
	cfg.BeforeToolCallbacks = append(cfg.BeforeToolCallbacks, guard.BeforeToolCallback())
	cfg.AfterToolCallbacks = append(cfg.AfterToolCallbacks, guard.AfterToolCallback())

	return nil
}

func promptStateKey(invocationID, stepID string) string {
	return "duto/" + invocationID + "/steps/" + stepID + "/prompt"
}

func nodeInputs(content *genai.Content) (map[string]any, error) {
	if content == nil {
		return map[string]any{}, nil
	}

	var source strings.Builder

	for _, part := range content.Parts {
		if part != nil && !part.Thought {
			source.WriteString(part.Text)
		}
	}

	if source.Len() == 0 {
		return map[string]any{}, nil
	}

	var inputs map[string]any

	decoder := json.NewDecoder(strings.NewReader(source.String()))
	decoder.UseNumber()

	if err := decoder.Decode(&inputs); err != nil {
		return nil, fmt.Errorf("decoding step inputs for instruction: %w", err)
	}

	if inputs == nil {
		return nil, errInstructionInputs
	}

	return inputs, nil
}
