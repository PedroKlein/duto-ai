package actiontest_test

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const (
	projectorResultFileEnv         = "DUTO_ACTION_RESULT_FILE"
	projectorRuntimeEvidenceDirEnv = "DUTO_ACTION_RUNTIME_EVIDENCE_DIR"
	projectorActionEvidenceDirEnv  = "DUTO_ACTION_ACTION_EVIDENCE_DIR"
	projectorWorkflowDigestEnv     = "DUTO_ACTION_WORKFLOW_DIGEST_PREFIX"
)

type projectorInvocation struct {
	resultPath           string
	runtimeEvidenceDir   string
	actionEvidenceDir    string
	githubOutputPath     string
	githubSummaryPath    string
	runnerTempPath       string
	workflowDigestPrefix string
}

func TestResultProjection_ClosedSevenOutputs(t *testing.T) {
	tests := []struct {
		name          string
		resultFixture string
		want          map[string]string
	}{
		{
			name:          "failed_result_and_failed_step",
			resultFixture: "result-failed.json",
			want: map[string]string{
				"status":                 "failed",
				"outcome":                "completed",
				"run-id":                 "run-failure-canary",
				"failed-step":            "compile",
				"clarification-required": "false",
			},
		},
		{
			name:          "awaiting_input_sets_clarification_boolean",
			resultFixture: "result-awaiting-input.json",
			want: map[string]string{
				"status":                 "succeeded",
				"outcome":                "awaiting_input",
				"run-id":                 "run-awaiting-input-canary",
				"failed-step":            "",
				"clarification-required": "true",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inv := setupProjectorInvocation(t, tc.resultFixture)

			result := runProjector(t, inv, nil)
			if result.exitCode != 0 {
				t.Fatalf("missing result projection behavior: action/project.sh must project one typed result into the closed seven Action outputs\nexit=%d\nstderr:\n%s", result.exitCode, result.stderr)
			}

			outputs := readGitHubEnvFile(t, inv.githubOutputPath)
			keys := mapKeysSorted(outputs)

			wantKeys := []string{"clarification-required", "evidence-path", "failed-step", "outcome", "result-path", "run-id", "status"}
			if !reflect.DeepEqual(keys, wantKeys) {
				t.Fatalf("missing result projection behavior: GITHUB_OUTPUT must contain exactly the seven closed metadata fields\nwant keys: %v\ngot keys:  %v\noutputs:   %v", wantKeys, keys, outputs)
			}

			for key, want := range tc.want {
				if got := outputs[key]; got != want {
					t.Fatalf("missing result projection behavior: output %q mismatch\nwant: %q\ngot:  %q\noutputs=%v", key, want, got, outputs)
				}
			}

			if got := outputs["result-path"]; got != inv.resultPath {
				t.Fatalf("missing result projection behavior: output result-path must be the local typed result path\nwant: %q\ngot:  %q", inv.resultPath, got)
			}

			if got := outputs["evidence-path"]; got != inv.actionEvidenceDir {
				t.Fatalf("missing result projection behavior: output evidence-path must be the Action evidence directory\nwant: %q\ngot:  %q", inv.actionEvidenceDir, got)
			}

			outputBytes, err := os.ReadFile(inv.githubOutputPath)
			if err != nil {
				t.Fatalf("result projection setup failure: read GITHUB_OUTPUT: %v", err)
			}

			assertNoCanaryLeak(t, "GITHUB_OUTPUT", outputBytes, projectionCanaries(t))
		})
	}
}

func TestStepSummary_ContentFreeAndBounded(t *testing.T) {
	inv := setupProjectorInvocation(t, "result-awaiting-input.json")

	result := runProjector(t, inv, nil)
	if result.exitCode != 0 {
		t.Fatalf("missing step summary behavior: action/project.sh must write a bounded content-free summary\nexit=%d\nstderr:\n%s", result.exitCode, result.stderr)
	}

	summary, err := os.ReadFile(inv.githubSummaryPath)
	if err != nil {
		t.Fatalf("missing step summary behavior: expected GITHUB_STEP_SUMMARY at %s: %v", inv.githubSummaryPath, err)
	}

	if len(summary) == 0 {
		t.Fatalf("missing step summary behavior: summary must not be empty")
	}

	if len(summary) > 8*1024 {
		t.Fatalf("missing step summary behavior: summary must stay under the 8 KiB project ceiling, got %d bytes", len(summary))
	}

	text := string(summary)
	for _, fragment := range []string{"status", "outcome", "clarification"} {
		if !containsAll(text, fragment) {
			t.Fatalf("missing step summary behavior: summary must include %q as safe metadata\nsummary:\n%s", fragment, text)
		}
	}

	assertNoCanaryLeak(t, "GITHUB_STEP_SUMMARY", summary, projectionCanaries(t))
}

