package tool_test

import (
	"errors"
	"testing"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

func TestRegistry_FilteredToolsetExposesOnlyExactNames(t *testing.T) {
	registry := setupTestRegistry(t)

	toolset, err := registry.FilteredToolset([]string{"files.read", "github.read.pr"})
	if err != nil {
		t.Fatalf("FilteredToolset() error = %v", err)
	}

	tools, err := toolset.Tools(nil)
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}

	if len(tools) != 2 || tools[0].Name() != "files.read" || tools[1].Name() != "github.read.pr" {
		t.Fatalf("filtered tools = %#v", tools)
	}
}

func TestRegistry_FilteredToolsetRejectsMissingTool(t *testing.T) {
	registry := setupTestRegistry(t)

	_, err := registry.FilteredToolset([]string{"web.fetch"})
	if !errors.Is(err, dtool.ErrToolUnavailable) {
		t.Fatalf("FilteredToolset() error = %v", err)
	}
}

func TestNewToolsetReturnsDefensiveSlices(t *testing.T) {
	registry := setupTestRegistry(t)

	toolset, err := registry.FilteredToolset([]string{"files.read"})
	if err != nil {
		t.Fatalf("FilteredToolset() error = %v", err)
	}

	first, err := toolset.Tools(nil)
	if err != nil {
		t.Fatalf("first Tools() error = %v", err)
	}

	first[0] = nil

	second, err := toolset.Tools(nil)
	if err != nil {
		t.Fatalf("second Tools() error = %v", err)
	}

	if len(second) != 1 || second[0] == nil || second[0].Name() != "files.read" {
		t.Fatalf("second tools = %#v", second)
	}
}
