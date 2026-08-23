package plan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/PedroKlein/duto-ai/internal/config"
)

const Version = 1

var (
	ErrNilConfig             = errors.New("config is nil")
	ErrNilWorkflow           = errors.New("workflow is nil")
	ErrUnknownModel          = errors.New("unknown model alias")
	ErrInvalidLimits         = errors.New("invalid limits")
	ErrUnsupportedCapability = errors.New("capability is not supported by the no-tools plan")
	ErrInvalidBinding        = errors.New("invalid binding")
	ErrInvalidResult         = errors.New("invalid terminal result")
)

type Projection struct {
	Version  int      `json:"version"`
	Digest   string   `json:"digest,omitempty"`
	Models   []string `json:"models"`
	Workflow Workflow `json:"workflow"`
}

type Workflow struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Model       string      `json:"model"`
	ModelConfig ModelConfig `json:"model_config"`
	Inputs      []Property  `json:"inputs"`
	Tools       []string    `json:"tools"`
	Limits      Limits      `json:"limits"`
	Steps       []Step      `json:"steps"`
	Result      Result      `json:"result"`
}

type ModelConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxOutputTokens *int     `json:"max_output_tokens,omitempty"`
}

type Limits struct {
	Timeout          string `json:"timeout"`
	MaxIterations    int    `json:"max_iterations"`
	MaxModelCalls    int    `json:"max_model_calls"`
	MaxToolCalls     int    `json:"max_tool_calls"`
	MaxConcurrency   int    `json:"max_concurrency"`
	MaxParallelCalls int    `json:"max_parallel_calls"`
	MaxArtifactBytes int    `json:"max_artifact_bytes"`
}

type Step struct {
	ID          string      `json:"id"`
	Needs       []string    `json:"needs"`
	Instruction string      `json:"instruction"`
	Model       string      `json:"model"`
	ModelConfig ModelConfig `json:"model_config"`
	Tools       []string    `json:"tools"`
	Input       Schema      `json:"input"`
	Bindings    []Binding   `json:"bindings"`
	Output      Schema      `json:"output"`
	Limits      Limits      `json:"limits"`
}

type Binding struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Input   string `json:"input,omitempty"`
	Literal string `json:"literal,omitempty"`
}

type Result struct {
	Step     string   `json:"step"`
	Outcomes []string `json:"outcomes"`
	Schema   Schema   `json:"schema"`
}

type Plan struct {
	json              []byte
	digest            string
	evidenceDirectory string
}

func Compile(cfg *config.Config, workflow *config.Workflow) (*Plan, error) {
	if cfg == nil {
		return nil, ErrNilConfig
	}

	if workflow == nil {
		return nil, ErrNilWorkflow
	}

	if err := config.ValidateWorkflow(workflow); err != nil {
		return nil, fmt.Errorf("validating workflow: %w", err)
	}

	models, err := resolveModels(cfg, workflow)
	if err != nil {
		return nil, err
	}

	if len(workflow.Tools) != 0 {
		return nil, fmt.Errorf("workflow tools: %w", ErrUnsupportedCapability)
	}

	if validationErr := validateModelConfig(workflow.ModelConfig); validationErr != nil {
		return nil, fmt.Errorf("workflow model config: %w", validationErr)
	}

	limits, err := compileLimits(workflow.Limits, Limits{})
	if err != nil {
		return nil, fmt.Errorf("workflow limits: %w", err)
	}

	inputs, err := compileProperties(workflow.Inputs)
	if err != nil {
		return nil, fmt.Errorf("workflow inputs: %w", err)
	}

	steps, err := compileSteps(workflow, inputs, limits)
	if err != nil {
		return nil, err
	}

	result, err := compileResult(workflow, steps)
	if err != nil {
		return nil, err
	}

	projection := Projection{
		Version: Version,
		Models:  models,
		Workflow: Workflow{
			Name:        workflow.Name,
			Description: workflow.Description,
			Model:       workflow.Model,
			ModelConfig: compileModelConfig(workflow.ModelConfig, ModelConfig{}),
			Inputs:      inputs,
			Tools:       []string{},
			Limits:      limits,
			Steps:       steps,
			Result:      result,
		},
	}

	body, err := json.Marshal(projection)
	if err != nil {
		return nil, fmt.Errorf("encoding plan body: %w", err)
	}

	digestBytes := sha256.Sum256(body)
	projection.Digest = hex.EncodeToString(digestBytes[:])

	encoded, err := json.Marshal(projection)
	if err != nil {
		return nil, fmt.Errorf("encoding plan: %w", err)
	}

	return &Plan{json: encoded, digest: projection.Digest, evidenceDirectory: cfg.Evidence.Directory}, nil
}

func (p *Plan) Digest() string {
	return p.digest
}

