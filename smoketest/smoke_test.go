//go:build smoke

package smoketest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PedroKlein/duto-ai/internal/runtime"
)

func TestSmoke_MockGitHubSetup(t *testing.T) {
	mock := setupMockGitHub(t)

	if mock.server.URL == "" {
		t.Fatal("mock server has no URL")
	}

	t.Logf("Mock GitHub server at %s", mock.server.URL)
}

func TestSmoke_FixturesLoad(t *testing.T) {
	fixtures := []string{"pr.json", "diff.patch", "files.json"}

	for _, name := range fixtures {
		data := loadFixture(t, name)
		if len(data) == 0 {
			t.Errorf("%s is empty", name)
		}
	}
}

func TestSmoke_PRReview_FullWorkflow(t *testing.T) {
	// Required env vars for AI Core
	envOrSkip(t, "AI_CORE_ENDPOINT")
	envOrSkip(t, "AI_CORE_CLIENT_ID")
	envOrSkip(t, "AI_CORE_CLIENT_SECRET")
	envOrSkip(t, "AI_CORE_AUTH_URL")
	envOrSkip(t, "AI_CORE_RESOURCE_GROUP")

	// Start mock GitHub server
	mock := setupMockGitHub(t)

	// Set GHA environment
	t.Setenv("GITHUB_TOKEN", "test-token")
	t.Setenv("GITHUB_API_URL", mock.server.URL)
	t.Setenv("GITHUB_REPOSITORY", "owner/repo")
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")

	eventPath, err := filepath.Abs("testdata/event.json")
	if err != nil {
		t.Fatalf("resolving event path: %v", err)
	}

	t.Setenv("GITHUB_EVENT_PATH", eventPath)

	// Run the workflow
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	configPath := filepath.Join("testdata", "config.yaml")
	workflowPath := filepath.Join("testdata", "pr-review.yaml")

	// Verify files exist
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config not found: %v", err)
	}

	if _, err := os.Stat(workflowPath); err != nil {
		t.Fatalf("workflow not found: %v", err)
	}

	t.Log("Starting full workflow with real AI Core...")

	runErr := runtime.Run(ctx, configPath, workflowPath,
		runtime.WithGitHubBaseURL(mock.server.URL),
	)
	if runErr != nil {
		t.Fatalf("runtime.Run: %v", runErr)
	}

	// Assert: GitHub mock received GET requests (read tools used)
	if !mock.hasGET() {
		t.Error("expected at least 1 GET request to mock GitHub (read tools)")
	}

	// Assert: GitHub mock received POST requests (write tools used)
	if !mock.hasPOST() {
		t.Error("expected at least 1 POST request to mock GitHub (write tools)")
	}

	// Log all requests for debugging
	for _, req := range mock.getRequests() {
		t.Logf("  %s %s", req.Method, req.Path)
	}
}
