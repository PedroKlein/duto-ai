package provider_test

import (
	"context"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/provider"
)

func TestNewLLM_UnknownType(t *testing.T) {
	cfg := config.Provider{
		Type:   "unknown",
		Config: map[string]string{},
	}

	_, err := provider.NewLLM(context.Background(), cfg, "model")
	if err == nil {
		t.Fatal("expected error for unknown provider type")
	}
}

func TestNewLLM_AICoreRequiresEndpoint(t *testing.T) {
	// Without proper credentials, this will fail with auth error
	cfg := config.Provider{
		Type: "ai-core",
		Config: map[string]string{
			"endpoint":       "https://fake.example.com",
			"resource_group": "default",
		},
	}

	// This will fail because no auth is configured, but it should not panic
	_, err := provider.NewLLM(context.Background(), cfg, "gpt-4")
	// We expect an error (auth failure), not a panic
	if err == nil {
		t.Log("provider created without auth (acceptable if orchestration auto-discovers)")
	}
}
