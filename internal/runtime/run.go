package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/compiler"
	"github.com/PedroKlein/duto-ai/internal/plan"
)

var (
	ErrExecution      = errors.New("workflow execution error")
	errTerminalOutput = errors.New("terminal output is not an object")
)

func Run(ctx context.Context, compiled *plan.Plan, resolve compiler.ModelResolver) (*Result, error) {
	started := time.Now().UTC()

	runID, err := newRunID()
	if err != nil {
		return nil, fmt.Errorf("creating run identity: %w", err)
	}

	result := newResult(compiled, runID, started)

	root, err := compiler.Compile(ctx, compiled, resolve)
	if err != nil {
		result.Status = StatusFailed
		result.FinishedAt = time.Now().UTC()
		result.Errors = append(result.Errors, ResultError{Kind: "construction"})

		return result, ErrExecution
	}

	writer := newEvidenceWriter(runID)

	evidencePlugin, err := writer.plugin()
	if err != nil {
		return nil, fmt.Errorf("creating evidence plugin: %w", err)
	}

	r, err := runner.New(runner.Config{
		AppName:         "duto-ai",
		Agent:           root,
		SessionService:  session.InMemoryService(),
		ArtifactService: artifact.InMemoryService(),
		PluginConfig: runner.PluginConfig{
			Plugins: []*plugin.Plugin{evidencePlugin},
		},
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, fmt.Errorf("creating runner: %w", err)
	}

	runErr := consumeEvents(ctx, r, runID, result)
	finishResult(result, ctx.Err(), runErr)

	if err := writer.finish(result.Status); err != nil {
		result.Status = StatusIncomplete
		result.Errors = append(result.Errors, ResultError{Kind: "evidence"})

		return result, ErrEvidence
	}

	if err := writeEvidenceBundle(compiled.EvidenceDirectory(), compiled.Digest(), result, writer); err != nil {
		result.Status = StatusIncomplete
		result.Errors = append(result.Errors, ResultError{Kind: "evidence"})

		return result, ErrEvidence
	}

	switch result.Status {
	case StatusSucceeded:
		return result, nil
	case StatusCancelled:
		return result, context.Canceled
	case StatusFailed, StatusIncomplete:
		return result, ErrExecution
	default:
		return result, ErrExecution
	}
}

func consumeEvents(ctx context.Context, r *runner.Runner, runID string, result *Result) error {
	var runErr error

	for event, iterationErr := range r.Run(ctx, "duto", runID, genai.NewContentFromText("{}", genai.RoleUser), agent.RunConfig{}) {
		if iterationErr != nil {
			if runErr == nil {
				runErr = iterationErr
			}

			continue
		}

		if event == nil {
			continue
		}

		if usage := usageFromEvent(event); usage != nil {
			result.Usage = usage
			result.Steps[0].Usage = usage
		}

		if event.Partial || !isTerminalEvent(event) || event.Output == nil {
			continue
		}

		output, outputErr := cloneObject(event.Output)
		if outputErr != nil {
			if runErr == nil {
				runErr = outputErr
			}

			continue
		}

		result.Output = output
		result.Steps[0].Output = output
	}

	return runErr
}

func newResult(compiled *plan.Plan, runID string, started time.Time) *Result {
	result := &Result{
		Version:   ResultVersion,
		RunID:     runID,
		Status:    StatusIncomplete,
		StartedAt: started,
		Steps:     []StepResult{},
		Errors:    []ResultError{},
	}
	if compiled == nil {
		return result
	}

	snapshot := compiled.Snapshot()

	result.Workflow = snapshot.Workflow.Name
	for _, step := range snapshot.Workflow.Steps {
		result.Steps = append(result.Steps, StepResult{ID: step.ID, Status: StatusIncomplete})
	}

	return result
}

func finishResult(result *Result, contextErr, runErr error) {
	result.FinishedAt = time.Now().UTC()

	switch {
	case errors.Is(contextErr, context.Canceled), errors.Is(contextErr, context.DeadlineExceeded):
		result.Status = StatusCancelled
		result.Errors = append(result.Errors, ResultError{Kind: "cancelled"}) //nolint:misspell // serialized contract spelling
	case runErr != nil:
		result.Status = StatusFailed
		result.Errors = append(result.Errors, ResultError{Kind: "execution"})
	case result.Output == nil:
		result.Status = StatusIncomplete
		result.Errors = append(result.Errors, ResultError{Kind: "missing_terminal_output"})
	default:
		result.Status = StatusSucceeded
		result.Outcome, _ = result.Output["outcome"].(string)
	}

	for i := range result.Steps {
		result.Steps[i].Status = result.Status
	}
}

func isTerminalEvent(event *session.Event) bool {
	if event.NodeInfo == nil {
		return false
	}

	path := event.NodeInfo.Path
	segment := path[strings.LastIndex(path, "/")+1:]

	return strings.HasPrefix(segment, compiler.TerminalNodeName+"@")
}

func cloneObject(value any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encoding terminal output: %w", err)
	}

	var output map[string]any
	if err := json.Unmarshal(encoded, &output); err != nil {
		return nil, fmt.Errorf("decoding terminal output: %w", err)
	}

	if output == nil {
		return nil, errTerminalOutput
	}

	return output, nil
}

func newRunID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}

	return "run-" + hex.EncodeToString(value[:]), nil
}
