// Package shell provides trusted, bounded process execution for AI workflows.
package shell

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool/functiontool"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

var ErrInvalidPolicy = errors.New("invalid shell tool policy")

type Policy struct {
	Executable     string
	Args           []string
	Workspace      string
	Environment    map[string]string
	MaxStdoutBytes int
	MaxStderrBytes int
	Limit          dtool.ToolLimit
}

type RunArgs struct{}

type RunResult struct {
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
}

func RegisterAll(registry *dtool.Registry, policy Policy) error {
	if registry == nil {
		return ErrInvalidPolicy
	}

	policy, err := normalizePolicy(policy)
	if err != nil {
		return err
	}

	current, err := functiontool.New[RunArgs, *RunResult](
		functiontool.Config{
			Name:        "shell.run",
			Description: "Run the exact trusted command with bounded output.",
		},
		func(ctx agent.Context, _ RunArgs) (*RunResult, error) {
			return Run(ctx, policy)
		},
	)
	if err != nil {
		return fmt.Errorf("creating tool shell.run: %w", err)
	}

	registry.Register("shell.run", current)

	return nil
}

func normalizePolicy(policy Policy) (Policy, error) {
	if err := validateCommandPolicy(policy); err != nil {
		return Policy{}, err
	}

	if err := validateEnvironment(policy.Environment); err != nil {
		return Policy{}, err
	}

	if policy.MaxStdoutBytes <= 0 || policy.MaxStderrBytes <= 0 || policy.Limit.MaxCalls <= 0 || policy.Limit.Timeout <= 0 || policy.Limit.MaxRequestBytes <= 0 || policy.Limit.MaxResultBytes <= 0 {
		return Policy{}, ErrInvalidPolicy
	}

	policy.Args = slices.Clone(policy.Args)
	policy.Environment = maps.Clone(policy.Environment)

	return policy, nil
}

func validateCommandPolicy(policy Policy) error {
	if !filepath.IsAbs(policy.Executable) || strings.ContainsRune(policy.Executable, '\x00') || !filepath.IsAbs(policy.Workspace) {
		return ErrInvalidPolicy
	}

	executable, err := os.Stat(policy.Executable)
	if err != nil {
		return fmt.Errorf("stating trusted executable: %w", err)
	}

	workspace, err := os.Stat(policy.Workspace)
	if err != nil {
		return fmt.Errorf("stating shell workspace: %w", err)
	}

	if !executable.Mode().IsRegular() || executable.Mode().Perm()&0o111 == 0 || !workspace.IsDir() {
		return ErrInvalidPolicy
	}

	for _, arg := range policy.Args {
		if strings.ContainsRune(arg, '\x00') {
			return ErrInvalidPolicy
		}
	}

	return nil
}

func validateEnvironment(environment map[string]string) error {
	for name, value := range environment {
		if !validEnvironmentName(name) || strings.ContainsRune(value, '\x00') {
			return ErrInvalidPolicy
		}
	}

	return nil
}

func validEnvironmentName(name string) bool {
	if name == "" {
		return false
	}

	for i, value := range name {
		if value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || i > 0 && value >= '0' && value <= '9' {
			continue
		}

		return false
	}

	return true
}
