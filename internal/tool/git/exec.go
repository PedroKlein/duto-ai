package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

const (
	execTimeout = 30 * time.Second
	maxOutput   = 1 << 20 // 1MB output limit
)

// ErrMissingPath is returned when a required path argument is empty.
var ErrMissingPath = errors.New("path is required")

// ErrMissingRef is returned when a required ref argument is empty.
var ErrMissingRef = errors.New("ref is required")

// ErrTimeout is returned when a git command exceeds the timeout.
var ErrTimeout = errors.New("git command timed out")

// GitLog executes git log with the given arguments.
func GitLog(root string, args LogArgs) (string, error) {
	count := args.Count
	if count <= 0 {
		count = 10
	}

	format := args.Format
	if format == "" {
		format = "oneline"
	}

	cmdArgs := []string{"log", "--format=" + format, "-n", strconv.Itoa(count)}

	if args.Path != "" {
		cmdArgs = append(cmdArgs, "--", args.Path)
	}

	return run(root, cmdArgs...)
}

// GitBlame executes git blame for a file, optionally restricted to a line range.
func GitBlame(root string, args BlameArgs) (string, error) {
	if args.Path == "" {
		return "", ErrMissingPath
	}

	cmdArgs := []string{"blame"}

	if args.StartLine > 0 && args.EndLine > 0 {
		cmdArgs = append(cmdArgs, fmt.Sprintf("-L%d,%d", args.StartLine, args.EndLine))
	} else if args.StartLine > 0 {
		cmdArgs = append(cmdArgs, fmt.Sprintf("-L%d,", args.StartLine))
	}

	cmdArgs = append(cmdArgs, args.Path)

	return run(root, cmdArgs...)
}

// GitShow executes git show for a ref.
func GitShow(root string, args ShowArgs) (string, error) {
	if args.Ref == "" {
		return "", ErrMissingRef
	}

	return run(root, "show", args.Ref)
}

// GitDiff executes git diff.
func GitDiff(root string, args DiffArgs) (string, error) {
	cmdArgs := []string{"diff"}

	if args.Ref != "" {
		cmdArgs = append(cmdArgs, args.Ref)
	}

	if args.Path != "" {
		cmdArgs = append(cmdArgs, "--", args.Path)
	}

	return run(root, cmdArgs...)
}

// run executes a git command in root with a timeout, returning stdout.
func run(root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // args are constructed internally
	cmd.Dir = root

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("%w after %s", ErrTimeout, execTimeout)
		}

		return "", fmt.Errorf("git %v: %s: %w", args, stderr.String(), err)
	}

	output := stdout.String()
	if len(output) > maxOutput {
		output = output[:maxOutput]
	}

	return output, nil
}
