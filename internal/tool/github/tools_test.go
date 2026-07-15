package github_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool/toolconfirmation"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
	gh "github.com/PedroKlein/duto-ai/internal/tool/github"
)

func TestRegisterAll(t *testing.T) {
	reg := dtool.NewRegistry()
	client := gh.NewClient("test-token", "https://api.github.com")

	err := gh.RegisterAll(reg, client)
	if err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	expectedTools := []string{
		"github.add-labels",
		"github.create-issue",
		"github.edit-issue",
		"github.list-changed-files",
		"github.merge-pr",
		"github.post-comment",
		"github.post-review",
		"github.read-checks",
		"github.read-comments",
		"github.read-diff",
		"github.read-pr",
		"github.read-reviews",
		"github.request-reviewers",
		"github.search-issues",
	}

	names := reg.Names()
	if len(names) != len(expectedTools) {
		t.Fatalf("registered %d tools, want %d: %v", len(names), len(expectedTools), names)
	}

	for i, name := range names {
		if name != expectedTools[i] {
			t.Errorf("names[%d] = %q, want %q", i, name, expectedTools[i])
		}
	}
}

func TestRegisterAll_ToolsHaveDescriptions(t *testing.T) {
	reg := dtool.NewRegistry()
	client := gh.NewClient("token", "https://api.github.com")

	if err := gh.RegisterAll(reg, client); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	for name, tool := range reg.All() {
		if tool.Description() == "" {
			t.Errorf("tool %q has empty description", name)
		}
	}
}

func TestToolExecution_ListChangedFiles(t *testing.T) {
	// Set up mock GitHub server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `[{"filename":"main.go","status":"modified","additions":5,"deletions":2,"patch":"@@ -1,3 +1,8 @@"}]`)
	}))
	defer srv.Close()

	reg := dtool.NewRegistry()
	client := gh.NewClient("token", srv.URL)

	if err := gh.RegisterAll(reg, client); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	tool, ok := reg.Get("github.list-changed-files")
	if !ok {
		t.Fatal("tool not found")
	}

	// Execute through ADK's tool.Run interface — this catches the map[string]any conversion bug
	type runner interface {
		Run(ctx agent.Context, args any) (map[string]any, error)
	}

	r, ok := tool.(runner)
	if !ok {
		t.Fatal("tool does not implement Run")
	}

	ctx := &toolTestContext{StrictContextMock: agent.StrictContextMock{Ctx: t.Context()}}

	result, err := r.Run(ctx, map[string]any{
		"owner":  "PedroKlein",
		"repo":   "duto-test",
		"number": float64(1), // JSON numbers are float64
	})
	if err != nil {
		t.Fatalf("tool.Run: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Result should have a "files" key with the array
	files, ok := result["files"]
	if !ok {
		t.Fatalf("result missing 'files' key, got: %v", result)
	}

	filesList, ok := files.([]any)
	if !ok {
		t.Fatalf("files is not []any, got %T: %v", files, files)
	}

	if len(filesList) != 1 {
		t.Errorf("expected 1 file, got %d", len(filesList))
	}
}

func TestToolExecution_PostReview(t *testing.T) {
	var received []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	reg := dtool.NewRegistry()
	client := gh.NewClient("token", srv.URL)

	if err := gh.RegisterAll(reg, client); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	tool, ok := reg.Get("github.post-review")
	if !ok {
		t.Fatal("tool not found")
	}

	type runner interface {
		Run(ctx agent.Context, args any) (map[string]any, error)
	}

	r, ok := tool.(runner)
	if !ok {
		t.Fatal("tool does not implement Run")
	}

	ctx := &toolTestContext{StrictContextMock: agent.StrictContextMock{Ctx: t.Context()}}

	result, err := r.Run(ctx, map[string]any{
		"owner":  "PedroKlein",
		"repo":   "duto-test",
		"number": float64(1),
		"body":   "LGTM",
		"event":  "COMMENT",
	})
	if err != nil {
		t.Fatalf("tool.Run: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if len(received) == 0 {
		t.Error("expected POST body to be sent")
	}
}

// toolTestContext overrides ToolConfirmation to return nil (no HITL required).
type toolTestContext struct {
	agent.StrictContextMock
}

func (c *toolTestContext) ToolConfirmation() *toolconfirmation.ToolConfirmation {
	return nil
}
