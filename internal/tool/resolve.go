package tool

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

const (
	CodeDuplicateToolProfile  = "duplicate_tool_profile"
	CodeUnknownToolProfile    = "unknown_tool_profile"
	CodeUnknownTool           = "unknown_tool"
	CodeInvalidToolSelector   = "invalid_tool_selector"
	CodeUnmatchedToolSelector = "unmatched_tool_selector"
	CodeToolCeilingExceeded   = "tool_ceiling_exceeded"
	CodeToolAuthorityWidening = "tool_authority_widening"
	CodeInvalidToolRemoval    = "invalid_tool_removal"
	CodeInvalidToolParent     = "invalid_tool_parent"
	FromEmpty                 = "empty"
	FromParent                = "parent"
)

type PolicyError struct {
	Code string
	Name string
}

func (e *PolicyError) Error() string {
	if e.Name == "" {
		return e.Code
	}

	return fmt.Sprintf("%s: %s", e.Code, e.Name)
}

type Expression struct {
	From           string   `json:"from"`
	AddProfiles    []string `json:"add_profiles"`
	Add            []string `json:"add"`
	RemoveProfiles []string `json:"remove_profiles"`
	Remove         []string `json:"remove"`
}

type ProfileExpansion struct {
	Operation string   `json:"operation"`
	Name      string   `json:"name"`
	Tools     []string `json:"tools"`
}

type ScopeRequest struct {
	Expression Expression
	Profiles   map[string][]string
	Ceiling    []string
	Parent     []string
	Root       bool
}

type Scope struct {
	Expression Expression
	Profiles   []ProfileExpansion
	Names      []string
	Parent     []string
}

func MergeProfiles(trusted, portable map[string][]string) (map[string][]string, error) {
	merged := cloneProfiles(trusted)
	for name, selectors := range portable {
		if _, exists := merged[name]; exists {
			return nil, &PolicyError{Code: CodeDuplicateToolProfile, Name: name}
		}

		merged[name] = slices.Clone(selectors)
	}

	return merged, nil
}

func (c Catalog) ResolveCeiling(selectors []string) ([]string, error) {
	selected := make(map[string]struct{})

	for _, selector := range selectors {
		names, err := c.expand(selector, true)
		if err != nil {
			return nil, err
		}

		for _, name := range names {
			selected[name] = struct{}{}
		}
	}

	return sortedSet(selected), nil
}

func (c Catalog) ResolveScope(request ScopeRequest) (Scope, error) {
	from, err := normalizedParent(request.Expression.From, request.Root)
	if err != nil {
		return Scope{}, err
	}

	selected := make(map[string]struct{})
	if from == FromParent {
		addNames(selected, request.Parent)
	}

	addProfiles, err := c.applyProfiles(selected, request.Expression.AddProfiles, request.Profiles, false)
	if err != nil {
		return Scope{}, err
	}

	if selectorErr := c.applySelectors(selected, request.Expression.Add, false); selectorErr != nil {
		return Scope{}, selectorErr
	}

	removeProfiles, err := c.applyProfiles(selected, request.Expression.RemoveProfiles, request.Profiles, true)
	if err != nil {
		return Scope{}, err
	}

	if selectorErr := c.applySelectors(selected, request.Expression.Remove, true); selectorErr != nil {
		return Scope{}, selectorErr
	}

	names := sortedSet(selected)
	if subsetErr := requireSubset(names, request.Ceiling, CodeToolCeilingExceeded); subsetErr != nil {
		return Scope{}, subsetErr
	}

	if !request.Root {
		if subsetErr := requireSubset(names, request.Parent, CodeToolAuthorityWidening); subsetErr != nil {
			return Scope{}, subsetErr
		}
	}

	return Scope{
		Expression: Expression{
			From:           from,
			AddProfiles:    slices.Clone(request.Expression.AddProfiles),
			Add:            slices.Clone(request.Expression.Add),
			RemoveProfiles: slices.Clone(request.Expression.RemoveProfiles),
			Remove:         slices.Clone(request.Expression.Remove),
		},
		Profiles: append(addProfiles, removeProfiles...),
		Names:    names,
		Parent:   slices.Clone(request.Parent),
	}, nil
}

