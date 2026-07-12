package prompt_test

import (
	"strings"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/prompt"
)

func TestBuildSystemPrompt_BasicStep(t *testing.T) {
	step := config.Step{
		ID:     "analyze",
		Output: "findings",
	}

	cfg := &config.Config{
		Defaults: config.Defaults{
			Tools: []string{"github.read-pr", "files.read"},
		},
	}

	result := prompt.BuildSystemPrompt(step, cfg, nil)

	if !strings.Contains(result, "analyze") {
		t.Error("should contain step ID")
	}

	if !strings.Contains(result, "findings") {
		t.Error("should contain output key")
	}

	if !strings.Contains(result, "github.read-pr") {
		t.Error("should list default tools")
	}
}

func TestBuildSystemPrompt_WithEventContext(t *testing.T) {
	step := config.Step{ID: "test"}
	cfg := &config.Config{}

	eventCtx := &prompt.EventContext{
		Repo:      "owner/repo",
		PRNumber:  42,
		Author:    "alice",
		EventName: "pull_request",
	}

	result := prompt.BuildSystemPrompt(step, cfg, eventCtx)

	if !strings.Contains(result, "owner/repo") {
		t.Error("should contain repo")
	}

	if !strings.Contains(result, "PR #42") {
		t.Error("should contain PR number")
	}

	if !strings.Contains(result, "alice") {
		t.Error("should contain author")
	}
}

func TestBuildSystemPrompt_WithSystemField(t *testing.T) {
	step := config.Step{
		ID:     "test",
		System: "You are an expert code reviewer.",
	}

	cfg := &config.Config{}

	result := prompt.BuildSystemPrompt(step, cfg, nil)

	if !strings.Contains(result, "expert code reviewer") {
		t.Error("should contain user system field")
	}
}

func TestBuildSystemPrompt_ToolResolution(t *testing.T) {
	tools := []string{"github.post-review"}

	step := config.Step{
		ID:    "report",
		Tools: &tools,
	}

	cfg := &config.Config{
		Defaults: config.Defaults{
			Tools: []string{"files.read"},
		},
	}

	result := prompt.BuildSystemPrompt(step, cfg, nil)

	if !strings.Contains(result, "files.read") {
		t.Error("should contain default tools")
	}

	if !strings.Contains(result, "github.post-review") {
		t.Error("should contain step tools")
	}
}

func TestBuildSystemPrompt_EmptyToolsOverride(t *testing.T) {
	emptyTools := []string{}

	step := config.Step{
		ID:    "quiet",
		Tools: &emptyTools,
	}

	cfg := &config.Config{
		Defaults: config.Defaults{
			Tools: []string{"files.read"},
		},
	}

	result := prompt.BuildSystemPrompt(step, cfg, nil)

	if strings.Contains(result, "Available tools") {
		t.Error("should not list tools when explicitly empty")
	}
}
