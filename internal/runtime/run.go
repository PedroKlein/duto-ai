// Package runtime orchestrates the execution of a duto-ai workflow.
package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/compiler"
	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/logging"
	"github.com/PedroKlein/duto-ai/internal/prompt"
	"github.com/PedroKlein/duto-ai/internal/provider"
	"github.com/PedroKlein/duto-ai/internal/tool"
	"github.com/PedroKlein/duto-ai/internal/tool/files"
	"github.com/PedroKlein/duto-ai/internal/tool/git"
	gh "github.com/PedroKlein/duto-ai/internal/tool/github"
	"github.com/PedroKlein/duto-ai/internal/tool/shell"
	"github.com/PedroKlein/duto-ai/internal/tool/web"
)

// Run executes a duto-ai workflow end-to-end using ADK's native workflow engine.
func Run(ctx context.Context, configPath, workflowPath string, opts ...Option) error {
	_, err := RunWithResult(ctx, configPath, workflowPath, opts...)
	return err
}

// RunWithResult executes a workflow and returns the structured result.
// On error, the result still contains partial step data for diagnostics.
func RunWithResult(ctx context.Context, configPath, workflowPath string, opts ...Option) (*WorkflowResult, error) {
	options := applyOptions(opts)

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	wf, err := config.LoadWorkflow(workflowPath)
	if err != nil {
		return nil, fmt.Errorf("loading workflow: %w", err)
	}

	if vErr := config.ValidateWorkflow(wf); vErr != nil {
		return nil, fmt.Errorf("validating workflow: %w", vErr)
	}

	slog.Info("running workflow", "name", wf.Name, "steps", len(wf.Steps))

	resolver, err := buildModelResolver(ctx, cfg, options)
	if err != nil {
		return nil, err
	}

	reg, err := buildRegistry(options)
	if err != nil {
		return nil, err
	}

	eventCtx, _ := prompt.LoadEventContext() //nolint:nolintlint // event context is optional

	root, err := compiler.Compile(wf, cfg, reg, resolver, eventCtx)
	if err != nil {
		return nil, fmt.Errorf("compiling workflow: %w", err)
	}

	result, err := execute(ctx, root, wf)
	if err != nil {
		logWorkflowFailure(result)

		return result, err
	}

	slog.Info("workflow completed", "name", wf.Name)

	return result, nil
}

