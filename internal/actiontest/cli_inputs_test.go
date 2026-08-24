package actiontest_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type commandResult struct {
	exitCode int
	stdout   string
	stderr   string
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("input transport setup failure: unable to resolve current file path")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func fixturePath(t *testing.T, name string) string {
	t.Helper()

	return filepath.Join(repoRoot(t), "internal", "actiontest", "testdata", "no-tools", name)
}

func buildCLI(t *testing.T) string {
	t.Helper()

	binPath := filepath.Join(t.TempDir(), "duto-ai")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/duto-ai")
	cmd.Dir = repoRoot(t)

	var stderr bytes.Buffer

	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("input transport setup failure: could not build CLI binary: %v\n%s", err, stderr.String())
	}

	return binPath
}

func runCommand(t *testing.T, cmd *exec.Cmd, stdin []byte) commandResult {
	t.Helper()

	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := commandResult{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		return result
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.exitCode = exitErr.ExitCode()

		return result
	}

	t.Fatalf("input transport setup failure: could not run command %q: %v", strings.Join(cmd.Args, " "), err)

	return commandResult{}
}

func runCLI(t *testing.T, cliPath string, stdin []byte, args ...string) commandResult {
	t.Helper()

	cmd := exec.Command(cliPath, args...)
	cmd.Dir = repoRoot(t)

	return runCommand(t, cmd, stdin)
}

func containsAll(haystack string, parts ...string) bool {
	lower := strings.ToLower(haystack)
	for _, part := range parts {
		if !strings.Contains(lower, strings.ToLower(part)) {
			return false
		}
	}

	return true
}

func TestCLI_RunInputs(t *testing.T) {
	cliPath := buildCLI(t)

	t.Run("workflow=- with --inputs file", func(t *testing.T) {
		workflowBytes, err := os.ReadFile(fixturePath(t, "workflow-inputs.yaml"))
		if err != nil {
			t.Fatalf("input transport setup failure: read workflow fixture: %v", err)
		}

		result := runCLI(
			t,
			cliPath,
			workflowBytes,
			"run",
			"--format", "json",
			"--config", fixturePath(t, "config.yaml"),
			"--inputs", fixturePath(t, "inputs-missing.json"),
			"-",
		)

		if result.exitCode == 0 {
			t.Fatalf("missing input transport behavior: workflow=- with --inputs FILE must reject missing required values")
		}

		if strings.Contains(result.stderr, "unknown flag: --inputs") {
			t.Fatalf("missing input transport behavior: run must accept --inputs FILE while workflow=- owns stdin\nstderr:\n%s", result.stderr)
		}

		if strings.Contains(result.stderr, "workflow requires host-supplied inputs unavailable in the CLI") {
			t.Fatalf("missing input transport behavior: legacy unconditional input rejection must be removed when --inputs FILE is provided\nstderr:\n%s", result.stderr)
		}

		if strings.TrimSpace(result.stdout) != "" {
			t.Fatalf("missing input transport behavior: invalid input files must not emit stdout, got %q", result.stdout)
		}

		if !containsAll(result.stderr, "objective") {
			t.Fatalf("missing input transport behavior: expected a missing-value diagnostic naming input 'objective'\nstderr:\n%s", result.stderr)
		}

		if strings.Contains(strings.ToLower(result.stderr), "workflow execution error") {
			t.Fatalf("missing input transport behavior: rejected input file must fail before provider/model execution\nstderr:\n%s", result.stderr)
		}
	})

	t.Run("reject --inputs=-", func(t *testing.T) {
		result := runCLI(
			t,
			cliPath,
			[]byte(`{"objective":"from-stdin"}`),
			"run",
			"--format", "json",
			"--config", fixturePath(t, "config.yaml"),
			"--inputs", "-",
			fixturePath(t, "workflow-inputs.yaml"),
		)

		if result.exitCode == 0 {
			t.Fatalf("missing input transport behavior: --inputs=- must be rejected because workflow stdin ownership is exclusive")
		}

		if strings.Contains(result.stderr, "unknown flag: --inputs") {
			t.Fatalf("missing input transport behavior: --inputs flag must exist and explicitly reject stdin mode\nstderr:\n%s", result.stderr)
		}

		if !containsAll(result.stderr, "inputs", "stdin") {
			t.Fatalf("missing input transport behavior: rejection should explain that --inputs=- is invalid stdin transport\nstderr:\n%s", result.stderr)
		}

		if strings.Contains(strings.ToLower(result.stderr), "workflow execution error") {
			t.Fatalf("missing input transport behavior: --inputs=- must fail before provider/model execution\nstderr:\n%s", result.stderr)
		}

		if strings.TrimSpace(result.stdout) != "" {
			t.Fatalf("missing input transport behavior: --inputs=- rejection must keep stdout empty, got %q", result.stdout)
		}
	})
}

func TestCLI_ProcessContract(t *testing.T) {
	cliPath := buildCLI(t)

	tests := []struct {
		name      string
		fixture   string
		fragments []string
	}{
		{
			name:      "missing required value",
			fixture:   "inputs-missing.json",
			fragments: []string{"objective"},
		},
		{
			name:      "trailing document",
			fixture:   "inputs-trailing-document.json",
			fragments: []string{"trailing", "document"},
		},
		{
			name:      "trailing token",
			fixture:   "inputs-trailing-token.json",
			fragments: []string{"trailing", "token"},
		},
		{
			name:      "non-object root",
			fixture:   "inputs-non-object.json",
			fragments: []string{"object"},
		},
		{
			name:      "invalid utf-8",
			fixture:   "inputs-invalid-utf8.json",
			fragments: []string{"utf-8"},
		},
		{
			name:      "mistyped value",
			fixture:   "inputs-mistyped.json",
			fragments: []string{"objective", "string"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := runCLI(
				t,
				cliPath,
				nil,
				"run",
				"--format", "json",
				"--config", fixturePath(t, "config.yaml"),
				"--inputs", fixturePath(t, tc.fixture),
				fixturePath(t, "workflow-inputs.yaml"),
			)

			if result.exitCode == 0 {
				t.Fatalf("missing input transport behavior: %s must be rejected", tc.name)
			}

			if strings.Contains(result.stderr, "unknown flag: --inputs") {
				t.Fatalf("missing input transport behavior: --inputs flag must exist for %s\nstderr:\n%s", tc.name, result.stderr)
			}

			if strings.Contains(result.stderr, "workflow requires host-supplied inputs unavailable in the CLI") {
				t.Fatalf("missing input transport behavior: legacy unconditional input rejection must not shadow %s diagnostics\nstderr:\n%s", tc.name, result.stderr)
			}

			if strings.TrimSpace(result.stdout) != "" {
				t.Fatalf("missing input transport behavior: %s rejection must keep stdout empty, got %q", tc.name, result.stdout)
			}

			if strings.Contains(strings.ToLower(result.stderr), "workflow execution error") {
				t.Fatalf("missing input transport behavior: %s must fail before provider/model execution\nstderr:\n%s", tc.name, result.stderr)
			}

			for _, fragment := range tc.fragments {
				if !containsAll(result.stderr, fragment) {
					t.Fatalf("missing input transport behavior: %s diagnostic must mention %q\nstderr:\n%s", tc.name, fragment, result.stderr)
				}
			}
		})
	}
}
