// Package files provides file system tools (read, find, grep) for AI workflows.
// All operations are sandboxed to a configured root directory to prevent traversal.
package files

import (
	"errors"
	"fmt"
	"maps"
	"os"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

const (
	maxFileSize    = 1 << 20 // 1MB
	maxFindResults = 100
	maxGrepMatches = 50
)

var ErrInvalidPolicy = errors.New("invalid file tool policy")

type Policy struct {
	Root   string
	Limits map[string]dtool.ToolLimit
}

// ReadArgs is the input schema for the files.read tool.
type ReadArgs struct {
	Path string `json:"path"` // file path relative to repo root
}

// ReadResult is the output of the files.read tool.
type ReadResult struct {
	Content   string `json:"content"`   // file content (truncated at 1MB)
	Truncated bool   `json:"truncated"` // whether content was truncated
}

// FindArgs is the input schema for the files.find tool.
type FindArgs struct {
	Pattern string `json:"pattern"` // glob pattern (e.g. "**/*.go")
	Dir     string `json:"dir"`     // subdirectory to search in (optional, defaults to root)
}

// FindResult is the output of the files.find tool.
type FindResult struct {
	Paths     []string `json:"paths"`     // matching file paths relative to root
	Truncated bool     `json:"truncated"` // whether results were truncated at max
}

// GrepArgs is the input schema for the files.grep tool.
type GrepArgs struct {
	Pattern string `json:"pattern"` // regex pattern to search for
	Path    string `json:"path"`    // file or directory to search (optional, defaults to root)
}

// GrepMatch represents a single line match.
type GrepMatch struct {
	File string `json:"file"` // file path relative to root
	Line int    `json:"line"` // line number (1-based)
	Text string `json:"text"` // matching line content
}

// GrepResult is the output of the files.grep tool.
type GrepResult struct {
	Matches   []GrepMatch `json:"matches"`   // matching lines
	Truncated bool        `json:"truncated"` // whether results were truncated at max
}

// RegisterAll creates all file tools confined to the trusted root.
func RegisterAll(reg *dtool.Registry, policy Policy) error {
	policy, err := normalizePolicy(policy)
	if err != nil {
		return err
	}

	tools := []struct {
		name   string
		create func() (tool.Tool, error)
	}{
		{"files.read", func() (tool.Tool, error) { return newReadTool(policy) }},
		{"files.find", func() (tool.Tool, error) { return newFindTool(policy) }},
		{"files.grep", func() (tool.Tool, error) { return newGrepTool(policy) }},
	}

	for _, t := range tools {
		if _, selected := policy.Limits[t.name]; !selected {
			continue
		}

		adkTool, err := t.create()
		if err != nil {
			return fmt.Errorf("creating tool %s: %w", t.name, err)
		}

		reg.Register(t.name, adkTool)
	}

	return nil
}

func normalizePolicy(policy Policy) (Policy, error) {
	root, err := os.OpenRoot(policy.Root)
	if err != nil {
		return Policy{}, fmt.Errorf("opening file tool root: %w", err)
	}

	if err := root.Close(); err != nil {
		return Policy{}, fmt.Errorf("closing file tool root: %w", err)
	}

	if len(policy.Limits) == 0 {
		return Policy{}, ErrInvalidPolicy
	}

	for name, limit := range policy.Limits {
		if name != "files.find" && name != "files.grep" && name != "files.read" || limit.MaxCalls <= 0 || limit.Timeout <= 0 || limit.MaxRequestBytes < 0 || limit.MaxResultBytes <= 0 {
			return Policy{}, fmt.Errorf("%w: %s", ErrInvalidPolicy, name)
		}
	}

	policy.Limits = maps.Clone(policy.Limits)

	return policy, nil
}

func (p Policy) resultLimit(name string) (int, error) {
	limit, exists := p.Limits[name]
	if !exists || limit.MaxResultBytes <= 0 {
		return 0, fmt.Errorf("%w: %s", ErrInvalidPolicy, name)
	}

	return limit.MaxResultBytes, nil
}

func newReadTool(policy Policy) (tool.Tool, error) {
	return functiontool.New[ReadArgs, *ReadResult](
		functiontool.Config{
			Name:        "files.read",
			Description: "Read file content by path. Returns the content (up to 1MB) and whether it was truncated.",
		},
		func(ctx agent.Context, args ReadArgs) (*ReadResult, error) {
			return ReadFile(ctx, policy, args.Path)
		},
	)
}

func newFindTool(policy Policy) (tool.Tool, error) {
	return functiontool.New[FindArgs, *FindResult](
		functiontool.Config{
			Name:        "files.find",
			Description: "Find files matching a glob pattern. Returns up to 100 matching paths.",
		},
		func(ctx agent.Context, args FindArgs) (*FindResult, error) {
			return FindFiles(ctx, policy, args.Pattern, args.Dir)
		},
	)
}

func newGrepTool(policy Policy) (tool.Tool, error) {
	return functiontool.New[GrepArgs, *GrepResult](
		functiontool.Config{
			Name:        "files.grep",
			Description: "Search file contents with a regex pattern. Returns matching lines with file and line number (max 50 matches).",
		},
		func(ctx agent.Context, args GrepArgs) (*GrepResult, error) {
			return GrepFiles(ctx, policy, args.Pattern, args.Path)
		},
	)
}
