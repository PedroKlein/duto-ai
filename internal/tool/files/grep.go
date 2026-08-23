package files

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

const maxLineLength = 500

func GrepFiles(ctx context.Context, policy Policy, pattern, name string) (*GrepResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("searching files: %w", err)
	}

	limit, err := policy.resultLimit("files.grep")
	if err != nil {
		return nil, err
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %w", err)
	}

	cleaned, err := localPath(name)
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
		return nil, fmt.Errorf("stating search path: %w", err)
	}

	result := &GrepResult{Matches: []GrepMatch{}}
	if info.IsDir() {
		if err := grepDirectory(ctx, workspace.FS(), filepath.ToSlash(cleaned), re, limit, result); err != nil {
			return nil, err
		}
	} else {
		if !info.Mode().IsRegular() {
			return nil, ErrNotRegular
		}

		if err := appendFileMatches(ctx, workspace.FS(), filepath.ToSlash(cleaned), re, limit, result); err != nil {
			return nil, err
		}
	}

	if !fitsResult(result, limit) {
		return nil, dtool.ErrToolResultLimit
	}

	return result, nil
}

func grepDirectory(ctx context.Context, workspace fs.FS, root string, re *regexp.Regexp, limit int, result *GrepResult) error {
	walker := grepWalker{ctx: ctx, workspace: workspace, re: re, limit: limit, result: result}

	if err := fs.WalkDir(workspace, root, walker.visit); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("searching files: %w", ctxErr)
		}

		return fmt.Errorf("walking workspace: %w", err)
	}

	return nil
}

type grepWalker struct {
	ctx       context.Context
	workspace fs.FS
	re        *regexp.Regexp
	limit     int
	result    *GrepResult
}

func (w grepWalker) visit(name string, entry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return walkErr
	}

	if err := w.ctx.Err(); err != nil {
		return fmt.Errorf("searching files: %w", err)
	}

	if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
		return nil
	}

	entryInfo, err := entry.Info()
	if err != nil {
		return fmt.Errorf("stating search entry: %w", err)
	}

	if !entryInfo.Mode().IsRegular() {
		return nil
	}

	if err := appendFileMatches(w.ctx, w.workspace, name, w.re, w.limit, w.result); err != nil {
		return err
	}

	if w.result.Truncated {
		return fs.SkipAll
	}

	return nil
}

func appendFileMatches(ctx context.Context, workspace fs.FS, name string, re *regexp.Regexp, limit int, result *GrepResult) error {
	matches, sourceTruncated, err := grepFile(ctx, workspace, name, re)
	if err != nil {
		return err
	}

	for _, match := range matches {
		if len(result.Matches) >= maxGrepMatches {
			result.Truncated = true

			return nil
		}

		candidate := &GrepResult{Matches: append(append([]GrepMatch{}, result.Matches...), match)}
		if !fitsResult(candidate, limit) {
			result.Truncated = true

			return nil
		}

		result.Matches = append(result.Matches, match)
	}

	result.Truncated = result.Truncated || sourceTruncated

	return nil
}

func grepFile(ctx context.Context, workspace fs.FS, name string, re *regexp.Regexp) ([]GrepMatch, bool, error) {
	file, err := workspace.Open(name)
	if err != nil {
		return nil, false, fmt.Errorf("opening search file: %w", err)
	}
	defer file.Close() //nolint:errcheck // read-only file

	data, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil {
		return nil, false, fmt.Errorf("reading search file: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, false, fmt.Errorf("searching file: %w", err)
	}

	truncated := len(data) > maxFileSize
	if truncated {
		data = data[:maxFileSize]
	}

	matches := make([]GrepMatch, 0)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), maxFileSize)

	for line := 1; scanner.Scan(); line++ {
		if err := ctx.Err(); err != nil {
			return nil, false, fmt.Errorf("searching file: %w", err)
		}

		text := scanner.Text()
		if re.MatchString(text) {
			matches = append(matches, GrepMatch{File: strings.TrimPrefix(name, "./"), Line: line, Text: truncateLine(text)})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, false, fmt.Errorf("scanning search file: %w", err)
	}

	return matches, truncated, nil
}

func truncateLine(value string) string {
	if len(value) <= maxLineLength {
		return value
	}

	return value[:maxLineLength] + "..."
}
