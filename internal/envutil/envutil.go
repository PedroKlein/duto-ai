// Package envutil provides shared environment variable expansion.
package envutil

import (
	"os"
	"regexp"
	"strings"
)

var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// Expand replaces ${VAR} patterns in s with their environment values.
// Empty string input returns empty string.
func Expand(s string) string {
	if s == "" {
		return ""
	}

	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		key := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")

		return os.Getenv(key)
	})
}
