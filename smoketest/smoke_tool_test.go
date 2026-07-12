//go:build smoke

package smoketest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/PedroKlein/duto-ai/internal/runtime"
)

func TestSmoke_SingleTool(t *testing.T) {
	envOrSkip(t, "AI_CORE_ENDPOINT")
	envOrSkip(t, "AI_CORE_CLIENT_ID")
	envOrSkip(t, "AI_CORE_CLIENT_SECRET")
	envOrSkip(t, "AI_CORE_AUTH_URL")
	envOrSkip(t, "AI_CORE_RESOURCE_GROUP")

	mock := setupMockGitHub(t)
	t.Setenv("GITHUB_TOKEN", "test-token")
	t.Setenv("GITHUB_API_URL", mock.server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err := runtime.Run(ctx,
		filepath.Join("testdata", "config.yaml"),
		filepath.Join("testdata", "single_tool_workflow.yaml"),
		runtime.WithGitHubBaseURL(mock.server.URL),
	)
	if err != nil {
		t.Fatalf("runtime.Run: %v", err)
	}

	if !mock.hasGET() {
		t.Error("expected at least 1 GET to mock GitHub")
	}

	t.Log("Single-tool workflow completed, mock received requests")
}
