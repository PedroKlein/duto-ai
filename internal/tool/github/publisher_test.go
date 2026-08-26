package github_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/publisher"
	"github.com/PedroKlein/duto-ai/internal/safeoutput"
	gh "github.com/PedroKlein/duto-ai/internal/tool/github"
)

func replyPublisherOperation() publisher.Operation {
	return publisher.Operation{
		RequestID: "req", CorrelationKey: "issue-42", Kind: safeoutput.KindReply,
		RepositoryOwner: "owner", RepositoryName: "repo", OriginKind: "issue", OriginNumber: 42,
		ReplyBody: "hello", PayloadSHA256: "payload",
	}
}

func TestPublisher_RejectsInvalidPolicyBeforeConstruction(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		policy gh.PublisherPolicy
	}{
		{name: "missing token", policy: gh.PublisherPolicy{BaseURL: "https://api.example.invalid", Owner: "owner", Repository: "repo"}},
		{name: "bad owner", policy: gh.PublisherPolicy{BaseURL: "https://api.example.invalid", Token: "t", Owner: "own er", Repository: "repo"}},
		{name: "empty base", policy: gh.PublisherPolicy{Token: "t", Owner: "owner", Repository: "repo"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := gh.NewPublisher(tc.policy); err == nil {
				t.Fatal("NewPublisher() error = nil, want rejection")
			}
		})
	}
}

func TestPublisher_ReplyPreflightThenApply(t *testing.T) {
	t.Parallel()

	var gets, posts atomic.Int32

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("missing trusted authorization header")
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues/42":
			gets.Add(1)

			_, _ = w.Write([]byte(`{"state":"open"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues/42/comments":
			gets.Add(1)

			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/42/comments":
			posts.Add(1)

			_, _ = w.Write([]byte(`{"html_url":"https://example.invalid/c/1"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	adapter, err := gh.NewPublisher(gh.PublisherPolicy{
		BaseURL: srv.URL, Token: "token", Owner: "owner", Repository: "repo", HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}

	operation := replyPublisherOperation()

	state, err := adapter.Preflight(context.Background(), operation)
	if err != nil || state.Disposition != "" {
		t.Fatalf("Preflight = %#v, err = %v, want absent", state, err)
	}

	if posts.Load() != 0 {
		t.Fatalf("preflight performed %d writes, want 0", posts.Load())
	}

	resource, err := adapter.Apply(context.Background(), operation)
	if err != nil || resource != "https://example.invalid/c/1" {
		t.Fatalf("Apply resource = %q, err = %v", resource, err)
	}

	if posts.Load() != 1 {
		t.Fatalf("apply performed %d writes, want 1", posts.Load())
	}
}

func TestPublisher_ReplyUnchangedWhenMarkerPresent(t *testing.T) {
	t.Parallel()

	var posts atomic.Int32

	operation := replyPublisherOperation()
	marker := publisher.Marker(operation)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/issues/42"):
			_, _ = w.Write([]byte(`{"state":"open"}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/comments"):
			_, _ = w.Write([]byte(`[{"body":"prior\n\n` + marker + `","html_url":"https://example.invalid/c/9"}]`))
		case r.Method == http.MethodPost:
			posts.Add(1)

			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	adapter, err := gh.NewPublisher(gh.PublisherPolicy{
		BaseURL: srv.URL, Token: "token", Owner: "owner", Repository: "repo", HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}

	state, err := adapter.Preflight(context.Background(), operation)
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}

	if state.Disposition != publisher.DispositionUnchanged || state.Resource != "https://example.invalid/c/9" {
		t.Fatalf("state = %#v, want unchanged with prior resource", state)
	}

	if posts.Load() != 0 {
		t.Fatalf("preflight performed %d writes, want 0", posts.Load())
	}
}

func TestPublisher_RejectsMismatchedRepository(t *testing.T) {
	t.Parallel()

	adapter, err := gh.NewPublisher(gh.PublisherPolicy{
		BaseURL: "https://api.example.invalid", Token: "token", Owner: "owner", Repository: "repo",
	})
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}

	operation := replyPublisherOperation()
	operation.RepositoryOwner = "other"

	if _, err := adapter.Preflight(context.Background(), operation); !errors.Is(err, publisher.ErrRejected) {
		t.Fatalf("Preflight(mismatched repo) error = %v, want ErrRejected", err)
	}

	if _, err := adapter.Apply(context.Background(), operation); !errors.Is(err, publisher.ErrRejected) {
		t.Fatalf("Apply(mismatched repo) error = %v, want ErrRejected", err)
	}
}
