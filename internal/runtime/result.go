package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

const ResultVersion = 1

type Status string

const (
	StatusSucceeded  Status = "succeeded"
	StatusFailed     Status = "failed"
	StatusCancelled  Status = "cancelled" //nolint:misspell // serialized contract spelling
	StatusIncomplete Status = "incomplete"
	StatusSkipped    Status = "skipped"
)

type Usage struct {
	InputTokens       *uint64 `json:"input_tokens,omitempty"`
	OutputTokens      *uint64 `json:"output_tokens,omitempty"`
	CachedInputTokens *uint64 `json:"cached_input_tokens,omitempty"`
}

type StepResult struct {
	ID     string         `json:"id"`
	Status Status         `json:"status"`
	Output map[string]any `json:"output,omitempty"`
	Usage  *Usage         `json:"usage,omitempty"`
}

type ResultError struct {
	Kind string `json:"kind"`
}

type Result struct {
	Version    int            `json:"version"`
	RunID      string         `json:"run_id"`
	Workflow   string         `json:"workflow"`
	Status     Status         `json:"status"`
	Outcome    string         `json:"outcome,omitempty"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt time.Time      `json:"finished_at"`
	Steps      []StepResult   `json:"steps"`
	Output     map[string]any `json:"output,omitempty"`
	Usage      *Usage         `json:"usage,omitempty"`
	Errors     []ResultError  `json:"errors"`
}

func (r *Result) JSON() ([]byte, error) {
	encoded, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("encoding result: %w", err)
	}

	return encoded, nil
}

func (r *Result) Text() ([]byte, error) {
	encoded, err := r.JSON()
	if err != nil {
		return nil, err
	}

	var output bytes.Buffer
	if err := json.Indent(&output, encoded, "", "  "); err != nil {
		return nil, fmt.Errorf("formatting result: %w", err)
	}

	return output.Bytes(), nil
}
