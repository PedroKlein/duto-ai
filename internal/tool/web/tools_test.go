package web_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/genai"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
	"github.com/PedroKlein/duto-ai/internal/tool/web"
)

func TestRegisterAll_ExposesOnlyBoundedFetch(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	client := newWebClient(t, webPolicy(srv))

	registry := dtool.NewRegistry()
	if err := web.RegisterAll(registry, client); err != nil {
		t.Fatalf("RegisterAll() error = %v", err)
	}

	if got := registry.Names(); len(got) != 1 || got[0] != "web.fetch" {
		t.Fatalf("tool names = %v", got)
	}

	current, _ := registry.Get("web.fetch")

	declarer, ok := current.(interface {
		Declaration() *genai.FunctionDeclaration
	})
	if !ok {
		t.Fatal("web.fetch has no declaration")
	}

	encoded, err := json.Marshal(declarer.Declaration().ParametersJsonSchema)
	if err != nil {
		t.Fatalf("encoding declaration: %v", err)
	}

	for _, forbidden := range []string{"method", "headers", "body"} {
		if strings.Contains(string(encoded), `"`+forbidden+`"`) {
			t.Fatalf("web.fetch exposes %q: %s", forbidden, encoded)
		}
	}
}

func TestFetch_GETAndResultBound(t *testing.T) {
	t.Parallel()

	var method string

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method

		fmt.Fprint(w, strings.Repeat("x", 256))
	}))
	defer srv.Close()

	p := webPolicy(srv)
	limit := p.Limits["web.fetch"]
	limit.MaxResultBytes = 128
	p.Limits["web.fetch"] = limit
	client := newWebClient(t, p)

	result, err := client.Fetch(t.Context(), srv.URL+"/page")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if method != http.MethodGet {
		t.Fatalf("method = %q, want GET", method)
	}

	if !result.Truncated {
		t.Fatal("expected bounded result to be truncated")
	}

	encoded, _ := json.Marshal(result)
	if len(encoded) > limit.MaxResultBytes {
		t.Fatalf("encoded result bytes = %d, limit = %d", len(encoded), limit.MaxResultBytes)
	}
}

func TestFetch_RejectsDisallowedRequestsBeforeBoundary(t *testing.T) {
	t.Parallel()

	t.Run("domain", func(t *testing.T) {
		var requests atomic.Int32

		target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
		defer target.Close()

		allowed := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		defer allowed.Close()

		client := newWebClient(t, webPolicy(allowed))

		_, err := client.Fetch(t.Context(), alternateHost(target.URL))
		if !errors.Is(err, web.ErrPolicyViolation) {
			t.Fatalf("Fetch() error = %v", err)
		}

		if requests.Load() != 0 {
			t.Fatalf("requests = %d, want 0", requests.Load())
		}
	})

	t.Run("http", func(t *testing.T) {
		var requests atomic.Int32

		target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
		defer target.Close()

		p := webPolicy(nil)
		parsed, _ := url.Parse(target.URL)
		p.AllowedDomains = []string{parsed.Hostname()}
		p.HTTPClient = target.Client()
		client := newWebClient(t, p)

		_, err := client.Fetch(t.Context(), target.URL)
		if !errors.Is(err, web.ErrPolicyViolation) {
			t.Fatalf("Fetch() error = %v", err)
		}

		if requests.Load() != 0 {
			t.Fatalf("requests = %d, want 0", requests.Load())
		}
	})

	t.Run("request bytes", func(t *testing.T) {
		var requests atomic.Int32

		srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
		defer srv.Close()

		p := webPolicy(srv)
		limit := p.Limits["web.fetch"]
		limit.MaxRequestBytes = len(srv.URL)
		p.Limits["web.fetch"] = limit
		client := newWebClient(t, p)

		_, err := client.Fetch(t.Context(), srv.URL+"/too-long")
		if !errors.Is(err, web.ErrRequestLimit) {
			t.Fatalf("Fetch() error = %v", err)
		}

		if requests.Load() != 0 {
			t.Fatalf("requests = %d, want 0", requests.Load())
		}
	})

	t.Run("canceled", func(t *testing.T) {
		var requests atomic.Int32

		srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
		defer srv.Close()

		client := newWebClient(t, webPolicy(srv))
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := client.Fetch(ctx, srv.URL)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Fetch() error = %v", err)
		}

		if requests.Load() != 0 {
			t.Fatalf("requests = %d, want 0", requests.Load())
		}
	})
}

func TestFetch_BlocksRedirectToDisallowedDomain(t *testing.T) {
	t.Parallel()

	var targetRequests atomic.Int32

	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetRequests.Add(1)
	}))
	defer target.Close()

	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, alternateHost(target.URL), http.StatusFound)
	}))
	defer source.Close()

	p := webPolicy(source)
	p.HTTPClient = source.Client()
	client := newWebClient(t, p)

	_, err := client.Fetch(t.Context(), source.URL)
	if !errors.Is(err, web.ErrPolicyViolation) {
		t.Fatalf("Fetch() error = %v", err)
	}

	if targetRequests.Load() != 0 {
		t.Fatalf("target requests = %d, want 0", targetRequests.Load())
	}
}

func TestFetch_BoundsRedirectCount(t *testing.T) {
	t.Parallel()

	var (
		requests  atomic.Int32
		serverURL string
	)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)

		switch r.URL.Path {
		case "/0":
			http.Redirect(w, r, serverURL+"/1", http.StatusFound)
		case "/1":
			http.Redirect(w, r, serverURL+"/2", http.StatusFound)
		default:
			fmt.Fprint(w, "unreachable")
		}
	}))
	defer server.Close()

	serverURL = server.URL

	client := newWebClient(t, webPolicy(server))

	_, err := client.Fetch(t.Context(), server.URL+"/0")
	if !errors.Is(err, web.ErrPolicyViolation) {
		t.Fatalf("Fetch() error = %v", err)
	}

	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2", requests.Load())
	}
}

func alternateHost(rawURL string) string {
	parsed, _ := url.Parse(rawURL)
	parsed.Host = "localhost:" + parsed.Port()

	return parsed.String()
}

func newWebClient(t *testing.T, p web.Policy) *web.Client {
	t.Helper()

	client, err := web.NewClient(p)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	return client
}

func webPolicy(srv *httptest.Server) web.Policy {
	domain := "example.invalid"

	var client *http.Client

	if srv != nil {
		parsed, _ := url.Parse(srv.URL)
		domain = parsed.Hostname()
		client = srv.Client()
	}

	return web.Policy{
		AllowedDomains: []string{domain},
		MaxRedirects:   1,
		Limits: map[string]dtool.ToolLimit{
			"web.fetch": {MaxCalls: 4, Timeout: time.Second, MaxRequestBytes: 4096, MaxResultBytes: 1 << 20},
		},
		HTTPClient: client,
	}
}
