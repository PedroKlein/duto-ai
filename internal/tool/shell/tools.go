// Package shell provides a sandboxed shell execution tool for AI workflows.
// Commands run with a configurable timeout and cwd locked to the repo root.
package shell

import (
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

// RunArgs is the input schema for the shell.run tool.
type RunArgs struct {
	Command string `json:"command"`           // shell command to execute
	Timeout int    `json:"timeout,omitempty"` // timeout in seconds (default: 30)
}

// RunResult is the output of the shell.run tool.
type RunResult struct {
	Stdout   string `json:"stdout"`    // standard output
	Stderr   string `json:"stderr"`    // standard error
	ExitCode int    `json:"exit_code"` // process exit code
}

// RegisterAll creates the shell.run tool sandboxed to root and registers it.
func RegisterAll(reg *dtool.Registry, root string) error {
	t, err := newRunTool(root)
	if err != nil {
		return fmt.Errorf("creating tool shell.run: %w", err)
	}

	reg.Register("shell.run", t)

	return nil
}

func newRunTool(root string) (tool.Tool, error) {
	return functiontool.New[RunArgs, *RunResult](
		functiontool.Config{
			Name:        "shell.run",
			Description: "Execute a shell command in the repository root. Returns stdout, stderr, and exit code. Timeout default is 30s.",
		},
		func(_ agent.Context, args RunArgs) (*RunResult, error) {
			return Run(root, args)
		},
	)
}
