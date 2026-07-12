package config_test

import (
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
)

func TestResolveModel(t *testing.T) {
	aliases := map[string]string{
		"light":  "gpt-4.1-mini",
		"medium": "gpt-4.1",
		"heavy":  "claude-sonnet-4",
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "alias resolves", input: "heavy", expected: "claude-sonnet-4"},
		{name: "alias resolves light", input: "light", expected: "gpt-4.1-mini"},
		{name: "no alias passthrough", input: "gpt-4o", expected: "gpt-4o"},
		{name: "empty returns empty", input: "", expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.ResolveModel(tt.input, aliases)
			if got != tt.expected {
				t.Errorf("ResolveModel(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
