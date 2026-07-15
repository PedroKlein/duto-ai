package files

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrPathTraversal is returned when a path attempts to escape the sandbox root.
var ErrPathTraversal = errors.New("path escapes sandbox root")

// safePath resolves a relative path within the sandbox root.
// It rejects paths that escape the root via ../ or absolute paths.
func safePath(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute path %q: %w", rel, ErrPathTraversal)
	}

	joined := filepath.Join(root, rel)
	resolved := filepath.Clean(joined)

	// Ensure the resolved path is within root.
	if !strings.HasPrefix(resolved, filepath.Clean(root)+string(filepath.Separator)) && resolved != filepath.Clean(root) {
		return "", fmt.Errorf("path %q resolves outside root: %w", rel, ErrPathTraversal)
	}

	return resolved, nil
}
