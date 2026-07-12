package tool_test

import (
	"testing"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool/functiontool"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

type mockArgs struct {
	Input string `json:"input"`
}

type mockResult struct {
	Output string `json:"output"`
}

func newMockADKTool(t *testing.T, name, description string) *dtool.Registry {
	t.Helper()

	reg := dtool.NewRegistry()

	tool, err := functiontool.New[mockArgs, mockResult](
		functiontool.Config{Name: name, Description: description},
		func(_ agent.Context, args mockArgs) (mockResult, error) {
			return mockResult{Output: args.Input}, nil
		},
	)
	if err != nil {
		t.Fatalf("creating mock tool %s: %v", name, err)
	}

	reg.Register(name, tool)

	return reg
}

func setupTestRegistry(t *testing.T) *dtool.Registry {
	t.Helper()

	reg := dtool.NewRegistry()

	names := []string{
		"github.read-pr",
		"github.read-diff",
		"github.post-review",
		"files.read",
		"files.grep",
	}

	for _, name := range names {
		tool, err := functiontool.New[mockArgs, mockResult](
			functiontool.Config{Name: name, Description: "mock " + name},
			func(_ agent.Context, args mockArgs) (mockResult, error) {
				return mockResult{Output: args.Input}, nil
			},
		)
		if err != nil {
			t.Fatalf("creating mock tool %s: %v", name, err)
		}

		reg.Register(name, tool)
	}

	return reg
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := newMockADKTool(t, "github.read-pr", "reads a PR")

	got, ok := reg.Get("github.read-pr")
	if !ok {
		t.Fatal("expected to find github.read-pr")
	}

	if got.Name() != "github.read-pr" {
		t.Errorf("name = %q, want %q", got.Name(), "github.read-pr")
	}

	_, ok = reg.Get("nonexistent")
	if ok {
		t.Fatal("expected not to find nonexistent tool")
	}
}

func TestRegistry_Names(t *testing.T) {
	reg := setupTestRegistry(t)

	names := reg.Names()
	expected := []string{"files.grep", "files.read", "github.post-review", "github.read-diff", "github.read-pr"}

	if len(names) != len(expected) {
		t.Fatalf("len = %d, want %d", len(names), len(expected))
	}

	for i, name := range names {
		if name != expected[i] {
			t.Errorf("names[%d] = %q, want %q", i, name, expected[i])
		}
	}
}

func TestRegistry_All(t *testing.T) {
	reg := setupTestRegistry(t)

	all := reg.All()
	if len(all) != 5 {
		t.Fatalf("len = %d, want 5", len(all))
	}
}
