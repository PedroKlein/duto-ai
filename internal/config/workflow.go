package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Result struct {
	Step string
}

type Workflow struct {
	Version     int
	Name        string
	Description string
	Inputs      map[string]Input
	Model       string
	ModelConfig ModelConfig
	Tools       []string
	Limits      Limits
	Steps       []Step
	Result      Result
}

func LoadWorkflow(path string) (*Workflow, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is selected by the caller
	if err != nil {
		return nil, fmt.Errorf("reading workflow: %w", err)
	}

	return DecodeWorkflow(path, data)
}

func DecodeWorkflow(name string, data []byte) (*Workflow, error) {
	root, err := decodeDocument(name, data)
	if err != nil {
		return nil, err
	}

	fields, err := mappingFields(name, root, "$", "version", "name", "description", "inputs", "model", "model_config", "tools", "limits", "steps", "result")
	if err != nil {
		return nil, err
	}

	version, err := requiredInt(name, fields, "version", "$.version")
	if err != nil {
		return nil, err
	}

	if version != Version {
		return nil, diagnostic(name, "$.version", fields["version"], CodeUnsupportedVers)
	}

	workflowName, err := requiredString(name, fields, "name", "$.name")
	if err != nil {
		return nil, err
	}

	model, err := requiredString(name, fields, "model", "$.model")
	if err != nil {
		return nil, err
	}

	description, err := optionalString(name, fields["description"], "$.description")
	if err != nil {
		return nil, err
	}

	inputs, err := decodeInputs(name, fields["inputs"])
	if err != nil {
		return nil, err
	}

	modelConfig, err := decodeModelConfig(name, fields["model_config"], "$.model_config")
	if err != nil {
		return nil, err
	}

	tools, err := decodeStringList(name, fields["tools"], "$.tools")
	if err != nil {
		return nil, err
	}

	limits, err := decodeLimits(name, fields["limits"], "$.limits")
	if err != nil {
		return nil, err
	}

	steps, err := decodeSteps(name, fields["steps"])
	if err != nil {
		return nil, err
	}

	result, err := decodeResult(name, fields["result"])
	if err != nil {
		return nil, err
	}

	workflow := &Workflow{
		Version:     version,
		Name:        workflowName,
		Description: description,
		Inputs:      inputs,
		Model:       model,
		ModelConfig: modelConfig,
		Tools:       tools,
		Limits:      limits,
		Steps:       steps,
		Result:      result,
	}
	if err := validateDecodedWorkflow(name, workflow, fields, fields["steps"]); err != nil {
		return nil, err
	}

	return workflow, nil
}

func decodeInputs(name string, node *yaml.Node) (map[string]Input, error) {
	if node == nil {
		return map[string]Input{}, nil
	}

	if node.Kind != yaml.MappingNode {
		return nil, diagnostic(name, "$.inputs", node, CodeInvalidType)
	}

	inputs := make(map[string]Input, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]

		path := "$.inputs." + key.Value
		if !namePattern.MatchString(key.Value) {
			return nil, diagnostic(name, path, key, CodeInvalidValue)
		}

		fields, err := mappingFields(name, value, path, "schema")
		if err != nil {
			return nil, err
		}

		schema, err := decodeRequiredSchema(name, fields["schema"], path+".schema")
		if err != nil {
			return nil, err
		}

		inputs[key.Value] = Input{Schema: schema}
	}

	return inputs, nil
}

func decodeModelConfig(name string, node *yaml.Node, path string) (ModelConfig, error) {
	if node == nil {
		return ModelConfig{}, nil
	}

	fields, err := mappingFields(name, node, path, "temperature", "max_output_tokens")
	if err != nil {
		return ModelConfig{}, err
	}

	var modelConfig ModelConfig

	if temperature := fields["temperature"]; temperature != nil {
		value, err := scalarFloat(name, temperature, path+".temperature")
		if err != nil {
			return ModelConfig{}, err
		}

		modelConfig.Temperature = value
		modelConfig.HasTemperature = true
	}

	if maxOutputTokens := fields["max_output_tokens"]; maxOutputTokens != nil {
		value, err := scalarInt(name, maxOutputTokens, path+".max_output_tokens")
		if err != nil {
			return ModelConfig{}, err
		}

		modelConfig.MaxOutputTokens = value
		modelConfig.HasMaxOutputTokens = true
	}

	return modelConfig, nil
}

