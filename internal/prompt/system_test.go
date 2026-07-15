package prompt_test

import (
	"strings"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/prompt"
)

func TestBuildSystemPrompt(t *testing.T) {
	tests := []struct {
		name     string
		step     config.Step
		cfg      *config.Config
		eventCtx *prompt.EventContext
		contains []string
		excludes []string
	}{
		{
			name: "basic step includes ID and output key",
			step: config.Step{ID: "analyze", Output: "findings"},
			cfg: &config.Config{
				Defaults: config.Defaults{Tools: []string{"github.read-pr", "files.read"}},
			},
			contains: []string{"analyze", "findings", "github.read-pr"},
		},
		{
			name: "with event context",
			step: config.Step{ID: "test"},
			cfg:  &config.Config{},
			eventCtx: &prompt.EventContext{
				Repo: "owner/repo", PRNumber: 42, Author: "alice", EventName: "pull_request",
			},
			contains: []string{"owner/repo", "PR Number: 42", "alice", "Owner: owner", "Repo: repo"},
		},
		{
			name:     "with user system field",
			step:     config.Step{ID: "test", System: "You are an expert code reviewer."},
			cfg:      &config.Config{},
			contains: []string{"expert code reviewer"},
		},
		{
			name: "step tools additive on defaults",
			step: config.Step{
				ID:    "report",
				Tools: &[]string{"github.post-review"},
			},
			cfg: &config.Config{
				Defaults: config.Defaults{Tools: []string{"files.read"}},
			},
			contains: []string{"files.read", "github.post-review"},
		},
		{
			name: "empty tools override removes all",
			step: config.Step{
				ID:    "quiet",
				Tools: &[]string{},
			},
			cfg: &config.Config{
				Defaults: config.Defaults{Tools: []string{"files.read"}},
			},
			excludes: []string{"Available tools"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prompt.BuildSystemPrompt(tt.step, tt.cfg, tt.eventCtx)

			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf("expected %q in result, got:\n%s", want, result)
				}
			}

			for _, notWant := range tt.excludes {
				if strings.Contains(result, notWant) {
					t.Errorf("expected %q NOT in result, got:\n%s", notWant, result)
				}
			}
		})
	}
}
