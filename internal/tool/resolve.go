package tool

import (
	"path"
	"sort"
)

// ResolveNames merges default tools with step-level tool overrides.
//   - stepTools == nil → defaults only
//   - len(*stepTools) == 0 → no tools (explicit empty override)
//   - otherwise → defaults + step tools (additive)
func ResolveNames(defaults []string, stepTools *[]string) []string {
	if stepTools == nil {
		return defaults
	}

	if len(*stepTools) == 0 {
		return []string{}
	}

	merged := make([]string, 0, len(defaults)+len(*stepTools))
	merged = append(merged, defaults...)
	merged = append(merged, *stepTools...)

	return merged
}

// Resolve returns tools from the registry that match the given glob patterns.
func (r *Registry) Resolve(patterns []string) []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)

	var result []Tool

	for _, pattern := range patterns {
		for name, t := range r.tools {
			if seen[name] {
				continue
			}

			matched, err := path.Match(pattern, name)
			if err != nil {
				// Invalid pattern treated as literal match
				matched = (pattern == name)
			}

			if matched {
				seen[name] = true

				result = append(result, t)
			}
		}
	}

	// Sort for deterministic output
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name() < result[j].Name()
	})

	return result
}
