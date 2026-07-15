package files

import (
	"fmt"
	"io/fs"
	"path/filepath"
)

// FindFiles searches for files matching a glob pattern under root/dir.
func FindFiles(root, pattern, dir string) (*FindResult, error) {
	searchRoot := root

	if dir != "" {
		resolved, err := safePath(root, dir)
		if err != nil {
			return nil, err
		}

		searchRoot = resolved
	}

	var paths []string

	truncated := false

	walkErr := filepath.WalkDir(searchRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable entries
		}

		if d.IsDir() {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil //nolint:nilerr // skip entries we cannot make relative
		}

		if matchesPattern(pattern, rel) {
			paths = append(paths, rel)
			if len(paths) >= maxFindResults {
				truncated = true
				return fs.SkipAll
			}
		}

		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walking directory: %w", walkErr)
	}

	return &FindResult{
		Paths:     paths,
		Truncated: truncated,
	}, nil
}

// matchesPattern checks if a relative path matches a glob pattern.
// It tries the basename first (for simple patterns like "*.go"), then
// falls back to matching the full relative path.
func matchesPattern(pattern, rel string) bool {
	base := filepath.Base(rel)
	patBase := filepath.Base(pattern)

	matched, err := filepath.Match(patBase, base)
	if err != nil {
		return false
	}

	return matched
}
