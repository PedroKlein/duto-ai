package github

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Client is a GitHub API HTTP client.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates a GitHub API client.
func NewClient(token, baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		token:      token,
		httpClient: http.DefaultClient,
	}
}

// BaseURL returns the client's base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}

func (c *Client) get(ctx context.Context, path, accept string) ([]byte, error) {
	url := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(req, accept)

	return c.do(req)
}

func (c *Client) post(ctx context.Context, path string, body []byte) error {
	return c.mutate(ctx, http.MethodPost, path, body)
}

func (c *Client) patch(ctx context.Context, path string, body []byte) error {
	return c.mutate(ctx, http.MethodPatch, path, body)
}

func (c *Client) put(ctx context.Context, path string, body []byte) error {
	return c.mutate(ctx, http.MethodPut, path, body)
}

func (c *Client) mutate(ctx context.Context, method, path string, body []byte) error {
	url := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(req, "application/vnd.github.v3+json")
	req.Header.Set("Content-Type", "application/json")

	_, err = c.do(req)

	return err
}

func (c *Client) setHeaders(req *http.Request, accept string) {
	if accept == "" {
		accept = "application/vnd.github.v3+json"
	}

	req.Header.Set("Accept", accept)

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// ErrAPIResponse is returned when the GitHub API returns an error status code.
var ErrAPIResponse = errors.New("GitHub API error")

func (c *Client) do(req *http.Request) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("%w: status %d: %s", ErrAPIResponse, resp.StatusCode, string(body))
	}

	return body, nil
}
