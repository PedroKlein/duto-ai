package files

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrPathTraversal is returned when a path attempts to escape the sandbox root.
var ErrPathTraversal = errors.New("path escapes sandbox root")

func openRoot(root string) (*os.Root, error) {
	opened, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("opening workspace root: %w", err)
	}

	return opened, nil
}

func localPath(name string) (string, error) {
	if name == "" {
		return ".", nil
	}

	cleaned := filepath.Clean(name)
	if !filepath.IsLocal(cleaned) {
		return "", fmt.Errorf("path is not local: %w", ErrPathTraversal)
	}

	return cleaned, nil
}
