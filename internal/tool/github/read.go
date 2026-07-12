package github

import (
	"context"
	"encoding/json"
	"fmt"
)

// ReadPRInput is the input for the read-pr tool.
type ReadPRInput struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
}

// ReadPROutput is the output from the read-pr tool.
type ReadPROutput struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Author string   `json:"author"`
	State  string   `json:"state"`
	Labels []string `json:"labels"`
	Base   string   `json:"base"`
	Head   string   `json:"head"`
}

// ReadPR returns PR metadata.
func (c *Client) ReadPR(ctx context.Context, input ReadPRInput) (*ReadPROutput, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", input.Owner, input.Repo, input.Number)

	data, err := c.get(ctx, path, "")
	if err != nil {
		return nil, fmt.Errorf("reading PR: %w", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing PR response: %w", err)
	}

	output := &ReadPROutput{
		Title: getString(raw, "title"),
		Body:  getString(raw, "body"),
		State: getString(raw, "state"),
	}

	if user, ok := raw["user"].(map[string]any); ok {
		output.Author = getString(user, "login")
	}

	if base, ok := raw["base"].(map[string]any); ok {
		output.Base = getString(base, "ref")
	}

	if head, ok := raw["head"].(map[string]any); ok {
		output.Head = getString(head, "ref")
	}

	if labels, ok := raw["labels"].([]any); ok {
		for _, l := range labels {
			if label, ok := l.(map[string]any); ok {
				output.Labels = append(output.Labels, getString(label, "name"))
			}
		}
	}

	return output, nil
}

// ReadDiff returns the PR diff as text.
func (c *Client) ReadDiff(ctx context.Context, input ReadPRInput) (string, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", input.Owner, input.Repo, input.Number)

	data, err := c.get(ctx, path, "application/vnd.github.v3.diff")
	if err != nil {
		return "", fmt.Errorf("reading diff: %w", err)
	}

	return string(data), nil
}

// ChangedFile represents a file changed in a PR.
type ChangedFile struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"`
}

// ListChangedFiles returns the files changed in a PR.
func (c *Client) ListChangedFiles(ctx context.Context, input ReadPRInput) ([]ChangedFile, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/files", input.Owner, input.Repo, input.Number)

	data, err := c.get(ctx, path, "")
	if err != nil {
		return nil, fmt.Errorf("listing changed files: %w", err)
	}

	var files []ChangedFile
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("parsing files response: %w", err)
	}

	return files, nil
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}

	return ""
}
