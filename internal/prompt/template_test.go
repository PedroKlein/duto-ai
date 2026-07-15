package prompt_test

import (
	"testing"

	"github.com/PedroKlein/duto-ai/internal/prompt"
)

func TestRenderTemplate_EventContext(t *testing.T) {
	eventCtx := &prompt.EventContext{
		Repo:      "octocat/hello-world",
		PRNumber:  42,
		Author:    "contributor",
		EventName: "pull_request",
	}

	raw := "Review PR #{{ .Event.PRNumber }} by {{ .Event.Author }} in {{ .Event.Owner }}/{{ .Event.Repo }}"

	got, err := prompt.RenderTemplate(raw, eventCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "Review PR #42 by contributor in octocat/hello-world"
	if got != want {
		t.Errorf("RenderTemplate() = %q, want %q", got, want)
	}
}

func TestRenderTemplate_EnvVar(t *testing.T) {
	t.Setenv("MY_TEST_VAR", "hello123")

	raw := "Value is {{ .Env.MY_TEST_VAR }}"

	got, err := prompt.RenderTemplate(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "Value is hello123"
	if got != want {
		t.Errorf("RenderTemplate() = %q, want %q", got, want)
	}
}

func TestRenderTemplate_NoTemplates(t *testing.T) {
	raw := "Plain text without any templates"

	got, err := prompt.RenderTemplate(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != raw {
		t.Errorf("RenderTemplate() = %q, want %q", got, raw)
	}
}

func TestRenderTemplate_NilEventContext(t *testing.T) {
	raw := "PR {{ .Event.PRNumber }} by {{ .Event.Author }}"

	got, err := prompt.RenderTemplate(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Missing keys should render as zero values.
	want := "PR 0 by "
	if got != want {
		t.Errorf("RenderTemplate() = %q, want %q", got, want)
	}
}

func TestRenderTemplate_InvalidTemplate(t *testing.T) {
	raw := "Broken {{ .Unclosed"

	got, err := prompt.RenderTemplate(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Invalid templates should pass through unchanged.
	if got != raw {
		t.Errorf("RenderTemplate() = %q, want original %q", got, raw)
	}
}
