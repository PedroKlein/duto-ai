package tool_test

import (
	"testing"

	"github.com/PedroKlein/duto-ai/internal/tool"
)

// mockTool is a minimal tool for testing.
type mockTool struct {
	name string
}

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string { return "mock tool: " + m.name }

func newMockTool(name string) *mockTool { return &mockTool{name: name} }

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register("github.read-pr", newMockTool("github.read-pr"))
	reg.Register("github.read-diff", newMockTool("github.read-diff"))

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
	reg := tool.NewRegistry()
	reg.Register("files.read", newMockTool("files.read"))
	reg.Register("github.read-pr", newMockTool("github.read-pr"))
	reg.Register("github.read-diff", newMockTool("github.read-diff"))

	names := reg.Names()
	expected := []string{"files.read", "github.read-diff", "github.read-pr"}

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
	reg := tool.NewRegistry()
	reg.Register("a", newMockTool("a"))
	reg.Register("b", newMockTool("b"))

	all := reg.All()
	if len(all) != 2 {
		t.Fatalf("len = %d, want 2", len(all))
	}
}
