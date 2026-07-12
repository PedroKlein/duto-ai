package runtime

import "google.golang.org/adk/v2/model"

// Options configures the runtime for testing.
type Options struct {
	// LLM overrides the provider-created LLM (for testing with mock).
	LLM model.LLM
	// GitHubBaseURL overrides the GitHub API URL (for httptest).
	GitHubBaseURL string
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
