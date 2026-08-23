package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ResultWhen struct {
	Step    string
	Outcome string
}

type ResultRoute struct {
	When ResultWhen
	Step string
}

type Result struct {
	Step   string
	Routes []ResultRoute
}

type Workflow struct {
	Version     int
	Name        string
	Description string
	Inputs      map[string]Input
	Model       string
	ModelConfig ModelConfig
	Tools       []string
	Skills      map[string]SkillSource
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

	fields, err := mappingFields(name, root, "$", "version", "name", "description", "inputs", "model", "model_config", "tools", "skills", "limits", "steps", "result")
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

	tools, skills, err := decodeWorkflowCapabilities(name, fields)
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
		Skills:      skills,
		Limits:      limits,
		Steps:       steps,
		Result:      result,
	}
	if err := validateDecodedWorkflow(name, workflow, fields, fields["steps"]); err != nil {
		return nil, err
	}

	return workflow, nil
}

func decodeWorkflowCapabilities(name string, fields map[string]*yaml.Node) (tools []string, skills map[string]SkillSource, err error) {
	tools, err = decodeStringList(name, fields["tools"], "$.tools")
	if err != nil {
		return nil, nil, err
	}

	skills, err = decodeSkills(name, fields["skills"])
	if err != nil {
		return nil, nil, err
	}

	return tools, skills, nil
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

func decodeStep(name string, node *yaml.Node, path string) (Step, error) { //nolint:gocyclo // A step is one closed source record decoded in contract order.
	fields, err := mappingFields(name, node, path, "id", "needs", "wait", "when", "instruction", "model", "model_config", "tools", "skills", "workspaces", "input", "with", "output", "limits")
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

	wait, err := optionalString(name, fields["wait"], path+".wait")
	if err != nil {
		return Step{}, err
	}

	if wait != "" && wait != "all_succeeded" {
		return Step{}, diagnostic(name, path+".wait", fields["wait"], CodeInvalidValue)
	}

	when, err := decodeConditions(name, fields["when"], path+".when")
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

	skills, err := decodeStringList(name, fields["skills"], path+".skills")
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

	bindingOrder := mappingKeyOrder(fields["with"])

	output, err := decodeRequiredSchema(name, fields["output"], path+".output")
	if err != nil {
		return Step{}, err
	}

	limits, err := decodeOptionalLimits(name, fields["limits"], path+".limits")
	if err != nil {
		return Step{}, err
	}

	return Step{ID: id, Needs: needs, Wait: wait, When: when, Instruction: instruction, Model: model, ModelConfig: modelConfig, Tools: tools, Skills: skills, Workspaces: workspaces, Input: input, With: bindings, WithOrder: bindingOrder, Output: output, Limits: limits}, nil
}

func decodeInstruction(name string, node *yaml.Node, path string) (Instruction, error) {
	if node == nil {
		return Instruction{}, diagnostic(name, path, nil, CodeMissingField)
	}

	if node.Kind == yaml.ScalarNode {
		text, err := scalarString(name, node, path)
		return Instruction{Kind: InstructionText, Text: text}, err
	}

	fields, err := mappingFields(name, node, path, "text", "file", "template")
	if err != nil {
		return Instruction{}, err
	}

	present := 0

	for _, field := range []string{"text", "file", "template"} {
		if fields[field] != nil {
			present++
		}
	}

	if present != 1 {
		return Instruction{}, diagnostic(name, path, node, CodeInvalidValue)
	}

	if fields["text"] != nil {
		text, err := scalarString(name, fields["text"], path+".text")
		return Instruction{Kind: InstructionText, Text: text}, err
	}

	if fields["file"] != nil {
		file, err := decodeFileSource(name, fields["file"], path+".file")
		return Instruction{Kind: InstructionFile, File: file}, err
	}

	return decodeTemplateInstruction(name, fields["template"], path+".template")
}

func decodeTemplateInstruction(name string, node *yaml.Node, path string) (Instruction, error) {
	fields, err := mappingFields(name, node, path, "text", "file", "max_output_bytes")
	if err != nil {
		return Instruction{}, err
	}

	if (fields["text"] == nil) == (fields["file"] == nil) {
		return Instruction{}, diagnostic(name, path, node, CodeInvalidValue)
	}

	maxOutputBytes, err := requiredInt(name, fields, "max_output_bytes", path+".max_output_bytes")
	if err != nil || maxOutputBytes <= 0 {
		if err != nil {
			return Instruction{}, err
		}

		return Instruction{}, diagnostic(name, path+".max_output_bytes", fields["max_output_bytes"], CodeInvalidValue)
	}

	if fields["text"] != nil {
		text, textErr := scalarString(name, fields["text"], path+".text")
		return Instruction{Kind: InstructionTemplate, Text: text, MaxOutputBytes: maxOutputBytes}, textErr
	}

	file, err := decodeFileSource(name, fields["file"], path+".file")

	return Instruction{Kind: InstructionTemplateFile, File: file, MaxOutputBytes: maxOutputBytes}, err
}

func decodeFileSource(name string, node *yaml.Node, path string) (FileSource, error) {
	fields, err := mappingFields(name, node, path, "workspace", "path", "max_bytes")
	if err != nil {
		return FileSource{}, err
	}

	workspace, err := requiredString(name, fields, "workspace", path+".workspace")
	if err != nil {
		return FileSource{}, err
	}

	filePath, err := requiredString(name, fields, "path", path+".path")
	if err != nil {
		return FileSource{}, err
	}

	maxBytes, err := requiredInt(name, fields, "max_bytes", path+".max_bytes")
	if err != nil {
		return FileSource{}, err
	}

	if workspace == "" || filePath == "" || maxBytes <= 0 {
		return FileSource{}, diagnostic(name, path, node, CodeInvalidValue)
	}

	return FileSource{Workspace: workspace, Path: filePath, MaxBytes: maxBytes}, nil
}

func decodeSkills(name string, node *yaml.Node) (map[string]SkillSource, error) {
	if node == nil {
		return map[string]SkillSource{}, nil
	}

	if node.Kind != yaml.MappingNode {
		return nil, diagnostic(name, "$.skills", node, CodeInvalidType)
	}

	skilled := make(map[string]SkillSource, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]

		path := "$.skills." + key.Value
		if !namePattern.MatchString(key.Value) {
			return nil, diagnostic(name, path, key, CodeInvalidValue)
		}

		fields, err := mappingFields(name, value, path, "workspace", "path")
		if err != nil {
			return nil, err
		}

		workspace, err := requiredString(name, fields, "workspace", path+".workspace")
		if err != nil {
			return nil, err
		}

		skillPath, err := requiredString(name, fields, "path", path+".path")
		if err != nil {
			return nil, err
		}

		skilled[key.Value] = SkillSource{Workspace: workspace, Path: skillPath}
	}

	return skilled, nil
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

func mappingKeyOrder(node *yaml.Node) []string {
	if node == nil || node.Kind != yaml.MappingNode {
		return []string{}
	}

	order := make([]string, 0, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		order = append(order, node.Content[i].Value)
	}

	return order
}

func decodeConditions(name string, node *yaml.Node, path string) ([]Condition, error) {
	if node == nil {
		return []Condition{}, nil
	}

	if node.Kind != yaml.SequenceNode {
		return nil, diagnostic(name, path, node, CodeInvalidType)
	}

	conditions := make([]Condition, 0, len(node.Content))
	for i, item := range node.Content {
		itemPath := fmt.Sprintf("%s[%d]", path, i)

		fields, err := mappingFields(name, item, itemPath, "step", "outcome_in")
		if err != nil {
			return nil, err
		}

		step, err := requiredString(name, fields, "step", itemPath+".step")
		if err != nil {
			return nil, err
		}

		outcomes, err := decodeStringList(name, fields["outcome_in"], itemPath+".outcome_in")
		if err != nil {
			return nil, err
		}

		if len(outcomes) == 0 {
			return nil, diagnostic(name, itemPath+".outcome_in", fields["outcome_in"], CodeInvalidValue)
		}

		conditions = append(conditions, Condition{Step: step, Outcomes: outcomes})
	}

	return conditions, nil
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

		binding, err := decodeBinding(name, value, itemPath)
		if err != nil {
			return nil, err
		}

		bindings[key.Value] = binding
	}

	return bindings, nil
}

