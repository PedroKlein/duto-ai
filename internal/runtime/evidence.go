package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

const (
	maxEvidenceEvents = 10_000
	maxEvidenceBytes  = 16 << 20
)

var ErrEvidence = errors.New("evidence bundle error")

type evidenceSource struct {
	InvocationID string `json:"invocation_id,omitempty"`
	EventID      string `json:"event_id,omitempty"`
	NodePath     string `json:"node_path,omitempty"`
	Author       string `json:"author,omitempty"`
}

type evidenceCorrelation struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Tool string `json:"tool"`
}

type evidencePayload struct {
	Class        string                `json:"class,omitempty"`
	Correlations []evidenceCorrelation `json:"correlations,omitempty"`
	OutputDigest string                `json:"output_digest,omitempty"`
	Usage        *Usage                `json:"usage,omitempty"`
}

type evidenceRecord struct {
	Version  int             `json:"version"`
	Sequence int             `json:"sequence"`
	Time     time.Time       `json:"time"`
	RunID    string          `json:"run_id"`
	Kind     string          `json:"kind"`
	Source   *evidenceSource `json:"source,omitempty"`
	Status   string          `json:"status"`
	Payload  evidencePayload `json:"payload"`
}

type evidenceWriter struct {
	mu       sync.Mutex
	runID    string
	records  []evidenceRecord
	closed   bool
	overflow bool
}

func newEvidenceWriter(runID string) *evidenceWriter {
	return &evidenceWriter{runID: runID, records: make([]evidenceRecord, 0, 8)}
}

func (w *evidenceWriter) plugin() (*plugin.Plugin, error) {
	value, err := plugin.New(plugin.Config{
		Name: "duto-evidence",
		BeforeRunCallback: func(ctx agent.InvocationContext) (*genai.Content, error) {
			w.append("run.start", "running", &evidenceSource{InvocationID: ctx.InvocationID()}, evidencePayload{})

			return nil, nil
		},
		OnEventCallback: func(_ agent.InvocationContext, event *session.Event) (*session.Event, error) {
			w.observe(event)

			return event, nil
		},
		AfterRunCallback: func(agent.InvocationContext) {
			w.mu.Lock()
			w.closed = true
			w.mu.Unlock()
		},
	})
	if err != nil {
		return nil, fmt.Errorf("creating ADK plugin: %w", err)
	}

	return value, nil
}

func (w *evidenceWriter) observe(event *session.Event) {
	if event == nil {
		return
	}

	source := &evidenceSource{
		InvocationID: event.InvocationID,
		EventID:      event.ID,
		Author:       event.Author,
	}
	if event.NodeInfo != nil {
		source.NodePath = event.NodeInfo.Path
	}

	payload := evidencePayload{Class: eventClass(event), Usage: usageFromEvent(event)}
	if event.Content != nil {
		for _, part := range event.Content.Parts {
			if part == nil {
				continue
			}

			if part.FunctionCall != nil {
				payload.Correlations = append(payload.Correlations, evidenceCorrelation{Kind: "call", ID: part.FunctionCall.ID, Tool: part.FunctionCall.Name})
			}

			if part.FunctionResponse != nil {
				payload.Correlations = append(payload.Correlations, evidenceCorrelation{Kind: "result", ID: part.FunctionResponse.ID, Tool: part.FunctionResponse.Name})
			}
		}
	}

	if event.Output != nil {
		payload.OutputDigest = digestValue(event.Output)
	}

	status := "observed"
	if event.Partial {
		status = "partial"
	} else if event.ErrorCode != "" {
		status = "failed"
	} else if event.Output != nil {
		status = "succeeded"
	}

	w.append("adk.event", status, source, payload)
}

func (w *evidenceWriter) finish(status Status, output map[string]any) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.closed || w.overflow {
		return ErrEvidence
	}

	payload := evidencePayload{}
	if output != nil {
		payload.OutputDigest = digestValue(output)
	}

	w.appendLocked("run.finish", string(status), nil, payload)

	return nil
}

func (w *evidenceWriter) append(kind, status string, source *evidenceSource, payload evidencePayload) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.appendLocked(kind, status, source, payload)
}

