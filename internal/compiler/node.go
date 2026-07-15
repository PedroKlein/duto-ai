package compiler

import (
	"fmt"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	adktool "google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/prompt"
	"github.com/PedroKlein/duto-ai/internal/tool"
)

func buildNode(step config.Step, cfg *config.Config, reg *tool.Registry, resolve ModelResolver, eventCtx *prompt.EventContext) (workflow.Node, agent.Agent, error) {
	instruction := prompt.BuildSystemPrompt(step, cfg, eventCtx)

	// The step prompt becomes part of the instruction.
	// In the ADK workflow, predecessor output arrives as node input automatically,
	// so Go template references ({{ .Steps.X.Output }}) are stripped.
	if step.Prompt != "" {
		instruction += "\n\n## Task\n" + stripTemplates(step.Prompt)
	}

	// Resolve the model for this step
	modelName := step.Model
	if modelName == "" {
		modelName = cfg.Defaults.Model
	}

	if cfg.Models != nil {
		modelName = config.ResolveModel(modelName, cfg.Models)
	}

	llm, err := resolve(modelName)
	if err != nil {
		return nil, nil, fmt.Errorf("resolving model %q: %w", modelName, err)
	}

	toolNames := tool.ResolveNames(cfg.Defaults.Tools, step.Tools)
	resolvedTools := reg.Resolve(toolNames)

	agentCfg := llmagent.Config{
		Name:        step.ID,
		Description: fmt.Sprintf("Step %q in workflow", step.ID),
		Instruction: instruction,
		Model:       llm,
		Mode:        llmagent.ModeSingleTurn,
	}

	if gcc := buildGCC(step, cfg); gcc != nil {
		agentCfg.GenerateContentConfig = gcc
	}

	if step.Output != "" {
		agentCfg.OutputKey = stateKey(step.ID)
	}

	if len(resolvedTools) > 0 {
		agentCfg.Toolsets = []adktool.Toolset{tool.NewToolset(resolvedTools)}
	}

	a, err := llmagent.New(agentCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("creating agent: %w", err)
	}

	node, err := workflow.NewAgentNode(a, workflow.NodeConfig{})
	if err != nil {
		return nil, nil, fmt.Errorf("creating node: %w", err)
	}

	return node, a, nil
}

func buildGCC(step config.Step, cfg *config.Config) *genai.GenerateContentConfig {
	mc := mergeModelConfig(step, cfg)

	if mc.Temperature == nil && mc.MaxTokens == nil {
		return nil
	}

	gcc := &genai.GenerateContentConfig{}

	if mc.Temperature != nil {
		temp := float32(*mc.Temperature)
		gcc.Temperature = &temp
	}

	if mc.MaxTokens != nil {
		gcc.MaxOutputTokens = int32(*mc.MaxTokens)
	}

	return gcc
}

func mergeModelConfig(step config.Step, cfg *config.Config) config.ModelConfig {
	var result config.ModelConfig

	if cfg != nil {
		result = cfg.Defaults.ModelConfig
	}

	if step.ModelConfig.Temperature != nil {
		result.Temperature = step.ModelConfig.Temperature
	}

	if step.ModelConfig.MaxTokens != nil {
		result.MaxTokens = step.ModelConfig.MaxTokens
	}

	return result
}

func stateKey(stepID string) string {
	return "steps." + stepID + ".output"
}

// stripTemplates removes Go template expressions like {{ .Steps.X.Output }}
// since ADK's workflow engine passes output between nodes automatically.
func stripTemplates(s string) string {
	var result strings.Builder

	for {
		idx := strings.Index(s, "{{")
		if idx == -1 {
			result.WriteString(s)

			break
		}

		result.WriteString(s[:idx])

		end := strings.Index(s[idx:], "}}")
		if end == -1 {
			result.WriteString(s[idx:])

			break
		}

		// Replace template expression with a placeholder that tells the LLM
		// to use the input it received from the previous step.
		result.WriteString("[previous step output]")

		s = s[idx+end+2:]
	}

	return strings.TrimSpace(result.String())
}
