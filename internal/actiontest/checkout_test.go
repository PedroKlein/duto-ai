package actiontest_test

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCheckoutAttestation_FixtureCatalog(t *testing.T) {
	got := []string{
		"workflow_path_traversal",
		"workflow_path_symlink",
		"pull_request_base_head_mismatch",
	}
	want := []string{
		"workflow_path_traversal",
		"workflow_path_symlink",
		"pull_request_base_head_mismatch",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("missing checkout attestation behavior: fixture table must contain exact traversal/symlink/PR-base cases\nwant: %v\ngot:  %v", want, got)
	}
}

func TestCheckoutAttestation_RejectsWorkflowPathTraversal(t *testing.T) {
	inv := setupPrepareInvocation(t, "workflow-all-inputs.yaml", "workflow_dispatch.json", nil)

	outsidePath := filepath.Join(filepath.Dir(inv.workspace), "outside-workflow.yaml")
	writeFixtureFile(t, eventsFixturePath(t, "workflow-all-inputs.yaml"), outsidePath, 0o644)

	relTraversal := filepath.Join("..", filepath.Base(outsidePath))

	result := runPrepareForCase(t, inv, checkoutEnv(overridesWith(map[string]string{
		"INPUT_WORKFLOW": relTraversal,
	}, "workflow_dispatch", "triggering-user", "9007199254740999", inv.headSHA, defaultMainRef)))

	if result.exitCode == 0 {
		t.Fatalf("missing checkout attestation behavior: traversal workflow path %q must be rejected", relTraversal)
	}

	for _, fragment := range []string{"workflow", "traversal"} {
		if !containsAll(result.stderr, fragment) {
			t.Fatalf("missing checkout attestation behavior: traversal rejection must mention %q\nstderr:\n%s", fragment, result.stderr)
		}
	}

	exports := readGitHubEnvFile(t, inv.githubEnvPath)
	if path := strings.TrimSpace(exports["DUTO_ACTION_INPUTS_FILE"]); path != "" {
		t.Fatalf("missing checkout attestation behavior: traversal rejection must not export DUTO_ACTION_INPUTS_FILE\nexports=%v", exports)
	}
}

func TestCheckoutAttestation_RejectsWorkflowPathSymlink(t *testing.T) {
	inv := setupPrepareInvocation(t, "workflow-all-inputs.yaml", "workflow_dispatch.json", nil)

	symlinkPath := filepath.Join(inv.workspace, "workflow-link.yaml")
	if err := os.Symlink(filepath.Join(inv.workspace, "workflow.yaml"), symlinkPath); err != nil {
		t.Fatalf("checkout attestation setup failure: create workflow symlink: %v", err)
	}

	result := runPrepareForCase(t, inv, checkoutEnv(overridesWith(map[string]string{
		"INPUT_WORKFLOW": "workflow-link.yaml",
	}, "workflow_dispatch", "triggering-user", "9007199254740999", inv.headSHA, defaultMainRef)))

	if result.exitCode == 0 {
		t.Fatalf("missing checkout attestation behavior: symlink workflow path must be rejected")
	}

	for _, fragment := range []string{"workflow", "symlink"} {
		if !containsAll(result.stderr, fragment) {
			t.Fatalf("missing checkout attestation behavior: symlink rejection must mention %q\nstderr:\n%s", fragment, result.stderr)
		}
	}

	exports := readGitHubEnvFile(t, inv.githubEnvPath)
	if path := strings.TrimSpace(exports["DUTO_ACTION_INPUTS_FILE"]); path != "" {
		t.Fatalf("missing checkout attestation behavior: symlink rejection must not export DUTO_ACTION_INPUTS_FILE\nexports=%v", exports)
	}
}

func TestCheckoutAttestation_RejectsPullRequestBaseHeadMismatch(t *testing.T) {
	inv := setupPrepareInvocation(t, "workflow-all-inputs.yaml", "pull_request.json", nil)

	result := runPrepareForCase(t, inv, checkoutEnv(map[string]string{
		"GITHUB_EVENT_NAME":       "pull_request",
		"GITHUB_REPOSITORY":       defaultRepositoryOwner + "/" + defaultRepositoryName,
		"GITHUB_REPOSITORY_OWNER": defaultRepositoryOwner,
		"GITHUB_REPOSITORY_ID":    defaultRepositoryID,
		"GITHUB_ACTOR":            "triggering-user",
		"GITHUB_ACTOR_ID":         "9007199254740999",
		"GITHUB_SHA":              defaultMergeSHA,
		"GITHUB_REF":              defaultPullRequestRef,
		"GITHUB_WORKFLOW_SHA":     defaultWorkflowSHA,
		"GITHUB_RUN_ID":           defaultRunID,
	}))

	if result.exitCode == 0 {
		t.Fatalf("missing checkout attestation behavior: pull_request checkout must reject when HEAD does not match base.sha")
	}

	for _, fragment := range []string{"checkout", "head", "base"} {
		if !containsAll(result.stderr, fragment) {
			t.Fatalf("missing checkout attestation behavior: PR base mismatch must mention %q\nstderr:\n%s", fragment, result.stderr)
		}
	}

	exports := readGitHubEnvFile(t, inv.githubEnvPath)
	if path := strings.TrimSpace(exports["DUTO_ACTION_INPUTS_FILE"]); path != "" {
		t.Fatalf("missing checkout attestation behavior: PR base mismatch must not export DUTO_ACTION_INPUTS_FILE\nexports=%v", exports)
	}
}

func checkoutEnv(extra map[string]string) map[string]string {
	return extra
}

func overridesWith(extra map[string]string, eventName, actor, actorID, sha, ref string) map[string]string {
	overrides := map[string]string{
		"GITHUB_EVENT_NAME":       eventName,
		"GITHUB_REPOSITORY":       defaultRepositoryOwner + "/" + defaultRepositoryName,
		"GITHUB_REPOSITORY_OWNER": defaultRepositoryOwner,
		"GITHUB_REPOSITORY_ID":    defaultRepositoryID,
		"GITHUB_ACTOR":            actor,
		"GITHUB_ACTOR_ID":         actorID,
		"GITHUB_SHA":              sha,
		"GITHUB_REF":              ref,
		"GITHUB_WORKFLOW_SHA":     defaultWorkflowSHA,
		"GITHUB_RUN_ID":           defaultRunID,
	}

	maps.Copy(overrides, extra)

	return overrides
}