func decodeLimits(name string, node *yaml.Node, path string) (Limits, error) {
	if node == nil {
		return Limits{}, diagnostic(name, path, nil, CodeMissingField)
	}

	fields, err := mappingFields(name, node, path, "timeout", "max_iterations", "max_model_calls", "max_tool_calls", "max_concurrency", "max_parallel_calls", "max_artifact_bytes")
	if err != nil {
		return Limits{}, err
	}

	var limits Limits
	if fields["timeout"] != nil {
		limits.Timeout, err = scalarString(name, fields["timeout"], path+".timeout")
		if err != nil {
			return Limits{}, err
		}
	}

	limits.MaxIterations, err = optionalInt(name, fields["max_iterations"], path+".max_iterations")
	if err != nil {
		return Limits{}, err
	}

	limits.MaxModelCalls, err = optionalInt(name, fields["max_model_calls"], path+".max_model_calls")
	if err != nil {
		return Limits{}, err
	}

	limits.MaxToolCalls, err = optionalInt(name, fields["max_tool_calls"], path+".max_tool_calls")
	if err != nil {
		return Limits{}, err
	}

	limits.MaxConcurrency, err = optionalInt(name, fields["max_concurrency"], path+".max_concurrency")
	if err != nil {
		return Limits{}, err
	}

	limits.MaxParallelCalls, err = optionalInt(name, fields["max_parallel_calls"], path+".max_parallel_calls")
	if err != nil {
		return Limits{}, err
	}

	limits.MaxArtifactBytes, err = optionalInt(name, fields["max_artifact_bytes"], path+".max_artifact_bytes")
	if err != nil {
		return Limits{}, err
	}

	return limits, nil
}

func decodeSteps(name string, node *yaml.Node) ([]Step, error) {
	if node == nil {
		return nil, diagnostic(name, "$.steps", nil, CodeMissingField)
	}

	if node.Kind != yaml.SequenceNode {
		return nil, diagnostic(name, "$.steps", node, CodeInvalidType)
	}

	steps := make([]Step, 0, len(node.Content))
	for i, stepNode := range node.Content {
		path := fmt.Sprintf("$.steps[%d]", i)

		step, err := decodeStep(name, stepNode, path)
		if err != nil {
			return nil, err
		}

		steps = append(steps, step)
	}

	return steps, nil
}

func decodeStep(name string, node *yaml.Node, path string) (Step, error) {
	fields, err := mappingFields(name, node, path, "id", "needs", "instruction", "model", "model_config", "tools", "workspaces", "input", "with", "output", "limits")
	if err != nil {
		return Step{}, err
	}

	id, err := requiredString(name, fields, "id", path+".id")
	if err != nil {
		return Step{}, err
	}

	needs, err := decodeStringList(name, fields["needs"], path+".needs")
	if err != nil {
		return Step{}, err
	}

	instruction, err := decodeInstruction(name, fields["instruction"], path+".instruction")
	if err != nil {
		return Step{}, err
	}

	model, err := optionalString(name, fields["model"], path+".model")
	if err != nil {
		return Step{}, err
	}

	modelConfig, err := decodeModelConfig(name, fields["model_config"], path+".model_config")
	if err != nil {
		return Step{}, err
	}

	tools, err := decodeStringList(name, fields["tools"], path+".tools")
	if err != nil {
		return Step{}, err
	}

	workspaces, err := decodeWorkspaces(name, fields["workspaces"], path+".workspaces")
	if err != nil {
		return Step{}, err
	}

	input, err := decodeRequiredSchema(name, fields["input"], path+".input")
	if err != nil {
		return Step{}, err
	}

	bindings, err := decodeBindings(name, fields["with"], path+".with")
	if err != nil {
		return Step{}, err
	}

	output, err := decodeRequiredSchema(name, fields["output"], path+".output")
	if err != nil {
		return Step{}, err
	}

	limits, err := decodeOptionalLimits(name, fields["limits"], path+".limits")
	if err != nil {
		return Step{}, err
	}

	return Step{ID: id, Needs: needs, Instruction: instruction, Model: model, ModelConfig: modelConfig, Tools: tools, Workspaces: workspaces, Input: input, With: bindings, Output: output, Limits: limits}, nil
}

