package github

import (
	"testing"
)

func TestPublisher_GitRemoteURLUsesRepositoryHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{name: "github dot com", baseURL: "https://api.github.com", want: "https://github.com/owner/repo.git"},
		{name: "enterprise", baseURL: "https://github.example.invalid/api/v3", want: "https://github.example.invalid/owner/repo.git"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			publisher, err := NewPublisher(PublisherPolicy{BaseURL: tc.baseURL, Token: "token", Owner: "owner", Repository: "repo"})
			if err != nil {
				t.Fatalf("NewPublisher() error = %v", err)
			}

			if got := publisher.gitRemoteURL(); got != tc.want {
				t.Fatalf("gitRemoteURL() = %q, want %q", got, tc.want)
			}
		})
	}
}
