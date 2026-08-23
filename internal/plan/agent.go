package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/prompt"
)

func compileAgents(workflow *config.Workflow, workflowInputs []Property, workflowLimits Limits, workspaceRoots map[string]string, skills []prompt.FrozenSkill, toolPolicy compiledToolPolicy) ([]Agent, error) {
	agents := make([]Agent, 0, len(workflow.AgentOrder))
	for _, name := range workflow.AgentOrder {
		source := workflow.Agents[name]

		workspaces, err := compileWorkspaces(source.Workspaces, workspaceRoots)
		if err != nil {
			return nil, fmt.Errorf("agent %q workspaces: %w", name, ErrInvalidAgent)
		}

		instruction, err := compileInstruction(source.Instruction, workspaces, workspaceRoots)
		if err != nil {
			return nil, fmt.Errorf("agent %q instruction: %w", name, err)
		}

		selectedSkills, err := compileSelectedSkills(source.Skills, workspaces, skills)
		if err != nil {
			return nil, fmt.Errorf("agent %q skills: %w", name, err)
		}

		input, err := compileSchema(source.Input, 0)
		if err != nil {
			return nil, fmt.Errorf("agent %q input: %w", name, err)
		}

		output, err := compileSchema(source.Output, 0)
		if err != nil {
			return nil, fmt.Errorf("agent %q output: %w", name, err)
		}

		if outcomeErr := validateOutcome(output); outcomeErr != nil {
			return nil, fmt.Errorf("agent %q output: %w", name, outcomeErr)
		}

		limits, err := compileLimits(source.Limits, workflowLimits)
		if err != nil {
			return nil, fmt.Errorf("agent %q limits: %w", name, err)
		}

		tools, err := compileToolScope(
			toolPolicy.catalog,
			source.Tools,
			toolPolicy.profiles,
			toolPolicy.ceiling,
			toolPolicy.scope.Names,
			false,
			source.ToolLimits,
			toolLimitMap(toolPolicy.scope.Limits),
			limits,
		)
		if err != nil {
			return nil, fmt.Errorf("agent %q tools: %w", name, err)
		}

		context, err := compileAgentContext(source.Context, workflowInputs, workspaces, workspaceRoots, limits)
		if err != nil {
			return nil, fmt.Errorf("agent %q context: %w", name, err)
		}

		agents = append(agents, Agent{
			Name:        name,
			Description: source.Description,
			Mode:        source.Mode,
			Model:       source.Model,
			ModelConfig: compileModelConfig(config.ModelConfig{}, compileModelConfig(workflow.ModelConfig, ModelConfig{})),
			Instruction: instruction,
			Tools:       tools,
			Skills:      selectedSkills,
			Workspaces:  workspaces,
			Context:     context,
			Input:       input,
			Output:      output,
			Limits:      limits,
			Subagents:   slices.Clone(source.Subagents),
		})
	}

	if err := validateAgentDefinitions(workflow, agents); err != nil {
		return nil, err
	}

	return agents, nil
}

func compileAgentContext(source config.AgentContext, workflowInputs []Property, workspaces []Workspace, workspaceRoots map[string]string, limits Limits) (AgentContext, error) {
	result := AgentContext{Mode: source.Mode, Include: []ContextSource{}}
	if source.Mode == config.ContextModeFresh {
		return result, nil
	}

	if source.Mode != config.ContextModeSnapshot || limits.MaxArtifactBytes <= 0 {
		return AgentContext{}, ErrInvalidAgent
	}

	result.MaxBytes = limits.MaxArtifactBytes

	workflowProperties := propertiesByName(workflowInputs)
	estimatedBytes := 2

	for _, item := range source.Include {
		compiled := ContextSource{}

		switch item.Kind {
		case config.ContextSourceInput:
			property, exists := workflowProperties[item.Input]
			if !exists {
				return AgentContext{}, ErrInvalidAgent
			}

			compiled.Kind = "input"
			compiled.Input = item.Input
			compiled.MaxBytes = schemaByteCeiling(property.Schema)
		case config.ContextSourceOutput:
			return AgentContext{}, ErrInvalidAgent
		case config.ContextSourceFile:
			if !containsWorkspace(workspaces, item.File.Workspace) {
				return AgentContext{}, ErrInvalidAgent
			}

			frozen, err := prompt.Admit(prompt.Source{
				Kind: prompt.KindFile,
				File: prompt.FileSource{Workspace: item.File.Workspace, Path: item.File.Path, MaxBytes: item.File.MaxBytes},
			}, workspaceRoots)
			if err != nil {
				return AgentContext{}, fmt.Errorf("freezing snapshot file: %w", err)
			}

			compiled.Kind = "file"
			compiled.Workspace = frozen.Workspace
			compiled.File = frozen.Path
			compiled.Content = frozen.Source
			compiled.Digest = frozen.Digest
			compiled.MaxBytes = frozen.MaxSourceBytes
		default:
			return AgentContext{}, ErrInvalidAgent
		}

		if compiled.Digest == "" {
			encoded, _ := json.Marshal(compiled)
			digest := sha256.Sum256(encoded)
			compiled.Digest = hex.EncodeToString(digest[:])
		}

		if compiled.MaxBytes <= 0 || compiled.MaxBytes > result.MaxBytes {
			return AgentContext{}, ErrInvalidAgent
		}

		estimatedBytes = boundedAdd(estimatedBytes, boundedAdd(compiled.MaxBytes, 256))
		if estimatedBytes > result.MaxBytes {
			return AgentContext{}, ErrInvalidAgent
		}

		result.Include = append(result.Include, compiled)
	}

	return result, nil
}

