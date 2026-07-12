package compiler

import (
	"strings"

	"github.com/PedroKlein/duto-ai/internal/config"
)

// DetectParallel groups steps that can execute in parallel (same set of dependencies).
func DetectParallel(steps []config.Step) [][]string {
	// Group by dependency set signature
	groups := make(map[string][]string)

	for _, step := range steps {
		key := depsKey(step.Needs)
		groups[key] = append(groups[key], step.ID)
	}

	var result [][]string

	for _, group := range groups {
		if len(group) > 1 {
			result = append(result, group)
		}
	}

	return result
}

func depsKey(needs []string) string {
	if len(needs) == 0 {
		return ""
	}

	return strings.Join(needs, ",")
}