func decodeInstruction(name string, node *yaml.Node, path string) (Instruction, error) {
	if node == nil {
		return Instruction{}, diagnostic(name, path, nil, CodeMissingField)
	}

	if node.Kind == yaml.ScalarNode {
		text, err := scalarString(name, node, path)
		return Instruction{Kind: InstructionText, Text: text}, err
	}

	fields, err := mappingFields(name, node, path, "text")
	if err != nil {
		return Instruction{}, err
	}

	text, err := requiredString(name, fields, "text", path+".text")
	if err != nil {
		return Instruction{}, err
	}

	return Instruction{Kind: InstructionText, Text: text}, nil
}

func decodeRequiredSchema(name string, node *yaml.Node, path string) (Schema, error) {
	if node == nil {
		return Schema{}, diagnostic(name, path, nil, CodeMissingField)
	}

	fields, err := mappingFields(name, node, path, "type", "properties", "required", "items", "max_length", "max_items", "enum", "minimum", "maximum")
	if err != nil {
		return Schema{}, err
	}

	typeName, err := requiredString(name, fields, "type", path+".type")
	if err != nil {
		return Schema{}, err
	}

	schema := Schema{Type: typeName}

	schema.Properties, err = decodeSchemaProperties(name, fields["properties"], path+".properties")
	if err != nil {
		return Schema{}, err
	}

	schema.Required, err = decodeStringList(name, fields["required"], path+".required")
	if err != nil {
		return Schema{}, err
	}

	schema.Enum, err = decodeStringList(name, fields["enum"], path+".enum")
	if err != nil {
		return Schema{}, err
	}

	if err := decodeSchemaShape(name, fields, path, &schema); err != nil {
		return Schema{}, err
	}

	return schema, nil
}

func decodeSchemaProperties(name string, node *yaml.Node, path string) (map[string]Schema, error) {
	properties := make(map[string]Schema)
	if node == nil {
		return properties, nil
	}

	if node.Kind != yaml.MappingNode {
		return nil, diagnostic(name, path, node, CodeInvalidType)
	}

	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]

		propertyPath := path + "." + key.Value
		if !namePattern.MatchString(key.Value) {
			return nil, diagnostic(name, propertyPath, key, CodeInvalidValue)
		}

		property, err := decodeRequiredSchema(name, value, propertyPath)
		if err != nil {
			return nil, err
		}

		properties[key.Value] = property
	}

	return properties, nil
}

func decodeSchemaShape(name string, fields map[string]*yaml.Node, path string, schema *Schema) error {
	var err error

	if items := fields["items"]; items != nil {
		itemSchema, decodeErr := decodeRequiredSchema(name, items, path+".items")
		if decodeErr != nil {
			return decodeErr
		}

		schema.Items = &itemSchema
	}

	schema.MaxLength, err = optionalInt(name, fields["max_length"], path+".max_length")
	if err != nil {
		return err
	}

	schema.MaxItems, err = optionalInt(name, fields["max_items"], path+".max_items")
	if err != nil {
		return err
	}

	if minimum := fields["minimum"]; minimum != nil {
		schema.Minimum, err = scalarFloat(name, minimum, path+".minimum")
		if err != nil {
			return err
		}

		schema.HasMinimum = true
	}

	if maximum := fields["maximum"]; maximum != nil {
		schema.Maximum, err = scalarFloat(name, maximum, path+".maximum")
		if err != nil {
			return err
		}

		schema.HasMaximum = true
	}

	return nil
}

