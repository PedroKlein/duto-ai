package compiler

import (
	"strings"
	"time"

	"google.golang.org/adk/v2/workflow"

	"github.com/PedroKlein/duto-ai/internal/config"
)

// BuildRetryConfig converts the YAML retry policy into ADK's RetryConfig.
// Returns nil (no retry) when the step has no retry configuration.
func BuildRetryConfig(step config.Step) *workflow.RetryConfig {
	if step.Retry == nil {
		return nil
	}

	rc := workflow.DefaultRetryConfig()

	if step.Retry.MaxAttempts > 0 {
		rc.MaxAttempts = step.Retry.MaxAttempts
	} else {
		rc.MaxAttempts = config.DefaultRetryAttempts
	}

	if step.Retry.InitialDelay != "" {
		if d, err := time.ParseDuration(step.Retry.InitialDelay); err == nil {
			rc.InitialDelay = d
		}
	} else {
		rc.InitialDelay = config.DefaultRetryDelay
	}

	rc.ShouldRetry = IsTransientError

	return rc
}

// IsTransientError returns true for errors that are likely transient and
// worth retrying (rate limits, server errors). It returns false for
// validation errors or permission issues where retrying would not help.
func IsTransientError(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()

	for _, code := range transientCodes {
		if strings.Contains(msg, code) {
			return true
		}
	}

	for _, hint := range transientHints {
		if strings.Contains(strings.ToLower(msg), hint) {
			return true
		}
	}

	return false
}

var transientCodes = []string{"429", "500", "502", "503", "504"}

var transientHints = []string{
	"rate limit",
	"too many requests",
	"server error",
	"service unavailable",
	"timeout",
	"temporarily",
	"overloaded",
}
