package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/trust"
)

type trustProjection struct {
	normalizedContext string
	admissionID       string
	policySHA256      string
	controlSHA256     string
	transport         string
	operationSet      string
	decisions         []CapabilityDecision
}

var capabilityOrder = []trust.Capability{
	trust.CapabilityWorkspaceRead,
	trust.CapabilityWorkspaceMutate,
	trust.CapabilityGitRead,
	trust.CapabilityGitMutate,
	trust.CapabilityGitPublish,
	trust.CapabilityProcessExec,
	trust.CapabilityNetworkRead,
	trust.CapabilityNetworkMutate,
	trust.CapabilityGitHubRead,
	trust.CapabilityGitHubMutate,
	trust.CapabilityAgentCall,
}

func compileTrustProjection(cfg *config.Config, toolPolicy compiledToolPolicy, agents []Agent, steps []Step, decision trust.Decision) (trustProjection, error) {
	if cfg.M3 == nil && !decision.Present {
		return trustProjection{}, nil
	}

	selected := selectedCapabilities(toolPolicy.scope, agents, steps)

	operationSet, err := selectedOperationSet(cfg, toolPolicy.scope, agents, steps, decision)
	if err != nil {
		return trustProjection{}, err
	}

	policyCapabilities, contextAllowed, err := admittedPolicy(cfg, decision)
	if err != nil {
		return trustProjection{}, err
	}

	projected := make([]CapabilityDecision, 0, len(capabilityOrder))
	for _, capability := range capabilityOrder {
		eligibility := trust.EligibilityFor(decision.Context, capability)
		_, requested := selected[string(capability)]
		admitted := capabilityAdmitted(cfg, decision, policyCapabilities, contextAllowed, capability, eligibility)

		projected = append(projected, CapabilityDecision{Capability: string(capability), Eligibility: string(eligibility), Admitted: admitted})
		if requested && !admitted {
			return trustProjection{}, fmt.Errorf("context %s denied capability %s: %w", displayContext(decision), capability, ErrTrustDenied)
		}
	}

	result := trustProjection{operationSet: operationSet, decisions: projected}
	if decision.Present {
		result.normalizedContext = string(decision.Context)
		result.admissionID = decision.AdmissionID
		result.controlSHA256 = decision.ControlSHA256
		result.transport = decision.Transport
	}

	if cfg.M3 != nil {
		result.policySHA256 = m3PolicyDigest(cfg, toolPolicy)
	}

	return result, nil
}

func admittedPolicy(cfg *config.Config, decision trust.Decision) (capabilities map[string]struct{}, contextAllowed bool, err error) {
	capabilities = make(map[string]struct{})
	if cfg.M3 == nil {
		return capabilities, false, nil
	}

	if decision.AdmissionID != cfg.M3.Admission.ID {
		return nil, false, fmt.Errorf("admission id mismatch: %w", ErrTrustDenied)
	}

	for _, capability := range cfg.M3.Admission.Capabilities {
		capabilities[capability] = struct{}{}
	}

	return capabilities, slices.Contains(cfg.M3.Admission.Contexts, string(decision.Context)), nil
}

func capabilityAdmitted(cfg *config.Config, decision trust.Decision, policyCapabilities map[string]struct{}, contextAllowed bool, capability trust.Capability, eligibility trust.Eligibility) bool {
	if eligibility == trust.EligibilityReadOnly {
		return true
	}

	if eligibility != trust.EligibilityGrant && eligibility != trust.EligibilityStaged {
		return false
	}

	_, explicitlyAdmitted := policyCapabilities[string(capability)]

	return cfg.M3 != nil && contextAllowed && explicitlyAdmitted && decision.Context != trust.ContextForkedPR && decision.Context != trust.ContextUnknown
}

func selectedOperationSet(cfg *config.Config, workflow ToolScope, agents []Agent, steps []Step, decision trust.Decision) (string, error) {
	all, selected := collectOperationTools(workflow, agents, steps)
	if len(selected) == 0 {
		return "", nil
	}

	if cfg.M3 == nil {
		return "", fmt.Errorf("safe-output requires M3 policy: %w", ErrTrustDenied)
	}

	operationSet, err := classifyOperationSet(all, selected, decision)
	if err != nil {
		return "", err
	}

	if !slices.Contains(cfg.M3.Publication.OperationSets, operationSet) {
		return "", fmt.Errorf("safe-output operation set is not admitted: %w", ErrTrustDenied)
	}

	return operationSet, nil
}

func collectOperationTools(workflow ToolScope, agents []Agent, steps []Step) (all, selected map[string]struct{}) {
	all = make(map[string]struct{})
	selected = make(map[string]struct{})
	add := func(scope ToolScope) {
		for _, name := range scope.Names {
			all[name] = struct{}{}
			if strings.HasPrefix(name, "safe-output.") {
				selected[name] = struct{}{}
			}
		}
	}
	add(workflow)

	for _, agent := range agents {
		add(agent.Tools)
	}

	for _, step := range steps {
		add(step.Tools)
	}

	return all, selected
}