func decodeWorkspaces(name string, node *yaml.Node, path string) ([]WorkspaceRef, error) {
	if node == nil {
		return []WorkspaceRef{}, nil
	}

	if node.Kind != yaml.SequenceNode {
		return nil, diagnostic(name, path, node, CodeInvalidType)
	}

	workspaces := make([]WorkspaceRef, 0, len(node.Content))
	for i, workspace := range node.Content {
		itemPath := fmt.Sprintf("%s[%d]", path, i)

		fields, err := mappingFields(name, workspace, itemPath, "name", "access")
		if err != nil {
			return nil, err
		}

		workspaceName, err := requiredString(name, fields, "name", itemPath+".name")
		if err != nil {
			return nil, err
		}

		if !namePattern.MatchString(workspaceName) {
			return nil, diagnostic(name, itemPath+".name", fields["name"], CodeInvalidValue)
		}

		access, err := requiredString(name, fields, "access", itemPath+".access")
		if err != nil {
			return nil, err
		}

		workspaces = append(workspaces, WorkspaceRef{Name: workspaceName, Access: access})
	}

	return workspaces, nil
}

func decodeBindings(name string, node *yaml.Node, path string) (map[string]Binding, error) {
	if node == nil {
		return nil, diagnostic(name, path, nil, CodeMissingField)
	}

	if node.Kind != yaml.MappingNode {
		return nil, diagnostic(name, path, node, CodeInvalidType)
	}

	bindings := make(map[string]Binding, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]

		itemPath := path + "." + key.Value
		if !namePattern.MatchString(key.Value) {
			return nil, diagnostic(name, itemPath, key, CodeInvalidValue)
		}

		fields, err := mappingFields(name, value, itemPath, "input", "literal")
		if err != nil {
			return nil, err
		}

		switch {
		case fields["input"] != nil && fields["literal"] == nil:
			input, err := scalarString(name, fields["input"], itemPath+".input")
			if err != nil {
				return nil, err
			}

			bindings[key.Value] = Binding{Kind: BindingInput, Input: input}
		case fields["literal"] != nil && fields["input"] == nil:
			literal, err := scalarString(name, fields["literal"], itemPath+".literal")
			if err != nil {
				return nil, err
			}

			bindings[key.Value] = Binding{Kind: BindingLiteral, Literal: literal}
		default:
			return nil, diagnostic(name, itemPath, value, CodeInvalidValue)
		}
	}

	return bindings, nil
}

func decodeResult(name string, node *yaml.Node) (Result, error) {
	fields, err := mappingFields(name, node, "$.result", "step")
	if err != nil {
		return Result{}, err
	}

	step, err := requiredString(name, fields, "step", "$.result.step")
	if err != nil {
		return Result{}, err
	}

	return Result{Step: step}, nil
}

func decodeStringList(name string, node *yaml.Node, path string) ([]string, error) {
	if node == nil {
		return []string{}, nil
	}

	if node.Kind != yaml.SequenceNode {
		return nil, diagnostic(name, path, node, CodeInvalidType)
	}

	values := make([]string, 0, len(node.Content))
	for i, item := range node.Content {
		value, err := scalarString(name, item, fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return nil, err
		}

		values = append(values, value)
	}

	return values, nil
}

func optionalString(name string, node *yaml.Node, path string) (string, error) {
	if node == nil {
		return "", nil
	}

	return scalarString(name, node, path)
}

func optionalInt(name string, node *yaml.Node, path string) (int, error) {
	if node == nil {
		return 0, nil
	}

	return scalarInt(name, node, path)
}

func decodeOptionalLimits(name string, node *yaml.Node, path string) (Limits, error) {
	if node == nil {
		return Limits{}, nil
	}

	return decodeLimits(name, node, path)
}
