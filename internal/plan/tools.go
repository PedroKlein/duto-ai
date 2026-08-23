package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/PedroKlein/duto-ai/internal/config"
	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

type compiledToolPolicy struct {
	catalog  dtool.Catalog
	profiles map[string][]string
	ceiling  []string
	scope    ToolScope
}

func compileWorkflowToolPolicy(cfg *config.Config, workflow *config.Workflow, limits Limits) (compiledToolPolicy, error) {
	catalog := dtool.BuiltinCatalog()

	profiles, err := dtool.MergeProfiles(cfg.ToolProfiles, workflow.ToolProfiles)
	if err != nil {
		return compiledToolPolicy{}, fmt.Errorf("merging tool profiles: %w", err)
	}

	ceiling, err := catalog.ResolveCeiling(cfg.Tools)
	if err != nil {
		return compiledToolPolicy{}, fmt.Errorf("resolving trusted tool ceiling: %w", err)
	}

	trustedLimits, err := compileTrustedToolLimits(catalog, cfg.ToolLimits)
	if err != nil {
		return compiledToolPolicy{}, err
	}

	scope, err := compileToolScope(catalog, workflow.Tools, profiles, ceiling, nil, true, workflow.ToolLimits, trustedLimits, limits)
	if err != nil {
		return compiledToolPolicy{}, fmt.Errorf("workflow tools: %w", err)
	}

	if err := validateToolBindings(cfg, scope.Names); err != nil {
		return compiledToolPolicy{}, fmt.Errorf("trusted tool bindings: %w", err)
	}

	return compiledToolPolicy{catalog: catalog, profiles: profiles, ceiling: ceiling, scope: scope}, nil
}

func validateToolBindings(cfg *config.Config, names []string) error {
	selected := make(map[string]bool)

	for _, name := range names {
		family, _, _ := strings.Cut(name, ".")
		selected[family] = true
	}

	if selected["files"] && (cfg.ToolConfig.Files == nil || !readWorkspaceExists(cfg, cfg.ToolConfig.Files.Workspace)) {
		return ErrUnsupportedCapability
	}

	if selected["git"] && (cfg.ToolConfig.Git == nil || !readWorkspaceExists(cfg, cfg.ToolConfig.Git.Workspace)) {
		return ErrUnsupportedCapability
	}

	if selected["github"] && cfg.ToolConfig.GitHub == nil {
		return ErrUnsupportedCapability
	}

	if selected["web"] && cfg.ToolConfig.Web == nil {
		return ErrUnsupportedCapability
	}

	if selected["shell"] && (cfg.ToolConfig.Shell == nil || !readWorkspaceExists(cfg, cfg.ToolConfig.Shell.Workspace)) {
		return ErrUnsupportedCapability
	}

	return nil
}

func readWorkspaceExists(cfg *config.Config, name string) bool {
	workspace, exists := cfg.Workspaces[name]
	return exists && workspace.Access == "read" && workspace.Root != ""
}

func compileTrustedToolLimits(catalog dtool.Catalog, source map[string]config.ToolLimit) (map[string]ToolLimit, error) {
	limits := make(map[string]ToolLimit, len(source))
	for name, value := range source {
		if _, exists := catalog.Definition(name); !exists {
			return nil, &dtool.PolicyError{Code: dtool.CodeUnknownTool, Name: name}
		}

		timeout, err := normalizedTimeout(value.Timeout)
		if err != nil || value.MaxCalls <= 0 || value.MaxRequestBytes < 0 || value.MaxResultBytes <= 0 {
			return nil, fmt.Errorf("trusted tool %q limits: %w", name, ErrInvalidLimits)
		}

		limits[name] = ToolLimit{
			Name:            name,
			MaxCalls:        value.MaxCalls,
			Timeout:         timeout,
			MaxRequestBytes: value.MaxRequestBytes,
			MaxResultBytes:  value.MaxResultBytes,
		}
	}

	return limits, nil
}

