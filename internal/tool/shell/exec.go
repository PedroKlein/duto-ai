package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

const processWaitDelay = 2 * time.Second

var ErrTimeout = errors.New("trusted command timed out")

func Run(ctx context.Context, policy Policy) (*RunResult, error) {
	policy, err := normalizePolicy(policy)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("shell context: %w", err)
	}

	bounded, cancel := context.WithTimeout(ctx, policy.Limit.Timeout)
	defer cancel()

	command := exec.CommandContext(bounded, policy.Executable, policy.Args...) //nolint:gosec // executable and arguments are exact validated trusted policy.
	command.Dir = policy.Workspace
	command.Env = environment(policy.Environment)
	configureProcessGroup(command)
	command.Cancel = func() error { return killProcessGroup(command.Process) }
	command.WaitDelay = processWaitDelay

	stdout := &limitedWriter{limit: policy.MaxStdoutBytes}
	stderr := &limitedWriter{limit: policy.MaxStderrBytes}
	command.Stdout = stdout
	command.Stderr = stderr

	runErr := command.Run()

	if bounded.Err() != nil {
		if errors.Is(bounded.Err(), context.DeadlineExceeded) {
			return nil, ErrTimeout
		}

		return nil, fmt.Errorf("running trusted command: %w", bounded.Err())
	}

	exitCode := 0

	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return nil, fmt.Errorf("running trusted command: %w", runErr)
		}

		exitCode = exitErr.ExitCode()
	}

	result := &RunResult{
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		ExitCode:        exitCode,
		StdoutTruncated: stdout.truncated,
		StderrTruncated: stderr.truncated,
	}
	if err := fitResult(result, policy.Limit.MaxResultBytes); err != nil {
		return nil, err
	}

	return result, nil
}

func environment(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}

	sort.Strings(names)

	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, name+"="+values[name])
	}

	return result
}

func fitResult(result *RunResult, limit int) error {
	if resultFits(result, limit) {
		return nil
	}

	result.StdoutTruncated = true
	result.StderrTruncated = true
	result.Stdout = fitField(result, limit, true)
	result.Stderr = fitField(result, limit, false)

	if !resultFits(result, limit) {
		return dtool.ErrToolResultLimit
	}

	return nil
}

func fitField(result *RunResult, limit int, stdout bool) string {
	value := result.Stderr
	if stdout {
		value = result.Stdout
	}

	low, high := 0, len(value)
	for low < high {
		middle := low + (high-low+1)/2
		if stdout {
			result.Stdout = value[:middle]
		} else {
			result.Stderr = value[:middle]
		}

		if resultFits(result, limit) {
			low = middle
		} else {
			high = middle - 1
		}
	}

	return value[:low]
}

func resultFits(result *RunResult, limit int) bool {
	encoded, err := json.Marshal(result)

	return err == nil && len(encoded) <= limit
}

type limitedWriter struct {
	data      []byte
	limit     int
	truncated bool
}

func (w *limitedWriter) Write(data []byte) (int, error) {
	remaining := max(0, w.limit-len(w.data))
	w.data = append(w.data, data[:min(len(data), remaining)]...)
	w.truncated = w.truncated || len(data) > remaining

	return len(data), nil
}

func (w *limitedWriter) String() string {
	return strings.ToValidUTF8(string(w.data), "�")
}
