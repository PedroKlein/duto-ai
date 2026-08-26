package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/plan"
	"github.com/PedroKlein/duto-ai/internal/safeoutput"
	dtool "github.com/PedroKlein/duto-ai/internal/tool"
	"github.com/PedroKlein/duto-ai/internal/tool/files"
	gittool "github.com/PedroKlein/duto-ai/internal/tool/git"
	githubtool "github.com/PedroKlein/duto-ai/internal/tool/github"
	"github.com/PedroKlein/duto-ai/internal/tool/shell"
	"github.com/PedroKlein/duto-ai/internal/tool/web"
)

var errToolBinding = errors.New("selected tool family has no valid trusted binding")

type authoringRuntime struct {
	files   *files.Authoring
	git     *gittool.Authoring
	staging *safeoutput.Collector
}

func buildToolRegistry(cfg *config.Config, compiled *plan.Plan) (*dtool.Registry, error) {
	registry, _, err := buildToolRegistryForRun(context.Background(), cfg, compiled)
	return registry, err
}

func buildToolRegistryForRun(ctx context.Context, cfg *config.Config, compiled *plan.Plan) (*dtool.Registry, *authoringRuntime, error) {
	if cfg == nil || compiled == nil {
		return nil, nil, errToolBinding
	}

	authoring, err := prepareAuthoring(ctx, compiled)
	if err != nil {
		return nil, nil, err
	}

	authoring, err = prepareStaging(compiled, authoring)
	if err != nil {
		if authoring != nil {
			_ = authoring.close()
		}

		return nil, nil, err
	}

	selected := selectedToolNames(compiled.Snapshot().Workflow)
	registry := dtool.NewRegistry()

	registrations := []struct {
		family string
		apply  func(*dtool.Registry, *config.Config, []string) error
	}{
		{family: "files", apply: func(registry *dtool.Registry, cfg *config.Config, names []string) error {
			return registerFiles(registry, cfg, names, authoring)
		}},
		{family: "git", apply: func(registry *dtool.Registry, cfg *config.Config, names []string) error {
			return registerGit(registry, cfg, names, authoring)
		}},
		{family: "safe-output", apply: func(registry *dtool.Registry, _ *config.Config, _ []string) error {
			return safeoutput.RegisterAll(registry, authoring.staging)
		}},
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
			if authoring != nil {
				_ = authoring.close()
			}

			return nil, nil, fmt.Errorf("%s: %w", registration.family, err)
		}
	}

	return registry, authoring, nil
}

func prepareAuthoring(ctx context.Context, compiled *plan.Plan) (*authoringRuntime, error) {
	spec := compiled.Authoring()
	if spec == nil {
		return nil, nil
	}

	gitAuthoring, err := gittool.NewAuthoring(ctx, gittool.AuthoringPolicy{
		Root: spec.Root, AllowedPaths: spec.AllowedPaths, MaxChangedFiles: spec.MaxChangedFiles,
		MaxCommitMessageBytes: spec.MaxCommitMessageBytes, MaxRecoveryBytes: spec.MaxTotalWriteBytes,
		AuthorName: spec.CommitAuthorName, AuthorEmail: spec.CommitAuthorEmail, BaseRef: spec.BaseRef,
		BaseSHA: spec.BaseSHA, EvidenceDirectory: compiled.EvidenceDirectory(),
	})
	if err != nil {
		return nil, fmt.Errorf("admitting Git authoring repository: %w", err)
	}

	fileAuthoring, err := files.NewAuthoring(spec.Root, spec.AllowedPaths, spec.MaxChangedFiles, spec.MaxFileBytes, spec.MaxTotalWriteBytes)
	if err != nil {
		return nil, fmt.Errorf("constructing file authoring: %w", err)
	}

	if err := gitAuthoring.BindWriter(fileAuthoring); err != nil {
		_ = fileAuthoring.Close()
		return nil, fmt.Errorf("binding authored files to Git: %w", err)
	}

	return &authoringRuntime{files: fileAuthoring, git: gitAuthoring}, nil
}

