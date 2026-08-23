package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
	gh "github.com/PedroKlein/duto-ai/internal/tool/github"
)

func TestClient_UsesTrustedBindingAndGETEndpoints(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)

		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}

		if r.Header.Get("Authorization") != "Bearer token" {
			t.Error("missing trusted authorization header")
		}

		switch r.URL.Path {
		case "/repos/owner/repo/issues/42":
			fmt.Fprint(w, `{"title":"issue","body":"body","state":"open","user":{"login":"alice"},"labels":[]}`)
		case "/repos/owner/repo/pulls/42":
			if r.Header.Get("Accept") == "application/vnd.github.v3.diff" {
				fmt.Fprint(w, "diff --git a/a b/a\n")
				return
			}

			fmt.Fprint(w, `{"title":"pr","body":"body","state":"open","user":{"login":"alice"},"base":{"ref":"main"},"head":{"ref":"topic"},"labels":[]}`)
		case "/repos/owner/repo/pulls/42/files":
			fmt.Fprint(w, `[{"filename":"a.go","status":"modified","additions":1,"deletions":0,"patch":"@@"}]`)
		case "/repos/owner/repo/issues/42/comments":
			fmt.Fprint(w, `[{"body":"comment","user":{"login":"alice"}}]`)
		case "/repos/owner/repo/pulls/42/reviews":
			fmt.Fprint(w, `[{"body":"review","state":"APPROVED","user":{"login":"bob"}}]`)
		case "/repos/owner/repo/commits/abc123/check-runs":
			fmt.Fprint(w, `{"check_runs":[{"name":"test","status":"completed","conclusion":"success"}]}`)
		case "/search/issues":
			if got := r.URL.Query().Get("q"); got != "bug repo:owner/repo" {
				t.Errorf("search query = %q", got)
			}

			fmt.Fprint(w, `{"items":[{"number":7,"title":"bug","state":"open","user":{"login":"carol"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := newClient(t, policy(srv))
	ctx := t.Context()

	if _, err := client.ReadIssue(ctx); err != nil {
		t.Fatalf("ReadIssue() error = %v", err)
	}

	if _, err := client.ReadPR(ctx); err != nil {
		t.Fatalf("ReadPR() error = %v", err)
	}

	if _, err := client.ReadDiff(ctx); err != nil {
		t.Fatalf("ReadDiff() error = %v", err)
	}

	if _, err := client.ListChangedFiles(ctx); err != nil {
		t.Fatalf("ListChangedFiles() error = %v", err)
	}

	if _, err := client.ReadComments(ctx); err != nil {
		t.Fatalf("ReadComments() error = %v", err)
	}

	if _, err := client.ReadReviews(ctx); err != nil {
		t.Fatalf("ReadReviews() error = %v", err)
	}

	if _, err := client.ReadChecks(ctx); err != nil {
		t.Fatalf("ReadChecks() error = %v", err)
	}

	if _, err := client.SearchIssues(ctx, gh.SearchIssuesInput{Query: "bug"}); err != nil {
		t.Fatalf("SearchIssues() error = %v", err)
	}

	if got := requests.Load(); got != 8 {
		t.Fatalf("requests = %d, want 8", got)
	}
}

func TestClient_PaginatesWithinTrustedBounds(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)

		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		wantPerPage := 100
		count := 100

		if page == 2 {
			wantPerPage = 50
			count = 1
		}

		if got := r.URL.Query().Get("per_page"); got != strconv.Itoa(wantPerPage) {
			t.Errorf("per_page = %q, want %d", got, wantPerPage)
		}

		comments := make([]map[string]any, count)
		for i := range comments {
			comments[i] = map[string]any{"body": "comment", "user": map[string]any{"login": "reader"}}
		}

		_ = json.NewEncoder(w).Encode(comments)
	}))
	defer srv.Close()

	p := policy(srv)
	p.MaxPages = 2
	p.MaxResults = 150
	client := newClient(t, p)

	result, err := client.ReadComments(t.Context())
	if err != nil {
		t.Fatalf("ReadComments() error = %v", err)
	}

	if len(result.Comments) != 101 || result.Truncated {
		t.Fatalf("comments = %d, truncated = %v", len(result.Comments), result.Truncated)
	}

	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2", requests.Load())
	}
}

func TestClient_RejectsPolicyViolationsBeforeProtectedRequest(t *testing.T) {
	t.Parallel()

	t.Run("invalid endpoint", func(t *testing.T) {
		var requests atomic.Int32

		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
		defer srv.Close()

		p := policy(nil)
		p.BaseURL = srv.URL

		p.HTTPClient = srv.Client()
		if _, err := gh.NewClient(p); !errors.Is(err, gh.ErrInvalidPolicy) {
			t.Fatalf("NewClient() error = %v", err)
		}

		if requests.Load() != 0 {
			t.Fatalf("requests = %d, want 0", requests.Load())
		}
	})

	t.Run("repository qualifier", func(t *testing.T) {
		var requests atomic.Int32

		srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
		defer srv.Close()

		client := newClient(t, policy(srv))

		_, err := client.SearchIssues(t.Context(), gh.SearchIssuesInput{Query: "repo:other/repo bug"})
		if !errors.Is(err, gh.ErrPolicyViolation) {
			t.Fatalf("SearchIssues() error = %v", err)
		}

		if requests.Load() != 0 {
			t.Fatalf("requests = %d, want 0", requests.Load())
		}
	})

	t.Run("request bytes", func(t *testing.T) {
		var requests atomic.Int32

		srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
		defer srv.Close()

		p := policy(srv)
		limit := p.Limits["github.read.search-issues"]
		limit.MaxRequestBytes = len(srv.URL)
		p.Limits["github.read.search-issues"] = limit
		client := newClient(t, p)

		_, err := client.SearchIssues(t.Context(), gh.SearchIssuesInput{Query: "bug"})
		if !errors.Is(err, gh.ErrRequestLimit) {
			t.Fatalf("SearchIssues() error = %v", err)
		}

		if requests.Load() != 0 {
			t.Fatalf("requests = %d, want 0", requests.Load())
		}
	})

	t.Run("canceled context", func(t *testing.T) {
		var requests atomic.Int32

		srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
		defer srv.Close()

		client := newClient(t, policy(srv))
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := client.ReadPR(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ReadPR() error = %v", err)
		}

		if requests.Load() != 0 {
			t.Fatalf("requests = %d, want 0", requests.Load())
		}
	})
}

func TestClient_BlocksRedirectOutsideTrustedEndpoint(t *testing.T) {
	t.Parallel()

	var targetRequests atomic.Int32

	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetRequests.Add(1)
	}))
	defer target.Close()

	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()

	p := policy(source)
	p.HTTPClient = source.Client()
	client := newClient(t, p)

	_, err := client.ReadPR(t.Context())
	if !errors.Is(err, gh.ErrPolicyViolation) {
		t.Fatalf("ReadPR() error = %v", err)
	}

	if targetRequests.Load() != 0 {
		t.Fatalf("target requests = %d, want 0", targetRequests.Load())
	}
}

func TestClient_RejectsOversizedResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", 129))
	}))
	defer srv.Close()

	p := policy(srv)
	limit := p.Limits["github.read.pr"]
	limit.MaxResultBytes = 128
	p.Limits["github.read.pr"] = limit
	client := newClient(t, p)

	_, err := client.ReadPR(t.Context())
	if !errors.Is(err, gh.ErrResponseLimit) {
		t.Fatalf("ReadPR() error = %v", err)
	}
}

func newClient(t *testing.T, p gh.Policy) *gh.Client {
	t.Helper()

	client, err := gh.NewClient(p)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	return client
}

func policy(srv *httptest.Server) gh.Policy {
	baseURL := "https://api.example.invalid"

	var client *http.Client

	if srv != nil {
		baseURL = srv.URL
		client = srv.Client()
	}

	limits := make(map[string]dtool.ToolLimit)
	for _, name := range []string{
		"github.read.changed-files",
		"github.read.checks",
		"github.read.comments",
		"github.read.diff",
		"github.read.issue",
		"github.read.pr",
		"github.read.reviews",
		"github.read.search-issues",
	} {
		limits[name] = dtool.ToolLimit{MaxCalls: 4, Timeout: time.Second, MaxRequestBytes: 4096, MaxResultBytes: 1 << 20}
	}

	return gh.Policy{
		BaseURL:    baseURL,
		Token:      "token",
		Owner:      "owner",
		Repository: "repo",
		Subject:    42,
		Ref:        "abc123",
		MaxPages:   2,
		MaxResults: 150,
		Limits:     limits,
		HTTPClient: client,
	}
}