func decodeBinding(name string, node *yaml.Node, path string) (Binding, error) {
	fields, err := mappingFields(name, node, path, "input", "output", "literal", "optional")
	if err != nil {
		return Binding{}, err
	}

	optional := fields["optional"] != nil && fields["optional"].Value == "true"
	if fields["optional"] != nil && (fields["optional"].Kind != yaml.ScalarNode || fields["optional"].Tag != yamlBoolTag) {
		return Binding{}, diagnostic(name, path+".optional", fields["optional"], CodeInvalidType)
	}

	present := 0

	for _, field := range []string{"input", "output", "literal"} {
		if fields[field] != nil {
			present++
		}
	}

	if present != 1 {
		return Binding{}, diagnostic(name, path, node, CodeInvalidValue)
	}

	if fields["output"] != nil {
		output, outputErr := decodeOutputRef(name, fields["output"], path+".output")
		return Binding{Kind: BindingOutput, Output: output, Optional: optional}, outputErr
	}

	if optional {
		return Binding{}, diagnostic(name, path+".optional", fields["optional"], CodeInvalidValue)
	}

	if fields["input"] != nil {
		input, inputErr := scalarString(name, fields["input"], path+".input")
		return Binding{Kind: BindingInput, Input: input}, inputErr
	}

	literal, err := decodeLiteral(name, fields["literal"], path+".literal")

	return Binding{Kind: BindingLiteral, Literal: literal}, err
}

