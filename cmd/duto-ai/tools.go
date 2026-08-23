package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/plan"
	dtool "github.com/PedroKlein/duto-ai/internal/tool"
	"github.com/PedroKlein/duto-ai/internal/tool/files"
	gittool "github.com/PedroKlein/duto-ai/internal/tool/git"
	githubtool "github.com/PedroKlein/duto-ai/internal/tool/github"
	"github.com/PedroKlein/duto-ai/internal/tool/shell"
	"github.com/PedroKlein/duto-ai/internal/tool/web"
)

var errToolBinding = errors.New("selected tool family has no valid trusted binding")

func buildToolRegistry(cfg *config.Config, compiled *plan.Plan) (*dtool.Registry, error) {
	if cfg == nil || compiled == nil {
		return nil, errToolBinding
	}

	selected := selectedToolNames(compiled.Snapshot().Workflow)
	registry := dtool.NewRegistry()

	registrations := []struct {
		family string
		apply  func(*dtool.Registry, *config.Config, []string) error
	}{
		{family: "files", apply: registerFiles},
		{family: "git", apply: registerGit},
		{family: "github", apply: registerGitHub},
		{family: "web", apply: registerWeb},
		{family: "shell", apply: registerShell},
	}
	for _, registration := range registrations {
		names := familyToolNames(selected, registration.family)
		if len(names) == 0 {
			continue
		}

		if err := registration.apply(registry, cfg, names); err != nil {
			return nil, fmt.Errorf("%s: %w", registration.family, err)
		}
	}

	return registry, nil
}

func registerFiles(registry *dtool.Registry, cfg *config.Config, names []string) error {
	root, err := boundWorkspace(cfg, bindingWorkspace(cfg.ToolConfig.Files))
	if err != nil {
		return err
	}

	limits, err := trustedToolLimits(cfg, names...)
	if err != nil {
		return err
	}

	if err := files.RegisterAll(registry, files.Policy{Root: root, Limits: limits}); err != nil {
		return fmt.Errorf("registering file tools: %w", err)
	}

	return nil
}

func registerGit(registry *dtool.Registry, cfg *config.Config, names []string) error {
	binding := cfg.ToolConfig.Git

	root, err := boundWorkspace(cfg, gitBindingWorkspace(binding))
	if err != nil {
		return err
	}

	limits, err := trustedToolLimits(cfg, names...)
	if err != nil {
		return err
	}

	if err := gittool.RegisterAll(registry, gittool.Policy{Root: root, Refs: binding.Refs, AllowWorkingTree: binding.AllowWorkingTree, MaxLogCount: binding.MaxLogCount, Limits: limits}); err != nil {
		return fmt.Errorf("registering Git tools: %w", err)
	}

	return nil
}

func registerGitHub(registry *dtool.Registry, cfg *config.Config, names []string) error {
	binding := cfg.ToolConfig.GitHub
	if binding == nil {
		return errToolBinding
	}

	limits, err := trustedToolLimits(cfg, names...)
	if err != nil {
		return err
	}

	client, err := githubtool.NewClient(githubtool.Policy{
		BaseURL: binding.BaseURL, Token: binding.Token, Owner: binding.Owner, Repository: binding.Repository,
		Subject: binding.Subject, Ref: binding.Ref, MaxPages: binding.MaxPages, MaxResults: binding.MaxResults, Limits: limits,
	})
	if err != nil {
		return fmt.Errorf("creating GitHub client: %w", err)
	}

	if err := githubtool.RegisterAll(registry, client); err != nil {
		return fmt.Errorf("registering GitHub tools: %w", err)
	}

	return nil
}

func registerWeb(registry *dtool.Registry, cfg *config.Config, names []string) error {
	binding := cfg.ToolConfig.Web
	if binding == nil {
		return errToolBinding
	}

	limits, err := trustedToolLimits(cfg, names...)
	if err != nil {
		return err
	}

	client, err := web.NewClient(web.Policy{AllowedDomains: binding.AllowedDomains, MaxRedirects: binding.MaxRedirects, Limits: limits})
	if err != nil {
		return fmt.Errorf("creating web client: %w", err)
	}

	if err := web.RegisterAll(registry, client); err != nil {
		return fmt.Errorf("registering web tools: %w", err)
	}

	return nil
}

func registerShell(registry *dtool.Registry, cfg *config.Config, names []string) error {
	binding := cfg.ToolConfig.Shell

	root, err := boundWorkspace(cfg, shellBindingWorkspace(binding))
	if err != nil {
		return err
	}

	limits, err := trustedToolLimits(cfg, names...)
	if err != nil {
		return err
	}

	if err := shell.RegisterAll(registry, shell.Policy{
		Executable: binding.Executable, Args: binding.Args, Workspace: root, Environment: binding.Environment,
		MaxStdoutBytes: binding.MaxStdoutBytes, MaxStderrBytes: binding.MaxStderrBytes, Limit: limits["shell.run"],
	}); err != nil {
		return fmt.Errorf("registering shell tool: %w", err)
	}

	return nil
}

func selectedToolNames(workflow plan.Workflow) []string {
	selected := make(map[string]struct{})
	add := func(scope plan.ToolScope) {
		for _, name := range scope.Names {
			selected[name] = struct{}{}
		}
	}
	add(workflow.Tools)

	for _, step := range workflow.Steps {
		add(step.Tools)
	}

	for _, agent := range workflow.Agents {
		add(agent.Tools)
	}

	names := make([]string, 0, len(selected))
	for name := range selected {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}

func familyToolNames(names []string, family string) []string {
	return slices.Collect(func(yield func(string) bool) {
		for _, name := range names {
			prefix, _, _ := strings.Cut(name, ".")
			if prefix == family && !yield(name) {
				return
			}
		}
	})
}

func boundWorkspace(cfg *config.Config, name string) (string, error) {
	if name == "" {
		return "", errToolBinding
	}

	workspace, ok := cfg.Workspaces[name]
	if !ok || workspace.Access != "read" || workspace.Root == "" {
		return "", errToolBinding
	}

	return workspace.Root, nil
}

func bindingWorkspace(binding *config.FilesToolConfig) string {
	if binding == nil {
		return ""
	}

	return binding.Workspace
}

func gitBindingWorkspace(binding *config.GitToolConfig) string {
	if binding == nil {
		return ""
	}

	return binding.Workspace
}

func shellBindingWorkspace(binding *config.ShellToolConfig) string {
	if binding == nil {
		return ""
	}

	return binding.Workspace
}

func trustedToolLimits(cfg *config.Config, names ...string) (map[string]dtool.ToolLimit, error) {
	limits := make(map[string]dtool.ToolLimit, len(names))
	for _, name := range names {
		source, ok := cfg.ToolLimits[name]
		if !ok {
			return nil, errToolBinding
		}

		timeout, err := time.ParseDuration(source.Timeout)
		if err != nil {
			return nil, errToolBinding
		}

		limits[name] = dtool.ToolLimit{MaxCalls: source.MaxCalls, Timeout: timeout, MaxRequestBytes: source.MaxRequestBytes, MaxResultBytes: source.MaxResultBytes}
	}

	return limits, nil
}
