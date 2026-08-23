package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/PedroKlein/adk-provider-sapaicore/sapaicore"
	"google.golang.org/adk/v2/model"

	"github.com/PedroKlein/duto-ai/internal/config"
)

var errUnknownBundledProvider = errors.New("unknown provider type")

type bundledProvider struct {
	provider *sapaicore.Provider
}

func newBundledProvider(ctx context.Context, source config.Provider) (*bundledProvider, error) {
	if source.Type != "ai-core" {
		return nil, errUnknownBundledProvider
	}

	provider, err := sapaicore.NewProvider(ctx, bundledProviderOptions(source)...)
	if err != nil {
		return nil, fmt.Errorf("creating bundled provider: %w", err)
	}

	return &bundledProvider{provider: provider}, nil
}

func (p *bundledProvider) model(name string) (model.LLM, error) {
	llm, err := p.provider.Model(name)
	if err != nil {
		return nil, fmt.Errorf("creating bundled model: %w", err)
	}

	return llm, nil
}

func bundledProviderOptions(source config.Provider) []sapaicore.Option {
	var options []sapaicore.Option

	if endpoint := source.Config["endpoint"]; endpoint != "" {
		options = append(options, sapaicore.WithEndpoint(endpoint))
	}

	if resourceGroup := source.Config["resource_group"]; resourceGroup != "" {
		options = append(options, sapaicore.WithResourceGroup(resourceGroup))
	}

	clientID := source.Config["client_id"]
	clientSecret := source.Config["client_secret"]

	authURL := source.Config["auth_url"]
	if clientID != "" && clientSecret != "" && authURL != "" {
		options = append(options, sapaicore.WithAuth(clientID, clientSecret, authURL))
	}

	if deploymentID := source.Config["deployment_id"]; deploymentID != "" {
		options = append(options, sapaicore.WithDeploymentID(deploymentID))
	} else {
		options = append(options, sapaicore.WithOrchestration())
	}

	return options
}
