package files

import (
	"errors"
	"fmt"
	"os"
)

// ErrIsDirectory is returned when attempting to read a directory as a file.
var ErrIsDirectory = errors.New("path is a directory, not a file")

// ReadFile reads a file within the sandbox, truncating at maxFileSize.
func ReadFile(root, path string) (*ReadResult, error) {
	abs, err := safePath(root, path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}

	if info.IsDir() {
		return nil, fmt.Errorf("path %q: %w", path, ErrIsDirectory)
	}

	data, err := os.ReadFile(abs) //nolint:gosec // path is validated by safePath
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", path, err)
	}

	truncated := len(data) > maxFileSize
	if truncated {
		data = data[:maxFileSize]
	}

	return &ReadResult{
		Content:   string(data),
		Truncated: truncated,
	}, nil
}
