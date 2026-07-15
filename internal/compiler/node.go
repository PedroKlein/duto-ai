package compiler

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	model "google.golang.org/adk/v2/model"
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
	// If the prompt is a file path (ends with .md), load the file content.
	// Go templates are rendered for event context and env vars.
	// References to {{ .Steps.X.Output }} are replaced with a placeholder
	// since ADK handles inter-step output passing natively.
	if step.Prompt != "" {
		promptContent, err := ResolvePrompt(step.Prompt)
		if err != nil {
			return nil, nil, fmt.Errorf("resolving prompt for step %q: %w", step.ID, err)
		}

		// Strip step-output templates before rendering (they're not in template data).
		promptContent = stripStepOutputRefs(promptContent)

		// Render remaining templates (event context, env vars).
		promptContent, err = prompt.RenderTemplate(promptContent, eventCtx)
		if err != nil {
			return nil, nil, fmt.Errorf("rendering prompt template for step %q: %w", step.ID, err)
		}

		instruction += "\n\n## Task\n" + promptContent
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

	maxIter := ResolveMaxIterations(step)

	agentCfg := llmagent.Config{
		Name:        step.ID,
		Description: fmt.Sprintf("Step %q in workflow", step.ID),
		Instruction: instruction,
		Model:       llm,
		Mode:        llmagent.ModeSingleTurn,
		BeforeModelCallbacks: []llmagent.BeforeModelCallback{
			NewIterationLimiter(step.ID, maxIter),
			logBeforeModel(step.ID),
		},
		BeforeToolCallbacks:  []llmagent.BeforeToolCallback{logBeforeTool(step.ID)},
		OnToolErrorCallbacks: []llmagent.OnToolErrorCallback{logToolError(step.ID)},
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

	nodeCfg := workflow.NodeConfig{
		Timeout: ResolveTimeout(step),
	}

	node, err := workflow.NewAgentNode(a, nodeCfg)
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

// promptFileExtensions lists file extensions that trigger file loading for prompts.
var promptFileExtensions = []string{".md", ".txt"}

// ResolvePrompt returns the prompt content. If the value looks like a file
// path (ends with .md or .txt), the file is read and its content returned.
// Otherwise, the string is returned as-is.
func ResolvePrompt(raw string) (string, error) {
	if !isPromptFile(raw) {
		return raw, nil
	}

	path := filepath.Clean(raw)

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading prompt file %q: %w", path, err)
	}

	return strings.TrimSpace(string(data)), nil
}

// isPromptFile checks whether a prompt value is a file path to load.
func isPromptFile(raw string) bool {
	ext := strings.ToLower(filepath.Ext(raw))

	return slices.Contains(promptFileExtensions, ext)
}

// stripStepOutputRefs replaces {{ .Steps.X.Output }} template expressions with
// a placeholder hint, since ADK's workflow engine passes output between nodes
// automatically. Other template expressions are left intact for rendering.
func stripStepOutputRefs(s string) string {
	var result strings.Builder

	for {
		idx := strings.Index(s, "{{")
		if idx == -1 {
			result.WriteString(s)

			break
		}

		end := strings.Index(s[idx:], "}}")
		if end == -1 {
			result.WriteString(s)

			break
		}

		expr := s[idx : idx+end+2]

		// Only strip .Steps.* references.
		if strings.Contains(expr, ".Steps.") {
			result.WriteString(s[:idx])
			result.WriteString("[previous step output]")
		} else {
			result.WriteString(s[:idx+end+2])
		}

		s = s[idx+end+2:]
	}

	return strings.TrimSpace(result.String())
}

func logBeforeModel(stepID string) llmagent.BeforeModelCallback {
	return func(_ agent.Context, req *model.LLMRequest) (*model.LLMResponse, error) {
		toolCount := 0

		if req.Config != nil {
			for _, t := range req.Config.Tools {
				if t != nil {
					toolCount += len(t.FunctionDeclarations)
				}
			}
		}

		slog.Debug("LLM call",
			"step", stepID,
			"tools", toolCount,
			"messages", len(req.Contents),
		)

		if toolCount > 0 {
			var names []string

			for _, t := range req.Config.Tools {
				for _, fd := range t.FunctionDeclarations {
					names = append(names, fd.Name)
				}
			}

			slog.Debug("available tools", "step", stepID, "names", strings.Join(names, ", "))
		}

		return nil, nil //nolint:nilnil // proceed with normal LLM call
	}
}

func logBeforeTool(stepID string) llmagent.BeforeToolCallback {
	return func(_ agent.Context, t adktool.Tool, args map[string]any) (map[string]any, error) {
		slog.Debug("tool call", "step", stepID, "tool", t.Name(), "args", args)

		return nil, nil //nolint:nilnil // proceed with tool execution
	}
}

func logToolError(stepID string) llmagent.OnToolErrorCallback {
	return func(_ agent.Context, t adktool.Tool, args map[string]any, err error) (map[string]any, error) {
		slog.Error("tool error", "step", stepID, "tool", t.Name(), "error", err, "args", args)

		return nil, nil //nolint:nilnil // let ADK handle the error normally
	}
}
