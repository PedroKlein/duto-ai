package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
)

func TestDecodeWorkflow_RetryIsStrictAndBounded(t *testing.T) {
	source := strings.Replace(minimalWorkflow, "    output:\n", "    retry: {max_attempts: 3, initial_delay: 1ms, max_delay: 4ms}\n    output:\n", 1)

	workflow, err := config.DecodeWorkflow("workflow.yaml", []byte(source))
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}

	if workflow.Steps[0].Retry.MaxAttempts != 3 || workflow.Steps[0].Retry.InitialDelay != "1ms" || workflow.Steps[0].Retry.MaxDelay != "4ms" {
		t.Fatalf("retry = %#v", workflow.Steps[0].Retry)
	}

	_, err = config.DecodeWorkflow("workflow.yaml", []byte(strings.Replace(source, "max_delay: 4ms", "max_delay: 4ms, predicate: all", 1)))

	var diagnostic *config.DiagnosticError
	if !errors.As(err, &diagnostic) || diagnostic.Code != config.CodeUnknownField {
		t.Fatalf("strict retry error = %v", err)
	}
}
