package git

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

const (
	processWaitDelay = 2 * time.Second
	literalPathspecs = "--literal-pathspecs"
	noPager          = "--no-pager"
)

var (
	ErrMissingPath           = errors.New("path is required")
	ErrMissingRef            = errors.New("ref is required")
	ErrPathTraversal         = errors.New("path escapes workspace root")
	ErrRefNotAllowed         = errors.New("ref is not allowed")
	ErrWorkingTreeNotAllowed = errors.New("working tree reads are not allowed")
	ErrTimeout               = errors.New("git command timed out")
)

func GitLog(ctx context.Context, policy Policy, args LogArgs) (*LogResult, error) {
	if !slices.Contains(policy.Refs, "HEAD") {
		return nil, ErrRefNotAllowed
	}

	if err := validatePath(policy.Root, args.Path, false); err != nil {
		return nil, err
	}

	count := args.Count
	if count <= 0 {
		count = min(10, policy.MaxLogCount)
	}

	count = min(count, policy.MaxLogCount)

	command := []string{noPager, literalPathspecs, "log", "--no-ext-diff", "--format=%H%x09%aI%x09%s", "-n", strconv.Itoa(count)}
	if args.Path != "" {
		command = append(command, "--", filepath.ToSlash(args.Path))
	}

	output, truncated, err := run(ctx, policy, "git.read.log", command...)
	if err != nil {
		return nil, err
	}

	output, truncated, err = fitCommandResult(output, truncated, policy.Limits["git.read.log"].MaxResultBytes)
	if err != nil {
		return nil, err
	}

	return &LogResult{Output: output, Truncated: truncated}, nil
}

func GitBlame(ctx context.Context, policy Policy, args BlameArgs) (*BlameResult, error) {
	if args.Path == "" {
		return nil, ErrMissingPath
	}

	if !slices.Contains(policy.Refs, "HEAD") {
		return nil, ErrRefNotAllowed
	}

	if err := validatePath(policy.Root, args.Path, true); err != nil {
		return nil, err
	}

	if args.StartLine < 0 || args.EndLine < 0 || (args.EndLine > 0 && args.EndLine < args.StartLine) {
		return nil, ErrInvalidPolicy
	}

	command := []string{noPager, literalPathspecs, "blame", "--no-progress"}
	if args.StartLine > 0 && args.EndLine > 0 {
		command = append(command, fmt.Sprintf("-L%d,%d", args.StartLine, args.EndLine))
	} else if args.StartLine > 0 {
		command = append(command, fmt.Sprintf("-L%d,", args.StartLine))
	}

	command = append(command, "HEAD", "--", filepath.ToSlash(args.Path))

	output, truncated, err := run(ctx, policy, "git.read.blame", command...)
	if err != nil {
		return nil, err
	}

	output, truncated, err = fitCommandResult(output, truncated, policy.Limits["git.read.blame"].MaxResultBytes)
	if err != nil {
		return nil, err
	}

	return &BlameResult{Output: output, Truncated: truncated}, nil
}

func GitShow(ctx context.Context, policy Policy, args ShowArgs) (*ShowResult, error) {
	if args.Ref == "" {
		return nil, ErrMissingRef
	}

	if !slices.Contains(policy.Refs, args.Ref) {
		return nil, ErrRefNotAllowed
	}

	output, truncated, err := run(ctx, policy, "git.read.show", noPager, "show", "--no-ext-diff", "--no-textconv", "--format=fuller", args.Ref, "--")
	if err != nil {
		return nil, err
	}

	output, truncated, err = fitCommandResult(output, truncated, policy.Limits["git.read.show"].MaxResultBytes)
	if err != nil {
		return nil, err
	}

	return &ShowResult{Output: output, Truncated: truncated}, nil
}

func GitDiff(ctx context.Context, policy Policy, args DiffArgs) (*DiffResult, error) {
	if !policy.AllowWorkingTree {
		return nil, ErrWorkingTreeNotAllowed
	}

	if args.Ref != "" && !slices.Contains(policy.Refs, args.Ref) {
		return nil, ErrRefNotAllowed
	}

	if err := validatePath(policy.Root, args.Path, false); err != nil {
		return nil, err
	}

	command := []string{noPager, literalPathspecs, "diff", "--no-ext-diff", "--no-textconv", "--src-prefix=a/", "--dst-prefix=b/"}
	if args.Ref != "" {
		command = append(command, args.Ref)
	}

	if args.Path != "" {
		command = append(command, "--", filepath.ToSlash(args.Path))
	}

	output, truncated, err := run(ctx, policy, "git.read.diff", command...)
	if err != nil {
		return nil, err
	}

	output, truncated, err = fitCommandResult(output, truncated, policy.Limits["git.read.diff"].MaxResultBytes)
	if err != nil {
		return nil, err
	}

	return &DiffResult{Output: output, Truncated: truncated}, nil
}

func validatePath(root, name string, mustExist bool) error {
	if name == "" {
		return nil
	}

	cleaned := filepath.Clean(name)
	if !filepath.IsLocal(cleaned) {
		return ErrPathTraversal
	}

	if !mustExist {
		return nil
	}

	workspace, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("opening Git root: %w", err)
	}
	defer workspace.Close() //nolint:errcheck // read-only root

	info, err := workspace.Stat(cleaned)
	if err != nil {
		return fmt.Errorf("stating Git path: %w", err)
	}

	if !info.Mode().IsRegular() {
		return ErrPathTraversal
	}

	return nil
}

func run(ctx context.Context, policy Policy, name string, args ...string) (output string, truncated bool, runErr error) {
	if err := ctx.Err(); err != nil {
		return "", false, fmt.Errorf("running Git command: %w", err)
	}

	limit, err := policy.resultLimit(name)
	if err != nil {
		return "", false, err
	}

	command := exec.CommandContext(ctx, "git", args...) //nolint:gosec // executable and argv shape are fixed by this package
	command.Dir = policy.Root
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_PAGER=cat",
		"GIT_TERMINAL_PROMPT=0",
	}
	configureProcessGroup(command)
	command.Cancel = func() error { return killProcessGroup(command.Process) }
	command.WaitDelay = processWaitDelay

	stdout := &limitedWriter{limit: limit}
	command.Stdout = stdout
	command.Stderr = io.Discard

	if err := command.Run(); err != nil {
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return "", false, ErrTimeout
		case ctx.Err() != nil:
			return "", false, fmt.Errorf("running Git command: %w", ctx.Err())
		default:
			return "", false, fmt.Errorf("running Git command: %w", err)
		}
	}

	return stdout.String(), stdout.truncated, nil
}

func fitCommandResult(output string, truncated bool, limit int) (fitted string, wasTruncated bool, fitErr error) {
	if commandResultFits(output, truncated, limit) {
		return output, truncated, nil
	}

	if !commandResultFits("", true, limit) {
		return "", false, dtool.ErrToolResultLimit
	}

	low, high := 0, len(output)
	for low < high {
		middle := low + (high-low+1)/2
		if commandResultFits(output[:middle], true, limit) {
			low = middle
		} else {
			high = middle - 1
		}
	}

	return output[:low], true, nil
}

func commandResultFits(output string, truncated bool, limit int) bool {
	encoded, err := json.Marshal(struct {
		Output    string `json:"output"`
		Truncated bool   `json:"truncated"`
	}{Output: output, Truncated: truncated})

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