func execute(ctx context.Context, root agent.Agent, wf *config.Workflow) (*WorkflowResult, error) {
	sessService := session.InMemoryService()

	r, err := runner.New(runner.Config{
		AppName:           "duto-ai",
		Agent:             root,
		SessionService:    sessService,
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, fmt.Errorf("creating runner: %w", err)
	}

	msg := genai.NewContentFromText(wf.Steps[0].Prompt, "user")

	result := newWorkflowResult(wf)

	for event, iterErr := range r.Run(ctx, "user", "run", msg, agent.RunConfig{}) {
		if iterErr != nil {
			recordStepFailure(result, iterErr)

			return result, fmt.Errorf("execution error: %w", iterErr)
		}

		if event == nil || event.Partial {
			continue
		}

		recordEvent(result, event)
	}

	finalizeResult(result)

	return result, nil
}

func newWorkflowResult(wf *config.Workflow) *WorkflowResult {
	steps := make([]StepResult, 0, len(wf.Steps))

	for _, s := range wf.Steps {
		steps = append(steps, StepResult{
			StepID: s.ID,
			Status: StepStatusPending,
		})
	}

	return &WorkflowResult{
		WorkflowName: wf.Name,
		Status:       StepStatusRunning,
		Steps:        steps,
		StartedAt:    time.Now(),
	}
}

func recordEvent(result *WorkflowResult, event *session.Event) {
	stepID := event.Author
	if stepID == "" {
		return
	}

	step := findStep(result, stepID)
	if step == nil {
		return
	}

	// Mark as running on first event for this step.
	if step.Status == StepStatusPending {
		step.Status = StepStatusRunning
		step.StartedAt = time.Now()

		logging.GHAGroup("Step: " + stepID)
	}

	output := extractOutput(event)
	if output != "" {
		step.Output = output
		step.Status = StepStatusCompleted
		step.Duration = time.Since(step.StartedAt)
		logging.GHAStepTiming(stepID, step.Duration)
		logging.GHAEndGroup()
	}
}

func recordStepFailure(result *WorkflowResult, err error) {
	now := time.Now()
	result.Status = StepStatusFailed
	result.Duration = now.Sub(result.StartedAt)

	// Mark the currently running step as failed.
	for i := range result.Steps {
		if result.Steps[i].Status != StepStatusRunning {
			continue
		}

		result.Steps[i].Status = StepStatusFailed
		result.Steps[i].Error = err
		result.Steps[i].ErrorMsg = err.Error()
		result.Steps[i].Duration = now.Sub(result.Steps[i].StartedAt)

		logging.GHAError(fmt.Sprintf("Step %q failed: %s", result.Steps[i].StepID, err.Error()))
		logging.GHAEndGroup()

		break
	}

	// Mark remaining pending steps as skipped.
	for i := range result.Steps {
		if result.Steps[i].Status == StepStatusPending {
			result.Steps[i].Status = StepStatusSkipped
		}
	}
}

func finalizeResult(result *WorkflowResult) {
	result.Duration = time.Since(result.StartedAt)
	result.Status = StepStatusCompleted

	for _, step := range result.Steps {
		if step.Status == StepStatusFailed {
			result.Status = StepStatusFailed

			break
		}
	}
}

func findStep(result *WorkflowResult, stepID string) *StepResult {
	for i := range result.Steps {
		if result.Steps[i].StepID == stepID {
			return &result.Steps[i]
		}
	}

	return nil
}

func extractOutput(event *session.Event) string {
	if event.Output != nil {
		if s, ok := event.Output.(string); ok {
			return s
		}
	}

	if event.Content == nil {
		return ""
	}

	var last string

	for _, part := range event.Content.Parts {
		if part.Text != "" && !part.Thought {
			last = part.Text
		}
	}

	return last
}

func logWorkflowFailure(result *WorkflowResult) {
	if result == nil {
		return
	}

	failed := result.Failed()
	if failed == nil {
		return
	}

	slog.Error("workflow failed",
		"workflow", result.WorkflowName,
		"failed_step", failed.StepID,
		"error", failed.ErrorMsg,
		"duration", result.Duration.Truncate(time.Millisecond),
	)

	completed := result.CompletedSteps()
	if len(completed) > 0 {
		names := make([]string, 0, len(completed))
		for _, s := range completed {
			names = append(names, s.StepID)
		}

		slog.Info("partial progress", "completed_steps", names)
	}

	fmt.Fprintln(os.Stderr, "\n"+result.FormatSummary())
}

func buildModelResolver(ctx context.Context, cfg *config.Config, options *Options) (compiler.ModelResolver, error) {
	// If a mock LLM is injected, return it for all model names.
	if options.LLM != nil {
		return func(_ string) (model.LLM, error) {
			return options.LLM, nil
		}, nil
	}

	p, err := provider.NewProvider(ctx, cfg.Provider)
	if err != nil {
		return nil, fmt.Errorf("creating provider: %w", err)
	}

	cache := make(map[string]model.LLM)

	return func(modelName string) (model.LLM, error) {
		if llm, ok := cache[modelName]; ok {
			return llm, nil
		}

		llm, err := p.Model(modelName)
		if err != nil {
			return nil, fmt.Errorf("creating model %q: %w", modelName, err)
		}

		cache[modelName] = llm

		return llm, nil
	}, nil
}

func buildRegistry(options *Options) (*tool.Registry, error) {
	reg := tool.NewRegistry()

	repoRoot := options.RepoRoot
	if repoRoot == "" {
		var err error

		repoRoot, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getting working directory: %w", err)
		}
	}

	// Register GitHub tools.
	token := os.Getenv("GITHUB_TOKEN")
	baseURL := options.GitHubBaseURL

	if baseURL == "" {
		baseURL = os.Getenv("GITHUB_API_URL")
	}

	if baseURL == "" {
		baseURL = "https://api.github.com"
	}

	client := gh.NewClient(token, baseURL)

	if err := gh.RegisterAll(reg, client); err != nil {
		return nil, fmt.Errorf("registering github tools: %w", err)
	}

	// Register file tools.
	if err := files.RegisterAll(reg, repoRoot); err != nil {
		return nil, fmt.Errorf("registering files tools: %w", err)
	}

	// Register git tools.
	if err := git.RegisterAll(reg, repoRoot); err != nil {
		return nil, fmt.Errorf("registering git tools: %w", err)
	}

	// Register shell tool.
	if err := shell.RegisterAll(reg, repoRoot); err != nil {
		return nil, fmt.Errorf("registering shell tools: %w", err)
	}

	// Register web tools.
	if err := web.RegisterAll(reg); err != nil {
		return nil, fmt.Errorf("registering web tools: %w", err)
	}

	return reg, nil
}
