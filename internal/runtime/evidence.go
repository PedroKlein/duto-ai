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
	"strings"
	"sync"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/plan"
	"github.com/PedroKlein/duto-ai/internal/safeoutput"
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

func writeEvidenceBundle(compiled *plan.Plan, result *Result, writer *evidenceWriter, staging *safeoutput.Collector) error {
	if compiled.EvidenceDirectory() == "" {
		return nil
	}

	if compiled.Staging() != nil {
		return writeM3EvidenceBundle(compiled, result, writer, staging)
	}

	return writeV1EvidenceBundle(compiled.EvidenceDirectory(), compiled.Digest(), result, writer)
}

func writeV1EvidenceBundle(directory, planDigest string, result *Result, writer *evidenceWriter) error {
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

func writeM3EvidenceBundle(compiled *plan.Plan, result *Result, writer *evidenceWriter, collector *safeoutput.Collector) error {
	if collector == nil {
		return ErrEvidence
	}

	spec := compiled.Staging()
	if spec == nil {
		return ErrEvidence
	}

	snapshot, files, err := m3BundleFiles(result, writer, collector)
	if err != nil {
		return err
	}

	sourceCommit := spec.BaseSHA
	if len(snapshot.Operations) > 0 {
		sourceCommit = snapshot.Operations[0].SourceCommit
	}

	manifestJSON, err := m3ManifestFor(compiled, result, spec, snapshot.OperationSet, sourceCommit, files)
	if err != nil {
		return err
	}

	return writeAtomicEvidenceDirectory(compiled.EvidenceDirectory(), files, manifestJSON, spec.MaxBundleBytes)
}

func m3BundleFiles(result *Result, writer *evidenceWriter, collector *safeoutput.Collector) (safeoutput.Snapshot, map[string][]byte, error) {
	snapshot, err := collector.Snapshot(result.RunID, result.Status == StatusSucceeded)
	if err != nil {
		return safeoutput.Snapshot{}, nil, ErrEvidence
	}

	events, err := writer.jsonLines()
	if err != nil {
		return safeoutput.Snapshot{}, nil, ErrEvidence
	}

	resultJSON, err := result.JSON()
	if err != nil {
		return safeoutput.Snapshot{}, nil, ErrEvidence
	}

	files := map[string][]byte{
		"control.json": snapshot.ControlJSON,
		"events.jsonl": events,
		"result.json":  append(resultJSON, '\n'),
		"summary.md":   fmt.Appendf(nil, "# Workflow result\n\n- Status: `%s`\n- Outcome: `%s`\n", result.Status, result.Outcome),
	}
	if len(snapshot.RecoveryMetadata) != 0 || len(snapshot.RecoveryPatch) != 0 {
		if len(snapshot.RecoveryMetadata) == 0 || len(snapshot.RecoveryPatch) == 0 {
			return safeoutput.Snapshot{}, nil, ErrEvidence
		}

		files["recovery/metadata.json"] = snapshot.RecoveryMetadata
		files["recovery/changes.patch"] = snapshot.RecoveryPatch
	}

	if result.Status != StatusSucceeded {
		return snapshot, files, nil
	}

	for index, operation := range snapshot.Operations {
		encoded, marshalErr := json.Marshal(operation)
		if marshalErr != nil || len(encoded)+1 > 131_072 {
			return safeoutput.Snapshot{}, nil, ErrEvidence
		}

		files[operationFileName(index, operation.Kind)] = append(encoded, '\n')
	}

	if snapshot.OperationSet == safeoutput.BranchPR {
		if len(snapshot.SourceBundle) == 0 {
			return safeoutput.Snapshot{}, nil, ErrEvidence
		}

		files["authored.bundle"] = snapshot.SourceBundle
	}

	return snapshot, files, nil
}

type m3Manifest struct {
	Version       int            `json:"version"`
	BundleKind    string         `json:"bundle_kind"`
	RunID         string         `json:"run_id"`
	Completion    string         `json:"completion"`
	OperationSet  string         `json:"operation_set"`
	PlanSHA256    string         `json:"plan_sha256"`
	PolicySHA256  string         `json:"policy_sha256"`
	ControlSHA256 string         `json:"control_sha256"`
	RepositoryID  string         `json:"repository_id"`
	BaseRef       string         `json:"base_ref"`
	BaseSHA       string         `json:"base_sha"`
	SourceCommit  string         `json:"source_commit"`
	Files         []manifestFile `json:"files"`
}

func m3ManifestFor(compiled *plan.Plan, result *Result, spec *plan.Staging, operationSet, sourceCommit string, files map[string][]byte) ([]byte, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		if name == "" || filepath.IsAbs(name) || filepath.Clean(name) != name || strings.HasPrefix(name, "../") {
			return nil, ErrEvidence
		}

		names = append(names, name)
	}

	slices.Sort(names)

	if len(names) > 12 {
		return nil, ErrEvidence
	}

	entries := make([]manifestFile, 0, len(names))
	for _, name := range names {
		content := files[name]
		if strings.HasSuffix(name, ".json") && len(content) > 131_072 {
			return nil, ErrEvidence
		}

		sum := sha256.Sum256(content)
		entries = append(entries, manifestFile{Name: name, Size: len(content), SHA256: hex.EncodeToString(sum[:])})
	}

	encoded, err := json.Marshal(m3Manifest{
		Version: 2, BundleKind: "m3-authoring", RunID: result.RunID, Completion: string(result.Status),
		OperationSet: operationSet, PlanSHA256: compiled.Digest(), PolicySHA256: spec.PolicySHA256,
		ControlSHA256: spec.ControlSHA256, RepositoryID: spec.Repository.ID, BaseRef: spec.BaseRef,
		BaseSHA: spec.BaseSHA, SourceCommit: sourceCommit, Files: entries,
	})
	if err != nil {
		return nil, ErrEvidence
	}

	return append(encoded, '\n'), nil
}

