package logging

import (
	"fmt"
	"os"
	"time"
)

// IsGitHubActions returns true when running inside a GitHub Actions workflow.
func IsGitHubActions() bool {
	return os.Getenv("GITHUB_ACTIONS") == "true"
}

// GHAGroup starts a foldable log group in GitHub Actions.
func GHAGroup(name string) {
	if !IsGitHubActions() {
		return
	}

	fmt.Fprintf(os.Stdout, "::group::%s\n", name) //nolint:errcheck // GHA log commands are best-effort
}

// GHAEndGroup ends the current log group.
func GHAEndGroup() {
	if !IsGitHubActions() {
		return
	}

	fmt.Fprintln(os.Stdout, "::endgroup::") //nolint:errcheck // GHA log commands are best-effort
}

// GHAWarning emits a warning annotation visible in the PR.
func GHAWarning(msg string) {
	if !IsGitHubActions() {
		return
	}

	fmt.Fprintf(os.Stdout, "::warning::%s\n", msg) //nolint:errcheck // GHA log commands are best-effort
}

// GHAError emits an error annotation visible in the PR.
func GHAError(msg string) {
	if !IsGitHubActions() {
		return
	}

	fmt.Fprintf(os.Stdout, "::error::%s\n", msg) //nolint:errcheck // GHA log commands are best-effort
}

// GHAStepTiming emits timing information for a step.
func GHAStepTiming(stepID string, duration time.Duration) {
	if !IsGitHubActions() {
		return
	}

	fmt.Fprintf(os.Stdout, "  ⏱ %s completed in %s\n", stepID, duration.Truncate(time.Millisecond)) //nolint:errcheck // GHA log commands are best-effort
}
