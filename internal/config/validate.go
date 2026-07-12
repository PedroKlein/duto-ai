package config

import (
	"errors"
	"fmt"
)

var (
	ErrNilWorkflow        = errors.New("workflow is nil")
	ErrNoSteps            = errors.New("workflow has no steps")
	ErrEmptyStepID        = errors.New("step has empty ID")
	ErrCircularDependency = errors.New("circular dependency detected")
)

// ValidateWorkflow checks a workflow for common errors:
// - Unique step IDs
// - Valid needs references
// - No circular dependencies
func ValidateWorkflow(wf *Workflow) error {
	if wf == nil {
		return ErrNilWorkflow
	}

	if len(wf.Steps) == 0 {
		return ErrNoSteps
	}

	ids := make(map[string]bool, len(wf.Steps))

	for _, step := range wf.Steps {
		if step.ID == "" {
			return ErrEmptyStepID
		}

		if ids[step.ID] {
			return fmt.Errorf("duplicate step ID %q: %w", step.ID, ErrEmptyStepID)
		}

		ids[step.ID] = true
	}

	for _, step := range wf.Steps {
		for _, need := range step.Needs {
			if !ids[need] {
				return fmt.Errorf("step %q references unknown dependency %q: %w", step.ID, need, ErrNilWorkflow)
			}
		}
	}

	if err := detectCycles(wf.Steps); err != nil {
		return err
	}

	return nil
}

// TopologicalSort returns the steps in a valid execution order.
func TopologicalSort(steps []Step) ([]Step, error) {
	graph := make(map[string][]string, len(steps))
	inDegree := make(map[string]int, len(steps))
	stepMap := make(map[string]Step, len(steps))

	for _, s := range steps {
		graph[s.ID] = nil
		inDegree[s.ID] = 0
		stepMap[s.ID] = s
	}

	for _, s := range steps {
		for _, dep := range s.Needs {
			graph[dep] = append(graph[dep], s.ID)
			inDegree[s.ID]++
		}
	}

	var queue []string

	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	var sorted []Step

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		sorted = append(sorted, stepMap[current])

		for _, neighbor := range graph[current] {
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

func detectCycles(steps []Step) error {
	_, err := TopologicalSort(steps)

	return err
}
