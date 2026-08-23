package files

import (
	"context"
	"errors"
	"fmt"
	"io"
)

var (
	ErrIsDirectory = errors.New("path is a directory, not a file")
	ErrNotRegular  = errors.New("path is not a regular file")
)

func ReadFile(ctx context.Context, policy Policy, path string) (*ReadResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	limit, err := policy.resultLimit("files.read")
	if err != nil {
		return nil, err
	}

	cleaned, err := localPath(path)
	if err != nil {
		return nil, err
	}

	workspace, err := openRoot(policy.Root)
	if err != nil {
		return nil, err
	}
	defer workspace.Close() //nolint:errcheck // read-only root

	info, err := workspace.Stat(cleaned)
	if err != nil {
		return nil, fmt.Errorf("stating file: %w", err)
	}

	if info.IsDir() {
		return nil, ErrIsDirectory
	}

	if !info.Mode().IsRegular() {
		return nil, ErrNotRegular
	}

	file, err := workspace.Open(cleaned)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer file.Close() //nolint:errcheck // read-only file

	data, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	truncated := len(data) > maxFileSize
	if truncated {
		data = data[:maxFileSize]
	}

	return fitReadResult(data, truncated, limit)
}
