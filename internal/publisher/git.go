package publisher

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const publisherGitTimeout = 30 * time.Second

func verifyAuthoredBundle(repository, bundle, baseCommit, sourceCommit string) error { //nolint:gocyclo // fixed Git verification is intentionally linear and fail-closed
	if repository == "" {
		return ErrRejected
	}

	ctx, cancel := context.WithTimeout(context.Background(), publisherGitTimeout)
	defer cancel()

	head, err := fixedGit(ctx, repository, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != baseCommit {
		return ErrRejected
	}

	temporary, err := os.MkdirTemp("", "duto-publisher-git-")
	if err != nil {
		return fmt.Errorf("creating publisher repository: %w", err)
	}
	defer os.RemoveAll(temporary) //nolint:errcheck // temporary publisher repository contains no credentials

	_, initErr := fixedGit(ctx, "", "init", "--bare", temporary)
	if initErr != nil {
		return initErr
	}

	_, fetchBaseErr := fixedGit(ctx, temporary, "fetch", "--no-tags", "--no-write-fetch-head", repository, baseCommit+":refs/duto/base")
	if fetchBaseErr != nil {
		return fetchBaseErr
	}

	_, verifyErr := fixedGit(ctx, temporary, "bundle", "verify", bundle)
	if verifyErr != nil {
		return verifyErr
	}

	heads, err := fixedGit(ctx, temporary, "bundle", "list-heads", bundle)
	if err != nil {
		return err
	}

	fields := strings.Fields(strings.TrimSpace(string(heads)))
	if len(fields) != 2 || fields[0] != sourceCommit || fields[1] != "HEAD" {
		return ErrRejected
	}

	_, fetchSourceErr := fixedGit(ctx, temporary, "fetch", "--no-tags", "--no-write-fetch-head", bundle, sourceCommit+":refs/duto/source")
	if fetchSourceErr != nil {
		return fetchSourceErr
	}

	parents, err := fixedGit(ctx, temporary, "rev-list", "--parents", "-n", "1", sourceCommit)
	if err != nil {
		return err
	}

	if fields = strings.Fields(string(parents)); len(fields) != 2 || fields[0] != sourceCommit || fields[1] != baseCommit {
		return ErrRejected
	}

	count, err := fixedGit(ctx, temporary, "rev-list", "--count", sourceCommit, "^"+baseCommit)
	if err != nil || strings.TrimSpace(string(count)) != "1" {
		return ErrRejected
	}

	return nil
}

func fixedGit(ctx context.Context, directory string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", args...) //nolint:gosec // publisher owns every Git argv shape
	command.Dir = directory
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_PAGER=cat",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PROTOCOL_FROM_USER=0",
	}

	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("running fixed publisher Git command: %w", err)
	}

	if len(output) > 1<<20 {
		return nil, ErrRejected
	}

	return output, nil
}
