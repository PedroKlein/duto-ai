// Package runtime orchestrates the execution of a duto-ai workflow.
package runtime

import (
	"context"
	"fmt"
	"log"
	"os"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/compiler"
	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/prompt"
	"github.com/PedroKlein/duto-ai/internal/provider"
	"github.com/PedroKlein/duto-ai/internal/tool"
	gh "github.com/PedroKlein/duto-ai/internal/tool/github"
)

// Run executes a duto-ai workflow end-to-end using ADK's native workflow engine.
func Run(ctx context.Context, configPath, workflowPath string, opts ...Option) error {
	options := applyOptions(opts)

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	wf, err := config.LoadWorkflow(workflowPath)
	if err != nil {
		return fmt.Errorf("loading workflow: %w", err)
	}

	if vErr := config.ValidateWorkflow(wf); vErr != nil {
		return fmt.Errorf("validating workflow: %w", vErr)
	}

	log.Printf("Running workflow %q with %d steps", wf.Name, len(wf.Steps))

	llm, err := resolveLLM(ctx, cfg, options)
	if err != nil {
		return err
	}

	reg, err := buildRegistry(options)
	if err != nil {
		return err
	}

	eventCtx, err := prompt.LoadEventContext()
	if err != nil {
		log.Printf("Warning: could not load event context: %v", err)
	}

	root, err := compiler.Compile(wf, cfg, reg, llm, eventCtx)
	if err != nil {
		return fmt.Errorf("compiling workflow: %w", err)
	}

	output, err := execute(ctx, root, wf)
	if err != nil {
		return err
	}

	_ = output // final output available for future use (e.g. CLI printing)

	log.Printf("Workflow %q completed successfully", wf.Name)

	return nil
}

func execute(ctx context.Context, root agent.Agent, wf *config.Workflow) (string, error) {
	sessService := session.InMemoryService()

	r, err := runner.New(runner.Config{
		AppName:           "duto-ai",
		Agent:             root,
		SessionService:    sessService,
		AutoCreateSession: true,
	})
	if err != nil {
		return "", fmt.Errorf("creating runner: %w", err)
	}

	msg := genai.NewContentFromText(wf.Steps[0].Prompt, "user")

	var lastOutput string

	for event, iterErr := range r.Run(ctx, "user", "run", msg, agent.RunConfig{}) {
		if iterErr != nil {
			return "", fmt.Errorf("execution error: %w", iterErr)
		}

		if event == nil || event.Partial {
			continue
		}

		lastOutput = extractOutput(event)
	}

	return lastOutput, nil
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

func resolveLLM(ctx context.Context, cfg *config.Config, options *Options) (model.LLM, error) {
	if options.LLM != nil {
		return options.LLM, nil
	}

	modelName := cfg.Defaults.Model
	if cfg.Models != nil {
		modelName = config.ResolveModel(modelName, cfg.Models)
	}

	llm, err := provider.NewLLM(ctx, cfg.Provider, modelName)
	if err != nil {
		return nil, fmt.Errorf("creating LLM provider: %w", err)
	}

	return llm, nil
}

func buildRegistry(options *Options) (*tool.Registry, error) {
	reg := tool.NewRegistry()

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
		return nil, fmt.Errorf("registering tools: %w", err)
	}

	return reg, nil
}