func compileToolScope(catalog dtool.Catalog, source config.ToolExpression, profiles map[string][]string, ceiling, parent []string, root bool, requested map[string]config.ToolLimit, parentLimits map[string]ToolLimit, scopeLimits Limits) (ToolScope, error) {
	resolved, err := catalog.ResolveScope(dtool.ScopeRequest{
		Expression: dtool.Expression{
			From:           source.From,
			AddProfiles:    source.AddProfiles,
			Add:            source.Add,
			RemoveProfiles: source.RemoveProfiles,
			Remove:         source.Remove,
		},
		Profiles: profiles,
		Ceiling:  ceiling,
		Parent:   parent,
		Root:     root,
	})
	if err != nil {
		return ToolScope{}, fmt.Errorf("resolving exact tool scope: %w", err)
	}

	if len(resolved.Names) > 0 && scopeLimits.MaxToolCalls <= 0 {
		return ToolScope{}, ErrInvalidLimits
	}

	selected := make(map[string]struct{}, len(resolved.Names))
	for _, name := range resolved.Names {
		selected[name] = struct{}{}
	}

	for name := range requested {
		if _, exists := catalog.Definition(name); !exists {
			return ToolScope{}, &dtool.PolicyError{Code: dtool.CodeUnknownTool, Name: name}
		}

		if _, exists := selected[name]; !exists {
			return ToolScope{}, ErrInvalidLimits
		}
	}

	limits := make([]ToolLimit, 0, len(resolved.Names))

	definitions := make([]ToolDefinition, 0, len(resolved.Names))
	for _, name := range resolved.Names {
		parentLimit, exists := parentLimits[name]
		if !exists {
			return ToolScope{}, ErrInvalidLimits
		}

		limit, err := narrowToolLimit(parentLimit, requested[name])
		if err != nil {
			return ToolScope{}, fmt.Errorf("tool %q limits: %w", name, err)
		}

		limits = append(limits, limit)
		definition, _ := catalog.Definition(name)
		definitions = append(definitions, projectDefinition(definition))
	}

	expansions := make([]ToolProfileExpansion, len(resolved.Profiles))
	for i, expansion := range resolved.Profiles {
		expansions[i] = ToolProfileExpansion{Operation: expansion.Operation, Name: expansion.Name, Tools: slices.Clone(expansion.Tools)}
	}

	return ToolScope{
		Source: ToolExpression{
			From:           resolved.Expression.From,
			AddProfiles:    slices.Clone(resolved.Expression.AddProfiles),
			Add:            slices.Clone(resolved.Expression.Add),
			RemoveProfiles: slices.Clone(resolved.Expression.RemoveProfiles),
			Remove:         slices.Clone(resolved.Expression.Remove),
		},
		Profiles:    expansions,
		Names:       slices.Clone(resolved.Names),
		Definitions: definitions,
		Ceiling:     slices.Clone(ceiling),
		Parent:      slices.Clone(parent),
		Limits:      limits,
	}, nil
}

func narrowToolLimit(parent ToolLimit, requested config.ToolLimit) (ToolLimit, error) {
	result := parent

	if requested.MaxCalls != 0 {
		if requested.MaxCalls < 0 || requested.MaxCalls > parent.MaxCalls {
			return ToolLimit{}, ErrInvalidLimits
		}

		result.MaxCalls = requested.MaxCalls
	}

	if requested.Timeout != "" {
		value, err := normalizedTimeout(requested.Timeout)
		if err != nil {
			return ToolLimit{}, err
		}

		requestedDuration, _ := time.ParseDuration(value)

		parentDuration, _ := time.ParseDuration(parent.Timeout)
		if requestedDuration > parentDuration {
			return ToolLimit{}, ErrInvalidLimits
		}

		result.Timeout = value
	}

	var err error

	result.MaxRequestBytes, err = narrowedBytes(requested.MaxRequestBytes, parent.MaxRequestBytes)
	if err != nil {
		return ToolLimit{}, err
	}

	result.MaxResultBytes, err = narrowedBytes(requested.MaxResultBytes, parent.MaxResultBytes)
	if err != nil {
		return ToolLimit{}, err
	}

	return result, nil
}

func narrowedBytes(requested, parent int) (int, error) {
	if requested == 0 {
		return parent, nil
	}

	if requested < 0 || requested > parent {
		return 0, ErrInvalidLimits
	}

	return requested, nil
}

func toolLimitMap(source []ToolLimit) map[string]ToolLimit {
	limits := make(map[string]ToolLimit, len(source))
	for _, limit := range source {
		limits[limit.Name] = limit
	}

	return limits
}

func projectDefinition(source dtool.Definition) ToolDefinition {
	capabilities := make([]string, len(source.Capabilities))
	for i, capability := range source.Capabilities {
		capabilities[i] = string(capability)
	}

	return ToolDefinition{Name: source.Name, Capabilities: capabilities, SideEffect: string(source.SideEffect)}
}

func validateToolConcurrency(steps []Step) error {
	for _, step := range steps {
		if unsafeToolScope(step.Tools) && step.Limits.MaxParallelCalls > 1 {
			return ErrInvalidLimits
		}
	}

	for i, left := range steps {
		for _, right := range steps[i+1:] {
			if isPlanAncestor(left.ID, right.ID, steps) || isPlanAncestor(right.ID, left.ID, steps) {
				continue
			}

			if unsafeToolScope(left.Tools) || unsafeToolScope(right.Tools) {
				return ErrInvalidLimits
			}
		}
	}

	return nil
}

func unsafeToolScope(scope ToolScope) bool {
	for _, definition := range scope.Definitions {
		if definition.SideEffect != string(dtool.SideEffectRead) {
			return true
		}
	}

	return false
}

func isPlanAncestor(candidate, stepID string, steps []Step) bool {
	needs := make(map[string][]string, len(steps))
	for _, step := range steps {
		needs[step.ID] = step.Needs
	}

	seen := make(map[string]bool)

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

func catalogDigest(catalog dtool.Catalog) string {
	definitions := make([]ToolDefinition, 0, len(catalog.Names()))
	for _, name := range catalog.Names() {
		definition, _ := catalog.Definition(name)
		definitions = append(definitions, projectDefinition(definition))
	}

	encoded, err := json.Marshal(definitions)
	if err != nil {
		panic(fmt.Sprintf("encoding static tool catalog: %v", err))
	}

	digest := sha256.Sum256(encoded)

	return hex.EncodeToString(digest[:])
}
