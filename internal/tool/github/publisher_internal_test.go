package github

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestPublisher_GitAuthorizationHeaderUsesBasicTokenCredential(t *testing.T) {
	t.Parallel()

	header := gitAuthorizationHeader("token")

	encoded, ok := strings.CutPrefix(header, "Authorization: Basic ")
	if !ok {
		t.Fatalf("gitAuthorizationHeader() = %q, want Basic authorization", header)
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}

	if got := string(decoded); got != "x-access-token:token" {
		t.Fatalf("decoded authorization = %q", got)
	}
}

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
