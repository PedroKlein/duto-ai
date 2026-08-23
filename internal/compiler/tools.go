package compiler

import (
	"fmt"
	"time"

	"github.com/PedroKlein/duto-ai/internal/plan"
	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

func newToolGuards(workflow plan.Workflow) (map[string]*dtool.Guard, error) {
	runTimeout, err := time.ParseDuration(workflow.Limits.Timeout)
	if err != nil {
		return nil, fmt.Errorf("parsing workflow tool timeout: %w", err)
	}

	scopes := make(map[string]dtool.ScopeLimit, len(workflow.Steps)+len(workflow.Agents))
	for _, step := range workflow.Steps {
		timeout, parseErr := time.ParseDuration(step.Limits.Timeout)
		if parseErr != nil {
			return nil, fmt.Errorf("parsing step tool timeout: %w", parseErr)
		}

		limits := make(map[string]dtool.ToolLimit, len(step.Tools.Limits))
		for _, source := range step.Tools.Limits {
			toolTimeout, parseErr := time.ParseDuration(source.Timeout)
			if parseErr != nil {
				return nil, fmt.Errorf("parsing exact tool timeout: %w", parseErr)
			}

			limits[source.Name] = dtool.ToolLimit{
				MaxCalls:        source.MaxCalls,
				Timeout:         toolTimeout,
				MaxRequestBytes: source.MaxRequestBytes,
				MaxResultBytes:  source.MaxResultBytes,
			}
		}

		scopes[step.ID] = dtool.ScopeLimit{
			Names:    step.Tools.Names,
			MaxCalls: step.Limits.MaxToolCalls,
			Timeout:  timeout,
			Tools:    limits,
		}
	}

	for _, namedAgent := range workflow.Agents {
		timeout, parseErr := time.ParseDuration(namedAgent.Limits.Timeout)
		if parseErr != nil {
			return nil, fmt.Errorf("parsing agent tool timeout: %w", parseErr)
		}

		limits := make(map[string]dtool.ToolLimit, len(namedAgent.Tools.Limits))
		for _, source := range namedAgent.Tools.Limits {
			toolTimeout, parseErr := time.ParseDuration(source.Timeout)
			if parseErr != nil {
				return nil, fmt.Errorf("parsing agent exact tool timeout: %w", parseErr)
			}

			limits[source.Name] = dtool.ToolLimit{MaxCalls: source.MaxCalls, Timeout: toolTimeout, MaxRequestBytes: source.MaxRequestBytes, MaxResultBytes: source.MaxResultBytes}
		}

		scopes[agentGuardKey(namedAgent.Name)] = dtool.ScopeLimit{Names: namedAgent.Tools.Names, MaxCalls: namedAgent.Limits.MaxToolCalls, Timeout: timeout, Tools: limits}
	}

	guards, err := dtool.NewGuards(dtool.RuntimeLimit{MaxCalls: workflow.Limits.MaxToolCalls, Timeout: runTimeout}, scopes)
	if err != nil {
		return nil, fmt.Errorf("building tool guards: %w", err)
	}

	return guards, nil
}
