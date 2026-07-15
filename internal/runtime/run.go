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
	"github.com/PedroKlein/duto-ai/internal/tool/files"
	"github.com/PedroKlein/duto-ai/internal/tool/git"
	gh "github.com/PedroKlein/duto-ai/internal/tool/github"
	"github.com/PedroKlein/duto-ai/internal/tool/shell"
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

	resolver, err := buildModelResolver(ctx, cfg, options)
	if err != nil {
		return err
	}

	reg, err := buildRegistry(options)
	if err != nil {
		return err
	}

	eventCtx, _ := prompt.LoadEventContext() //nolint:nolintlint // event context is optional

	root, err := compiler.Compile(wf, cfg, reg, resolver, eventCtx)
	if err != nil {
		return fmt.Errorf("compiling workflow: %w", err)
	}

	if _, err := execute(ctx, root, wf); err != nil {
		return err
	}

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

	return reg, nil
}