func schemaByteCeiling(schema Schema) int {
	switch schema.Type {
	case TypeString:
		if schema.MaxLength > 0 {
			return boundedAdd(boundedMultiply(schema.MaxLength, 4), 2)
		}

		maximum := 0
		for _, value := range schema.Enum {
			maximum = max(maximum, boundedAdd(boundedMultiply(len(value), 4), 2))
		}

		return maximum
	case TypeBoolean:
		return 5
	case TypeInteger, TypeNumber:
		return 32
	case TypeArray:
		if schema.Items == nil {
			return 0
		}

		return boundedAdd(2, boundedMultiply(schema.MaxItems, boundedAdd(schemaByteCeiling(*schema.Items), 1)))
	case TypeObject:
		total := 2
		for _, property := range schema.Properties {
			total = boundedAdd(total, boundedAdd(len(property.Name)+4, schemaByteCeiling(property.Schema)))
		}

		return total
	default:
		return 0
	}
}

func boundedAdd(left, right int) int {
	if left < 0 || right < 0 || left > math.MaxInt-right {
		return math.MaxInt
	}

	return left + right
}

func boundedMultiply(left, right int) int {
	if left < 0 || right < 0 || (right != 0 && left > math.MaxInt/right) {
		return math.MaxInt
	}

	return left * right
}

func validateAgentDefinitions(workflow *config.Workflow, agents []Agent) error { //nolint:gocyclo // Authority, mode, depth, and concurrency invariants are checked together.
	byName := agentsByName(agents)
	for _, parent := range agents {
		if len(parent.Subagents) > 0 && parent.Limits.MaxToolCalls <= 0 {
			return fmt.Errorf("agent %q subagents: %w", parent.Name, ErrInvalidAgent)
		}

		for _, childName := range parent.Subagents {
			child, exists := byName[childName]
			if !exists || child.Mode == config.AgentModeChat || !subset(child.Tools.Names, parent.Tools.Names) || !workspaceSubset(child.Workspaces, parent.Workspaces) || !limitsSubset(child.Limits, parent.Limits) {
				return fmt.Errorf("agent %q subagent %q: %w", parent.Name, childName, ErrInvalidAgent)
			}

			if parent.Limits.MaxParallelCalls > 1 && child.Mode == config.AgentModeSingleTurn && (len(child.Subagents) != 0 || unsafeToolScope(child.Tools)) {
				return fmt.Errorf("agent %q parallel subagent %q: %w", parent.Name, childName, ErrInvalidAgent)
			}
		}
	}

	for _, agent := range agents {
		if agentDepth(agent.Name, byName, map[string]bool{}) > workflow.Limits.MaxIterations {
			return fmt.Errorf("agent %q depth: %w", agent.Name, ErrInvalidAgent)
		}
	}

	return nil
}

func validateAgentUses(agents []Agent, steps []Step) error {
	byName := agentsByName(agents)

	for _, step := range steps {
		if step.Agent == "" {
			continue
		}

		definition, exists := byName[step.Agent]
		if !exists || (len(definition.Subagents) > 0 && definition.Mode != config.AgentModeChat) {
			return fmt.Errorf("step %q agent: %w", step.ID, ErrInvalidAgent)
		}
	}

	return nil
}

func compileNamedAgentStep(source config.Step, allSteps []config.Step, workflowInputs []Property, agents map[string]Agent) (Step, error) {
	definition, exists := agents[source.Agent]
	if !exists || definition.Mode == config.AgentModeTask {
		return Step{}, ErrInvalidAgent
	}

	bindings, err := compileBindings(source, definition.Input, workflowInputs, allSteps)
	if err != nil {
		return Step{}, err
	}

	conditions, err := compileConditions(source, allSteps)
	if err != nil {
		return Step{}, err
	}

	return Step{
		ID:          source.ID,
		Agent:       source.Agent,
		Needs:       slices.Clone(source.Needs),
		When:        conditions,
		Instruction: definition.Instruction,
		Model:       definition.Model,
		ModelConfig: definition.ModelConfig,
		Tools:       definition.Tools,
		Skills:      slices.Clone(definition.Skills),
		Workspaces:  slices.Clone(definition.Workspaces),
		Input:       definition.Input,
		Bindings:    bindings,
		Output:      definition.Output,
		Retry:       Retry{MaxAttempts: 1},
		Limits:      definition.Limits,
	}, nil
}

func agentsByName(agents []Agent) map[string]Agent {
	result := make(map[string]Agent, len(agents))
	for _, agent := range agents {
		result[agent.Name] = agent
	}

	return result
}

func subset(child, parent []string) bool {
	for _, value := range child {
		if !slices.Contains(parent, value) {
			return false
		}
	}

	return true
}

func workspaceSubset(child, parent []Workspace) bool {
	for _, value := range child {
		if !slices.Contains(parent, value) {
			return false
		}
	}

	return true
}

func limitsSubset(child, parent Limits) bool {
	childTimeout, childErr := time.ParseDuration(child.Timeout)
	parentTimeout, parentErr := time.ParseDuration(parent.Timeout)

	return childErr == nil && parentErr == nil && childTimeout <= parentTimeout &&
		child.MaxIterations <= parent.MaxIterations && child.MaxModelCalls <= parent.MaxModelCalls &&
		child.MaxToolCalls <= parent.MaxToolCalls && child.MaxParallelCalls <= parent.MaxParallelCalls &&
		child.MaxArtifactBytes <= parent.MaxArtifactBytes
}

func agentDepth(name string, agents map[string]Agent, visiting map[string]bool) int {
	if visiting[name] {
		return math.MaxInt
	}

	visiting[name] = true

	depth := 1
	for _, child := range agents[name].Subagents {
		depth = max(depth, 1+agentDepth(child, agents, visiting))
	}

	delete(visiting, name)

	return depth
}
