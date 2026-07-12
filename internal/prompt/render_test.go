package prompt_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/prompt"
)

func TestRenderPrompt(t *testing.T) {
	tests := []struct {
		name     string
		tmpl     string
		data     prompt.TemplateData
		expected string
		wantErr  bool
	}{
		{
			name: "inline template with step output",
			tmpl: "Analyze: {{ .Steps.gather.Output }}",
			data: prompt.TemplateData{
				Steps: map[string]prompt.StepOutput{
					"gather": {Output: "found 3 issues"},
				},
			},
			expected: "Analyze: found 3 issues",
		},
		{
			name:     "plain text no template",
			tmpl:     "plain text prompt",
			data:     prompt.TemplateData{},
			expected: "plain text prompt",
		},
		{
			name:    "invalid template syntax",
			tmpl:    "{{ .Invalid",
			data:    prompt.TemplateData{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := prompt.RenderPrompt(tt.tmpl, tt.data)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
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