func (p *Plan) JSON() []byte {
	return slices.Clone(p.json)
}

func (p *Plan) EvidenceDirectory() string {
	return p.evidenceDirectory
}

func (p *Plan) Text() []byte {
	var output bytes.Buffer
	if err := json.Indent(&output, p.json, "", "  "); err != nil {
		panic(fmt.Sprintf("indenting validated plan JSON: %v", err))
	}

	return output.Bytes()
}

func (p *Plan) Snapshot() Projection {
	var projection Projection
	if err := json.Unmarshal(p.json, &projection); err != nil {
		panic(fmt.Sprintf("decoding validated plan JSON: %v", err))
	}

	return projection
}

func resolveModels(cfg *config.Config, workflow *config.Workflow) ([]string, error) {
	models := make([]string, 0, len(workflow.Steps)+1)
	seen := make(map[string]struct{}, len(workflow.Steps)+1)

	aliases := make([]string, 0, len(workflow.Steps)+1)

	aliases = append(aliases, workflow.Model)
	for _, step := range workflow.Steps {
		if step.Model != "" {
			aliases = append(aliases, step.Model)
		}
	}

	for _, alias := range aliases {
		if _, exists := cfg.Models[alias]; !exists {
			return nil, fmt.Errorf("model alias %q: %w", alias, ErrUnknownModel)
		}

		if _, exists := seen[alias]; exists {
			continue
		}

		seen[alias] = struct{}{}
		models = append(models, alias)
	}

	return models, nil
}

func compileSteps(workflow *config.Workflow, workflowInputs []Property, workflowLimits Limits) ([]Step, error) {
	steps := make([]Step, 0, len(workflow.Steps))
	for _, source := range workflow.Steps {
		if len(source.Tools) != 0 || len(source.Workspaces) != 0 {
			return nil, fmt.Errorf("step %q capabilities: %w", source.ID, ErrUnsupportedCapability)
		}

		if err := validateModelConfig(source.ModelConfig); err != nil {
			return nil, fmt.Errorf("step %q model config: %w", source.ID, err)
		}

		input, err := compileSchema(source.Input, 0)
		if err != nil {
			return nil, fmt.Errorf("step %q input: %w", source.ID, err)
		}

		output, err := compileSchema(source.Output, 0)
		if err != nil {
			return nil, fmt.Errorf("step %q output: %w", source.ID, err)
		}

		if validationErr := validateOutcome(output); validationErr != nil {
			return nil, fmt.Errorf("step %q output: %w", source.ID, validationErr)
		}

		bindings, err := compileBindings(source, input, workflowInputs)
		if err != nil {
			return nil, fmt.Errorf("step %q: %w", source.ID, err)
		}

		limits, err := compileLimits(source.Limits, workflowLimits)
		if err != nil {
			return nil, fmt.Errorf("step %q limits: %w", source.ID, err)
		}

		model := source.Model
		if model == "" {
			model = workflow.Model
		}

		steps = append(steps, Step{
			ID:          source.ID,
			Needs:       slices.Clone(source.Needs),
			Instruction: source.Instruction.Text,
			Model:       model,
			ModelConfig: compileModelConfig(source.ModelConfig, compileModelConfig(workflow.ModelConfig, ModelConfig{})),
			Tools:       []string{},
			Input:       input,
			Bindings:    bindings,
			Output:      output,
			Limits:      limits,
		})
	}

	return steps, nil
}

func validateModelConfig(modelConfig config.ModelConfig) error {
	if modelConfig.HasTemperature && (math.IsNaN(modelConfig.Temperature) || math.IsInf(modelConfig.Temperature, 0)) {
		return ErrInvalidLimits
	}

	if modelConfig.HasMaxOutputTokens && modelConfig.MaxOutputTokens <= 0 {
		return ErrInvalidLimits
	}

	return nil
}

func compileModelConfig(source config.ModelConfig, parent ModelConfig) ModelConfig {
	result := parent

	if source.HasTemperature {
		value := source.Temperature
		result.Temperature = &value
	}

	if source.HasMaxOutputTokens {
		value := source.MaxOutputTokens
		result.MaxOutputTokens = &value
	}

	return result
}

func compileLimits(source config.Limits, parent Limits) (Limits, error) {
	if parent.Timeout == "" {
		return compileWorkflowLimits(source)
	}

	return compileStepLimits(source, parent)
}

