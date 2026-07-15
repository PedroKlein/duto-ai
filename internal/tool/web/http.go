package web

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	requestTimeout = 30 * time.Second
	maxBody        = 1 << 20 // 1MB
)

// ErrMissingURL is returned when the URL argument is empty.
var ErrMissingURL = errors.New("url is required")

// Fetch performs an HTTP GET request.
func Fetch(url string) (*FetchResult, error) {
	result, err := Request(RequestArgs{Method: http.MethodGet, URL: url})
	if err != nil {
		return nil, err
	}

	return &FetchResult{
		Status:    result.Status,
		Body:      result.Body,
		Truncated: result.Truncated,
	}, nil
}

// Request performs an HTTP request with the given parameters.
func Request(args RequestArgs) (*RequestResult, error) {
	if args.URL == "" {
		return nil, ErrMissingURL
	}

	method := args.Method
	if method == "" {
		method = http.MethodGet
	}

	var bodyReader io.Reader
	if args.Body != "" {
		bodyReader = strings.NewReader(args.Body)
	}

	req, err := http.NewRequest(method, args.URL, bodyReader) //nolint:noctx // tool context doesn't carry http context
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	for key, val := range args.Headers {
		req.Header.Set(key, val)
	}

	client := &http.Client{Timeout: requestTimeout}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	truncated := len(body) > maxBody
	if truncated {
		body = body[:maxBody]
	}

	return &RequestResult{
		Status:    resp.StatusCode,
		Body:      string(body),
		Truncated: truncated,
	}, nil
}
