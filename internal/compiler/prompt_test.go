package compiler_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/compiler"
	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/testing/mockllm"
	"github.com/PedroKlein/duto-ai/internal/tool"

	"google.golang.org/adk/v2/model"
)

func TestCompile_PromptFromMDFile(t *testing.T) {
	promptPath := filepath.Join("testdata", "review_prompt.md")

	wf := &config.Workflow{
		Name: "file-prompt",
		Steps: []config.Step{
			{
				ID:     "review",
				Prompt: promptPath,
			},
		},
	}

	cfg := &config.Config{
		Defaults: config.Defaults{
			Model: "test-model",
			Tools: []string{},
		},
	}

	reg := tool.NewRegistry()
	mock := mockllm.New(mockllm.Response{Text: "done"})
	resolve := func(_ string) (model.LLM, error) { return mock, nil }

	// Compile should succeed — prompt file exists.
	_, err := compiler.Compile(wf, cfg, reg, resolve, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompile_PromptFromTxtFile(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "summarize.txt")

	err := os.WriteFile(promptPath, []byte("Summarize the changes"), 0o644)
	if err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	wf := &config.Workflow{
		Name: "txt-prompt",
		Steps: []config.Step{
			{
				ID:     "summarize",
				Prompt: promptPath,
			},
		},
	}

	cfg := &config.Config{
		Defaults: config.Defaults{
			Model: "test-model",
			Tools: []string{},
		},
	}

	reg := tool.NewRegistry()
	mock := mockllm.New(mockllm.Response{Text: "done"})
	resolve := func(_ string) (model.LLM, error) { return mock, nil }

	_, err = compiler.Compile(wf, cfg, reg, resolve, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompile_PromptInlineNotTreatedAsFile(t *testing.T) {
	wf := &config.Workflow{
		Name: "inline-prompt",
		Steps: []config.Step{
			{
				ID:     "step1",
				Prompt: "Analyze the code and find bugs",
			},
		},
	}

	cfg := &config.Config{
		Defaults: config.Defaults{
			Model: "test-model",
			Tools: []string{},
		},
	}

	reg := tool.NewRegistry()
	mock := mockllm.New(mockllm.Response{Text: "done"})
	resolve := func(_ string) (model.LLM, error) { return mock, nil }

	_, err := compiler.Compile(wf, cfg, reg, resolve, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompile_PromptMissingFileReturnsError(t *testing.T) {
	wf := &config.Workflow{
		Name: "missing-file",
		Steps: []config.Step{
			{
				ID:     "step1",
				Prompt: "nonexistent/path.md",
			},
		},
	}

	cfg := &config.Config{
		Defaults: config.Defaults{
			Model: "test-model",
			Tools: []string{},
		},
	}

	reg := tool.NewRegistry()
	mock := mockllm.New(mockllm.Response{Text: "done"})
	resolve := func(_ string) (model.LLM, error) { return mock, nil }

	_, err := compiler.Compile(wf, cfg, reg, resolve, nil)
	if err == nil {
		t.Fatal("expected error for missing prompt file")
	}

	if !strings.Contains(err.Error(), "reading prompt file") {
		t.Errorf("error should mention file reading, got: %v", err)
	}
}

func TestResolvePrompt_FileContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.md")

	content := "Review the PR for security issues"

	err := os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	got, err := compiler.ResolvePrompt(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != content {
		t.Errorf("ResolvePrompt() = %q, want %q", got, content)
	}
}

func TestResolvePrompt_InlinePassthrough(t *testing.T) {
	inline := "Do something useful"

	got, err := compiler.ResolvePrompt(inline)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != inline {
		t.Errorf("ResolvePrompt() = %q, want %q", got, inline)
	}
}

func TestResolvePrompt_TrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "padded.md")

	err := os.WriteFile(path, []byte("\n  Review the code  \n\n"), 0o644)
	if err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	got, err := compiler.ResolvePrompt(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "Review the code"
	if got != want {
		t.Errorf("ResolvePrompt() = %q, want %q", got, want)
	}
}
