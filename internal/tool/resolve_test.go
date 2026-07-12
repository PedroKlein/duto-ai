package tool_test

import (
	"testing"

	"github.com/PedroKlein/duto-ai/internal/tool"
)

func TestResolveNames(t *testing.T) {
	defaults := []string{"github.read-diff", "github.read-pr", "files.read"}

	tests := []struct {
		name      string
		stepTools *[]string
		expected  []string
	}{
		{
			name:      "nil stepTools returns defaults",
			stepTools: nil,
			expected:  defaults,
		},
		{
			name:      "empty stepTools returns empty",
			stepTools: &[]string{},
			expected:  []string{},
		},
		{
			name:      "stepTools are additive",
			stepTools: &[]string{"github.post-review", "github.add-labels"},
			expected:  []string{"github.read-diff", "github.read-pr", "files.read", "github.post-review", "github.add-labels"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tool.ResolveNames(defaults, tt.stepTools)
			if len(got) != len(tt.expected) {
				t.Fatalf("len = %d, want %d: got %v", len(got), len(tt.expected), got)
			}

			for i, v := range got {
				if v != tt.expected[i] {
					t.Errorf("got[%d] = %q, want %q", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestResolve_GlobMatching(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register("github.read-pr", newMockTool("github.read-pr"))
	reg.Register("github.read-diff", newMockTool("github.read-diff"))
	reg.Register("github.post-review", newMockTool("github.post-review"))
	reg.Register("files.read", newMockTool("files.read"))
	reg.Register("files.grep", newMockTool("files.grep"))

	tests := []struct {
		name     string
		patterns []string
		expected []string
	}{
		{
			name:     "wildcard matches all",
			patterns: []string{"*"},
			expected: []string{"files.grep", "files.read", "github.post-review", "github.read-diff", "github.read-pr"},
		},
		{
			name:     "namespace glob",
			patterns: []string{"github.*"},
			expected: []string{"github.post-review", "github.read-diff", "github.read-pr"},
		},
		{
			name:     "prefix glob",
			patterns: []string{"github.read-*"},
			expected: []string{"github.read-diff", "github.read-pr"},
		},
		{
			name:     "exact name",
			patterns: []string{"files.read"},
			expected: []string{"files.read"},
		},
		{
			name:     "multiple patterns",
			patterns: []string{"github.read-pr", "files.*"},
			expected: []string{"files.grep", "files.read", "github.read-pr"},
		},
		{
			name:     "no match",
			patterns: []string{"web.*"},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reg.Resolve(tt.patterns)
			if len(got) != len(tt.expected) {
				names := make([]string, len(got))
				for i, tool := range got {
					names[i] = tool.Name()
				}

				t.Fatalf("len = %d, want %d: got %v", len(got), len(tt.expected), names)
			}

			for i, tool := range got {
				if tool.Name() != tt.expected[i] {
					t.Errorf("got[%d] = %q, want %q", i, tool.Name(), tt.expected[i])
				}
			}
		})
	}
}

func TestToolset(t *testing.T) {
	tools := []tool.Tool{newMockTool("a"), newMockTool("b")}
	ts := tool.NewToolset(tools)

	if ts.Name() != "duto" {
		t.Errorf("name = %q, want %q", ts.Name(), "duto")
	}

	if len(ts.Tools()) != 2 {
		t.Errorf("tools len = %d, want 2", len(ts.Tools()))
	}
}
