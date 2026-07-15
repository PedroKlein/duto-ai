package files

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

// GrepFiles searches file contents for a regex pattern.
func GrepFiles(root, pattern, path string) (*GrepResult, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex %q: %w", pattern, err)
	}

	searchRoot := root

	if path != "" {
		searchPath, pathErr := safePath(root, path)
		if pathErr != nil {
			return nil, pathErr
		}

		searchRoot = searchPath
	}

	info, err := os.Stat(searchRoot)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}

	if !info.IsDir() {
		return grepSingleTarget(root, searchRoot, re)
	}

	return grepDirectory(root, searchRoot, re)
}

func grepSingleTarget(root, absPath string, re *regexp.Regexp) (*GrepResult, error) {
	rel, _ := filepath.Rel(root, absPath)

	matches, err := grepSingleFile(absPath, rel, re)
	if err != nil {
		return nil, err
	}

	truncated := len(matches) > maxGrepMatches
	if truncated {
		matches = matches[:maxGrepMatches]
	}

	return &GrepResult{
		Matches:   matches,
		Truncated: truncated,
	}, nil
}

func grepDirectory(root, searchRoot string, re *regexp.Regexp) (*GrepResult, error) {
	var matches []GrepMatch

	truncated := false

	walkErr := filepath.WalkDir(searchRoot, func(fpath string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // skip unreadable or directories
		}

		rel, relErr := filepath.Rel(root, fpath)
		if relErr != nil {
			return nil //nolint:nilerr // skip
		}

		fileMatches, grepErr := grepSingleFile(fpath, rel, re)
		if grepErr != nil {
			return nil //nolint:nilerr // skip unreadable files
		}

		matches = append(matches, fileMatches...)
		if len(matches) >= maxGrepMatches {
			matches = matches[:maxGrepMatches]
			truncated = true

			return fs.SkipAll
		}

		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walking directory: %w", walkErr)
	}

	return &GrepResult{
		Matches:   matches,
		Truncated: truncated,
	}, nil
}

// grepSingleFile searches one file for regex matches.
func grepSingleFile(absPath, relPath string, re *regexp.Regexp) ([]GrepMatch, error) {
	f, err := os.Open(absPath) //nolint:gosec // path is validated by safePath
	if err != nil {
		return nil, fmt.Errorf("opening %q: %w", relPath, err)
	}
	defer f.Close() //nolint:errcheck // read-only file

	var matches []GrepMatch

	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++

		line := scanner.Text()
		if re.MatchString(line) {
			matches = append(matches, GrepMatch{
				File: relPath,
				Line: lineNum,
				Text: truncateLine(line),
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return matches, fmt.Errorf("scanning %q: %w", relPath, err)
	}

	return matches, nil
}

const maxLineLength = 500

// truncateLine caps a line at maxLineLength characters.
func truncateLine(s string) string {
	if len(s) <= maxLineLength {
		return s
	}

	return s[:maxLineLength] + "..."
}
