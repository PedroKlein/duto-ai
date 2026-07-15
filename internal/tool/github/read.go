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

// ReadCommentsInput is the input for reading comments.
type ReadCommentsInput struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
}

// Comment represents a PR/issue comment.
type Comment struct {
	Author string `json:"author"`
	Body   string `json:"body"`
}

// CommentsResult wraps the list of comments.
type CommentsResult struct {
	Comments []Comment `json:"comments"`
}

// ReadComments returns comments on an issue or PR.
func (c *Client) ReadComments(ctx context.Context, input ReadCommentsInput) (*CommentsResult, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", input.Owner, input.Repo, input.Number)

	data, err := c.get(ctx, path, "")
	if err != nil {
		return nil, fmt.Errorf("reading comments: %w", err)
	}

	var raw []map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing comments: %w", err)
	}

	result := &CommentsResult{Comments: make([]Comment, 0, len(raw))}

	for _, c := range raw {
		author := ""
		if user, ok := c["user"].(map[string]any); ok {
			author = getString(user, "login")
		}

		result.Comments = append(result.Comments, Comment{
			Author: author,
			Body:   getString(c, "body"),
		})
	}

	return result, nil
}

// ReadReviewsInput is the input for reading reviews.
type ReadReviewsInput struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
}

// Review represents a PR review.
type Review struct {
	Author string `json:"author"`
	State  string `json:"state"`
	Body   string `json:"body"`
}

// ReviewsResult wraps the list of reviews.
type ReviewsResult struct {
	Reviews []Review `json:"reviews"`
}

// ReadReviews returns reviews on a PR.
func (c *Client) ReadReviews(ctx context.Context, input ReadReviewsInput) (*ReviewsResult, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", input.Owner, input.Repo, input.Number)

	data, err := c.get(ctx, path, "")
	if err != nil {
		return nil, fmt.Errorf("reading reviews: %w", err)
	}

	var raw []map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing reviews: %w", err)
	}

	result := &ReviewsResult{Reviews: make([]Review, 0, len(raw))}

	for _, r := range raw {
		author := ""
		if user, ok := r["user"].(map[string]any); ok {
			author = getString(user, "login")
		}

		result.Reviews = append(result.Reviews, Review{
			Author: author,
			State:  getString(r, "state"),
			Body:   getString(r, "body"),
		})
	}

	return result, nil
}

// ReadChecksInput is the input for reading CI checks.
type ReadChecksInput struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Ref   string `json:"ref"` // branch name, tag, or SHA
}

// CheckRun represents a CI check.
type CheckRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// ChecksResult wraps the check runs.
type ChecksResult struct {
	Checks []CheckRun `json:"checks"`
}

// ReadChecks returns CI check runs for a ref.
func (c *Client) ReadChecks(ctx context.Context, input ReadChecksInput) (*ChecksResult, error) {
	path := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs", input.Owner, input.Repo, input.Ref)

	data, err := c.get(ctx, path, "")
	if err != nil {
		return nil, fmt.Errorf("reading checks: %w", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing checks: %w", err)
	}

	result := &ChecksResult{}

	runs, _ := resp["check_runs"].([]any)
	for _, r := range runs {
		run, ok := r.(map[string]any)
		if !ok {
			continue
		}

		result.Checks = append(result.Checks, CheckRun{
			Name:       getString(run, "name"),
			Status:     getString(run, "status"),
			Conclusion: getString(run, "conclusion"),
		})
	}

	return result, nil
}

// SearchIssuesInput is the input for searching issues.
type SearchIssuesInput struct {
	Query string `json:"query"` // GitHub search query
}

// SearchIssue represents a search result.
type SearchIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Author string `json:"author"`
}

// SearchIssuesResult wraps issue search results.
type SearchIssuesResult struct {
	Issues []SearchIssue `json:"issues"`
}

// SearchIssues searches GitHub issues and PRs.
func (c *Client) SearchIssues(ctx context.Context, input SearchIssuesInput) (*SearchIssuesResult, error) {
	path := "/search/issues?q=" + input.Query

	data, err := c.get(ctx, path, "")
	if err != nil {
		return nil, fmt.Errorf("searching issues: %w", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing search results: %w", err)
	}

	result := &SearchIssuesResult{}

	items, _ := resp["items"].([]any)
	for _, item := range items {
		i, ok := item.(map[string]any)
		if !ok {
			continue
		}

		author := ""
		if user, ok := i["user"].(map[string]any); ok {
			author = getString(user, "login")
		}

		number := 0
		if n, ok := i["number"].(float64); ok {
			number = int(n)
		}

		result.Issues = append(result.Issues, SearchIssue{
			Number: number,
			Title:  getString(i, "title"),
			State:  getString(i, "state"),
			Author: author,
		})
	}

	return result, nil
}
