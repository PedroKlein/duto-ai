package config

import (
	"errors"
	"fmt"
	"regexp"

	"gopkg.in/yaml.v3"
)

var (
	ErrNilWorkflow        = errors.New("workflow is nil")
	ErrNoSteps            = errors.New("workflow has no steps")
	ErrDuplicateStepID    = errors.New("duplicate step id")
	ErrUnknownDependency  = errors.New("unknown dependency")
	ErrCircularDependency = errors.New("circular dependency")
	ErrInvalidName        = errors.New("invalid name")
	ErrUnknownResultStep  = errors.New("unknown result step")
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

func ValidateWorkflow(workflow *Workflow) error { //nolint:gocyclo // The closed graph validation order is intentionally linear.
	if workflow == nil {
		return ErrNilWorkflow
	}

	if !namePattern.MatchString(workflow.Name) {
		return fmt.Errorf("workflow name: %w", ErrInvalidName)
	}

	if !namePattern.MatchString(workflow.Model) {
		return fmt.Errorf("workflow model: %w", ErrInvalidName)
	}

	if len(workflow.Steps) == 0 {
		return ErrNoSteps
	}

	if err := validateAgentGraph(workflow); err != nil {
		return err
	}

	ids := make(map[string]struct{}, len(workflow.Steps))
	for _, step := range workflow.Steps {
		if !namePattern.MatchString(step.ID) {
			return fmt.Errorf("step id: %w", ErrInvalidName)
		}

		if _, exists := ids[step.ID]; exists {
			return fmt.Errorf("step %q: %w", step.ID, ErrDuplicateStepID)
		}

		ids[step.ID] = struct{}{}
	}

	for _, step := range workflow.Steps {
		for _, dependency := range step.Needs {
			if _, exists := ids[dependency]; !exists {
				return fmt.Errorf("step %q: %w", step.ID, ErrUnknownDependency)
			}
		}
	}

	if workflow.Result.Step != "" {
		if _, exists := ids[workflow.Result.Step]; !exists {
			return ErrUnknownResultStep
		}
	}

	for _, route := range workflow.Result.Routes {
		if route.Step != route.When.Step {
			return ErrUnknownResultStep
		}

		if _, exists := ids[route.Step]; !exists {
			return ErrUnknownResultStep
		}
	}

	_, err := TopologicalSort(workflow.Steps)

	return err
}

func TopologicalSort(steps []Step) ([]Step, error) {
	indices := make(map[string]int, len(steps))
	inDegree := make([]int, len(steps))

	edges := make([][]int, len(steps))
	for i, step := range steps {
		indices[step.ID] = i
	}

	for i, step := range steps {
		for _, dependency := range step.Needs {
			dependencyIndex, exists := indices[dependency]
			if !exists {
				return nil, fmt.Errorf("step %q: %w", step.ID, ErrUnknownDependency)
			}

			edges[dependencyIndex] = append(edges[dependencyIndex], i)
			inDegree[i]++
		}
	}

	queue := make([]int, 0, len(steps))

	for i, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, i)
		}
	}

	sorted := make([]Step, 0, len(steps))
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		sorted = append(sorted, steps[current])
		for _, neighbor := range edges[current] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(sorted) != len(steps) {
		return nil, ErrCircularDependency
	}

	return sorted, nil
}

func validateDecodedWorkflow(name string, workflow *Workflow, rootFields map[string]*yaml.Node, stepsNode *yaml.Node) error { //nolint:gocyclo // Source diagnostics mirror the closed graph checks.
	if !namePattern.MatchString(workflow.Name) {
		return diagnostic(name, "$.name", rootFields["name"], CodeInvalidValue)
	}

	if !namePattern.MatchString(workflow.Model) {
		return diagnostic(name, "$.model", rootFields["model"], CodeInvalidValue)
	}

	if len(workflow.Steps) == 0 {
		return diagnostic(name, "$.steps", stepsNode, CodeInvalidValue)
	}

	ids := make(map[string]int, len(workflow.Steps))
	for i, step := range workflow.Steps {
		stepFields, err := mappingFields(name, stepsNode.Content[i], fmt.Sprintf("$.steps[%d]", i), "id", "needs", "wait", "when", "agent", "instruction", "model", "model_config", "tools", "tool_limits", "skills", "workspaces", "input", "with", "output", "limits")
		if err != nil {
			return err
		}

		if !namePattern.MatchString(step.ID) {
			return diagnostic(name, fmt.Sprintf("$.steps[%d].id", i), stepFields["id"], CodeInvalidValue)
		}

		if _, exists := ids[step.ID]; exists {
			return diagnostic(name, fmt.Sprintf("$.steps[%d].id", i), stepFields["id"], CodeDuplicateKey)
		}

		ids[step.ID] = i
	}

	if err := validateDecodedAgents(name, workflow, rootFields["agents"], stepsNode); err != nil {
		return err
	}

	for i, step := range workflow.Steps {
		for j, dependency := range step.Needs {
			if _, exists := ids[dependency]; !exists {
				return diagnostic(name, fmt.Sprintf("$.steps[%d].needs[%d]", i, j), stepsNode.Content[i], CodeInvalidValue)
			}
		}
	}

	if workflow.Result.Step != "" {
		if _, exists := ids[workflow.Result.Step]; !exists {
			return diagnostic(name, "$.result.step", rootFields["result"], CodeInvalidValue)
		}
	}

	for _, route := range workflow.Result.Routes {
		if route.Step != route.When.Step {
			return diagnostic(name, "$.result.routes", rootFields["result"], CodeInvalidValue)
		}

		if _, exists := ids[route.Step]; !exists {
			return diagnostic(name, "$.result.routes", rootFields["result"], CodeInvalidValue)
		}
	}

	if _, err := TopologicalSort(workflow.Steps); err != nil {
		return diagnostic(name, "$.steps", stepsNode, CodeInvalidValue)
	}

	return nil
}