func operationFileName(index int, kind string) string {
	switch kind {
	case safeoutput.KindReply:
		return "operations/0001-conversation-reply.json"
	case safeoutput.KindBranch:
		return "operations/0001-branch.json"
	case safeoutput.KindDraftPR:
		return "operations/0002-draft-pr.json"
	default:
		return fmt.Sprintf("operations/%04d-invalid.json", index+1)
	}
}

func writeAtomicEvidenceDirectory(directory string, files map[string][]byte, manifestJSON []byte, maximum int) error {
	parent := filepath.Dir(directory)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return ErrEvidence
	}

	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		return ErrEvidence
	}

	temporary, err := os.MkdirTemp(parent, ".duto-evidence-")
	if err != nil {
		return ErrEvidence
	}
	defer func() { _ = os.RemoveAll(temporary) }()

	names := make([]string, 0, len(files))
	total := len(manifestJSON)

	for name, content := range files {
		names = append(names, name)
		total += len(content)
	}

	if maximum <= 0 || total > maximum {
		return ErrEvidence
	}

	slices.Sort(names)

	for _, name := range names {
		if err := writeEvidenceFile(temporary, name, files[name]); err != nil {
			return ErrEvidence
		}
	}

	if err := writeEvidenceFile(temporary, "manifest.json", manifestJSON); err != nil {
		return ErrEvidence
	}

	if err := syncEvidenceDirectory(temporary); err != nil {
		return ErrEvidence
	}

	if err := os.Rename(temporary, directory); err != nil {
		return ErrEvidence
	}

	if err := syncEvidenceDirectory(parent); err != nil {
		return ErrEvidence
	}

	return nil
}

func writeEvidenceFile(root, name string, content []byte) error {
	fullName := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(fullName), 0o700); err != nil {
		return fmt.Errorf("creating evidence subdirectory: %w", err)
	}

	file, err := os.OpenFile(fullName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // name is selected from fixed runtime-owned bundle entries
	if err != nil {
		return fmt.Errorf("creating evidence file: %w", err)
	}

	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("writing evidence file: %w", err)
	}

	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("syncing evidence file: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("closing evidence file: %w", err)
	}

	if err := syncEvidenceDirectory(filepath.Dir(fullName)); err != nil {
		return err
	}

	return nil
}

func syncEvidenceDirectory(name string) error {
	directory, err := os.Open(name) //nolint:gosec // trusted evidence directory
	if err != nil {
		return fmt.Errorf("opening evidence directory: %w", err)
	}
	defer directory.Close() //nolint:errcheck // explicit sync result is authoritative

	if err := directory.Sync(); err != nil {
		return fmt.Errorf("syncing evidence directory: %w", err)
	}

	return nil
}
