package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gh "github.com/PedroKlein/duto-ai/internal/tool/github"
)

func TestReadPR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}

		if r.URL.Path != "/repos/owner/repo/pulls/42" {
			t.Errorf("path = %s, want /repos/owner/repo/pulls/42", r.URL.Path)
		}

		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing auth header")
		}

		resp := map[string]any{
			"title": "Fix bug",
			"body":  "This fixes a bug",
			"state": "open",
			"user":  map[string]any{"login": "alice"},
			"base":  map[string]any{"ref": "main"},
			"head":  map[string]any{"ref": "fix/bug"},
			"labels": []any{
				map[string]any{"name": "bug"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := gh.NewClient("test-token", srv.URL)

	pr, err := client.ReadPR(context.Background(), gh.ReadPRInput{
		Owner: "owner", Repo: "repo", Number: 42,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pr.Title != "Fix bug" {
		t.Errorf("title = %q, want %q", pr.Title, "Fix bug")
	}

	if pr.Author != "alice" {
		t.Errorf("author = %q, want %q", pr.Author, "alice")
	}

	if pr.Base != "main" {
		t.Errorf("base = %q, want %q", pr.Base, "main")
	}

	if len(pr.Labels) != 1 || pr.Labels[0] != "bug" {
		t.Errorf("labels = %v, want [bug]", pr.Labels)
	}
}

func TestReadDiff(t *testing.T) {
	expectedDiff := "diff --git a/file.go b/file.go\n+added line"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github.v3.diff" {
			t.Error("wrong accept header for diff")
		}

		w.Write([]byte(expectedDiff))
	}))
	defer srv.Close()

	client := gh.NewClient("token", srv.URL)

	diff, err := client.ReadDiff(context.Background(), gh.ReadPRInput{
		Owner: "o", Repo: "r", Number: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if diff != expectedDiff {
		t.Errorf("diff = %q, want %q", diff, expectedDiff)
	}
}

func TestListChangedFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		files := []map[string]any{
			{"filename": "main.go", "status": "modified", "additions": 10, "deletions": 2, "patch": "@@ -1,2 +1,10 @@"},
		}
		json.NewEncoder(w).Encode(files)
	}))
	defer srv.Close()

	client := gh.NewClient("token", srv.URL)

	files, err := client.ListChangedFiles(context.Background(), gh.ReadPRInput{
		Owner: "o", Repo: "r", Number: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("len = %d, want 1", len(files))
	}

	if files[0].Filename != "main.go" {
		t.Errorf("filename = %q, want %q", files[0].Filename, "main.go")
	}
}

func TestPostReview(t *testing.T) {
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}

		if r.URL.Path != "/repos/o/r/pulls/1/reviews" {
			t.Errorf("path = %s", r.URL.Path)
		}

		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id": 1}`))
	}))
	defer srv.Close()

	client := gh.NewClient("token", srv.URL)

	err := client.PostReview(context.Background(), gh.PostReviewInput{
		Owner: "o", Repo: "r", Number: 1,
		Body: "LGTM", Event: "COMMENT",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedBody["body"] != "LGTM" {
		t.Errorf("body = %v, want LGTM", receivedBody["body"])
	}

	if receivedBody["event"] != "COMMENT" {
		t.Errorf("event = %v, want COMMENT", receivedBody["event"])
	}
}

func TestPostComment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues/1/comments" {
			t.Errorf("path = %s", r.URL.Path)
		}

		w.Write([]byte(`{"id": 1}`))
	}))
	defer srv.Close()

	client := gh.NewClient("token", srv.URL)

	err := client.PostComment(context.Background(), gh.PostCommentInput{
		Owner: "o", Repo: "r", Number: 1, Body: "Nice!",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message": "Not Found"}`))
	}))
	defer srv.Close()

	client := gh.NewClient("token", srv.URL)

	_, err := client.ReadPR(context.Background(), gh.ReadPRInput{
		Owner: "o", Repo: "r", Number: 999,
	})
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}
