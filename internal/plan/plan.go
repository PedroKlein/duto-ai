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
	"github.com/PedroKlein/duto-ai/internal/prompt"
	dtool "github.com/PedroKlein/duto-ai/internal/tool"
	"github.com/PedroKlein/duto-ai/internal/trust"
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
	ErrInvalidAgent          = errors.New("invalid named agent")
	ErrTrustDenied           = errors.New("trust capability denied")
)

type Projection struct {
	Version  int      `json:"version"`
	Digest   string   `json:"digest,omitempty"`
	Models   []string `json:"models"`
	Workflow Workflow `json:"workflow"`
}

type Workflow struct {
	Name                string               `json:"name"`
	Description         string               `json:"description,omitempty"`
	Model               string               `json:"model"`
	ModelConfig         ModelConfig          `json:"model_config"`
	Inputs              []Property           `json:"inputs"`
	Tools               ToolScope            `json:"tools"`
	CatalogDigest       string               `json:"catalog_digest"`
	NormalizedContext   string               `json:"normalized_context,omitempty"`
	AdmissionID         string               `json:"admission_id,omitempty"`
	PolicySHA256        string               `json:"policy_sha256,omitempty"`
	ControlSHA256       string               `json:"control_sha256,omitempty"`
	Transport           string               `json:"transport,omitempty"`
	CapabilityDecisions []CapabilityDecision `json:"capability_decisions,omitempty"`
	Limits              Limits               `json:"limits"`
	Skills              []prompt.FrozenSkill `json:"skills"`
	Agents              []Agent              `json:"agents"`
	Steps               []Step               `json:"steps"`
	Result              Result               `json:"result"`
}

type CapabilityDecision struct {
	Capability  string `json:"capability"`
	Eligibility string `json:"eligibility"`
	Admitted    bool   `json:"admitted"`
}

type ModelConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxOutputTokens *int     `json:"max_output_tokens,omitempty"`
}

type Retry struct {
	MaxAttempts  int    `json:"max_attempts"`
	InitialDelay string `json:"initial_delay,omitempty"`
	MaxDelay     string `json:"max_delay,omitempty"`
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

type Agent struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Mode        string        `json:"mode"`
	Model       string        `json:"model"`
	ModelConfig ModelConfig   `json:"model_config"`
	Instruction prompt.Frozen `json:"instruction"`
	Tools       ToolScope     `json:"tools"`
	Skills      []string      `json:"skills"`
	Workspaces  []Workspace   `json:"workspaces"`
	Context     AgentContext  `json:"context"`
	Input       Schema        `json:"input"`
	Output      Schema        `json:"output"`
	Limits      Limits        `json:"limits"`
	Subagents   []string      `json:"subagents"`
}

type AgentContext struct {
	Mode     string          `json:"mode"`
	MaxBytes int             `json:"max_bytes,omitempty"`
	Include  []ContextSource `json:"include,omitempty"`
}

