package github_test

import (
	"testing"

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
		"github.list-changed-files",
		"github.post-comment",
		"github.post-review",
		"github.read-diff",
		"github.read-pr",
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
