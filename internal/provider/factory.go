package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"

	"google.golang.org/adk/v2/model"

	"github.com/PedroKlein/adk-provider-sapaicore/sapaicore"
	"github.com/PedroKlein/duto-ai/internal/config"
)

// ErrUnknownProviderType is returned when the provider type is not supported.
var ErrUnknownProviderType = errors.New("unknown provider type")

// NewLLM creates a model.LLM from the config provider definition and model name.
func NewLLM(ctx context.Context, cfg config.Provider, modelName string) (model.LLM, error) {
	switch cfg.Type {
	case "ai-core":
		return newAICoreLLM(ctx, cfg, modelName)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownProviderType, cfg.Type)
	}
}

func newAICoreLLM(ctx context.Context, cfg config.Provider, modelName string) (model.LLM, error) {
	opts := buildAICoreOptions(cfg)

	provider, err := sapaicore.NewProvider(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating ai-core provider: %w", err)
	}

	llm, err := provider.Model(modelName)
	if err != nil {
		return nil, fmt.Errorf("creating model %q: %w", modelName, err)
	}

	return llm, nil
}

func buildAICoreOptions(cfg config.Provider) []sapaicore.Option {
	var opts []sapaicore.Option

	// Endpoint
	if endpoint := expandEnv(cfg.Config["endpoint"]); endpoint != "" {
		opts = append(opts, sapaicore.WithEndpoint(endpoint))
	}

	// Resource group
	if rg := expandEnv(cfg.Config["resource_group"]); rg != "" {
		opts = append(opts, sapaicore.WithResourceGroup(rg))
	}

	// Auth credentials
	clientID := expandEnv(cfg.Config["client_id"])
	clientSecret := expandEnv(cfg.Config["client_secret"])
	authURL := expandEnv(cfg.Config["auth_url"])

	if clientID != "" && clientSecret != "" && authURL != "" {
		opts = append(opts, sapaicore.WithAuth(clientID, clientSecret, authURL))
	}

	// Deployment ID (foundation mode)
	if depID := expandEnv(cfg.Config["deployment_id"]); depID != "" {
		opts = append(opts, sapaicore.WithDeploymentID(depID))
	}

	// Default to orchestration mode
	if cfg.Config["deployment_id"] == "" {
		opts = append(opts, sapaicore.WithOrchestration())
	}

	return opts
}

var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

func expandEnv(s string) string {
	if s == "" {
		return ""
	}

	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		key := match[2 : len(match)-1]

		return os.Getenv(key)
	})
}
