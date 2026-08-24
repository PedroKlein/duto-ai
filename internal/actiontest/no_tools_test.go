package actiontest_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAction_NoToolsTracer(t *testing.T) {
	root := repoRoot(t)
	runScript := filepath.Join(root, "action", "run.sh")

	workspace := t.TempDir()
	writeFixtureFile(t, fixturePath(t, "workflow-inputs.yaml"), filepath.Join(workspace, "workflow.yaml"), 0o644)
	writeFixtureFile(t, fixturePath(t, "config.yaml"), filepath.Join(workspace, "config.yaml"), 0o644)
	writeFixtureFile(t, fixturePath(t, "inputs-valid.json"), filepath.Join(workspace, "inputs.json"), 0o600)

	traceFile := filepath.Join(t.TempDir(), "fake-duto-args.txt")
	fakeBinary := filepath.Join(t.TempDir(), "duto-ai-fake")

	fakeBinaryContent := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" > \"${DUTO_FAKE_TRACE_FILE}\"\nprintf '{\"outcome\":\"completed\",\"report\":\"NO_TOOLS_CANARY\"}\\n'\n"
	if err := os.WriteFile(fakeBinary, []byte(fakeBinaryContent), 0o755); err != nil {
		t.Fatalf("no-tools tracer setup failure: write fake duto binary: %v", err)
	}

	evidenceDir := filepath.Join(t.TempDir(), "evidence")
	cmd := exec.Command("bash", runScript, fakeBinary)
	cmd.Dir = root

	env := append(
		os.Environ(),
		"GITHUB_WORKSPACE="+workspace,
		"RUNNER_TEMP="+t.TempDir(),
		"INPUT_WORKFLOW=workflow.yaml",
		"INPUT_CONFIG=config.yaml",
		"INPUT_VERSION=v0.2.2",
		"INPUT_EVIDENCE_RETENTION_DAYS=7",
		"DUTO_ACTION_INPUTS_FILE="+filepath.Join(workspace, "inputs.json"),
		"DUTO_ACTION_EVIDENCE_DIR="+evidenceDir,
		"DUTO_FAKE_TRACE_FILE="+traceFile,
	)
	cmd.Env = env

	result := runCommand(t, cmd, nil)
	if result.exitCode != 0 {
		t.Fatalf("missing no-tools Action behavior: run.sh must support one local no-tools invocation through a positional duto binary path\nexit=%d\nstderr:\n%s", result.exitCode, result.stderr)
	}

	if strings.TrimSpace(result.stdout) == "" {
		t.Fatalf("missing no-tools Action behavior: run.sh must capture one JSON result from the runtime path")
	}

	if strings.Contains(result.stderr, "NO_TOOLS_CANARY") {
		t.Fatalf("missing no-tools Action behavior: wrapper logs must stay content-free\nstderr:\n%s", result.stderr)
	}

	lines := splitNonEmptyLines(result.stdout)
	if len(lines) != 1 {
		t.Fatalf("missing no-tools Action behavior: expected exactly one result line, got %d lines\nstdout:\n%s", len(lines), result.stdout)
	}

	var payload map[string]any

	if err := json.Unmarshal([]byte(lines[0]), &payload); err != nil {
		t.Fatalf("missing no-tools Action behavior: result line must be valid JSON: %v\nline=%q", err, lines[0])
	}

	if got, ok := payload["outcome"].(string); !ok || got != "completed" {
		t.Fatalf("missing no-tools Action behavior: expected JSON outcome=completed, got %v", payload["outcome"])
	}

	trace, err := os.ReadFile(traceFile)
	if err != nil {
		t.Fatalf("missing no-tools Action behavior: positional binary was not invoked or did not leave trace: %v", err)
	}

	traceLine := string(trace)
	for _, fragment := range []string{"run", "--format", "json", "--inputs", "--evidence-directory"} {
		if !strings.Contains(traceLine, fragment) {
			t.Fatalf("missing no-tools Action behavior: expected fake binary args to include %q\nargs: %s", fragment, traceLine)
		}
	}
}

func writeFixtureFile(t *testing.T, src, dst string, mode os.FileMode) {
	t.Helper()

	content, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("no-tools tracer setup failure: read fixture %s: %v", src, err)
	}

	if err := os.WriteFile(dst, content, mode); err != nil {
		t.Fatalf("no-tools tracer setup failure: write fixture %s: %v", dst, err)
	}
}

func splitNonEmptyLines(text string) []string {
	parts := strings.Split(text, "\n")
	lines := make([]string, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}

	return lines
}
