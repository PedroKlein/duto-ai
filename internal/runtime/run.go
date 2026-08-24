package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/platform"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/compiler"
	"github.com/PedroKlein/duto-ai/internal/plan"
)

const resultErrorInput = "input"

var (
	ErrExecution         = errors.New("workflow execution error")
	errTerminalOutput    = errors.New("terminal output is not an object")
	errAmbiguousTerminal = errors.New("ambiguous terminal result")
)

func Run(ctx context.Context, compiled *plan.Plan, resolve compiler.ModelResolver) (*Result, error) {
	return RunWithInputs(ctx, compiled, resolve, map[string]any{})
}

func RunWithInputs(ctx context.Context, compiled *plan.Plan, resolve compiler.ModelResolver, inputs map[string]any) (*Result, error) {
	return RunWithInputsAndToolsets(ctx, compiled, resolve, nil, inputs)
}

func RunWithInputsAndToolsets(ctx context.Context, compiled *plan.Plan, resolve compiler.ModelResolver, resolveToolset compiler.ToolsetResolver, inputs map[string]any) (*Result, error) {
	started := time.Now().UTC()

	runID, err := newRunID()
	if err != nil {
		return nil, fmt.Errorf("creating run identity: %w", err)
	}

	result := newResult(compiled, runID, started)

	if validationErr := compiler.ValidateInputs(compiled, inputs); validationErr != nil {
		result.Status = StatusFailed
		result.FinishedAt = time.Now().UTC()
		result.Errors = append(result.Errors, ResultError{Kind: resultErrorInput})

		return result, ErrExecution
	}

	root, err := compiler.CompileWithToolsets(ctx, compiled, resolve, resolveToolset)
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

	runInputs, terminalStepID, terminalAgentName, err := executionInputs(compiled.Snapshot().Workflow, inputs)
	if err != nil {
		result.Status = StatusFailed
		result.FinishedAt = time.Now().UTC()
		result.Errors = append(result.Errors, ResultError{Kind: resultErrorInput})

		return result, ErrExecution
	}

	encodedInputs, err := json.Marshal(runInputs)
	if err != nil {
		result.Status = StatusFailed
		result.FinishedAt = time.Now().UTC()
		result.Errors = append(result.Errors, ResultError{Kind: resultErrorInput})

		return result, ErrExecution
	}

	ctx = compiler.WithWorkflowInputs(ctx, inputs)
	ctx = platform.WithTaskRunner(ctx, boundedTaskRunner(compiled.Snapshot().Workflow.Limits.MaxParallelCalls))
	runErr := consumeEvents(ctx, r, root, runID, string(encodedInputs), terminalStepID, terminalAgentName, result)
	finishResult(result, ctx.Err(), runErr)

	if err := writer.finish(result.Status, result.Output); err != nil {
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

func consumeEvents(ctx context.Context, r *runner.Runner, root agent.Agent, runID, inputs, terminalStepID, terminalAgentName string, result *Result) error { //nolint:gocyclo,gocognit // One fold keeps event and terminal status semantics consistent.
	var runErr error

	terminalOutputs := make([]map[string]any, 0, 1)

	stepIndices := make(map[string]int, len(result.Steps))
	for i, step := range result.Steps {
		stepIndices[step.ID] = i
	}

	for event, iterationErr := range r.Run(ctx, "duto", runID, genai.NewContentFromText(inputs, genai.RoleUser), agent.RunConfig{}) {
		if iterationErr != nil {
			if runErr == nil {
				runErr = iterationErr
			}

			continue
		}

		if event == nil {
			continue
		}

		if terminalAgentName != "" && event.Author == terminalAgentName {
			if outputErr := llmagent.ProcessLLMAgentOutput(root, event); outputErr != nil {
				if runErr == nil {
					runErr = outputErr
				}

				continue
			}
		}

		stepID := eventNodeName(event)
		terminalEvent := isTerminalEvent(event)

		if terminalEvent {
			stepID = terminalEventStepID(event)
		}

		if terminalAgentName != "" && event.Author == terminalAgentName {
			stepID = terminalStepID
		}

		if usage := usageFromEvent(event); usage != nil {
			result.Usage = usage
			if index, ok := stepIndices[stepID]; ok {
				result.Steps[index].Usage = usage
			}
		}

		if event.Partial || event.Output == nil {
			continue
		}

		output, outputErr := cloneObject(event.Output)
		if outputErr != nil {
			continue
		}

		if index, ok := stepIndices[stepID]; ok {
			result.Steps[index].Output = output
			result.Steps[index].Status = StatusSucceeded
		}

		if terminalEvent || (terminalAgentName != "" && event.Author == terminalAgentName) {
			terminalOutputs = append(terminalOutputs, output)
		}
	}

	if runErr == nil {
		switch len(terminalOutputs) {
		case 1:
			result.Output = terminalOutputs[0]
		case 0:
		default:
			runErr = errAmbiguousTerminal
		}
	}

	return runErr
}

func executionInputs(workflow plan.Workflow, inputs map[string]any) (runInputs map[string]any, terminalStepID, terminalAgentName string, err error) {
	if len(workflow.Steps) != 1 || workflow.Steps[0].Agent == "" {
		return inputs, "", "", nil
	}

	step := workflow.Steps[0]
	for _, definition := range workflow.Agents {
		if definition.Name != step.Agent || definition.Mode != "chat" {
			continue
		}

		bound := make(map[string]any, len(step.Bindings))
		for _, binding := range step.Bindings {
			switch binding.Kind {
			case "input":
				value, exists := inputs[binding.Input]
				if !exists {
					return nil, "", "", errTerminalOutput
				}

				bound[binding.Name] = value
			case "literal":
				bound[binding.Name] = binding.Literal
			default:
				return nil, "", "", errTerminalOutput
			}
		}

		return bound, step.ID, definition.Name, nil
	}

	return inputs, "", "", nil
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

	if result.Status == StatusSucceeded {
		for i := range result.Steps {
			if result.Steps[i].Status == StatusIncomplete {
				result.Steps[i].Status = StatusSkipped
			}
		}
	}
}

func isTerminalEvent(event *session.Event) bool {
	return strings.HasPrefix(eventNodeName(event), compiler.TerminalNodeName+"-")
}

func terminalEventStepID(event *session.Event) string {
	name := strings.TrimPrefix(eventNodeName(event), compiler.TerminalNodeName+"-")
	separator := strings.LastIndex(name, "-")

	if separator <= 0 {
		return ""
	}

	return name[:separator]
}

func eventNodeName(event *session.Event) string {
	if event == nil || event.NodeInfo == nil {
		return ""
	}

	path := event.NodeInfo.Path

	segment := path[strings.LastIndex(path, "/")+1:]
	if index := strings.LastIndex(segment, "@"); index >= 0 {
		segment = segment[:index]
	}

	return segment
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

func boundedTaskRunner(limit int) platform.TaskRunner {
	return func(ctx context.Context, tasks []func(context.Context)) {
		jobs := make(chan func(context.Context), len(tasks))
		for _, task := range tasks {
			jobs <- task
		}

		close(jobs)

		var workers sync.WaitGroup
		for range min(limit, len(tasks)) {
			workers.Go(func() {
				for task := range jobs {
					task(ctx)
				}
			})
		}

		workers.Wait()
	}
}

func newRunID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}

	return "run-" + hex.EncodeToString(value[:]), nil
}
