package provider

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/adk/v2/model"

	"github.com/PedroKlein/adk-provider-sapaicore/sapaicore"
	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/envutil"
)

// ErrUnknownProviderType is returned when the provider type is not supported.
var ErrUnknownProviderType = errors.New("unknown provider type")

// Provider wraps a configured LLM provider that can create model instances.
type Provider struct {
	sap *sapaicore.Provider
}

// NewProvider creates a Provider from the config definition.
func NewProvider(ctx context.Context, cfg config.Provider) (*Provider, error) {
	switch cfg.Type {
	case "ai-core":
		opts := buildAICoreOptions(cfg)

		p, err := sapaicore.NewProvider(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("creating ai-core provider: %w", err)
		}

		return &Provider{sap: p}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownProviderType, cfg.Type)
	}
}

// Model returns a model.LLM for the given model name.
func (p *Provider) Model(name string) (model.LLM, error) {
	llm, err := p.sap.Model(name)
	if err != nil {
		return nil, fmt.Errorf("creating model %q: %w", name, err)
	}

	return llm, nil
}

// NewLLM creates a model.LLM from the config provider definition and model name.
// Convenience function that creates a provider and immediately resolves one model.
func NewLLM(ctx context.Context, cfg config.Provider, modelName string) (model.LLM, error) {
	p, err := NewProvider(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return p.Model(modelName)
}

func buildAICoreOptions(cfg config.Provider) []sapaicore.Option {
	var opts []sapaicore.Option

	if endpoint := envutil.Expand(cfg.Config["endpoint"]); endpoint != "" {
		opts = append(opts, sapaicore.WithEndpoint(endpoint))
	}

	if rg := envutil.Expand(cfg.Config["resource_group"]); rg != "" {
		opts = append(opts, sapaicore.WithResourceGroup(rg))
	}

	clientID := envutil.Expand(cfg.Config["client_id"])
	clientSecret := envutil.Expand(cfg.Config["client_secret"])
	authURL := envutil.Expand(cfg.Config["auth_url"])

	if clientID != "" && clientSecret != "" && authURL != "" {
		opts = append(opts, sapaicore.WithAuth(clientID, clientSecret, authURL))
	}

	if depID := envutil.Expand(cfg.Config["deployment_id"]); depID != "" {
		opts = append(opts, sapaicore.WithDeploymentID(depID))
	}

	if cfg.Config["deployment_id"] == "" {
		opts = append(opts, sapaicore.WithOrchestration())
	}

	return opts
}