func (w *evidenceWriter) appendLocked(kind, status string, source *evidenceSource, payload evidencePayload) {
	if len(w.records) >= maxEvidenceEvents {
		w.overflow = true
		return
	}

	record := evidenceRecord{
		Version:  1,
		Sequence: len(w.records) + 1,
		Time:     time.Now().UTC(),
		RunID:    w.runID,
		Kind:     kind,
		Source:   source,
		Status:   status,
		Payload:  payload,
	}
	w.records = append(w.records, record)
}

func (w *evidenceWriter) jsonLines() ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	var result []byte

	for _, record := range w.records {
		line, err := json.Marshal(record)
		if err != nil {
			return nil, fmt.Errorf("encoding evidence record: %w", err)
		}

		if len(result)+len(line)+1 > maxEvidenceBytes {
			return nil, ErrEvidence
		}

		result = append(result, line...)
		result = append(result, '\n')
	}

	return slices.Clone(result), nil
}

func eventClass(event *session.Event) string {
	switch {
	case event.ErrorCode != "":
		return "error"
	case event.Output != nil:
		return "node"
	case event.Content != nil:
		return "model"
	default:
		return "event"
	}
}

func usageFromEvent(event *session.Event) *Usage {
	metadata := event.UsageMetadata
	if metadata == nil {
		return nil
	}

	usage := &Usage{}

	if metadata.PromptTokenCount > 0 {
		value := uint64(metadata.PromptTokenCount)
		usage.InputTokens = &value
	}

	if metadata.CandidatesTokenCount > 0 {
		value := uint64(metadata.CandidatesTokenCount)
		usage.OutputTokens = &value
	}

	if metadata.CachedContentTokenCount > 0 {
		value := uint64(metadata.CachedContentTokenCount)
		usage.CachedInputTokens = &value
	}

	if usage.InputTokens == nil && usage.OutputTokens == nil && usage.CachedInputTokens == nil {
		return nil
	}

	return usage
}

func digestValue(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}

	sum := sha256.Sum256(encoded)

	return hex.EncodeToString(sum[:])
}

type manifestFile struct {
	Name   string `json:"name"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

type manifest struct {
	Version    int            `json:"version"`
	RunID      string         `json:"run_id"`
	PlanDigest string         `json:"plan_digest"`
	Completion string         `json:"completion"`
	Files      []manifestFile `json:"files"`
}

func writeEvidenceBundle(directory, planDigest string, result *Result, writer *evidenceWriter) error {
	if directory == "" {
		return nil
	}

	events, err := writer.jsonLines()
	if err != nil {
		return ErrEvidence
	}

	resultJSON, err := result.JSON()
	if err != nil {
		return ErrEvidence
	}

	summary := fmt.Appendf(nil, "# Workflow result\n\n- Status: `%s`\n- Outcome: `%s`\n", result.Status, result.Outcome)
	files := map[string][]byte{
		"events.jsonl": events,
		"result.json":  append(resultJSON, '\n'),
		"summary.md":   summary,
	}

	parent := filepath.Dir(directory)

	mkdirErr := os.MkdirAll(parent, 0o700)
	if mkdirErr != nil {
		return ErrEvidence
	}

	_, statErr := os.Stat(directory)
	if !errors.Is(statErr, os.ErrNotExist) {
		return ErrEvidence
	}

	temporary, tempErr := os.MkdirTemp(parent, ".duto-evidence-")
	if tempErr != nil {
		return ErrEvidence
	}
	defer func() { _ = os.RemoveAll(temporary) }()

	manifestFiles := make([]manifestFile, 0, len(files))
	for _, name := range []string{"events.jsonl", "result.json", "summary.md"} {
		content := files[name]

		writeErr := os.WriteFile(filepath.Join(temporary, name), content, 0o600)
		if writeErr != nil {
			return ErrEvidence
		}

		sum := sha256.Sum256(content)
		manifestFiles = append(manifestFiles, manifestFile{Name: name, Size: len(content), SHA256: hex.EncodeToString(sum[:])})
	}

	manifestJSON, err := json.Marshal(manifest{
		Version:    1,
		RunID:      result.RunID,
		PlanDigest: planDigest,
		Completion: string(result.Status),
		Files:      manifestFiles,
	})
	if err != nil {
		return ErrEvidence
	}

	manifestWriteErr := os.WriteFile(filepath.Join(temporary, "manifest.json"), append(manifestJSON, '\n'), 0o600)
	if manifestWriteErr != nil {
		return ErrEvidence
	}

	renameErr := os.Rename(temporary, directory)
	if renameErr != nil {
		return ErrEvidence
	}

	return nil
}
