// Package web provides a bounded HTTPS read tool for AI workflows.
package web

import (
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool/functiontool"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

type FetchArgs struct {
	URL string `json:"url"`
}

type FetchResult struct {
	Status    int    `json:"status"`
	Body      string `json:"body"`
	Truncated bool   `json:"truncated"`
}

func RegisterAll(registry *dtool.Registry, client *Client) error {
	if registry == nil || client == nil {
		return ErrInvalidPolicy
	}

	current, err := functiontool.New[FetchArgs, *FetchResult](
		functiontool.Config{Name: "web.fetch", Description: "Fetch an HTTPS URL admitted by trusted domain and redirect policy."},
		func(ctx agent.Context, args FetchArgs) (*FetchResult, error) { return client.Fetch(ctx, args.URL) },
	)
	if err != nil {
		return err
	}

	registry.Register("web.fetch", current)

	return nil
}
