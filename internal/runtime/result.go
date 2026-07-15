package runtime

import (
	"fmt"
	"strings"
	"time"
)

// StepStatus represents the outcome of a workflow step execution.
type StepStatus int

const (
	StepStatusPending   StepStatus = iota // not yet started
	StepStatusRunning                     // currently executing
	StepStatusCompleted                   // finished successfully
	StepStatusFailed                      // encountered an error
	StepStatusSkipped                     // skipped (predecessor failed)
)

// String returns the human-readable name of the status.
func (s StepStatus) String() string {
	switch s {
	case StepStatusPending:
		return "pending"
	case StepStatusRunning:
		return "running"
	case StepStatusCompleted:
		return "completed"
	case StepStatusFailed:
		return "failed"
	case StepStatusSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// StepResult captures the execution outcome of a single workflow step.
type StepResult struct {
	StepID    string        `json:"step_id"`
	Status    StepStatus    `json:"status"`
	Output    string        `json:"output,omitempty"`
	Error     error         `json:"-"`
	ErrorMsg  string        `json:"error,omitempty"`
	Duration  time.Duration `json:"duration_ms"`
	StartedAt time.Time     `json:"started_at"`
}

// WorkflowResult aggregates results from all steps in a workflow run.
type WorkflowResult struct {
	WorkflowName string        `json:"workflow_name"`
	Status       StepStatus    `json:"status"`
	Steps        []StepResult  `json:"steps"`
	Duration     time.Duration `json:"duration_ms"`
	StartedAt    time.Time     `json:"started_at"`
}

// Failed returns the first step that failed, or nil if none failed.
func (r *WorkflowResult) Failed() *StepResult {
	for i := range r.Steps {
		if r.Steps[i].Status == StepStatusFailed {
			return &r.Steps[i]
		}
	}

	return nil
}

// CompletedSteps returns only the steps that finished successfully.
func (r *WorkflowResult) CompletedSteps() []StepResult {
	var completed []StepResult

	for _, s := range r.Steps {
		if s.Status == StepStatusCompleted {
			completed = append(completed, s)
		}
	}

	return completed
}

// FormatSummary produces a human-readable summary of the workflow execution.
// On failure, it shows which step failed, partial results from completed steps,
// and the error message.
func (r *WorkflowResult) FormatSummary() string {
	var b strings.Builder

	fmt.Fprintf(&b, "Workflow: %s\n", r.WorkflowName)
	fmt.Fprintf(&b, "Status:   %s\n", r.Status)
	fmt.Fprintf(&b, "Duration: %s\n", r.Duration.Truncate(time.Millisecond))
	fmt.Fprintf(&b, "Steps:    %d total\n", len(r.Steps))
	b.WriteString("\n")

	for _, step := range r.Steps {
		icon := statusIcon(step.Status)
		fmt.Fprintf(&b, "  %s %s (%s)\n", icon, step.StepID, step.Duration.Truncate(time.Millisecond))

		if step.Status == StepStatusFailed && step.ErrorMsg != "" {
			fmt.Fprintf(&b, "    error: %s\n", step.ErrorMsg)
		}

		if step.Status == StepStatusCompleted && step.Output != "" {
			output := truncateOutput(step.Output, maxOutputPreview)
			fmt.Fprintf(&b, "    output: %s\n", output)
		}
	}

	return b.String()
}

const maxOutputPreview = 200

func statusIcon(s StepStatus) string {
	switch s {
	case StepStatusCompleted:
		return "✓"
	case StepStatusFailed:
		return "✗"
	case StepStatusSkipped:
		return "○"
	case StepStatusRunning:
		return "▶"
	default:
		return "·"
	}
}

func truncateOutput(s string, limit int) string {
	// Collapse newlines for single-line preview.
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")

	if len(s) <= limit {
		return s
	}

	return s[:limit] + "…"
}
