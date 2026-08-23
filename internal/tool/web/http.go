package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

var (
	ErrInvalidPolicy   = errors.New("invalid web tool policy")
	ErrPolicyViolation = errors.New("web request violates trusted policy")
	ErrRequestLimit    = errors.New("web request byte limit exceeded")
	ErrResponseLimit   = errors.New("web response byte limit exceeded")
)

type Policy struct {
	AllowedDomains []string
	MaxRedirects   int
	Limits         map[string]dtool.ToolLimit
	HTTPClient     *http.Client
}

type Client struct {
	allowedDomains map[string]struct{}
	limit          dtool.ToolLimit
	httpClient     *http.Client
}

func NewClient(policy Policy) (*Client, error) {
	limit, ok := policy.Limits["web.fetch"]
	if !ok || limit.MaxCalls <= 0 || limit.Timeout <= 0 || limit.MaxRequestBytes <= 0 || limit.MaxResultBytes <= 0 || policy.MaxRedirects < 0 || len(policy.AllowedDomains) == 0 {
		return nil, ErrInvalidPolicy
	}

	allowed := make(map[string]struct{}, len(policy.AllowedDomains))
	for _, domain := range slices.Clone(policy.AllowedDomains) {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if domain == "" || strings.ContainsAny(domain, "/:@\x00\r\n") {
			return nil, ErrInvalidPolicy
		}

		allowed[domain] = struct{}{}
	}

	base := http.DefaultClient
	if policy.HTTPClient != nil {
		base = policy.HTTPClient
	}

	clientCopy := *base
	clientCopy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > policy.MaxRedirects {
			return ErrPolicyViolation
		}

		if err := validateURL(req.URL, allowed); err != nil {
			return err
		}

		return nil
	}

	return &Client{allowedDomains: allowed, limit: limit, httpClient: &clientCopy}, nil
}

func (c *Client) Fetch(ctx context.Context, rawURL string) (*FetchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("web request context: %w", err)
	}

	if c == nil {
		return nil, ErrInvalidPolicy
	}

	if len(rawURL) > c.limit.MaxRequestBytes {
		return nil, ErrRequestLimit
	}

	endpoint, err := url.Parse(rawURL)
	if err != nil {
		return nil, ErrPolicyViolation
	}

	if validationErr := validateURL(endpoint, c.allowedDomains); validationErr != nil {
		return nil, validationErr
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating web request: %w", err)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("executing web request: %w", err)
	}

	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(response.Body, int64(c.limit.MaxResultBytes)+1))
	if err != nil {
		return nil, fmt.Errorf("reading web response: %w", err)
	}

	result := &FetchResult{Status: response.StatusCode, Body: string(body), Truncated: len(body) > c.limit.MaxResultBytes}
	if result.Truncated {
		result.Body = string(body[:c.limit.MaxResultBytes])
	}

	if err := fitResult(result, c.limit.MaxResultBytes); err != nil {
		return nil, err
	}

	return result, nil
}

func validateURL(endpoint *url.URL, allowed map[string]struct{}) error {
	if endpoint == nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil {
		return ErrPolicyViolation
	}

	if _, ok := allowed[strings.ToLower(endpoint.Hostname())]; !ok {
		return ErrPolicyViolation
	}

	return nil
}

func fitResult(result *FetchResult, limit int) error {
	encoded, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encoding web result: %w", err)
	}

	if len(encoded) <= limit {
		return nil
	}

	result.Truncated = true
	for len(encoded) > limit && result.Body != "" {
		remove := min(len(encoded)-limit, len(result.Body))
		result.Body = result.Body[:len(result.Body)-remove]

		encoded, err = json.Marshal(result)
		if err != nil {
			return fmt.Errorf("encoding bounded web result: %w", err)
		}
	}

	if len(encoded) > limit {
		return ErrResponseLimit
	}

	return nil
}
