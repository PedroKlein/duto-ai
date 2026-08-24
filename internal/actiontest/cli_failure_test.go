package actiontest_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCLI_RunFailureResult(t *testing.T) {
	root := repoRoot(t)
	runScript := filepath.Join(root, "action", "run.sh")

	workspace := t.TempDir()
	writeFixtureFile(t, fixturePath(t, "workflow-inputs.yaml"), filepath.Join(workspace, "workflow.yaml"), 0o644)
	writeFixtureFile(t, fixturePath(t, "config.yaml"), filepath.Join(workspace, "config.yaml"), 0o644)
	writeFixtureFile(t, fixturePath(t, "inputs-valid.json"), filepath.Join(workspace, "inputs.json"), 0o600)

	canaries := projectionCanaries(t)

	tests := []struct {
		name          string
		resultFixture string
		exitCode      int
		wantStatus    string
	}{
		{
			name:          "exit_4_emits_typed_failed_result",
			resultFixture: "result-failed.json",
			exitCode:      4,
			wantStatus:    "failed",
		},
		{
			name:          "exit_130_emits_typed_canceled_result",
			resultFixture: "result-canceled.json",
			exitCode:      130,
			wantStatus:    "cancelled", //nolint:misspell // Serialized result status is frozen by the runtime contract.
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeBinary := filepath.Join(t.TempDir(), "duto-ai-fake")

			fakeBinaryContent := "#!/usr/bin/env bash\nset -euo pipefail\ncat \"${DUTO_FAKE_RESULT_FILE}\"\nprintf '\\n'\nexit \"${DUTO_FAKE_EXIT_CODE}\"\n"
			if err := os.WriteFile(fakeBinary, []byte(fakeBinaryContent), 0o755); err != nil {
				t.Fatalf("CLI failure-result setup failure: write fake duto binary: %v", err)
			}

			resultPath := projectionFixturePath(t, tc.resultFixture)
			cmd := exec.Command("bash", runScript, fakeBinary)
			cmd.Dir = root

			cmd.Env = append(
				os.Environ(),
				"GITHUB_WORKSPACE="+workspace,
				"RUNNER_TEMP="+t.TempDir(),
				"INPUT_WORKFLOW=workflow.yaml",
				"INPUT_CONFIG=config.yaml",
				"INPUT_VERSION=v0.2.2",
				"INPUT_EVIDENCE_RETENTION_DAYS=7",
				"DUTO_ACTION_INPUTS_FILE="+filepath.Join(workspace, "inputs.json"),
				"DUTO_ACTION_EVIDENCE_DIR="+filepath.Join(t.TempDir(), "runtime-evidence"),
				"DUTO_FAKE_RESULT_FILE="+resultPath,
				"DUTO_FAKE_EXIT_CODE="+strconv.Itoa(tc.exitCode),
			)

			result := runCommand(t, cmd, nil)
			if result.exitCode != tc.exitCode {
				t.Fatalf("missing failure-result behavior: run.sh must preserve CLI exit code %d when the runtime emits a typed result\nwant exit=%d\ngot exit=%d\nstderr:\n%s", tc.exitCode, tc.exitCode, result.exitCode, result.stderr)
			}

			if !strings.HasSuffix(result.stdout, "\n") {
				t.Fatalf("missing failure-result behavior: emitted typed result must be newline-terminated before exit %d\nstdout=%q", tc.exitCode, result.stdout)
			}

			dec := json.NewDecoder(bytes.NewBufferString(result.stdout))
			dec.UseNumber()

			var payload map[string]any
			if err := dec.Decode(&payload); err != nil {
				t.Fatalf("missing failure-result behavior: stdout must contain one JSON result object before exit %d\nstdout=%q\nerr=%v", tc.exitCode, result.stdout, err)
			}

			var trailing any
			if err := dec.Decode(&trailing); err != io.EOF {
				t.Fatalf("missing failure-result behavior: stdout must contain exactly one JSON result object before exit %d\nstdout=%q", tc.exitCode, result.stdout)
			}

			if gotStatus, ok := payload["status"].(string); !ok || gotStatus != tc.wantStatus {
				t.Fatalf("missing failure-result behavior: expected status=%q in emitted typed result, got %v", tc.wantStatus, payload["status"])
			}

			for _, canary := range canaries {
				if strings.Contains(result.stderr, canary) {
					t.Fatalf("missing failure-result behavior: stderr leaked canary content %q on exit %d\nstderr:\n%s", canary, tc.exitCode, result.stderr)
				}
			}
		})
	}
}
