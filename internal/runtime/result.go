package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrUnknownFormat is returned when an unsupported output format is requested.
var ErrUnknownFormat = errors.New("unknown output format")

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

// OutputFormat specifies the structured output format for workflow results.
type OutputFormat int

const (
	OutputFormatText     OutputFormat = iota // human-readable text
	OutputFormatJSON                         // machine-parseable JSON
	OutputFormatMarkdown                     // markdown for PR comments
)

// ParseOutputFormat converts a string flag value to an OutputFormat.
func ParseOutputFormat(s string) (OutputFormat, error) {
	switch strings.ToLower(s) {
	case "text", "":
		return OutputFormatText, nil
	case "json":
		return OutputFormatJSON, nil
	case "markdown", "md":
		return OutputFormatMarkdown, nil
	default:
		return OutputFormatText, fmt.Errorf("%w: %q (use text, json, or markdown)", ErrUnknownFormat, s)
	}
}

// FormatJSON produces a JSON representation of the workflow result.
func (r *WorkflowResult) FormatJSON() (string, error) {
	data, err := json.MarshalIndent(r.toJSON(), "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling result: %w", err)
	}

	return string(data), nil
}

// FormatMarkdown produces a markdown summary suitable for PR comments.
func (r *WorkflowResult) FormatMarkdown() string {
	var b strings.Builder

	fmt.Fprintf(&b, "## Workflow: %s\n\n", r.WorkflowName)
	fmt.Fprintf(&b, "**Status:** %s | **Duration:** %s\n\n", r.Status, r.Duration.Truncate(time.Millisecond))

	b.WriteString("| Step | Status | Duration |\n")
	b.WriteString("|------|--------|----------|\n")

	for _, step := range r.Steps {
		fmt.Fprintf(&b, "| %s | %s %s | %s |\n",
			step.StepID,
			statusIcon(step.Status),
			step.Status,
			step.Duration.Truncate(time.Millisecond),
		)
	}

	if failed := r.Failed(); failed != nil {
		fmt.Fprintf(&b, "\n### Error\n\n```\n%s\n```\n", failed.ErrorMsg)
	}

	completed := r.CompletedSteps()
	if len(completed) > 0 {
		b.WriteString("\n### Step Outputs\n\n")

		for _, step := range completed {
			if step.Output == "" {
				continue
			}

			fmt.Fprintf(&b, "<details>\n<summary>%s</summary>\n\n%s\n\n</details>\n\n", step.StepID, step.Output)
		}
	}

	return b.String()
}

// FormatOutput returns the formatted output for the given format.
func (r *WorkflowResult) FormatOutput(format OutputFormat) (string, error) {
	switch format {
	case OutputFormatJSON:
		return r.FormatJSON()
	case OutputFormatMarkdown:
		return r.FormatMarkdown(), nil
	case OutputFormatText:
		return r.FormatSummary(), nil
	default:
		return r.FormatSummary(), nil
	}
}

// jsonResult is the JSON-serializable form of WorkflowResult.
type jsonResult struct {
	WorkflowName string     `json:"workflow_name"`
	Status       string     `json:"status"`
	DurationMS   int64      `json:"duration_ms"`
	Steps        []jsonStep `json:"steps"`
}

type jsonStep struct {
	StepID     string `json:"step_id"`
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	Output     string `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (r *WorkflowResult) toJSON() jsonResult {
	steps := make([]jsonStep, 0, len(r.Steps))

	for _, s := range r.Steps {
		steps = append(steps, jsonStep{
			StepID:     s.StepID,
			Status:     s.Status.String(),
			DurationMS: s.Duration.Milliseconds(),
			Output:     s.Output,
			Error:      s.ErrorMsg,
		})
	}

	return jsonResult{
		WorkflowName: r.WorkflowName,
		Status:       r.Status.String(),
		DurationMS:   r.Duration.Milliseconds(),
		Steps:        steps,
	}
}
