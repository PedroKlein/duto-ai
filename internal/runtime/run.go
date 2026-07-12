package runtime

import (
	"context"
	"fmt"
	"log"
	"os"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/prompt"
	"github.com/PedroKlein/duto-ai/internal/provider"
	dtool "github.com/PedroKlein/duto-ai/internal/tool"
	gh "github.com/PedroKlein/duto-ai/internal/tool/github"
)

// Run executes a duto-ai workflow end-to-end.
func Run(ctx context.Context, configPath, workflowPath string, opts ...Option) error {
	var options Options
	for _, opt := range opts {
		opt(&options)
	}
	// 1. Load config
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// 2. Load workflow
	wf, err := config.LoadWorkflow(workflowPath)
	if err != nil {
		return fmt.Errorf("loading workflow: %w", err)
	}

	// 3. Validate
	if vErr := config.ValidateWorkflow(wf); vErr != nil {
		return fmt.Errorf("validating workflow: %w", vErr)
	}

	log.Printf("Running workflow %q with %d steps", wf.Name, len(wf.Steps))

	// 4. Resolve model name
	modelName := cfg.Defaults.Model
	if cfg.Models != nil {
		modelName = config.ResolveModel(modelName, cfg.Models)
	}

	// 5. Create provider (or use injected LLM)
	var llm model.LLM

	if options.LLM != nil {
		llm = options.LLM
	} else {
		var llmErr error

		llm, llmErr = provider.NewLLM(ctx, cfg.Provider, modelName)
		if llmErr != nil {
			return fmt.Errorf("creating LLM provider: %w", llmErr)
		}
	}

	// 6. Create tool registry
	reg := dtool.NewRegistry()

	if regErr := registerTools(reg, options.GitHubBaseURL); regErr != nil {
		return fmt.Errorf("registering tools: %w", regErr)
	}

	// 7. Load event context
	eventCtx, err := prompt.LoadEventContext()
	if err != nil {
		log.Printf("Warning: could not load event context: %v", err)
	}

	// 8. Topological sort steps
	sorted, err := config.TopologicalSort(wf.Steps)
	if err != nil {
		return fmt.Errorf("sorting steps: %w", err)
	}

	// 9. Execute steps sequentially (respecting DAG order)
	outputs := make(map[string]string)

	for _, step := range sorted {
		log.Printf("Executing step %q", step.ID)

		output, err := executeStep(ctx, step, cfg, llm, reg, eventCtx, outputs)
		if err != nil {
			return fmt.Errorf("step %q: %w", step.ID, err)
		}

		if step.Output != "" {
			outputs[step.ID] = output
		}

		log.Printf("Step %q completed", step.ID)
	}

	log.Printf("Workflow %q completed successfully", wf.Name)

	return nil
}

func executeStep(ctx context.Context, step config.Step, cfg *config.Config, llm model.LLM, reg *dtool.Registry, eventCtx *prompt.EventContext, outputs map[string]string) (string, error) {
	agentCfg := buildAgentConfig(step, cfg, llm, reg, eventCtx)

	a, err := llmagent.New(agentCfg)
	if err != nil {
		return "", fmt.Errorf("creating agent: %w", err)
	}

	userPrompt, err := renderStepPrompt(step, outputs)
	if err != nil {
		return "", fmt.Errorf("rendering prompt: %w", err)
	}

	return runAgent(ctx, a, userPrompt)
}

func buildAgentConfig(step config.Step, cfg *config.Config, llm model.LLM, reg *dtool.Registry, eventCtx *prompt.EventContext) llmagent.Config {
	systemPrompt := prompt.BuildSystemPrompt(step, cfg, eventCtx)

	toolNames := dtool.ResolveNames(cfg.Defaults.Tools, step.Tools)
	resolvedTools := reg.Resolve(toolNames)

	agentCfg := llmagent.Config{
		Name:        step.ID,
		Description: fmt.Sprintf("Step %q", step.ID),
		Instruction: systemPrompt,
		Model:       llm,
		Mode:        llmagent.ModeChat,
	}

	gcc := buildGCC(step, cfg)
	if gcc != nil {
		agentCfg.GenerateContentConfig = gcc
	}

	if len(resolvedTools) > 0 {
		agentCfg.Toolsets = []tool.Toolset{dtool.NewToolset(resolvedTools)}
	}

	return agentCfg
}

func renderStepPrompt(step config.Step, outputs map[string]string) (string, error) {
	templateData := prompt.TemplateData{
		Steps: make(map[string]prompt.StepOutput),
	}

	for id, out := range outputs {
		templateData.Steps[id] = prompt.StepOutput{Output: out}
	}

	rendered, err := prompt.RenderPrompt(step.Prompt, templateData)
	if err != nil {
		return "", fmt.Errorf("rendering step %q prompt: %w", step.ID, err)
	}

	return rendered, nil
}

func runAgent(ctx context.Context, a agent.Agent, userPrompt string) (string, error) {
	sessService := session.InMemoryService()

	r, err := runner.New(runner.Config{
		AppName:        "duto-ai",
		Agent:          a,
		SessionService: sessService,
	})
	if err != nil {
		return "", fmt.Errorf("creating runner: %w", err)
	}

	createResp, err := sessService.Create(ctx, &session.CreateRequest{
		AppName: "duto-ai",
		UserID:  "user",
	})
	if err != nil {
		return "", fmt.Errorf("creating session: %w", err)
	}

	sessID := createResp.Session.ID()
	msg := genai.NewContentFromText(userPrompt, "user")

	var lastOutput string

	for event, iterErr := range r.Run(ctx, "user", sessID, msg, agent.RunConfig{}) {
		if iterErr != nil {
			return "", fmt.Errorf("execution error: %w", iterErr)
		}

		if event == nil {
			continue
		}

		if event.Output != nil {
			if s, ok := event.Output.(string); ok {
				lastOutput = s
			}
		}

		if event.Content != nil {
			for _, part := range event.Content.Parts {
				if part.Text != "" {
					lastOutput = part.Text
				}
			}
		}
	}

	return lastOutput, nil
}

func buildGCC(step config.Step, cfg *config.Config) *genai.GenerateContentConfig {
	var temp *float32

	var maxTokens int32

	if cfg != nil && cfg.Defaults.ModelConfig.Temperature != nil {
		t := float32(*cfg.Defaults.ModelConfig.Temperature)
		temp = &t
	}

	if step.ModelConfig.Temperature != nil {
		t := float32(*step.ModelConfig.Temperature)
		temp = &t
	}

	if cfg != nil && cfg.Defaults.ModelConfig.MaxTokens != nil {
		maxTokens = int32(*cfg.Defaults.ModelConfig.MaxTokens)
	}

	if step.ModelConfig.MaxTokens != nil {
		maxTokens = int32(*step.ModelConfig.MaxTokens)
	}

	if temp == nil && maxTokens == 0 {
		return nil
	}

	gcc := &genai.GenerateContentConfig{}

	if temp != nil {
		gcc.Temperature = temp
	}

	if maxTokens > 0 {
		gcc.MaxOutputTokens = maxTokens
	}

	return gcc
}

func registerTools(reg *dtool.Registry, githubBaseURL string) error {
	token := os.Getenv("GITHUB_TOKEN")
	baseURL := githubBaseURL

	if baseURL == "" {
		baseURL = os.Getenv("GITHUB_API_URL")
	}

	if baseURL == "" {
		baseURL = "https://api.github.com"
	}

	client := gh.NewClient(token, baseURL)

	if err := gh.RegisterAll(reg, client); err != nil {
		return fmt.Errorf("registering github tools: %w", err)
	}

	return nil
}
