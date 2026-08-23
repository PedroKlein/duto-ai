package config

import (
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"
)

func decodeAgents(name string, node *yaml.Node) (agents map[string]AgentSpec, order []string, err error) {
	if node == nil {
		return map[string]AgentSpec{}, []string{}, nil
	}

	if node.Kind != yaml.MappingNode {
		return nil, nil, diagnostic(name, "$.agents", node, CodeInvalidType)
	}

	agents = make(map[string]AgentSpec, len(node.Content)/2)

	order = make([]string, 0, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]

		path := "$.agents." + key.Value
		if !namePattern.MatchString(key.Value) {
			return nil, nil, diagnostic(name, path, key, CodeInvalidValue)
		}

		agent, err := decodeAgent(name, value, path)
		if err != nil {
			return nil, nil, err
		}

		agents[key.Value] = agent
		order = append(order, key.Value)
	}

	return agents, order, nil
}

func decodeAgent(name string, node *yaml.Node, path string) (AgentSpec, error) { //nolint:gocyclo // Closed source record decoded in contract order.
	fields, err := mappingFields(name, node, path, "description", "mode", "model", "instruction", "tools", "tool_limits", "skills", "workspaces", "context", "input", "output", "limits", "subagents")
	if err != nil {
		return AgentSpec{}, err
	}

	description, err := requiredString(name, fields, "description", path+".description")
	if err != nil {
		return AgentSpec{}, err
	}

	mode, err := requiredString(name, fields, "mode", path+".mode")
	if err != nil {
		return AgentSpec{}, err
	}

	if mode != AgentModeSingleTurn && mode != AgentModeTask && mode != AgentModeChat {
		return AgentSpec{}, diagnostic(name, path+".mode", fields["mode"], CodeInvalidValue)
	}

	model, err := requiredString(name, fields, "model", path+".model")
	if err != nil {
		return AgentSpec{}, err
	}

	instruction, err := decodeInstruction(name, fields["instruction"], path+".instruction")
	if err != nil {
		return AgentSpec{}, err
	}

	tools, err := decodeToolExpression(name, fields["tools"], path+".tools")
	if err != nil {
		return AgentSpec{}, err
	}

	toolLimits, err := decodeToolLimits(name, fields["tool_limits"], path+".tool_limits")
	if err != nil {
		return AgentSpec{}, err
	}

	skills, err := decodeStringList(name, fields["skills"], path+".skills")
	if err != nil {
		return AgentSpec{}, err
	}

	workspaces, err := decodeWorkspaces(name, fields["workspaces"], path+".workspaces")
	if err != nil {
		return AgentSpec{}, err
	}

	context, err := decodeAgentContext(name, fields["context"], path+".context")
	if err != nil {
		return AgentSpec{}, err
	}

	input, err := decodeRequiredSchema(name, fields["input"], path+".input")
	if err != nil {
		return AgentSpec{}, err
	}

	output, err := decodeRequiredSchema(name, fields["output"], path+".output")
	if err != nil {
		return AgentSpec{}, err
	}

	limits, err := decodeOptionalLimits(name, fields["limits"], path+".limits")
	if err != nil {
		return AgentSpec{}, err
	}

	subagents, err := decodeStringList(name, fields["subagents"], path+".subagents")
	if err != nil {
		return AgentSpec{}, err
	}

	return AgentSpec{
		Description: description,
		Mode:        mode,
		Model:       model,
		Instruction: instruction,
		Tools:       tools,
		ToolLimits:  toolLimits,
		Skills:      skills,
		Workspaces:  workspaces,
		Context:     context,
		Input:       input,
		Output:      output,
		Limits:      limits,
		Subagents:   subagents,
	}, nil
}

func decodeAgentContext(name string, node *yaml.Node, path string) (AgentContext, error) {
	fields, err := mappingFields(name, node, path, "mode", "include")
	if err != nil {
		return AgentContext{}, err
	}

	mode, err := requiredString(name, fields, "mode", path+".mode")
	if err != nil {
		return AgentContext{}, err
	}

	if mode != ContextModeFresh && mode != ContextModeSnapshot {
		return AgentContext{}, diagnostic(name, path+".mode", fields["mode"], CodeInvalidValue)
	}

	include, err := decodeContextSources(name, fields["include"], path+".include")
	if err != nil {
		return AgentContext{}, err
	}

	if (mode == ContextModeFresh && len(include) != 0) || (mode == ContextModeSnapshot && len(include) == 0) {
		return AgentContext{}, diagnostic(name, path, node, CodeInvalidValue)
	}

	return AgentContext{Mode: mode, Include: include}, nil
}

