// Package web provides HTTP tools (fetch, request) for AI workflows.
package web

import (
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

// FetchArgs is the input schema for the web.fetch tool.
type FetchArgs struct {
	URL string `json:"url"` // URL to fetch (GET)
}

// FetchResult is the output of the web.fetch tool.
type FetchResult struct {
	Status    int    `json:"status"`    // HTTP status code
	Body      string `json:"body"`      // response body (truncated at 1MB)
	Truncated bool   `json:"truncated"` // whether body was truncated
}

// RequestArgs is the input schema for the web.request tool.
type RequestArgs struct {
	Method  string            `json:"method"`            // HTTP method (GET, POST, PUT, etc.)
	URL     string            `json:"url"`               // request URL
	Headers map[string]string `json:"headers,omitempty"` // request headers
	Body    string            `json:"body,omitempty"`    // request body
}

// RequestResult is the output of the web.request tool.
type RequestResult struct {
	Status    int    `json:"status"`    // HTTP status code
	Body      string `json:"body"`      // response body (truncated at 1MB)
	Truncated bool   `json:"truncated"` // whether body was truncated
}

// RegisterAll creates web tools and registers them.
func RegisterAll(reg *dtool.Registry) error {
	tools := []struct {
		name   string
		create func() (tool.Tool, error)
	}{
		{"web.fetch", newFetchTool},
		{"web.request", newRequestTool},
	}

	for _, t := range tools {
		adkTool, err := t.create()
		if err != nil {
			return fmt.Errorf("creating tool %s: %w", t.name, err)
		}

		reg.Register(t.name, adkTool)
	}

	return nil
}

func newFetchTool() (tool.Tool, error) {
	return functiontool.New[FetchArgs, *FetchResult](
		functiontool.Config{
			Name:        "web.fetch",
			Description: "Fetch a URL via HTTP GET. Returns status code and body (up to 1MB).",
		},
		func(_ agent.Context, args FetchArgs) (*FetchResult, error) {
			return Fetch(args.URL)
		},
	)
}

func newRequestTool() (tool.Tool, error) {
	return functiontool.New[RequestArgs, *RequestResult](
		functiontool.Config{
			Name:        "web.request",
			Description: "Make an HTTP request with configurable method, headers, and body. Returns status code and response body (up to 1MB).",
		},
		func(_ agent.Context, args RequestArgs) (*RequestResult, error) {
			return Request(args)
		},
	)
}