func setupProjectorInvocation(t *testing.T, resultFixture string) projectorInvocation {
	t.Helper()

	scratch := t.TempDir()
	resultPath := filepath.Join(scratch, "result.json")
	copyProjectionFixture(t, resultFixture, resultPath, 0o600)

	runtimeEvidenceDir := filepath.Join(scratch, "runtime-evidence")
	if err := os.MkdirAll(runtimeEvidenceDir, 0o755); err != nil {
		t.Fatalf("projector setup failure: create runtime evidence directory: %v", err)
	}

	copyProjectionFixture(t, "source-events.jsonl", filepath.Join(runtimeEvidenceDir, "events.jsonl"), 0o600)
	copyProjectionFixture(t, "source-result.json", filepath.Join(runtimeEvidenceDir, "result.json"), 0o600)
	copyProjectionFixture(t, "source-summary.md", filepath.Join(runtimeEvidenceDir, "summary.md"), 0o600)
	copyProjectionFixture(t, "source-manifest.json", filepath.Join(runtimeEvidenceDir, "manifest.json"), 0o600)

	githubOutputPath := filepath.Join(scratch, "github-output.txt")
	githubSummaryPath := filepath.Join(scratch, "github-step-summary.md")

	if err := os.WriteFile(githubOutputPath, nil, 0o600); err != nil {
		t.Fatalf("projector setup failure: create GITHUB_OUTPUT: %v", err)
	}

	if err := os.WriteFile(githubSummaryPath, nil, 0o600); err != nil {
		t.Fatalf("projector setup failure: create GITHUB_STEP_SUMMARY: %v", err)
	}

	return projectorInvocation{
		resultPath:           resultPath,
		runtimeEvidenceDir:   runtimeEvidenceDir,
		actionEvidenceDir:    filepath.Join(scratch, "action-evidence"),
		githubOutputPath:     githubOutputPath,
		githubSummaryPath:    githubSummaryPath,
		runnerTempPath:       filepath.Join(scratch, "runner-temp"),
		workflowDigestPrefix: "cafebabe",
	}
}

func runProjector(t *testing.T, inv projectorInvocation, extra map[string]string) commandResult {
	t.Helper()

	projectScript := filepath.Join(repoRoot(t), "action", "project.sh")
	cmd := exec.Command("bash", projectScript)
	cmd.Dir = repoRoot(t)

	env := append([]string{}, os.Environ()...)

	merged := map[string]string{
		"GITHUB_OUTPUT":                 inv.githubOutputPath,
		"GITHUB_STEP_SUMMARY":           inv.githubSummaryPath,
		"RUNNER_TEMP":                   inv.runnerTempPath,
		"INPUT_EVIDENCE_RETENTION_DAYS": "7",
		projectorResultFileEnv:          inv.resultPath,
		projectorRuntimeEvidenceDirEnv:  inv.runtimeEvidenceDir,
		projectorActionEvidenceDirEnv:   inv.actionEvidenceDir,
		projectorWorkflowDigestEnv:      inv.workflowDigestPrefix,
	}
	maps.Copy(merged, extra)

	keys := mapKeysSorted(merged)
	for _, key := range keys {
		env = append(env, key+"="+merged[key])
	}

	cmd.Env = env

	return runCommand(t, cmd, nil)
}

func copyProjectionFixture(t *testing.T, fixtureName, target string, mode os.FileMode) {
	t.Helper()

	content, err := os.ReadFile(projectionFixturePath(t, fixtureName))
	if err != nil {
		t.Fatalf("projector setup failure: read fixture %s: %v", fixtureName, err)
	}

	if err := os.WriteFile(target, content, mode); err != nil {
		t.Fatalf("projector setup failure: write fixture %s: %v", fixtureName, err)
	}
}

func projectionFixturePath(t *testing.T, name string) string {
	t.Helper()

	return filepath.Join(repoRoot(t), "internal", "actiontest", "testdata", "projection", name)
}

func projectionCanaries(t *testing.T) []string {
	t.Helper()

	content, err := os.ReadFile(projectionFixturePath(t, "canaries.txt"))
	if err != nil {
		t.Fatalf("projector setup failure: read canary fixture: %v", err)
	}

	canaries := make([]string, 0)

	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		canaries = append(canaries, line)
	}

	if len(canaries) == 0 {
		t.Fatalf("projector setup failure: canary fixture must contain at least one marker")
	}

	return canaries
}

func assertNoCanaryLeak(t *testing.T, location string, content []byte, canaries []string) {
	t.Helper()

	text := string(content)
	for _, canary := range canaries {
		if strings.Contains(text, canary) {
			t.Fatalf("missing redaction behavior: %s leaked canary %q\ncontent:\n%s", location, canary, text)
		}
	}
}

func mapKeysSorted[V any](in map[string]V) []string {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}
