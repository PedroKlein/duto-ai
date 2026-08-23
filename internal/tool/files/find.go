package files

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

func FindFiles(ctx context.Context, policy Policy, pattern, dir string) (*FindResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("finding files: %w", err)
	}

	limit, err := policy.resultLimit("files.find")
	if err != nil {
		return nil, err
	}

	if _, patternErr := path.Match(pattern, "candidate"); patternErr != nil {
		return nil, fmt.Errorf("parsing file pattern: %w", patternErr)
	}

	base, err := localPath(dir)
	if err != nil {
		return nil, err
	}

	workspace, err := openRoot(policy.Root)
	if err != nil {
		return nil, err
	}
	defer workspace.Close() //nolint:errcheck // read-only root

	result := &FindResult{Paths: []string{}}
	walker := findWalker{ctx: ctx, pattern: pattern, limit: limit, result: result}

	walkErr := fs.WalkDir(workspace.FS(), filepath.ToSlash(base), walker.visit)
	if walkErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("finding files: %w", ctxErr)
		}

		return nil, fmt.Errorf("walking workspace: %w", walkErr)
	}

	if !fitsResult(result, limit) {
		return nil, dtool.ErrToolResultLimit
	}

	return result, nil
}

type findWalker struct {
	ctx     context.Context
	pattern string
	limit   int
	result  *FindResult
}

func (w findWalker) visit(name string, entry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return walkErr
	}

	if err := w.ctx.Err(); err != nil {
		return fmt.Errorf("finding files: %w", err)
	}

	if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
		return nil
	}

	info, err := entry.Info()
	if err != nil {
		return fmt.Errorf("stating file entry: %w", err)
	}

	if !info.Mode().IsRegular() {
		return nil
	}

	rel := strings.TrimPrefix(name, "./")
	if !matchesPattern(w.pattern, rel) {
		return nil
	}

	if len(w.result.Paths) >= maxFindResults {
		w.result.Truncated = true

		return fs.SkipAll
	}

	candidate := &FindResult{Paths: append(append([]string{}, w.result.Paths...), rel)}
	if !fitsResult(candidate, w.limit) {
		w.result.Truncated = true

		return fs.SkipAll
	}

	w.result.Paths = append(w.result.Paths, rel)

	return nil
}

func matchesPattern(pattern, rel string) bool {
	candidate := path.Base(filepath.ToSlash(rel))
	if strings.Contains(pattern, "/") {
		candidate = filepath.ToSlash(rel)
	}

	matched, _ := path.Match(pattern, candidate)

	return matched
}