func normalizedParent(from string, root bool) (string, error) {
	if from == "" {
		from = FromEmpty
	}

	if (from != FromEmpty && from != FromParent) || (root && from == FromParent) {
		return "", &PolicyError{Code: CodeInvalidToolParent, Name: from}
	}

	return from, nil
}

func (c Catalog) applyProfiles(selected map[string]struct{}, names []string, profiles map[string][]string, remove bool) ([]ProfileExpansion, error) {
	expansions := make([]ProfileExpansion, 0, len(names))
	for _, name := range names {
		selectors, exists := profiles[name]
		if !exists {
			return nil, &PolicyError{Code: CodeUnknownToolProfile, Name: name}
		}

		expanded := make([]string, 0)

		for _, selector := range selectors {
			matches, err := c.expand(selector, false)
			if err != nil {
				return nil, err
			}

			expanded = append(expanded, matches...)
			if remove {
				if err := removeNames(selected, matches, selector); err != nil {
					return nil, err
				}
			} else {
				addNames(selected, matches)
			}
		}

		operation := "add"
		if remove {
			operation = "remove"
		}

		expansions = append(expansions, ProfileExpansion{Operation: operation, Name: name, Tools: expanded})
	}

	return expansions, nil
}

func (c Catalog) applySelectors(selected map[string]struct{}, selectors []string, remove bool) error {
	for _, selector := range selectors {
		names, err := c.expand(selector, false)
		if err != nil {
			return err
		}

		if remove {
			if err := removeNames(selected, names, selector); err != nil {
				return err
			}
		} else {
			addNames(selected, names)
		}
	}

	return nil
}

func (c Catalog) expand(selector string, trusted bool) ([]string, error) {
	if selector == "*" {
		if trusted {
			return c.Names(), nil
		}

		return nil, &PolicyError{Code: CodeInvalidToolSelector, Name: selector}
	}

	if strings.Contains(selector, "*") {
		if !slices.Contains([]string{"files.*", "git.read.*", "github.read.*"}, selector) {
			return nil, &PolicyError{Code: CodeInvalidToolSelector, Name: selector}
		}

		prefix := strings.TrimSuffix(selector, "*")
		matches := make([]string, 0)

		for _, name := range c.names {
			if strings.HasPrefix(name, prefix) {
				matches = append(matches, name)
			}
		}

		if len(matches) == 0 {
			return nil, &PolicyError{Code: CodeUnmatchedToolSelector, Name: selector}
		}

		return matches, nil
	}

	if !validToolName(selector) {
		return nil, &PolicyError{Code: CodeInvalidToolSelector, Name: selector}
	}

	if _, exists := c.definitions[selector]; !exists {
		return nil, &PolicyError{Code: CodeUnknownTool, Name: selector}
	}

	return []string{selector}, nil
}

func validToolName(name string) bool {
	parts := strings.Split(name, ".")
	if len(parts) < 2 {
		return false
	}

	for _, part := range parts {
		if part == "" || part[0] < 'a' || part[0] > 'z' {
			return false
		}

		for i := 1; i < len(part); i++ {
			character := part[i]
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}

	return true
}

func addNames(selected map[string]struct{}, names []string) {
	for _, name := range names {
		selected[name] = struct{}{}
	}
}

func removeNames(selected map[string]struct{}, names []string, selector string) error {
	removed := false

	for _, name := range names {
		if _, exists := selected[name]; exists {
			delete(selected, name)

			removed = true
		}
	}

	if !removed {
		return &PolicyError{Code: CodeInvalidToolRemoval, Name: selector}
	}

	return nil
}

func requireSubset(names, allowed []string, code string) error {
	set := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		set[name] = struct{}{}
	}

	for _, name := range names {
		if _, exists := set[name]; !exists {
			return &PolicyError{Code: code, Name: name}
		}
	}

	return nil
}

func sortedSet(set map[string]struct{}) []string {
	names := slices.Collect(maps.Keys(set))
	slices.Sort(names)

	return names
}

func cloneProfiles(source map[string][]string) map[string][]string {
	result := make(map[string][]string, len(source))
	for name, selectors := range source {
		result[name] = slices.Clone(selectors)
	}

	return result
}
