package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

var (
	ErrAPIResponse     = errors.New("GitHub API error")
	ErrInvalidPolicy   = errors.New("invalid GitHub tool policy")
	ErrPolicyViolation = errors.New("GitHub request violates trusted policy")
	ErrRequestLimit    = errors.New("GitHub request byte limit exceeded")
	ErrResponseLimit   = errors.New("GitHub response byte limit exceeded")
)

var repositoryName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

var githubToolNames = []string{
	"github.read.changed-files",
	"github.read.checks",
	"github.read.comments",
	"github.read.diff",
	"github.read.issue",
	"github.read.pr",
	"github.read.reviews",
	"github.read.search-issues",
}

type Policy struct {
	BaseURL    string
	Token      string
	Owner      string
	Repository string
	Subject    int
	Ref        string
	MaxPages   int
	MaxResults int
	Limits     map[string]dtool.ToolLimit
	HTTPClient *http.Client
}

type Client struct {
	baseURL    *url.URL
	token      string
	owner      string
	repository string
	subject    int
	ref        string
	maxPages   int
	maxResults int
	limits     map[string]dtool.ToolLimit
	httpClient *http.Client
}

func NewClient(policy Policy) (*Client, error) {
	baseURL, err := parseBaseURL(policy.BaseURL)
	if err != nil {
		return nil, err
	}

	if err := validateBinding(policy); err != nil {
		return nil, err
	}

	if err := validateLimits(policy.Limits); err != nil {
		return nil, err
	}

	return &Client{
		baseURL:    baseURL,
		token:      policy.Token,
		owner:      policy.Owner,
		repository: policy.Repository,
		subject:    policy.Subject,
		ref:        policy.Ref,
		maxPages:   policy.MaxPages,
		maxResults: policy.MaxResults,
		limits:     maps.Clone(policy.Limits),
		httpClient: redirectRejectingClient(policy.HTTPClient),
	}, nil
}

func parseBaseURL(raw string) (*url.URL, error) {
	baseURL, err := url.Parse(raw)
	if err != nil || baseURL.Scheme != "https" || baseURL.Host == "" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, ErrInvalidPolicy
	}

	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	baseURL.RawPath = ""

	return baseURL, nil
}

func validateBinding(policy Policy) error {
	if !repositoryName.MatchString(policy.Owner) || !repositoryName.MatchString(policy.Repository) {
		return ErrInvalidPolicy
	}

	if policy.Subject <= 0 || policy.Ref == "" || strings.ContainsAny(policy.Ref, "\x00\r\n") {
		return ErrInvalidPolicy
	}

	if policy.MaxPages <= 0 || policy.MaxResults <= 0 {
		return ErrInvalidPolicy
	}

	return nil
}

func validateLimits(limits map[string]dtool.ToolLimit) error {
	if len(limits) == 0 {
		return ErrInvalidPolicy
	}

	for name, limit := range limits {
		if !slices.Contains(githubToolNames, name) || limit.MaxCalls <= 0 || limit.Timeout <= 0 || limit.MaxRequestBytes < 0 || limit.MaxResultBytes <= 0 {
			return fmt.Errorf("%w: %s", ErrInvalidPolicy, name)
		}
	}

	return nil
}

func redirectRejectingClient(source *http.Client) *http.Client {
	if source == nil {
		source = http.DefaultClient
	}

	result := *source
	result.CheckRedirect = func(*http.Request, []*http.Request) error { return ErrPolicyViolation }

	return &result
}

func (c *Client) get(ctx context.Context, toolName, path, accept string, query url.Values, maxBytes int) ([]byte, error) {
	request, responseLimit, err := c.newRequest(ctx, toolName, path, accept, query, maxBytes)
	if err != nil {
		return nil, err
	}

	return c.execute(request, responseLimit)
}

func (c *Client) newRequest(ctx context.Context, toolName, path, accept string, query url.Values, maxBytes int) (*http.Request, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, fmt.Errorf("GitHub request context: %w", err)
	}

	if c == nil || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\x00\r\n") {
		return nil, 0, ErrPolicyViolation
	}

	limit, ok := c.limits[toolName]
	if !ok {
		return nil, 0, ErrPolicyViolation
	}

	endpoint := strings.TrimRight(c.baseURL.String(), "/") + path
	if encodedQuery := query.Encode(); encodedQuery != "" {
		endpoint += "?" + encodedQuery
	}

	if limit.MaxRequestBytes > 0 && len(endpoint) > limit.MaxRequestBytes {
		return nil, 0, ErrRequestLimit
	}

	if maxBytes <= 0 || maxBytes > limit.MaxResultBytes {
		maxBytes = limit.MaxResultBytes
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, 0, fmt.Errorf("creating GitHub request: %w", err)
	}

	if accept == "" {
		accept = "application/vnd.github.v3+json"
	}

	request.Header.Set("Accept", accept)

	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	return request, maxBytes, nil
}

func (c *Client) execute(request *http.Request, maxBytes int) ([]byte, error) {
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("executing GitHub request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(response.Body, int64(maxBytes)+1))
	if err != nil {
		return nil, fmt.Errorf("reading GitHub response: %w", err)
	}

	if len(body) > maxBytes {
		return nil, ErrResponseLimit
	}

	if response.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("%w: status %d", ErrAPIResponse, response.StatusCode)
	}

	return body, nil
}

func (c *Client) repositoryPath(resource string) string {
	return "/repos/" + url.PathEscape(c.owner) + "/" + url.PathEscape(c.repository) + resource
}