func prepareStaging(compiled *plan.Plan, authoring *authoringRuntime) (*authoringRuntime, error) {
	spec := compiled.Staging()
	if spec == nil {
		return authoring, nil
	}

	policy := safeoutput.Policy{
		OperationSet: spec.OperationSet, PlanSHA256: compiled.Digest(), PolicySHA256: spec.PolicySHA256,
		ControlSHA256: spec.ControlSHA256, ControlJSON: spec.ControlJSON, CorrelationKey: spec.CorrelationKey,
		Repository: safeoutput.Repository{ID: spec.Repository.ID, Owner: spec.Repository.Owner, Name: spec.Repository.Name},
		Origin:     safeoutput.Origin{Kind: spec.Origin.Kind, Number: spec.Origin.Number},
		Base:       safeoutput.Base{Ref: spec.BaseRef, SHA: spec.BaseSHA}, BranchPrefix: spec.BranchPrefix,
		MaxReplyBytes: spec.MaxReplyBytes, MaxPRTitleBytes: spec.MaxPRTitleBytes,
		MaxPRBodyBytes: spec.MaxPRBodyBytes, MaxBundleBytes: spec.MaxBundleBytes,
	}
	selected := familyToolNames(selectedToolNames(compiled.Snapshot().Workflow), "safe-output")

	limits, err := trustedToolLimitsFromPlan(compiled, selected)
	if err != nil {
		return authoring, err
	}

	policy.Limits = limits

	if spec.OperationSet == safeoutput.BranchPR {
		if authoring == nil || authoring.git == nil {
			return authoring, errToolBinding
		}

		policy.Source = func(ctx context.Context) (safeoutput.Source, error) {
			publication, publicationErr := authoring.git.Publication(ctx)
			if publicationErr != nil {
				return safeoutput.Source{}, fmt.Errorf("reading Git publication source: %w", publicationErr)
			}

			return safeoutput.Source{Commit: publication.Commit, Tree: publication.Tree, Bundle: publication.Bundle}, nil
		}
	}

	collector, err := safeoutput.New(policy)
	if err != nil {
		return authoring, fmt.Errorf("constructing staged operation collector: %w", err)
	}

	if authoring == nil {
		authoring = &authoringRuntime{}
	}

	authoring.staging = collector

	return authoring, nil
}

func trustedToolLimitsFromPlan(compiled *plan.Plan, names []string) (map[string]dtool.ToolLimit, error) {
	limits := make(map[string]dtool.ToolLimit, len(names))
	workflow := compiled.Snapshot().Workflow

	byName := make(map[string]plan.ToolLimit, len(workflow.Tools.Limits))
	for _, limit := range workflow.Tools.Limits {
		byName[limit.Name] = limit
	}

	for _, name := range names {
		limit, exists := byName[name]
		if !exists {
			return nil, errToolBinding
		}

		timeout, err := time.ParseDuration(limit.Timeout)
		if err != nil {
			return nil, errToolBinding
		}

		limits[name] = dtool.ToolLimit{MaxCalls: limit.MaxCalls, Timeout: timeout, MaxRequestBytes: limit.MaxRequestBytes, MaxResultBytes: limit.MaxResultBytes}
	}

	return limits, nil
}

func (a *authoringRuntime) finish(ctx context.Context, succeeded bool) error {
	if a == nil || a.git == nil {
		return nil
	}
	defer a.close() //nolint:errcheck // the lifecycle result reports the meaningful verification or recovery error

	var verifyErr error
	if succeeded {
		verifyErr = a.git.Verify(ctx)
		if verifyErr == nil {
			return nil
		}
	}

	recoveryContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	if err := a.git.Recover(recoveryContext, "execution"); err != nil {
		return fmt.Errorf("recovering authored repository: %w", err)
	}

	if a.staging != nil {
		recovery, err := a.git.TakeRecoveryArtifacts()
		if err != nil {
			return fmt.Errorf("reading recovery artifacts: %w", err)
		}

		if err := a.staging.SetRecovery(recovery.Metadata, recovery.Patch); err != nil {
			return fmt.Errorf("binding recovery artifacts: %w", err)
		}
	}

	if succeeded {
		return fmt.Errorf("verifying authored repository: %w", verifyErr)
	}

	return nil
}

func (a *authoringRuntime) close() error {
	if a == nil || a.files == nil {
		return nil
	}

	if err := a.files.Close(); err != nil {
		return fmt.Errorf("closing file authoring: %w", err)
	}

	return nil
}

func registerFiles(registry *dtool.Registry, cfg *config.Config, names []string, authoring *authoringRuntime) error {
	root, err := boundWorkspace(cfg, bindingWorkspace(cfg.ToolConfig.Files))
	if err != nil {
		return err
	}

	limits, err := trustedToolLimits(cfg, names...)
	if err != nil {
		return err
	}

	policy := files.Policy{Root: root, Limits: limits}
	if authoring != nil {
		policy.Authoring = authoring.files
	}

	if err := files.RegisterAll(registry, policy); err != nil {
		return fmt.Errorf("registering file tools: %w", err)
	}

	return nil
}

func registerGit(registry *dtool.Registry, cfg *config.Config, names []string, authoring *authoringRuntime) error {
	binding := cfg.ToolConfig.Git

	root, err := boundWorkspace(cfg, gitBindingWorkspace(binding))
	if err != nil {
		return err
	}

	limits, err := trustedToolLimits(cfg, names...)
	if err != nil {
		return err
	}

	policy := gittool.Policy{Root: root, Refs: binding.Refs, AllowWorkingTree: binding.AllowWorkingTree, MaxLogCount: binding.MaxLogCount, Limits: limits}
	if authoring != nil {
		policy.Authoring = authoring.git
	}

	if err := gittool.RegisterAll(registry, policy); err != nil {
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
	if !ok || (workspace.Access != config.WorkspaceAccessRead && workspace.Access != config.WorkspaceAccessWrite) || workspace.Root == "" {
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