func classifyOperationSet(all, selected map[string]struct{}, decision trust.Decision) (string, error) {
	if len(selected) == 1 && containsTool(selected, "safe-output.conversation-reply") {
		if decision.Origin.Kind != "issue" && decision.Origin.Kind != "pull_request" {
			return "", fmt.Errorf("conversation reply requires a bound subject: %w", ErrTrustDenied)
		}

		if containsTool(all, "files.write") || containsTool(all, "git.write.commit") {
			return "", fmt.Errorf("conversation reply cannot publish authored changes: %w", ErrUnsupportedCapability)
		}

		return "conversation-reply", nil
	}

	if len(selected) == 2 && containsTool(selected, "safe-output.branch") && containsTool(selected, "safe-output.draft-pr") {
		if !containsTool(all, "files.write") || !containsTool(all, "git.write.commit") {
			return "", fmt.Errorf("branch publication requires local authoring: %w", ErrUnsupportedCapability)
		}

		return "branch-pr", nil
	}

	return "", fmt.Errorf("invalid safe-output operation set: %w", ErrUnsupportedCapability)
}

func containsTool(selected map[string]struct{}, name string) bool {
	_, exists := selected[name]
	return exists
}

func selectedCapabilities(workflow ToolScope, agents []Agent, steps []Step) map[string]struct{} {
	selected := make(map[string]struct{})
	add := func(scope ToolScope) {
		for _, definition := range scope.Definitions {
			for _, capability := range definition.Capabilities {
				selected[capability] = struct{}{}
			}
		}
	}
	add(workflow)

	for _, namedAgent := range agents {
		add(namedAgent.Tools)
	}

	for _, step := range steps {
		add(step.Tools)
	}

	return selected
}

func displayContext(decision trust.Decision) trust.Context {
	if decision.Context == "" {
		return trust.ContextUnknown
	}

	return decision.Context
}

func m3PolicyDigest(cfg *config.Config, toolPolicy compiledToolPolicy) string {
	type limit struct {
		Name            string `json:"name"`
		MaxCalls        int    `json:"max_calls"`
		Timeout         string `json:"timeout"`
		MaxRequestBytes int    `json:"max_request_bytes"`
		MaxResultBytes  int    `json:"max_result_bytes"`
	}

	type policy struct {
		M3          *config.M3       `json:"m3"`
		Definitions []ToolDefinition `json:"definitions"`
		Limits      []limit          `json:"limits"`
	}

	m3Names := []string{"files.write", "git.write.commit", "safe-output.branch", "safe-output.conversation-reply", "safe-output.draft-pr"}
	definitions := make([]ToolDefinition, 0, len(m3Names))

	limits := make([]limit, 0, len(m3Names))
	for _, name := range m3Names {
		definition, _ := toolPolicy.catalog.Definition(name)
		definitions = append(definitions, projectDefinition(definition))
		value := toolPolicy.trustedLimits[name]
		limits = append(limits, limit{Name: name, MaxCalls: value.MaxCalls, Timeout: value.Timeout, MaxRequestBytes: value.MaxRequestBytes, MaxResultBytes: value.MaxResultBytes})
	}

	encoded, err := json.Marshal(policy{M3: cfg.M3, Definitions: definitions, Limits: limits})
	if err != nil {
		panic(fmt.Sprintf("encoding normalized m3 policy: %v", err))
	}

	digest := sha256.Sum256(encoded)

	return hex.EncodeToString(digest[:])
}

func validateAuthoringScopes(agents []Agent, steps []Step) error {
	validate := func(workspaces []Workspace, tools ToolScope) error {
		hasWrite := slices.ContainsFunc(workspaces, func(workspace Workspace) bool {
			return workspace.Access == config.WorkspaceAccessWrite
		})

		hasMutation := slices.ContainsFunc(tools.Definitions, func(definition ToolDefinition) bool {
			return slices.Contains(definition.Capabilities, string(trust.CapabilityWorkspaceMutate)) ||
				slices.Contains(definition.Capabilities, string(trust.CapabilityGitMutate))
		})
		if hasWrite != hasMutation {
			return ErrUnsupportedCapability
		}

		return nil
	}

	for _, agent := range agents {
		if err := validate(agent.Workspaces, agent.Tools); err != nil {
			return fmt.Errorf("agent %q authoring scope: %w", agent.Name, err)
		}
	}

	for _, step := range steps {
		if step.Agent == "" {
			if err := validate(step.Workspaces, step.Tools); err != nil {
				return fmt.Errorf("step %q authoring scope: %w", step.ID, err)
			}
		}
	}

	return nil
}

func validateWorkspaceRequests(cfg *config.Config, workflow *config.Workflow) error {
	validate := func(refs []config.WorkspaceRef) error {
		seen := make(map[string]struct{}, len(refs))
		for _, ref := range refs {
			workspace, exists := cfg.Workspaces[ref.Name]
			if !exists || (ref.Access != config.WorkspaceAccessRead && ref.Access != config.WorkspaceAccessWrite) {
				return ErrUnsupportedCapability
			}

			if ref.Access == config.WorkspaceAccessWrite && (workspace.Access != config.WorkspaceAccessWrite || cfg.M3 == nil || cfg.M3.Authoring.Workspace != ref.Name) {
				return ErrUnsupportedCapability
			}

			if _, duplicate := seen[ref.Name]; duplicate {
				return ErrUnsupportedCapability
			}

			seen[ref.Name] = struct{}{}
		}

		return nil
	}
	for _, name := range workflow.AgentOrder {
		if err := validate(workflow.Agents[name].Workspaces); err != nil {
			return fmt.Errorf("agent %q workspaces: %w", name, err)
		}
	}

	for _, step := range workflow.Steps {
		if step.Agent == "" {
			if err := validate(step.Workspaces); err != nil {
				return fmt.Errorf("step %q workspaces: %w", step.ID, err)
			}
		}
	}

	return nil
}
