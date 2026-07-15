package shell_test

import (
	"runtime"
	"strings"
	"testing"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
	"github.com/PedroKlein/duto-ai/internal/tool/shell"
)

func TestRegisterAll(t *testing.T) {
	root := t.TempDir()
	reg := dtool.NewRegistry()

	if err := shell.RegisterAll(reg, root); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	names := reg.Names()
	if len(names) != 1 || names[0] != "shell.run" {
		t.Errorf("registered = %v, want [shell.run]", names)
	}
}

func TestRun_SimpleCommand(t *testing.T) {
	root := t.TempDir()

	result, err := shell.Run(root, shell.RunArgs{Command: "echo hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}

	if strings.TrimSpace(result.Stdout) != "hello" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "hello\n")
	}
}

func TestRun_CwdIsLocked(t *testing.T) {
	root := t.TempDir()

	result, err := shell.Run(root, shell.RunArgs{Command: "pwd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.TrimSpace(result.Stdout) != root {
		t.Errorf("cwd = %q, want %q", strings.TrimSpace(result.Stdout), root)
	}
}

func TestRun_NonZeroExit(t *testing.T) {
	root := t.TempDir()

	result, err := shell.Run(root, shell.RunArgs{Command: "exit 42"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ExitCode != 42 {
		t.Errorf("exit code = %d, want 42", result.ExitCode)
	}
}

func TestRun_Stderr(t *testing.T) {
	root := t.TempDir()

	result, err := shell.Run(root, shell.RunArgs{Command: "echo error >&2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.TrimSpace(result.Stderr) != "error" {
		t.Errorf("stderr = %q, want %q", result.Stderr, "error\n")
	}
}

func TestRun_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep command differs on windows")
	}

	root := t.TempDir()

	result, err := shell.Run(root, shell.RunArgs{
		Command: "sleep 10",
		Timeout: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ExitCode != -1 {
		t.Errorf("exit code = %d, want -1 for timeout", result.ExitCode)
	}

	if !strings.Contains(result.Stderr, "timed out") {
		t.Errorf("stderr should mention timeout, got: %q", result.Stderr)
	}
}

func TestRun_EmptyCommand(t *testing.T) {
	root := t.TempDir()

	result, err := shell.Run(root, shell.RunArgs{Command: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1 for empty command", result.ExitCode)
	}
}