func compileWorkflowLimits(source config.Limits) (Limits, error) {
	timeout, err := normalizedTimeout(source.Timeout)
	if err != nil {
		return Limits{}, err
	}

	result := Limits{
		Timeout:          timeout,
		MaxIterations:    source.MaxIterations,
		MaxModelCalls:    source.MaxModelCalls,
		MaxToolCalls:     source.MaxToolCalls,
		MaxConcurrency:   source.MaxConcurrency,
		MaxParallelCalls: source.MaxParallelCalls,
		MaxArtifactBytes: source.MaxArtifactBytes,
	}
	if !positive(result.MaxIterations, result.MaxModelCalls, result.MaxConcurrency, result.MaxParallelCalls) || result.MaxToolCalls < 0 || result.MaxArtifactBytes < 0 {
		return Limits{}, ErrInvalidLimits
	}

	return result, nil
}

func compileStepLimits(source config.Limits, parent Limits) (Limits, error) {
	result := parent

	if source.Timeout != "" {
		timeout, err := normalizedTimeout(source.Timeout)
		if err != nil {
			return Limits{}, err
		}

		requested, _ := time.ParseDuration(timeout)

		ceiling, _ := time.ParseDuration(parent.Timeout)
		if requested > ceiling {
			return Limits{}, ErrInvalidLimits
		}

		result.Timeout = timeout
	}

	var err error
	if result.MaxIterations, err = narrowedLimit(source.MaxIterations, parent.MaxIterations); err != nil {
		return Limits{}, err
	}

	if result.MaxModelCalls, err = narrowedLimit(source.MaxModelCalls, parent.MaxModelCalls); err != nil {
		return Limits{}, err
	}

	if result.MaxToolCalls, err = narrowedLimit(source.MaxToolCalls, parent.MaxToolCalls); err != nil {
		return Limits{}, err
	}

	if result.MaxConcurrency, err = narrowedLimit(source.MaxConcurrency, parent.MaxConcurrency); err != nil {
		return Limits{}, err
	}

	if result.MaxParallelCalls, err = narrowedLimit(source.MaxParallelCalls, parent.MaxParallelCalls); err != nil {
		return Limits{}, err
	}

	if result.MaxArtifactBytes, err = narrowedLimit(source.MaxArtifactBytes, parent.MaxArtifactBytes); err != nil {
		return Limits{}, err
	}

	return result, nil
}

func normalizedTimeout(value string) (string, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return "", ErrInvalidLimits
	}

	return duration.String(), nil
}

func narrowedLimit(requested, parent int) (int, error) {
	if requested == 0 {
		return parent, nil
	}

	if requested < 0 || requested > parent {
		return 0, ErrInvalidLimits
	}

	return requested, nil
}

func positive(values ...int) bool {
	for _, value := range values {
		if value <= 0 {
			return false
		}
	}

	return true
}

func compileBindings(step config.Step, input Schema, workflowInputs []Property) ([]Binding, error) {
	if input.Type != TypeObject {
		return nil, ErrInvalidBinding
	}

	properties := propertiesByName(input.Properties)
	workflowProperties := propertiesByName(workflowInputs)
	bindings := make([]Binding, 0, len(step.With))

	for _, property := range input.Properties {
		source, exists := step.With[property.Name]
		if !exists {
			if slices.Contains(input.Required, property.Name) {
				return nil, ErrInvalidBinding
			}

			continue
		}

		switch source.Kind {
		case config.BindingInput:
			workflowProperty, found := workflowProperties[source.Input]
			if !found || !assignable(workflowProperty.Schema, properties[property.Name].Schema) {
				return nil, ErrInvalidBinding
			}

			bindings = append(bindings, Binding{Name: property.Name, Kind: "input", Input: source.Input})
		case config.BindingLiteral:
			if property.Schema.Type != TypeString {
				return nil, ErrInvalidBinding
			}

			bindings = append(bindings, Binding{Name: property.Name, Kind: "literal", Literal: source.Literal})
		default:
			return nil, ErrInvalidBinding
		}
	}

	if len(bindings) != len(step.With) {
		return nil, ErrInvalidBinding
	}

	return bindings, nil
}

func compileResult(workflow *config.Workflow, steps []Step) (Result, error) {
	terminal := make(map[string]bool, len(steps))
	outputs := make(map[string]Schema, len(steps))

	for _, step := range steps {
		terminal[step.ID] = true
		outputs[step.ID] = step.Output
	}

	for _, step := range steps {
		for _, dependency := range step.Needs {
			terminal[dependency] = false
		}
	}

	if !terminal[workflow.Result.Step] {
		return Result{}, ErrInvalidResult
	}

	output, exists := outputs[workflow.Result.Step]
	if !exists {
		return Result{}, ErrInvalidResult
	}

	outcome, exists := propertiesByName(output.Properties)["outcome"]
	if !exists || len(outcome.Schema.Enum) == 0 {
		return Result{}, ErrInvalidResult
	}

	return Result{Step: workflow.Result.Step, Outcomes: slices.Clone(outcome.Schema.Enum), Schema: output}, nil
}
