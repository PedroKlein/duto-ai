package runtime

import "google.golang.org/adk/v2/model"

// Options configures the runtime for testing.
type Options struct {
	// LLM overrides the provider-created LLM (for testing with mock).
	LLM model.LLM
	// GitHubBaseURL overrides the GitHub API URL (for httptest).
	GitHubBaseURL string
	// RepoRoot overrides the repository root directory for sandboxed tools.
	RepoRoot string
}

// Option is a functional option for Run.
type Option func(*Options)

// WithLLM injects a custom LLM, bypassing provider creation.
func WithLLM(llm model.LLM) Option {
	return func(o *Options) {
		o.LLM = llm
	}
}

// WithGitHubBaseURL overrides the GitHub API base URL.
func WithGitHubBaseURL(url string) Option {
	return func(o *Options) {
		o.GitHubBaseURL = url
	}
}

// WithRepoRoot overrides the repository root directory.
func WithRepoRoot(root string) Option {
	return func(o *Options) {
		o.RepoRoot = root
	}
}

func applyOptions(opts []Option) *Options {
	var options Options
	for _, opt := range opts {
		opt(&options)
	}

	return &options
}