func decodeOutputRef(name string, node *yaml.Node, path string) (OutputRef, error) {
	fields, err := mappingFields(name, node, path, "step", "path")
	if err != nil {
		return OutputRef{}, err
	}

	step, err := requiredString(name, fields, "step", path+".step")
	if err != nil {
		return OutputRef{}, err
	}

	parts, err := decodeStringList(name, fields["path"], path+".path")
	if err != nil {
		return OutputRef{}, err
	}

	if len(parts) == 0 {
		return OutputRef{}, diagnostic(name, path+".path", fields["path"], CodeInvalidValue)
	}

	return OutputRef{Step: step, Path: parts}, nil
}

func decodeLiteral(name string, node *yaml.Node, path string) (any, error) {
	if node.Kind != yaml.ScalarNode {
		return nil, diagnostic(name, path, node, CodeInvalidType)
	}

	switch node.Tag {
	case yamlStringTag:
		return node.Value, nil
	case yamlBoolTag:
		return node.Value == "true", nil
	case yamlIntTag:
		return scalarInt(name, node, path)
	case yamlFloatTag:
		return scalarFloat(name, node, path)
	default:
		return nil, diagnostic(name, path, node, CodeInvalidType)
	}
}

func decodeResult(name string, node *yaml.Node) (Result, error) {
	fields, err := mappingFields(name, node, "$.result", "step", "routes")
	if err != nil {
		return Result{}, err
	}

	if (fields["step"] == nil) == (fields["routes"] == nil) {
		return Result{}, diagnostic(name, "$.result", node, CodeInvalidValue)
	}

	if fields["step"] != nil {
		step, err := scalarString(name, fields["step"], "$.result.step")
		return Result{Step: step}, err
	}

	if fields["routes"].Kind != yaml.SequenceNode || len(fields["routes"].Content) == 0 {
		return Result{}, diagnostic(name, "$.result.routes", fields["routes"], CodeInvalidType)
	}

	routes := make([]ResultRoute, 0, len(fields["routes"].Content))
	for i, routeNode := range fields["routes"].Content {
		path := fmt.Sprintf("$.result.routes[%d]", i)

		routeFields, err := mappingFields(name, routeNode, path, "when", "step")
		if err != nil {
			return Result{}, err
		}

		step, err := requiredString(name, routeFields, "step", path+".step")
		if err != nil {
			return Result{}, err
		}

		whenFields, err := mappingFields(name, routeFields["when"], path+".when", "step", "outcome")
		if err != nil {
			return Result{}, err
		}

		whenStep, err := requiredString(name, whenFields, "step", path+".when.step")
		if err != nil {
			return Result{}, err
		}

		outcome, err := requiredString(name, whenFields, "outcome", path+".when.outcome")
		if err != nil {
			return Result{}, err
		}

		routes = append(routes, ResultRoute{When: ResultWhen{Step: whenStep, Outcome: outcome}, Step: step})
	}

	return Result{Routes: routes}, nil
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
