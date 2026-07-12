package prompt_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/prompt"
)

func TestRenderPrompt_InlineTemplate(t *testing.T) {
	data := prompt.TemplateData{
		Steps: map[string]prompt.StepOutput{
			"gather": {Output: "found 3 issues"},
		},
	}

	result, err := prompt.RenderPrompt("Analyze: {{ .Steps.gather.Output }}", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "Analyze: found 3 issues"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestRenderPrompt_NoTemplate(t *testing.T) {
	result, err := prompt.RenderPrompt("plain text prompt", prompt.TemplateData{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "plain text prompt" {
		t.Errorf("got %q, want %q", result, "plain text prompt")
	}
}

func TestRenderPrompt_FileTemplate(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "prompt.md")

	err := os.WriteFile(tmplPath, []byte("Review: {{ .Steps.analyze.Output }}"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	data := prompt.TemplateData{
		Steps: map[string]prompt.StepOutput{
			"analyze": {Output: "LGTM"},
		},
	}

	result, err := prompt.RenderPrompt(tmplPath, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "Review: LGTM" {
		t.Errorf("got %q, want %q", result, "Review: LGTM")
	}
}

func TestRenderPrompt_InvalidTemplate(t *testing.T) {
	_, err := prompt.RenderPrompt("{{ .Invalid", prompt.TemplateData{})
	if err == nil {
		t.Fatal("expected error for invalid template")
	}
}
