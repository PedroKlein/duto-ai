// Package git provides git tools (log, blame, show, diff) for AI workflows.
// All commands are executed via os/exec with a 30s timeout, sandboxed to a repo root.
package git

import (
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

// LogArgs is the input schema for the git.log tool.
type LogArgs struct {
	Count  int    `json:"count"`            // number of commits to show (default: 10)
	Path   string `json:"path,omitempty"`   // limit to commits affecting this path
	Format string `json:"format,omitempty"` // git log format string (default: oneline)
}

// LogResult is the output of the git.log tool.
type LogResult struct {
	Output string `json:"output"` // git log output
}

// BlameArgs is the input schema for the git.blame tool.
type BlameArgs struct {
	Path      string `json:"path"`                 // file to blame
	StartLine int    `json:"start_line,omitempty"` // start of line range (optional)
	EndLine   int    `json:"end_line,omitempty"`   // end of line range (optional)
}

// BlameResult is the output of the git.blame tool.
type BlameResult struct {
	Output string `json:"output"` // git blame output
}

// ShowArgs is the input schema for the git.show tool.
type ShowArgs struct {
	Ref string `json:"ref"` // commit hash or ref to show
}

// ShowResult is the output of the git.show tool.
type ShowResult struct {
	Output string `json:"output"` // git show output
}

// DiffArgs is the input schema for the git.diff tool.
type DiffArgs struct {
	Ref  string `json:"ref,omitempty"`  // compare against this ref (default: working tree)
	Path string `json:"path,omitempty"` // limit diff to this path
}

// DiffResult is the output of the git.diff tool.
type DiffResult struct {
	Output string `json:"output"` // diff output
}

// RegisterAll creates all git tools sandboxed to root and registers them.
func RegisterAll(reg *dtool.Registry, root string) error {
	tools := []struct {
		name   string
		create func() (tool.Tool, error)
	}{
		{"git.log", func() (tool.Tool, error) { return newLogTool(root) }},
		{"git.blame", func() (tool.Tool, error) { return newBlameTool(root) }},
		{"git.show", func() (tool.Tool, error) { return newShowTool(root) }},
		{"git.diff", func() (tool.Tool, error) { return newDiffTool(root) }},
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

func newLogTool(root string) (tool.Tool, error) {
	return functiontool.New[LogArgs, *LogResult](
		functiontool.Config{
			Name:        "git.log",
			Description: "Show recent git commits. Configure count, path filter, and format.",
		},
		func(_ agent.Context, args LogArgs) (*LogResult, error) {
			output, err := GitLog(root, args)
			if err != nil {
				return nil, err
			}

			return &LogResult{Output: output}, nil
		},
	)
}

func newBlameTool(root string) (tool.Tool, error) {
	return functiontool.New[BlameArgs, *BlameResult](
		functiontool.Config{
			Name:        "git.blame",
			Description: "Show git blame for a file, optionally restricted to a line range.",
		},
		func(_ agent.Context, args BlameArgs) (*BlameResult, error) {
			output, err := GitBlame(root, args)
			if err != nil {
				return nil, err
			}

			return &BlameResult{Output: output}, nil
		},
	)
}

func newShowTool(root string) (tool.Tool, error) {
	return functiontool.New[ShowArgs, *ShowResult](
		functiontool.Config{
			Name:        "git.show",
			Description: "Show the details of a commit (message, diff, author, etc.).",
		},
		func(_ agent.Context, args ShowArgs) (*ShowResult, error) {
			output, err := GitShow(root, args)
			if err != nil {
				return nil, err
			}

			return &ShowResult{Output: output}, nil
		},
	)
}

func newDiffTool(root string) (tool.Tool, error) {
	return functiontool.New[DiffArgs, *DiffResult](
		functiontool.Config{
			Name:        "git.diff",
			Description: "Show git diff for working tree changes or between refs.",
		},
		func(_ agent.Context, args DiffArgs) (*DiffResult, error) {
			output, err := GitDiff(root, args)
			if err != nil {
				return nil, err
			}

			return &DiffResult{Output: output}, nil
		},
	)
}
