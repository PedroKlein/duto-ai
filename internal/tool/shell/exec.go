package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

const (
	defaultTimeout = 30 * time.Second
	maxOutput      = 1 << 20 // 1MB output limit
)

// ErrTimeout is returned when a command exceeds the configured timeout.
var ErrTimeout = errors.New("command timed out")

// Run executes a shell command in the given root directory with a timeout.
func Run(root string, args RunArgs) (*RunResult, error) {
	if args.Command == "" {
		return &RunResult{Stderr: "command is required", ExitCode: 1}, nil
	}

	timeout := defaultTimeout
	if args.Timeout > 0 {
		timeout = time.Duration(args.Timeout) * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", args.Command) //nolint:gosec // user-provided command by design
	cmd.Dir = root

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &RunResult{
			Stdout:   truncate(stdout.String()),
			Stderr:   fmt.Sprintf("%s\n%s", truncate(stderr.String()), ErrTimeout.Error()),
			ExitCode: -1,
		}, nil
	}

	exitCode := 0

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("executing command: %w", err)
		}
	}

	return &RunResult{
		Stdout:   truncate(stdout.String()),
		Stderr:   truncate(stderr.String()),
		ExitCode: exitCode,
	}, nil
}

// truncate caps output at maxOutput bytes.
func truncate(s string) string {
	if len(s) <= maxOutput {
		return s
	}

	return s[:maxOutput]
}
