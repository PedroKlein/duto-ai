//go:build integration

package actiontest_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/plan"
	"github.com/PedroKlein/duto-ai/internal/trust"
)

func TestM3Scenario_CompilesDutoTestFocusedAuthoring(t *testing.T) {
	dutoTestDir := m3ScenarioDutoTestDir(t)
	t.Setenv("DUTO_TEST_PROVIDER_TYPE", "custom-provider")
	t.Setenv("DUTO_TEST_PROVIDER_ENDPOINT", "https://provider.example.invalid")
	t.Setenv("DUTO_TEST_PROVIDER_RESOURCE_GROUP", "example-resource-group")
	t.Setenv("DUTO_TEST_PROVIDER_CLIENT_ID", "example-client-id")
	t.Setenv("DUTO_TEST_PROVIDER_CLIENT_SECRET", "example-client-secret")
	t.Setenv("DUTO_TEST_PROVIDER_AUTH_URL", "https://auth.example.invalid")
	t.Setenv("DUTO_TEST_MODEL_TARGET", "example-model")
	t.Setenv("DUTO_TEST_WORKSPACE_ROOT", dutoTestDir)
	t.Setenv("DUTO_TEST_EVIDENCE_DIR", filepath.Join(t.TempDir(), "evidence"))

	cfg, err := config.LoadConfig(filepath.Join(dutoTestDir, ".github/ai-workflows/config-m3.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	workflowPath := filepath.Join(dutoTestDir, ".github/ai-workflows/m3/workflow.yaml")
	workflowData, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("ReadFile(workflow) error = %v", err)
	}

	workflow, err := config.DecodeWorkflow(workflowPath, workflowData)
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	compiled, err := plan.CompileWithTrust(cfg, workflow, trust.Decision{
		Context: trust.ContextSameRepository, AdmissionID: "focused-m3", CorrelationKey: "hosted-proof",
		ControlSHA256: strings.Repeat("c", 64), Transport: "staged", CheckoutRef: "refs/heads/main",
		CheckoutSHA: strings.Repeat("1", 40), Present: true,
	})
	if err != nil {
		t.Fatalf("CompileWithTrust() error = %v", err)
	}

	if compiled.Staging() == nil || compiled.Staging().OperationSet != "branch-pr" {
		t.Fatalf("staging = %#v", compiled.Staging())
	}
}

func TestM3Scenario_HostedWorkflowUsesPinnedSeparateJobs(t *testing.T) {
	dutoTestDir := m3ScenarioDutoTestDir(t)
	content, err := os.ReadFile(filepath.Join(dutoTestDir, ".github/workflows/m3-authoring.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(hosted workflow) error = %v", err)
	}

	text := string(content)
	for _, required := range []string{"author:", "publish:", "needs: author", "permission-profile: branch-pr", "correlation_id:", "duto_action_sha:"} {
		if !strings.Contains(text, required) {
			t.Fatalf("hosted workflow missing %q", required)
		}
	}

	pin := regexp.MustCompile(`PedroKlein/duto-ai/(?:author|publish)@[0-9a-f]{40}`)
	if len(pin.FindAllString(text, -1)) != 2 {
		t.Fatalf("hosted workflow must pin both M3 Actions to full SHAs")
	}

	if issues := m3ActionForbiddenMarkerIssues("m3-authoring.yaml", text); len(issues) != 0 {
		t.Fatalf("hosted workflow markers: %v", issues)
	}
}

func m3ScenarioDutoTestDir(t *testing.T) string {
	t.Helper()

	if configured := strings.TrimSpace(os.Getenv("DUTO_TEST_DIR")); configured != "" {
		return configured
	}

	candidate := filepath.Clean(filepath.Join(repoRoot(t), "..", "duto-test", "main"))
	if _, err := os.Stat(candidate); err != nil {
		t.Skip("duto-test checkout is unavailable")
	}

	return candidate
}
