package prompt_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/prompt"
)

func TestLoadEventContext_FromEnv(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "owner/repo")
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_EVENT_PATH", "")

	ctx, err := prompt.LoadEventContext()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ctx.Repo != "owner/repo" {
		t.Errorf("repo = %q, want %q", ctx.Repo, "owner/repo")
	}

	if ctx.EventName != "pull_request" {
		t.Errorf("event = %q, want %q", ctx.EventName, "pull_request")
	}
}

func TestLoadEventContext_ParsesEventFile(t *testing.T) {
	event := map[string]any{
		"pull_request": map[string]any{
			"number": float64(123),
			"user": map[string]any{
				"login": "testuser",
			},
		},
	}

	dir := t.TempDir()
	eventPath := filepath.Join(dir, "event.json")

	data, _ := json.Marshal(event)
	if err := os.WriteFile(eventPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GITHUB_REPOSITORY", "owner/repo")
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_EVENT_PATH", eventPath)

	ctx, err := prompt.LoadEventContext()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ctx.PRNumber != 123 {
		t.Errorf("PRNumber = %d, want 123", ctx.PRNumber)
	}

	if ctx.Author != "testuser" {
		t.Errorf("Author = %q, want %q", ctx.Author, "testuser")
	}
}

func TestLoadEventContext_NoEnvVars(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("GITHUB_EVENT_NAME", "")
	t.Setenv("GITHUB_EVENT_PATH", "")

	ctx, err := prompt.LoadEventContext()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ctx.Repo != "" {
		t.Errorf("repo should be empty, got %q", ctx.Repo)
	}
}
