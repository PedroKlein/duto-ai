package config

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrNilWorkflow        = errors.New("workflow is nil")
	ErrNoSteps            = errors.New("workflow has no steps")
	ErrEmptyStepID        = errors.New("step has empty ID")
	ErrDuplicateStepID    = errors.New("duplicate step ID")
	ErrUnknownDependency  = errors.New("unknown dependency")
	ErrCircularDependency = errors.New("circular dependency detected")
	ErrInvalidTimeout     = errors.New("invalid timeout duration")
	ErrInvalidIterations  = errors.New("max_iterations must be positive")
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
			return fmt.Errorf("step ID %q: %w", step.ID, ErrDuplicateStepID)
		}

		ids[step.ID] = true
	}

	for _, step := range wf.Steps {
		for _, need := range step.Needs {
			if !ids[need] {
				return fmt.Errorf("step %q references %q: %w", step.ID, need, ErrUnknownDependency)
			}
		}

		if step.Timeout != "" {
			if _, err := time.ParseDuration(step.Timeout); err != nil {
				return fmt.Errorf("step %q: %w: %q", step.ID, ErrInvalidTimeout, step.Timeout)
			}
		}

		if step.MaxIterations < 0 {
			return fmt.Errorf("step %q: %w: %d", step.ID, ErrInvalidIterations, step.MaxIterations)
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
