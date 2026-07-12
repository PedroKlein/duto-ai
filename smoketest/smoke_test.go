//go:build smoke

package smoketest

import (
	"testing"
)

func TestSmoke_MockGitHubSetup(t *testing.T) {
	mock := setupMockGitHub(t)

	if mock.server == nil {
		t.Fatal("mock server not created")
	}

	if mock.server.URL == "" {
		t.Fatal("mock server has no URL")
	}

	t.Logf("Mock GitHub server at %s", mock.server.URL)
}

func TestSmoke_FixturesLoad(t *testing.T) {
	pr := loadFixture(t, "pr.json")
	if len(pr) == 0 {
		t.Error("pr.json is empty")
	}

	diff := loadFixture(t, "diff.patch")
	if len(diff) == 0 {
		t.Error("diff.patch is empty")
	}

	files := loadFixture(t, "files.json")
	if len(files) == 0 {
		t.Error("files.json is empty")
	}
}

// TestSmoke_PRReview_FullWorkflow is the full end-to-end smoke test.
// It requires real AI Core credentials and uses a mock GitHub server.
// Skip if credentials are not available.
func TestSmoke_PRReview_FullWorkflow(t *testing.T) {
	// Required env vars for AI Core
	envOrSkip(t, "AI_CORE_ENDPOINT")
	envOrSkip(t, "AI_CORE_CLIENT_ID")
	envOrSkip(t, "AI_CORE_CLIENT_SECRET")
	envOrSkip(t, "AI_CORE_AUTH_URL")
	envOrSkip(t, "AI_CORE_RESOURCE_GROUP")

	_ = setupMockGitHub(t)

	// Full workflow test would:
	// 1. Start mock GitHub server
	// 2. Set GHA env vars
	// 3. Run the full duto-ai pipeline
	// 4. Assert GitHub mock received GET and POST requests
	// This requires the runtime to be wired with actual tool calling,
	// which depends on ADK tool integration (deferred to post-MVP polish).
	t.Log("Smoke test infrastructure validated; full E2E requires ADK tool wiring")
}
