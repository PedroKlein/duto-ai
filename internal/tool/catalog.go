package tool

import (
	"errors"
	"slices"
)

var ErrInvalidCatalog = errors.New("invalid tool catalog")

type Capability string

const (
	CapabilityWorkspaceRead   Capability = "workspace.read"
	CapabilityWorkspaceMutate Capability = "workspace.mutate"
	CapabilityGitRead         Capability = "git.read"
	CapabilityGitMutate       Capability = "git.mutate"
	CapabilityGitPublish      Capability = "git.publish"
	CapabilityProcessExec     Capability = "process.exec"
	CapabilityNetworkRead     Capability = "network.read"
	CapabilityGitHubRead      Capability = "github.read"
	CapabilityGitHubMutate    Capability = "github.mutate"
)

type SideEffect string

const (
	SideEffectRead    SideEffect = "read"
	SideEffectProcess SideEffect = "process"
	SideEffectLocal   SideEffect = "local_mutation"
	SideEffectStaged  SideEffect = "staged"
)

type Definition struct {
	Name         string       `json:"name"`
	Capabilities []Capability `json:"capabilities"`
	SideEffect   SideEffect   `json:"side_effect"`
}

type Catalog struct {
	definitions map[string]Definition
	names       []string
}

func NewCatalog(definitions []Definition) (Catalog, error) {
	catalog := Catalog{definitions: make(map[string]Definition, len(definitions)), names: make([]string, 0, len(definitions))}
	for _, definition := range definitions {
		if !validToolName(definition.Name) || len(definition.Capabilities) == 0 || definition.SideEffect == "" {
			return Catalog{}, ErrInvalidCatalog
		}

		if _, exists := catalog.definitions[definition.Name]; exists {
			return Catalog{}, ErrInvalidCatalog
		}

		definition.Capabilities = slices.Clone(definition.Capabilities)
		catalog.definitions[definition.Name] = definition
		catalog.names = append(catalog.names, definition.Name)
	}

	slices.Sort(catalog.names)

	return catalog, nil
}

func BuiltinCatalog() Catalog {
	catalog, err := NewCatalog([]Definition{
		{Name: "files.find", Capabilities: []Capability{CapabilityWorkspaceRead}, SideEffect: SideEffectRead},
		{Name: "files.grep", Capabilities: []Capability{CapabilityWorkspaceRead}, SideEffect: SideEffectRead},
		{Name: "files.read", Capabilities: []Capability{CapabilityWorkspaceRead}, SideEffect: SideEffectRead},
		{Name: "git.read.blame", Capabilities: []Capability{CapabilityGitRead}, SideEffect: SideEffectRead},
		{Name: "git.read.diff", Capabilities: []Capability{CapabilityGitRead}, SideEffect: SideEffectRead},
		{Name: "git.read.log", Capabilities: []Capability{CapabilityGitRead}, SideEffect: SideEffectRead},
		{Name: "git.read.show", Capabilities: []Capability{CapabilityGitRead}, SideEffect: SideEffectRead},
		{Name: "github.read.changed-files", Capabilities: []Capability{CapabilityGitHubRead}, SideEffect: SideEffectRead},
		{Name: "github.read.checks", Capabilities: []Capability{CapabilityGitHubRead}, SideEffect: SideEffectRead},
		{Name: "github.read.comments", Capabilities: []Capability{CapabilityGitHubRead}, SideEffect: SideEffectRead},
		{Name: "github.read.diff", Capabilities: []Capability{CapabilityGitHubRead}, SideEffect: SideEffectRead},
		{Name: "github.read.issue", Capabilities: []Capability{CapabilityGitHubRead}, SideEffect: SideEffectRead},
		{Name: "github.read.pr", Capabilities: []Capability{CapabilityGitHubRead}, SideEffect: SideEffectRead},
		{Name: "github.read.reviews", Capabilities: []Capability{CapabilityGitHubRead}, SideEffect: SideEffectRead},
		{Name: "github.read.search-issues", Capabilities: []Capability{CapabilityGitHubRead}, SideEffect: SideEffectRead},
		{Name: "shell.run", Capabilities: []Capability{CapabilityProcessExec}, SideEffect: SideEffectProcess},
		{Name: "web.fetch", Capabilities: []Capability{CapabilityNetworkRead}, SideEffect: SideEffectRead},
	})
	if err != nil {
		panic(err)
	}

	return catalog
}

func M3Catalog() Catalog {
	definitions := make([]Definition, 0, len(BuiltinCatalog().Names())+5)

	base := BuiltinCatalog()
	for _, name := range base.Names() {
		definition, _ := base.Definition(name)
		definitions = append(definitions, definition)
	}

	definitions = append(definitions,
		Definition{Name: "files.write", Capabilities: []Capability{CapabilityWorkspaceMutate}, SideEffect: SideEffectLocal},
		Definition{Name: "git.write.commit", Capabilities: []Capability{CapabilityGitMutate}, SideEffect: SideEffectLocal},
		Definition{Name: "safe-output.branch", Capabilities: []Capability{CapabilityGitPublish}, SideEffect: SideEffectStaged},
		Definition{Name: "safe-output.conversation-reply", Capabilities: []Capability{CapabilityGitHubMutate}, SideEffect: SideEffectStaged},
		Definition{Name: "safe-output.draft-pr", Capabilities: []Capability{CapabilityGitHubMutate}, SideEffect: SideEffectStaged},
	)

	catalog, err := NewCatalog(definitions)
	if err != nil {
		panic(err)
	}

	return catalog
}

func (c Catalog) Names() []string {
	return slices.Clone(c.names)
}

func (c Catalog) Definition(name string) (Definition, bool) {
	definition, exists := c.definitions[name]
	definition.Capabilities = slices.Clone(definition.Capabilities)

	return definition, exists
}
