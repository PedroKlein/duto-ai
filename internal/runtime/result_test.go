package runtime_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/PedroKlein/duto-ai/internal/runtime"
)

func TestStepStatus_String(t *testing.T) {
	tests := []struct {
		status runtime.StepStatus
		want   string
	}{
		{runtime.StepStatusPending, "pending"},
		{runtime.StepStatusRunning, "running"},
		{runtime.StepStatusCompleted, "completed"},
		{runtime.StepStatusFailed, "failed"},
		{runtime.StepStatusSkipped, "skipped"},
		{runtime.StepStatus(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("StepStatus(%d).String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestWorkflowResult_Failed(t *testing.T) {
	t.Run("returns nil when no steps failed", func(t *testing.T) {
		r := &runtime.WorkflowResult{
			Steps: []runtime.StepResult{
				{StepID: "a", Status: runtime.StepStatusCompleted},
				{StepID: "b", Status: runtime.StepStatusCompleted},
			},
		}

		if got := r.Failed(); got != nil {
			t.Errorf("Failed() = %v, want nil", got)
		}
	})

	t.Run("returns first failed step", func(t *testing.T) {
		r := &runtime.WorkflowResult{
			Steps: []runtime.StepResult{
				{StepID: "a", Status: runtime.StepStatusCompleted},
				{StepID: "b", Status: runtime.StepStatusFailed, ErrorMsg: "boom"},
				{StepID: "c", Status: runtime.StepStatusSkipped},
			},
		}

		got := r.Failed()
		if got == nil {
			t.Fatal("Failed() = nil, want step b")
		}

		if got.StepID != "b" {
			t.Errorf("Failed().StepID = %q, want %q", got.StepID, "b")
		}
	})
}

func TestWorkflowResult_CompletedSteps(t *testing.T) {
	r := &runtime.WorkflowResult{
		Steps: []runtime.StepResult{
			{StepID: "a", Status: runtime.StepStatusCompleted},
			{StepID: "b", Status: runtime.StepStatusFailed},
			{StepID: "c", Status: runtime.StepStatusCompleted},
			{StepID: "d", Status: runtime.StepStatusSkipped},
		},
	}

	got := r.CompletedSteps()
	if len(got) != 2 {
		t.Fatalf("CompletedSteps() returned %d steps, want 2", len(got))
	}

	if got[0].StepID != "a" || got[1].StepID != "c" {
		t.Errorf("CompletedSteps() = [%q, %q], want [a, c]", got[0].StepID, got[1].StepID)
	}
}

func TestWorkflowResult_FormatSummary(t *testing.T) {
	r := &runtime.WorkflowResult{
		WorkflowName: "test-workflow",
		Status:       runtime.StepStatusFailed,
		Duration:     2500 * time.Millisecond,
		Steps: []runtime.StepResult{
			{
				StepID:   "gather",
				Status:   runtime.StepStatusCompleted,
				Output:   "collected data",
				Duration: 1 * time.Second,
			},
			{
				StepID:   "analyze",
				Status:   runtime.StepStatusFailed,
				ErrorMsg: "model timeout",
				Duration: 1500 * time.Millisecond,
			},
			{
				StepID: "report",
				Status: runtime.StepStatusSkipped,
			},
		},
	}

	summary := r.FormatSummary()

	expectations := []string{
		"test-workflow",
		"failed",
		"gather",
		"✓",
		"analyze",
		"✗",
		"model timeout",
		"report",
		"○",
	}

	for _, want := range expectations {
		if !strings.Contains(summary, want) {
			t.Errorf("FormatSummary() missing %q in:\n%s", want, summary)
		}
	}
}

func TestWorkflowResult_FormatSummary_TruncatesLongOutput(t *testing.T) {
	longOutput := strings.Repeat("x", 300)

	r := &runtime.WorkflowResult{
		WorkflowName: "wf",
		Status:       runtime.StepStatusCompleted,
		Duration:     1 * time.Second,
		Steps: []runtime.StepResult{
			{
				StepID:   "step1",
				Status:   runtime.StepStatusCompleted,
				Output:   longOutput,
				Duration: 500 * time.Millisecond,
			},
		},
	}

	summary := r.FormatSummary()

	// The 300-char output should be truncated with "…"
	if !strings.Contains(summary, "…") {
		t.Errorf("FormatSummary() should truncate long output with ellipsis, got:\n%s", summary)
	}
}

func TestParseOutputFormat(t *testing.T) {
	tests := []struct {
		input   string
		want    runtime.OutputFormat
		wantErr bool
	}{
		{"", runtime.OutputFormatText, false},
		{"text", runtime.OutputFormatText, false},
		{"json", runtime.OutputFormatJSON, false},
		{"markdown", runtime.OutputFormatMarkdown, false},
		{"md", runtime.OutputFormatMarkdown, false},
		{"JSON", runtime.OutputFormatJSON, false},
		{"invalid", runtime.OutputFormatText, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := runtime.ParseOutputFormat(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseOutputFormat(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}

			if got != tt.want {
				t.Errorf("ParseOutputFormat(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestWorkflowResult_FormatJSON(t *testing.T) {
	r := &runtime.WorkflowResult{
		WorkflowName: "test-wf",
		Status:       runtime.StepStatusCompleted,
		Duration:     2 * time.Second,
		Steps: []runtime.StepResult{
			{
				StepID:   "step1",
				Status:   runtime.StepStatusCompleted,
				Output:   "done",
				Duration: 1 * time.Second,
			},
		},
	}

	got, err := r.FormatJSON()
	if err != nil {
		t.Fatalf("FormatJSON() error = %v", err)
	}

	// Verify it's valid JSON.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("FormatJSON() produced invalid JSON: %v", err)
	}

	if parsed["workflow_name"] != "test-wf" {
		t.Errorf("workflow_name = %v, want test-wf", parsed["workflow_name"])
	}

	if parsed["status"] != "completed" {
		t.Errorf("status = %v, want completed", parsed["status"])
	}
}

func TestWorkflowResult_FormatMarkdown(t *testing.T) {
	r := &runtime.WorkflowResult{
		WorkflowName: "review-pr",
		Status:       runtime.StepStatusFailed,
		Duration:     3 * time.Second,
		Steps: []runtime.StepResult{
			{
				StepID:   "gather",
				Status:   runtime.StepStatusCompleted,
				Output:   "data collected",
				Duration: 1 * time.Second,
			},
			{
				StepID:   "analyze",
				Status:   runtime.StepStatusFailed,
				ErrorMsg: "timeout",
				Duration: 2 * time.Second,
			},
		},
	}

	md := r.FormatMarkdown()

	expectations := []string{
		"## Workflow: review-pr",
		"| gather",
		"| analyze",
		"### Error",
		"timeout",
		"### Step Outputs",
		"<details>",
		"data collected",
	}

	for _, want := range expectations {
		if !strings.Contains(md, want) {
			t.Errorf("FormatMarkdown() missing %q in:\n%s", want, md)
		}
	}
}
