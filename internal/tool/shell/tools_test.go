package shell_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"google.golang.org/genai"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
	"github.com/PedroKlein/duto-ai/internal/tool/shell"
)

func TestRegisterAll_RequiresExplicitTrustedPolicy(t *testing.T) {
	registry := dtool.NewRegistry()

	err := shell.RegisterAll(registry, shell.Policy{})
	if !errors.Is(err, shell.ErrInvalidPolicy) {
		t.Fatalf("RegisterAll() error = %v, want ErrInvalidPolicy", err)
	}

	if names := registry.Names(); len(names) != 0 {
		t.Fatalf("registered tools = %v, want none", names)
	}
}

func TestRegisterAll_HidesCommandAndTimeoutFromModel(t *testing.T) {
	policy := testPolicy(t, "printf '%s' trusted")
	registry := dtool.NewRegistry()

	if err := shell.RegisterAll(registry, policy); err != nil {
		t.Fatalf("RegisterAll() error = %v", err)
	}

	current, ok := registry.Get("shell.run")
	if !ok {
		t.Fatal("shell.run was not registered")
	}

	declarer, ok := current.(interface {
		Declaration() *genai.FunctionDeclaration
	})
	if !ok {
		t.Fatal("shell.run has no declaration")
	}

	encoded, err := json.Marshal(declarer.Declaration().ParametersJsonSchema)
	if err != nil {
		t.Fatalf("encoding declaration: %v", err)
	}

	for _, forbidden := range []string{"command", "timeout", "executable", "argv"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("model-visible schema contains %q: %s", forbidden, encoded)
		}
	}
}

func TestRun_UsesOnlyTrustedCommandWorkspaceAndEnvironment(t *testing.T) {
	t.Setenv("BLOCKED", "host-secret")

	policy := testPolicy(t, `printf '%s\n%s\n%s\n%s' "$PWD" "$ALLOWED" "${BLOCKED-unset}" "$1"`, "fixed-arg")
	policy.Environment = map[string]string{"ALLOWED": "trusted-value"}

	result, err := shell.Run(t.Context(), policy)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantWorkspace, err := filepath.EvalSymlinks(policy.Workspace)
	if err != nil {
		t.Fatal(err)
	}

	want := strings.Join([]string{wantWorkspace, "trusted-value", "unset", "fixed-arg"}, "\n")
	if strings.TrimSpace(result.Stdout) != want {
		t.Fatalf("stdout = %q, want %q", result.Stdout, want)
	}

	if result.ExitCode != 0 || result.StdoutTruncated || result.StderrTruncated {
		t.Fatalf("result = %#v", result)
	}
}

func TestRun_BoundsStdoutStderrAndResult(t *testing.T) {
	policy := testPolicy(t, `printf 'abcdefghijklmnop'; printf 'qrstuvwxyz' >&2`)
	policy.MaxStdoutBytes = 7
	policy.MaxStderrBytes = 5
	policy.Limit.MaxResultBytes = 160

	result, err := shell.Run(t.Context(), policy)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Stdout != "abcdefg" || result.Stderr != "qrstu" {
		t.Fatalf("bounded output = %#v", result)
	}

	if !result.StdoutTruncated || !result.StderrTruncated {
		t.Fatalf("truncation flags = %#v", result)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("encoding result: %v", err)
	}

	if len(encoded) > policy.Limit.MaxResultBytes {
		t.Fatalf("encoded result bytes = %d, limit = %d", len(encoded), policy.Limit.MaxResultBytes)
	}
}

func TestRun_CancelledContextStartsNoProcess(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "started")
	policy := testPolicy(t, `printf started > "$1"`, marker)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := shell.Run(ctx, policy)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}

	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("process start marker error = %v, want not exist", statErr)
	}
}

func TestRun_NonZeroExitIsTypedResult(t *testing.T) {
	policy := testPolicy(t, `printf problem >&2; exit 42`)

	result, err := shell.Run(t.Context(), policy)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.ExitCode != 42 || result.Stderr != "problem" {
		t.Fatalf("result = %#v", result)
	}
}

func testPolicy(t *testing.T, command string, args ...string) shell.Policy {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("trusted shell fixture requires /bin/sh")
	}

	return shell.Policy{
		Executable: "/bin/sh",
		Args:       append([]string{"-c", command, "shell.run"}, args...),
		Workspace:  t.TempDir(),
		Environment: map[string]string{
			"LC_ALL": "C",
		},
		MaxStdoutBytes: 1024,
		MaxStderrBytes: 1024,
		Limit: dtool.ToolLimit{
			MaxCalls:        2,
			Timeout:         2 * time.Second,
			MaxRequestBytes: 64,
			MaxResultBytes:  4096,
		},
	}
}
