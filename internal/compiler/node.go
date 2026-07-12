package compiler

import (
	"fmt"

	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/prompt"
	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

// BuildNode creates an ADK AgentNode from a step definition.
func BuildNode(step config.Step, cfg *config.Config, reg *dtool.Registry, eventCtx *prompt.EventContext) (workflow.Node, error) {
	// Build system prompt
	systemPrompt := prompt.BuildSystemPrompt(step, cfg, eventCtx)

	// Resolve tools
	toolNames := dtool.ResolveNames(cfg.Defaults.Tools, step.Tools)
	resolvedTools := reg.Resolve(toolNames)

	// Build ADK agent config
	agentCfg := llmagent.Config{
		Name:        step.ID,
		Description: fmt.Sprintf("Step %q in workflow", step.ID),
		Instruction: systemPrompt,
		Mode:        llmagent.ModeSingleTurn,
	}

	// Apply model config
	gcc := buildGenerateContentConfig(step, cfg)
	if gcc != nil {
		agentCfg.GenerateContentConfig = gcc
	}

	// Set output key for state persistence
	if step.Output != "" {
		agentCfg.OutputKey = stateKey(step.ID)
	}

	// Wire tools as ADK Toolset
	if len(resolvedTools) > 0 {
		agentCfg.Toolsets = []tool.Toolset{dtool.NewToolset(resolvedTools)}
	}

	a, err := llmagent.New(agentCfg)
	if err != nil {
		return nil, fmt.Errorf("creating agent for step %q: %w", step.ID, err)
	}

	node, err := workflow.NewAgentNode(a, workflow.NodeConfig{})
	if err != nil {
		return nil, fmt.Errorf("creating node for step %q: %w", step.ID, err)
	}

	return node, nil
}

// SetModel sets the model on the agent config. Called by the runtime after provider creation.
func SetModel(agentCfg *llmagent.Config, llm model.LLM) {
	agentCfg.Model = llm
}

func buildGenerateContentConfig(step config.Step, cfg *config.Config) *genai.GenerateContentConfig {
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

	// Start with defaults
	if cfg != nil {
		result = cfg.Defaults.ModelConfig
	}

	// Override with step-level config
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
