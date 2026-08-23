// Package git provides git tools (log, blame, show, diff) for AI workflows.
// All commands are executed via os/exec with a 30s timeout, sandboxed to a repo root.
package git

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

var ErrInvalidPolicy = errors.New("invalid git tool policy")

type Policy struct {
	Root             string
	Refs             []string
	AllowWorkingTree bool
	MaxLogCount      int
	Limits           map[string]dtool.ToolLimit
}

// LogArgs is the input schema for the git.read.log tool.
type LogArgs struct {
	Count int    `json:"count"`          // number of commits to show (default: 10)
	Path  string `json:"path,omitempty"` // limit to commits affecting this path
}

// LogResult is the output of the git.read.log tool.
type LogResult struct {
	Output    string `json:"output"`
	Truncated bool   `json:"truncated"`
}

// BlameArgs is the input schema for the git.read.blame tool.
type BlameArgs struct {
	Path      string `json:"path"`                 // file to blame
	StartLine int    `json:"start_line,omitempty"` // start of line range (optional)
	EndLine   int    `json:"end_line,omitempty"`   // end of line range (optional)
}

// BlameResult is the output of the git.read.blame tool.
type BlameResult struct {
	Output    string `json:"output"`
	Truncated bool   `json:"truncated"`
}

// ShowArgs is the input schema for the git.read.show tool.
type ShowArgs struct {
	Ref string `json:"ref"` // commit hash or ref to show
}

// ShowResult is the output of the git.read.show tool.
type ShowResult struct {
	Output    string `json:"output"`
	Truncated bool   `json:"truncated"`
}

// DiffArgs is the input schema for the git.read.diff tool.
type DiffArgs struct {
	Ref  string `json:"ref,omitempty"`  // compare against this ref (default: working tree)
	Path string `json:"path,omitempty"` // limit diff to this path
}

// DiffResult is the output of the git.read.diff tool.
type DiffResult struct {
	Output    string `json:"output"`
	Truncated bool   `json:"truncated"`
}

// RegisterAll creates all Git read tools under trusted policy.
func RegisterAll(reg *dtool.Registry, policy Policy) error {
	policy, err := normalizePolicy(policy)
	if err != nil {
		return err
	}

	tools := []struct {
		name   string
		create func() (tool.Tool, error)
	}{
		{"git.read.log", func() (tool.Tool, error) { return newLogTool(policy) }},
		{"git.read.blame", func() (tool.Tool, error) { return newBlameTool(policy) }},
		{"git.read.show", func() (tool.Tool, error) { return newShowTool(policy) }},
		{"git.read.diff", func() (tool.Tool, error) { return newDiffTool(policy) }},
	}

	for _, t := range tools {
		adkTool, err := t.create()
		if err != nil {
			return fmt.Errorf("creating tool %s: %w", t.name, err)
		}

		reg.Register(t.name, adkTool)
	}

	return nil
}

func normalizePolicy(policy Policy) (Policy, error) {
	info, err := os.Stat(policy.Root)
	if err != nil {
		return Policy{}, fmt.Errorf("stating Git root: %w", err)
	}

	if !info.IsDir() || policy.MaxLogCount <= 0 {
		return Policy{}, ErrInvalidPolicy
	}

	for _, ref := range policy.Refs {
		if ref == "" || strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, "\x00\r\n") {
			return Policy{}, ErrInvalidPolicy
		}
	}

	for _, name := range []string{"git.read.blame", "git.read.diff", "git.read.log", "git.read.show"} {
		limit, exists := policy.Limits[name]
		if !exists || limit.MaxCalls <= 0 || limit.Timeout <= 0 || limit.MaxRequestBytes < 0 || limit.MaxResultBytes <= 0 {
			return Policy{}, fmt.Errorf("%w: %s", ErrInvalidPolicy, name)
		}
	}

	policy.Refs = slices.Clone(policy.Refs)
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

func newLogTool(policy Policy) (tool.Tool, error) {
	return functiontool.New[LogArgs, *LogResult](
		functiontool.Config{
			Name:        "git.read.log",
			Description: "Show recent Git commits with a fixed format and optional path filter.",
		},
		func(ctx agent.Context, args LogArgs) (*LogResult, error) {
			return GitLog(ctx, policy, args)
		},
	)
}

func newBlameTool(policy Policy) (tool.Tool, error) {
	return functiontool.New[BlameArgs, *BlameResult](
		functiontool.Config{
			Name:        "git.read.blame",
			Description: "Show git blame for a file, optionally restricted to a line range.",
		},
		func(ctx agent.Context, args BlameArgs) (*BlameResult, error) {
			return GitBlame(ctx, policy, args)
		},
	)
}

func newShowTool(policy Policy) (tool.Tool, error) {
	return functiontool.New[ShowArgs, *ShowResult](
		functiontool.Config{
			Name:        "git.read.show",
			Description: "Show the details of a commit (message, diff, author, etc.).",
		},
		func(ctx agent.Context, args ShowArgs) (*ShowResult, error) {
			return GitShow(ctx, policy, args)
		},
	)
}

func newDiffTool(policy Policy) (tool.Tool, error) {
	return functiontool.New[DiffArgs, *DiffResult](
		functiontool.Config{
			Name:        "git.read.diff",
			Description: "Show git diff for working tree changes or between refs.",
		},
		func(ctx agent.Context, args DiffArgs) (*DiffResult, error) {
			return GitDiff(ctx, policy, args)
		},
	)
}