type ContextSource struct {
	Kind      string `json:"kind"`
	Input     string `json:"input,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	File      string `json:"file,omitempty"`
	Content   string `json:"content,omitempty"`
	Digest    string `json:"digest"`
	MaxBytes  int    `json:"max_bytes"`
}

type Step struct {
	ID          string        `json:"id"`
	Agent       string        `json:"agent,omitempty"`
	Needs       []string      `json:"needs"`
	When        []Condition   `json:"when,omitempty"`
	Instruction prompt.Frozen `json:"instruction"`
	Model       string        `json:"model"`
	ModelConfig ModelConfig   `json:"model_config"`
	Tools       ToolScope     `json:"tools"`
	Skills      []string      `json:"skills"`
	Workspaces  []Workspace   `json:"workspaces"`
	Input       Schema        `json:"input"`
	Bindings    []Binding     `json:"bindings"`
	Output      Schema        `json:"output"`
	Retry       Retry         `json:"retry"`
	Limits      Limits        `json:"limits"`
}

type Workspace struct {
	Name   string `json:"name"`
	Access string `json:"access"`
}

type ToolExpression struct {
	From           string   `json:"from"`
	AddProfiles    []string `json:"add_profiles"`
	Add            []string `json:"add"`
	RemoveProfiles []string `json:"remove_profiles"`
	Remove         []string `json:"remove"`
}

type ToolProfileExpansion struct {
	Operation string   `json:"operation"`
	Name      string   `json:"name"`
	Tools     []string `json:"tools"`
}

type ToolDefinition struct {
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	SideEffect   string   `json:"side_effect"`
}

type ToolLimit struct {
	Name            string `json:"name"`
	MaxCalls        int    `json:"max_calls"`
	Timeout         string `json:"timeout"`
	MaxRequestBytes int    `json:"max_request_bytes"`
	MaxResultBytes  int    `json:"max_result_bytes"`
}

type ToolScope struct {
	Source      ToolExpression         `json:"source"`
	Profiles    []ToolProfileExpansion `json:"profiles"`
	Names       []string               `json:"names"`
	Definitions []ToolDefinition       `json:"definitions"`
	Ceiling     []string               `json:"ceiling"`
	Parent      []string               `json:"parent"`
	Limits      []ToolLimit            `json:"limits"`
}

type Binding struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Input    string   `json:"input,omitempty"`
	Step     string   `json:"step,omitempty"`
	Path     []string `json:"path,omitempty"`
	Literal  any      `json:"literal,omitempty"`
	Optional bool     `json:"optional,omitempty"`
}

type Condition struct {
	Step     string   `json:"step"`
	Outcomes []string `json:"outcomes"`
}

type ResultRoute struct {
	Step    string `json:"step"`
	Outcome string `json:"outcome"`
	Schema  Schema `json:"schema"`
}

type terminalPair struct {
	step    string
	outcome string
}

type Result struct {
	Step     string        `json:"step,omitempty"`
	Outcomes []string      `json:"outcomes,omitempty"`
	Routes   []ResultRoute `json:"routes,omitempty"`
	Schema   Schema        `json:"schema"`
}

type Authoring struct {
	Root                  string
	AllowedPaths          []string
	MaxChangedFiles       int
	MaxFileBytes          int
	MaxTotalWriteBytes    int
	MaxCommitMessageBytes int
	CommitAuthorName      string
	CommitAuthorEmail     string
	BaseRef               string
	BaseSHA               string
}

type Plan struct {
	json              []byte
	digest            string
	evidenceDirectory string
	authoring         *Authoring
}

func Compile(cfg *config.Config, workflow *config.Workflow) (*Plan, error) {
	return compile(cfg, workflow, trust.Decision{})
}

func CompileWithTrust(cfg *config.Config, workflow *config.Workflow, decision trust.Decision) (*Plan, error) {
	return compile(cfg, workflow, decision)
}

func compile(cfg *config.Config, workflow *config.Workflow, decision trust.Decision) (*Plan, error) { //nolint:gocyclo // Admission follows the fixed contract order before construction.
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

	if validationErr := validateModelConfig(workflow.ModelConfig); validationErr != nil {
		return nil, fmt.Errorf("workflow model config: %w", validationErr)
	}

	limits, err := compileLimits(workflow.Limits, Limits{})
	if err != nil {
		return nil, fmt.Errorf("workflow limits: %w", err)
	}

	toolPolicy, err := compileWorkflowToolPolicy(cfg, workflow, limits)
	if err != nil {
		return nil, err
	}

	inputs, err := compileProperties(workflow.Inputs)
	if err != nil {
		return nil, fmt.Errorf("workflow inputs: %w", err)
	}

	workspaceRoots, frozenSkills, err := compilePromptResources(cfg, workflow)
	if err != nil {
		return nil, err
	}

	if workspaceErr := validateWorkspaceRequests(cfg, workflow); workspaceErr != nil {
		return nil, workspaceErr
	}

	agents, err := compileAgents(workflow, inputs, limits, workspaceRoots, frozenSkills, toolPolicy)
	if err != nil {
		return nil, err
	}

	steps, err := compileSteps(workflow, inputs, limits, workspaceRoots, frozenSkills, toolPolicy.catalog, toolPolicy.profiles, toolPolicy.ceiling, toolPolicy.scope, agents)
	if err != nil {
		return nil, err
	}

	if agentUseErr := validateAgentUses(agents, steps); agentUseErr != nil {
		return nil, agentUseErr
	}

	if authoringErr := validateAuthoringScopes(agents, steps); authoringErr != nil {
		return nil, authoringErr
	}

	if concurrencyErr := validateToolConcurrency(steps); concurrencyErr != nil {
		return nil, fmt.Errorf("tool concurrency: %w", concurrencyErr)
	}

	result, err := compileResult(workflow, steps)
	if err != nil {
		return nil, err
	}

	trustProjection, err := compileTrustProjection(cfg, toolPolicy, agents, steps, decision)
	if err != nil {
		return nil, err
	}

	projection := Projection{
		Version: Version,
		Models:  models,
		Workflow: Workflow{
			Name:                workflow.Name,
			Description:         workflow.Description,
			Model:               workflow.Model,
			ModelConfig:         compileModelConfig(workflow.ModelConfig, ModelConfig{}),
			Inputs:              inputs,
			Tools:               toolPolicy.scope,
			CatalogDigest:       catalogDigest(toolPolicy.catalog),
			NormalizedContext:   trustProjection.normalizedContext,
			AdmissionID:         trustProjection.admissionID,
			PolicySHA256:        trustProjection.policySHA256,
			ControlSHA256:       trustProjection.controlSHA256,
			Transport:           trustProjection.transport,
			CapabilityDecisions: trustProjection.decisions,
			Limits:              limits,
			Skills:              frozenSkills,
			Agents:              agents,
			Steps:               steps,
			Result:              result,
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

	return &Plan{
		json:              encoded,
		digest:            projection.Digest,
		evidenceDirectory: cfg.Evidence.Directory,
		authoring:         compileAuthoring(cfg, decision),
	}, nil
}

func compilePromptResources(cfg *config.Config, workflow *config.Workflow) (map[string]string, []prompt.FrozenSkill, error) {
	workspaceRoots := make(map[string]string, len(cfg.Workspaces))
	for name, workspace := range cfg.Workspaces {
		if workspace.Access == config.WorkspaceAccessRead || workspace.Access == config.WorkspaceAccessWrite {
			workspaceRoots[name] = workspace.Root
		}
	}

	skillRequests := make(map[string]prompt.SkillRequest, len(workflow.Skills))
	for name, source := range workflow.Skills {
		skillRequests[name] = prompt.SkillRequest{Workspace: source.Workspace, Path: source.Path}
	}

	frozenSkills, err := prompt.FreezeSkills(skillRequests, workspaceRoots)
	if err != nil {
		return nil, nil, fmt.Errorf("freezing skills: %w", err)
	}

	return workspaceRoots, frozenSkills, nil
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

func (p *Plan) Authoring() *Authoring {
	if p == nil || p.authoring == nil {
		return nil
	}

	value := *p.authoring
	value.AllowedPaths = slices.Clone(p.authoring.AllowedPaths)

	return &value
}

func compileAuthoring(cfg *config.Config, decision trust.Decision) *Authoring {
	if cfg.M3 == nil {
		return nil
	}

	workspace := cfg.Workspaces[cfg.M3.Authoring.Workspace]

	return &Authoring{
		Root:                  workspace.Root,
		AllowedPaths:          slices.Clone(cfg.M3.Authoring.AllowedPaths),
		MaxChangedFiles:       cfg.M3.Authoring.MaxChangedFiles,
		MaxFileBytes:          cfg.M3.Authoring.MaxFileBytes,
		MaxTotalWriteBytes:    cfg.M3.Authoring.MaxTotalWriteBytes,
		MaxCommitMessageBytes: cfg.M3.Authoring.MaxCommitMessageBytes,
		CommitAuthorName:      cfg.M3.Authoring.CommitAuthorName,
		CommitAuthorEmail:     cfg.M3.Authoring.CommitAuthorEmail,
		BaseRef:               decision.CheckoutRef,
		BaseSHA:               decision.CheckoutSHA,
	}
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
	models := make([]string, 0, len(workflow.Steps)+len(workflow.Agents)+1)
	seen := make(map[string]struct{}, len(workflow.Steps)+len(workflow.Agents)+1)

	aliases := make([]string, 0, len(workflow.Steps)+len(workflow.Agents)+1)

	aliases = append(aliases, workflow.Model)
	for _, name := range workflow.AgentOrder {
		aliases = append(aliases, workflow.Agents[name].Model)
	}

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

func compileSteps(workflow *config.Workflow, workflowInputs []Property, workflowLimits Limits, workspaceRoots map[string]string, skills []prompt.FrozenSkill, catalog dtool.Catalog, profiles map[string][]string, ceiling []string, workflowTools ToolScope, agents []Agent) ([]Step, error) { //nolint:gocyclo,gocognit // Compilation follows the fixed admission sequence.
	steps := make([]Step, 0, len(workflow.Steps))
	agentByName := agentsByName(agents)

	for _, source := range workflow.Steps {
		if source.Agent != "" {
			step, err := compileNamedAgentStep(source, workflow.Steps, workflowInputs, agentByName)
			if err != nil {
				return nil, fmt.Errorf("step %q: %w", source.ID, err)
			}

			steps = append(steps, step)

			continue
		}

		if len(source.Needs) > 1 && source.Wait != "all_succeeded" {
			return nil, fmt.Errorf("step %q fan-in: %w", source.ID, ErrInvalidBinding)
		}

		workspaces, err := compileWorkspaces(source.Workspaces, workspaceRoots)
		if err != nil {
			return nil, fmt.Errorf("step %q workspaces: %w", source.ID, err)
		}

		instruction, err := compileInstruction(source.Instruction, workspaces, workspaceRoots)
		if err != nil {
			return nil, fmt.Errorf("step %q instruction: %w", source.ID, err)
		}

		selectedSkills, err := compileSelectedSkills(source.Skills, workspaces, skills)
		if err != nil {
			return nil, fmt.Errorf("step %q skills: %w", source.ID, err)
		}

		if validationErr := validateModelConfig(source.ModelConfig); validationErr != nil {
			return nil, fmt.Errorf("step %q model config: %w", source.ID, validationErr)
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

		bindings, err := compileBindings(source, input, workflowInputs, workflow.Steps)
		if err != nil {
			return nil, fmt.Errorf("step %q: %w", source.ID, err)
		}

		limits, err := compileLimits(source.Limits, workflowLimits)
		if err != nil {
			return nil, fmt.Errorf("step %q limits: %w", source.ID, err)
		}

		toolLimits := toolLimitMap(workflowTools.Limits)

		tools, err := compileToolScope(catalog, source.Tools, profiles, ceiling, workflowTools.Names, false, source.ToolLimits, toolLimits, limits)
		if err != nil {
			return nil, fmt.Errorf("step %q tools: %w", source.ID, err)
		}

		model := source.Model
		if model == "" {
			model = workflow.Model
		}

		conditions, err := compileConditions(source, workflow.Steps)
		if err != nil {
			return nil, fmt.Errorf("step %q conditions: %w", source.ID, err)
		}

		retry, err := compileRetry(source.Retry, tools)
		if err != nil {
			return nil, fmt.Errorf("step %q retry: %w", source.ID, err)
		}

		steps = append(steps, Step{
			ID:          source.ID,
			Needs:       slices.Clone(source.Needs),
			When:        conditions,
			Instruction: instruction,
			Model:       model,
			ModelConfig: compileModelConfig(source.ModelConfig, compileModelConfig(workflow.ModelConfig, ModelConfig{})),
			Tools:       tools,
			Skills:      selectedSkills,
			Workspaces:  workspaces,
			Input:       input,
			Bindings:    bindings,
			Output:      output,
			Retry:       retry,
			Limits:      limits,
		})
	}

	return steps, nil
}

func compileWorkspaces(source []config.WorkspaceRef, roots map[string]string) ([]Workspace, error) {
	result := make([]Workspace, 0, len(source))

	seen := make(map[string]struct{}, len(source))
	for _, workspace := range source {
		if (workspace.Access != config.WorkspaceAccessRead && workspace.Access != config.WorkspaceAccessWrite) || roots[workspace.Name] == "" {
			return nil, ErrUnsupportedCapability
		}

		if _, exists := seen[workspace.Name]; exists {
			return nil, ErrUnsupportedCapability
		}

		seen[workspace.Name] = struct{}{}
		result = append(result, Workspace{Name: workspace.Name, Access: workspace.Access})
	}

	return result, nil
}

func compileInstruction(source config.Instruction, workspaces []Workspace, roots map[string]string) (prompt.Frozen, error) {
	request := prompt.Source{}

	switch source.Kind {
	case config.InstructionText:
		request.Kind = prompt.KindText
		request.Text = source.Text
	case config.InstructionFile:
		request.Kind = prompt.KindFile
		request.File = prompt.FileSource{Workspace: source.File.Workspace, Path: source.File.Path, MaxBytes: source.File.MaxBytes}
	case config.InstructionTemplate:
		request.Kind = prompt.KindTemplate
		request.Text = source.Text
		request.MaxOutputBytes = source.MaxOutputBytes
	case config.InstructionTemplateFile:
		request.Kind = prompt.KindTemplateFile
		request.File = prompt.FileSource{Workspace: source.File.Workspace, Path: source.File.Path, MaxBytes: source.File.MaxBytes}
		request.MaxOutputBytes = source.MaxOutputBytes
	default:
		return prompt.Frozen{}, prompt.ErrInvalidSource
	}

	if request.File.Workspace != "" && !containsWorkspace(workspaces, request.File.Workspace) {
		return prompt.Frozen{}, ErrUnsupportedCapability
	}

	frozen, err := prompt.Admit(request, roots)
	if err != nil {
		return prompt.Frozen{}, fmt.Errorf("admitting instruction: %w", err)
	}

	return frozen, nil
}

func compileSelectedSkills(selected []string, workspaces []Workspace, skills []prompt.FrozenSkill) ([]string, error) {
	available := make(map[string]prompt.FrozenSkill, len(skills))
	for _, skill := range skills {
		available[skill.Name] = skill
	}

	result := make([]string, 0, len(selected))

	seen := make(map[string]struct{}, len(selected))
	for _, name := range selected {
		skill, exists := available[name]
		if !exists || !containsWorkspace(workspaces, skill.Workspace) {
			return nil, ErrUnsupportedCapability
		}

		if _, duplicate := seen[name]; duplicate {
			return nil, ErrUnsupportedCapability
		}

		seen[name] = struct{}{}
		result = append(result, name)
	}

	return result, nil
}

func containsWorkspace(workspaces []Workspace, name string) bool {
	for _, workspace := range workspaces {
		if workspace.Name == name {
			return true
		}
	}

	return false
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

func compileRetry(source config.Retry, tools ToolScope) (Retry, error) {
	if source.MaxAttempts == 0 {
		return Retry{MaxAttempts: 1}, nil
	}

	if source.MaxAttempts < 1 || source.InitialDelay == "" || source.MaxDelay == "" {
		return Retry{}, ErrInvalidLimits
	}

	initialDelay, err := normalizedTimeout(source.InitialDelay)
	if err != nil {
		return Retry{}, err
	}

	maxDelay, err := normalizedTimeout(source.MaxDelay)
	if err != nil {
		return Retry{}, err
	}

	initialDuration, _ := time.ParseDuration(initialDelay)

	maxDuration, _ := time.ParseDuration(maxDelay)
	if maxDuration < initialDuration || source.MaxAttempts > 1 && unsafeToolScope(tools) {
		return Retry{}, ErrInvalidLimits
	}

	return Retry{MaxAttempts: source.MaxAttempts, InitialDelay: initialDelay, MaxDelay: maxDelay}, nil
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

func compileBindings(step config.Step, input Schema, workflowInputs []Property, allSteps []config.Step) ([]Binding, error) { //nolint:gocyclo,gocognit // The closed three-source union is validated in one pass.
	if input.Type != TypeObject {
		return nil, ErrInvalidBinding
	}

	properties := propertiesByName(input.Properties)
	workflowProperties := propertiesByName(workflowInputs)
	bindings := make([]Binding, 0, len(step.With))

	for _, name := range step.WithOrder {
		property, targetExists := properties[name]
		if !targetExists {
			return nil, ErrInvalidBinding
		}

		source, exists := step.With[name]
		if !exists {
			return nil, ErrInvalidBinding
		}

		target := property.Schema

		switch source.Kind {
		case config.BindingInput:
			workflowProperty, found := workflowProperties[source.Input]
			if !found || !assignable(workflowProperty.Schema, target) {
				return nil, ErrInvalidBinding
			}

			bindings = append(bindings, Binding{Name: name, Kind: "input", Input: source.Input})
		case config.BindingOutput:
			sourceSchema, required, found := outputPathSchema(source.Output, allSteps)
			if !found || !isAncestor(source.Output.Step, step.ID, allSteps) || !assignable(sourceSchema, target) {
				return nil, ErrInvalidBinding
			}

			if source.Optional {
				if slices.Contains(input.Required, name) {
					return nil, ErrInvalidBinding
				}
			} else if !required {
				return nil, ErrInvalidBinding
			}

			bindings = append(bindings, Binding{Name: name, Kind: "output", Step: source.Output.Step, Path: slices.Clone(source.Output.Path), Optional: source.Optional})
		case config.BindingLiteral:
			if !literalAssignable(source.Literal, target) {
				return nil, ErrInvalidBinding
			}

			bindings = append(bindings, Binding{Name: name, Kind: "literal", Literal: source.Literal})
		default:
			return nil, ErrInvalidBinding
		}
	}

	if len(bindings) != len(step.With) {
		return nil, ErrInvalidBinding
	}

	for _, property := range input.Required {
		if _, exists := step.With[property]; !exists {
			return nil, ErrInvalidBinding
		}
	}

	return bindings, nil
}

func compileConditions(step config.Step, allSteps []config.Step) ([]Condition, error) {
	needs := make(map[string]struct{}, len(step.Needs))
	for _, need := range step.Needs {
		needs[need] = struct{}{}
	}

	conditions := make([]Condition, 0, len(step.When))

	seen := make(map[string]struct{}, len(step.When))
	for _, source := range step.When {
		if _, ok := needs[source.Step]; !ok {
			return nil, ErrInvalidBinding
		}

		if _, duplicate := seen[source.Step]; duplicate {
			return nil, ErrInvalidBinding
		}

		output, ok := stepOutput(source.Step, allSteps)
		if !ok {
			return nil, ErrInvalidBinding
		}

		outcome := propertiesByName(output.Properties)["outcome"].Schema.Enum
		for _, value := range source.Outcomes {
			if !slices.Contains(outcome, value) {
				return nil, ErrInvalidBinding
			}
		}

		seen[source.Step] = struct{}{}
		conditions = append(conditions, Condition{Step: source.Step, Outcomes: slices.Clone(source.Outcomes)})
	}

	return conditions, nil
}

func outputPathSchema(ref config.OutputRef, allSteps []config.Step) (Schema, bool, bool) {
	output, ok := stepOutput(ref.Step, allSteps)
	if !ok {
		return Schema{}, false, false
	}

	current := output
	required := true

	for _, part := range ref.Path {
		if current.Type != TypeObject {
			return Schema{}, false, false
		}

		property, exists := propertiesByName(current.Properties)[part]
		if !exists {
			return Schema{}, false, false
		}

		required = required && slices.Contains(current.Required, part)
		current = property.Schema
	}

	return current, required, true
}

func stepOutput(id string, allSteps []config.Step) (Schema, bool) {
	for _, step := range allSteps {
		if step.ID != id {
			continue
		}

		output, err := compileSchema(step.Output, 0)

		return output, err == nil
	}

	return Schema{}, false
}

func isAncestor(candidate, stepID string, allSteps []config.Step) bool {
	needs := make(map[string][]string, len(allSteps))
	for _, step := range allSteps {
		needs[step.ID] = step.Needs
	}

	seen := map[string]bool{}

	var visit func(string) bool

	visit = func(id string) bool {
		if seen[id] {
			return false
		}

		seen[id] = true
		for _, need := range needs[id] {
			if need == candidate || visit(need) {
				return true
			}
		}

		return false
	}

	return visit(stepID)
}

func literalAssignable(value any, schema Schema) bool {
	switch typed := value.(type) {
	case string:
		return schema.Type == TypeString && (schema.MaxLength == 0 || len([]rune(typed)) <= schema.MaxLength) && (len(schema.Enum) == 0 || slices.Contains(schema.Enum, typed))
	case bool:
		return schema.Type == TypeBoolean
	case int:
		return (schema.Type == TypeInteger || schema.Type == TypeNumber) && withinNumericBounds(float64(typed), schema)
	case float64:
		return schema.Type == TypeNumber && withinNumericBounds(typed, schema)
	default:
		return false
	}
}

func withinNumericBounds(value float64, schema Schema) bool {
	return (schema.Minimum == nil || value >= *schema.Minimum) && (schema.Maximum == nil || value <= *schema.Maximum)
}

func compileResult(workflow *config.Workflow, steps []Step) (Result, error) {
	outputs := make(map[string]Schema, len(steps))
	for _, step := range steps {
		outputs[step.ID] = step.Output
	}

	terminals := make(map[terminalPair]Schema)

	for _, step := range steps {
		outcomes := propertiesByName(step.Output.Properties)["outcome"].Schema.Enum
		for _, outcome := range outcomes {
			if !outcomeConsumed(step.ID, outcome, steps) {
				terminals[terminalPair{step.ID, outcome}] = step.Output
			}
		}
	}

	if len(terminals) == 0 || terminalPairsCanOverlap(terminals, steps) {
		return Result{}, ErrInvalidResult
	}

	if workflow.Result.Step != "" {
		return compileDirectResult(workflow.Result.Step, outputs, terminals)
	}

	return compileRoutedResult(workflow.Result.Routes, terminals)
}

func compileDirectResult(step string, outputs map[string]Schema, terminals map[terminalPair]Schema) (Result, error) {
	output, exists := outputs[step]
	if !exists {
		return Result{}, ErrInvalidResult
	}

	outcomes := propertiesByName(output.Properties)["outcome"].Schema.Enum
	if len(terminals) != len(outcomes) {
		return Result{}, ErrInvalidResult
	}

	for _, outcome := range outcomes {
		if _, ok := terminals[terminalPair{step, outcome}]; !ok {
			return Result{}, ErrInvalidResult
		}
	}

	return Result{Step: step, Outcomes: slices.Clone(outcomes), Schema: output}, nil
}

func compileRoutedResult(source []config.ResultRoute, terminals map[terminalPair]Schema) (Result, error) {
	routes := make([]ResultRoute, 0, len(source))
	covered := make(map[terminalPair]struct{}, len(source))

	var resultSchema Schema

	outcomeSchemas := make(map[string]Schema)

	for _, route := range source {
		key := terminalPair{route.Step, route.When.Outcome}

		output, exists := terminals[key]
		if route.Step != route.When.Step || !exists {
			return Result{}, ErrInvalidResult
		}

		if _, duplicate := covered[key]; duplicate {
			return Result{}, ErrInvalidResult
		}

		if prior, duplicateOutcome := outcomeSchemas[route.When.Outcome]; duplicateOutcome && !schemasEqual(prior, output) {
			return Result{}, ErrInvalidResult
		}

		covered[key] = struct{}{}

		outcomeSchemas[route.When.Outcome] = output
		if len(routes) == 0 {
			resultSchema = output
		}

		routes = append(routes, ResultRoute{Step: route.Step, Outcome: route.When.Outcome, Schema: output})
	}

	if len(covered) != len(terminals) {
		return Result{}, ErrInvalidResult
	}

	return Result{Routes: routes, Schema: resultSchema}, nil
}

func terminalPairsCanOverlap(terminals map[terminalPair]Schema, steps []Step) bool {
	keys := make([]terminalPair, 0, len(terminals))
	for key := range terminals {
		keys = append(keys, key)
	}

	for i := range keys {
		left := terminalConstraints(keys[i].step, keys[i].outcome, steps)
		for j := i + 1; j < len(keys); j++ {
			right := terminalConstraints(keys[j].step, keys[j].outcome, steps)
			if !constraintsConflict(left, right) {
				return true
			}
		}
	}

	return false
}

func terminalConstraints(stepID, outcome string, steps []Step) map[string][]string {
	constraints := map[string][]string{stepID: {outcome}}
	seen := map[string]bool{}

	var visit func(string)

	visit = func(id string) {
		if seen[id] {
			return
		}

		seen[id] = true
		for _, step := range steps {
			if step.ID != id {
				continue
			}

			for _, condition := range step.When {
				constraints[condition.Step] = slices.Clone(condition.Outcomes)
			}

			for _, need := range step.Needs {
				visit(need)
			}

			return
		}
	}
	visit(stepID)

	return constraints
}

func constraintsConflict(left, right map[string][]string) bool {
	for step, leftValues := range left {
		rightValues, ok := right[step]
		if !ok {
			continue
		}

		for _, value := range leftValues {
			if slices.Contains(rightValues, value) {
				return false
			}
		}

		return true
	}

	return false
}

func outcomeConsumed(stepID, outcome string, steps []Step) bool {
	for _, successor := range steps {
		if !slices.Contains(successor.Needs, stepID) {
			continue
		}

		conditioned := false

		for _, condition := range successor.When {
			if condition.Step == stepID {
				conditioned = true

				if slices.Contains(condition.Outcomes, outcome) {
					return true
				}
			}
		}

		if !conditioned {
			return true
		}
	}

	return false
}

func schemasEqual(left, right Schema) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)

	return bytes.Equal(leftJSON, rightJSON)
}