func validateAgentGraph(workflow *Workflow) error { //nolint:gocognit,gocyclo // The finite graph, mode placement, and references are validated together.
	for name, definition := range workflow.Agents {
		if !namePattern.MatchString(name) || !namePattern.MatchString(definition.Model) {
			return ErrInvalidName
		}

		seen := make(map[string]struct{}, len(definition.Subagents))
		for _, child := range definition.Subagents {
			childDefinition, exists := workflow.Agents[child]
			if !exists {
				return fmt.Errorf("agent %q subagent %q: %w", name, child, ErrUnknownDependency)
			}

			if childDefinition.Mode == AgentModeChat {
				return fmt.Errorf("agent %q subagent %q: %w", name, child, ErrInvalidName)
			}

			if _, duplicate := seen[child]; duplicate {
				return fmt.Errorf("agent %q subagent %q: %w", name, child, ErrDuplicateStepID)
			}

			seen[child] = struct{}{}
		}
	}

	state := make(map[string]uint8, len(workflow.Agents))

	var visit func(string) error

	visit = func(name string) error {
		switch state[name] {
		case 1:
			return ErrCircularDependency
		case 2:
			return nil
		}

		state[name] = 1
		for _, child := range workflow.Agents[name].Subagents {
			if err := visit(child); err != nil {
				return err
			}
		}

		state[name] = 2

		return nil
	}
	for _, name := range workflow.AgentOrder {
		if err := visit(name); err != nil {
			return err
		}
	}

	for _, step := range workflow.Steps {
		if step.Agent == "" {
			continue
		}

		definition, exists := workflow.Agents[step.Agent]
		if !exists || definition.Mode == AgentModeTask {
			return fmt.Errorf("step %q agent: %w", step.ID, ErrInvalidName)
		}

		if definition.Mode == AgentModeChat && (len(workflow.Steps) != 1 || len(step.Needs) != 0 || workflow.Result.Step != step.ID) {
			return fmt.Errorf("step %q chat agent: %w", step.ID, ErrInvalidName)
		}
	}

	return nil
}

func validateDecodedAgents(name string, workflow *Workflow, agentsNode, stepsNode *yaml.Node) error { //nolint:gocyclo // Mirrors graph errors to stable source paths.
	if err := validateAgentGraph(workflow); err == nil {
		return nil
	}

	for agentIndex, agentName := range workflow.AgentOrder {
		definition := workflow.Agents[agentName]
		for childIndex, child := range definition.Subagents {
			childDefinition, exists := workflow.Agents[child]
			if !exists || childDefinition.Mode == AgentModeChat {
				return diagnostic(name, fmt.Sprintf("$.agents.%s.subagents[%d]", agentName, childIndex), agentsNode.Content[agentIndex*2+1], CodeInvalidValue)
			}
		}
	}

	state := make(map[string]uint8, len(workflow.Agents))

	var visit func(string) bool

	visit = func(agentName string) bool {
		if state[agentName] == 1 {
			return true
		}

		if state[agentName] == 2 {
			return false
		}

		state[agentName] = 1
		if slices.ContainsFunc(workflow.Agents[agentName].Subagents, visit) {
			return true
		}

		state[agentName] = 2

		return false
	}
	if slices.ContainsFunc(workflow.AgentOrder, visit) {
		return diagnostic(name, "$.agents", agentsNode, CodeInvalidValue)
	}

	for i, step := range workflow.Steps {
		if step.Agent == "" {
			continue
		}

		definition, exists := workflow.Agents[step.Agent]
		if !exists || definition.Mode == AgentModeTask || (definition.Mode == AgentModeChat && (len(workflow.Steps) != 1 || len(step.Needs) != 0 || workflow.Result.Step != step.ID)) {
			return diagnostic(name, fmt.Sprintf("$.steps[%d].agent", i), stepsNode.Content[i], CodeInvalidValue)
		}
	}

	return diagnostic(name, "$.agents", agentsNode, CodeInvalidValue)
}

func decodeContextSources(name string, node *yaml.Node, path string) ([]ContextSource, error) {
	if node == nil {
		return []ContextSource{}, nil
	}

	if node.Kind != yaml.SequenceNode {
		return nil, diagnostic(name, path, node, CodeInvalidType)
	}

	sources := make([]ContextSource, 0, len(node.Content))
	for i, item := range node.Content {
		itemPath := fmt.Sprintf("%s[%d]", path, i)

		fields, err := mappingFields(name, item, itemPath, "input", "output", "file")
		if err != nil {
			return nil, err
		}

		present := 0

		for _, field := range []string{"input", "output", "file"} {
			if fields[field] != nil {
				present++
			}
		}

		if present != 1 {
			return nil, diagnostic(name, itemPath, item, CodeInvalidValue)
		}

		source := ContextSource{}

		switch {
		case fields["input"] != nil:
			source.Kind = ContextSourceInput
			source.Input, err = scalarString(name, fields["input"], itemPath+".input")
		case fields["output"] != nil:
			source.Kind = ContextSourceOutput
			source.Output, err = decodeOutputRef(name, fields["output"], itemPath+".output")
		case fields["file"] != nil:
			source.Kind = ContextSourceFile
			source.File, err = decodeFileSource(name, fields["file"], itemPath+".file")
		}

		if err != nil {
			return nil, err
		}

		sources = append(sources, source)
	}

	return sources, nil
}
