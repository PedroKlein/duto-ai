package github_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
	gh "github.com/PedroKlein/duto-ai/internal/tool/github"
)

func TestRegisterAll_ExposesOnlyHierarchicalReadTools(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	client := newClient(t, policy(srv))

	registry := dtool.NewRegistry()
	if err := gh.RegisterAll(registry, client); err != nil {
		t.Fatalf("RegisterAll() error = %v", err)
	}

	want := []string{
		"github.read.changed-files",
		"github.read.checks",
		"github.read.comments",
		"github.read.diff",
		"github.read.issue",
		"github.read.pr",
		"github.read.reviews",
		"github.read.search-issues",
	}
	if got := registry.Names(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("tool names = %v, want %v", got, want)
	}

	for _, name := range want {
		current, ok := registry.Get(name)
		if !ok || current.Description() == "" {
			t.Fatalf("tool %q missing or undescribed", name)
		}
	}
}

func TestGitHubToolSchemasDoNotExposeTrustedBindings(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	registry := dtool.NewRegistry()
	if err := gh.RegisterAll(registry, newClient(t, policy(srv))); err != nil {
		t.Fatalf("RegisterAll() error = %v", err)
	}

	for _, name := range registry.Names() {
		current, _ := registry.Get(name)

		declarer, ok := current.(interface {
			Declaration() *genai.FunctionDeclaration
		})
		if !ok {
			t.Fatalf("tool %q has no function declaration", name)
		}

		encoded, err := json.Marshal(declarer.Declaration().ParametersJsonSchema)
		if err != nil {
			t.Fatalf("encoding %q declaration: %v", name, err)
		}

		schema := string(encoded)
		for _, forbidden := range []string{"owner", "repo", "repository", "number", "subject", "ref", "url", "method"} {
			if strings.Contains(schema, `"`+forbidden+`"`) {
				t.Errorf("tool %q exposes trusted %q binding: %s", name, forbidden, schema)
			}
		}
	}
}

func TestToolExecution_UsesBoundSubject(t *testing.T) {
	t.Parallel()

	var path string

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path

		fmt.Fprint(w, `{"title":"pr","state":"open","user":{"login":"alice"},"base":{"ref":"main"},"head":{"ref":"topic"},"labels":[]}`)
	}))
	defer srv.Close()

	registry := dtool.NewRegistry()
	if err := gh.RegisterAll(registry, newClient(t, policy(srv))); err != nil {
		t.Fatalf("RegisterAll() error = %v", err)
	}

	current, _ := registry.Get("github.read.pr")

	runner, ok := current.(interface {
		Run(agent.Context, any) (map[string]any, error)
	})
	if !ok {
		t.Fatal("github.read.pr does not implement ADK Run")
	}

	ctx := &toolContext{StrictContextMock: agent.StrictContextMock{Ctx: t.Context()}}

	_, err := runner.Run(ctx, map[string]any{
		"owner":  "other",
		"repo":   "other",
		"number": float64(999),
		"ref":    "other",
	})
	if err == nil {
		t.Fatal("model-controlled binding unexpectedly accepted")
	}

	if path != "" {
		t.Fatalf("policy rejection made request to %q", path)
	}

	result, err := runner.Run(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if path != "/repos/owner/repo/pulls/42" {
		t.Fatalf("path = %q", path)
	}

	if result["title"] != "pr" {
		t.Fatalf("result = %#v", result)
	}
}

type toolContext struct {
	agent.StrictContextMock
}

func (*toolContext) ToolConfirmation() *toolconfirmation.ToolConfirmation {
	return nil
}
