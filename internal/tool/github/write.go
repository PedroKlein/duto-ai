package github

import (
	"context"
	"encoding/json"
	"fmt"
)

// ReviewComment is an inline comment on a PR review.
type ReviewComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Body string `json:"body"`
}

// PostReviewInput is the input for posting a PR review.
type PostReviewInput struct {
	Owner    string          `json:"owner"`
	Repo     string          `json:"repo"`
	Number   int             `json:"number"`
	Body     string          `json:"body"`
	Event    string          `json:"event"`
	Comments []ReviewComment `json:"comments,omitempty"`
}

// PostReview posts a review on a PR.
func (c *Client) PostReview(ctx context.Context, input PostReviewInput) error {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", input.Owner, input.Repo, input.Number)

	payload := map[string]any{
		"body":  input.Body,
		"event": input.Event,
	}

	if len(input.Comments) > 0 {
		payload["comments"] = input.Comments
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling review: %w", err)
	}

	err = c.post(ctx, path, body)
	if err != nil {
		return fmt.Errorf("posting review: %w", err)
	}

	return nil
}

// PostCommentInput is the input for posting a comment.
type PostCommentInput struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Body   string `json:"body"`
}

// PostComment posts a comment on an issue/PR.
func (c *Client) PostComment(ctx context.Context, input PostCommentInput) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", input.Owner, input.Repo, input.Number)

	payload := map[string]any{
		"body": input.Body,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling comment: %w", err)
	}

	err = c.post(ctx, path, body)
	if err != nil {
		return fmt.Errorf("posting comment: %w", err)
	}

	return nil
}

// AddLabelsInput is the input for adding labels.
type AddLabelsInput struct {
	Owner  string   `json:"owner"`
	Repo   string   `json:"repo"`
	Number int      `json:"number"`
	Labels []string `json:"labels"`
}

// AddLabels adds labels to an issue/PR.
func (c *Client) AddLabels(ctx context.Context, input AddLabelsInput) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/labels", input.Owner, input.Repo, input.Number)

	body, err := json.Marshal(map[string]any{"labels": input.Labels})
	if err != nil {
		return fmt.Errorf("marshaling labels: %w", err)
	}

	if err := c.post(ctx, path, body); err != nil {
		return fmt.Errorf("adding labels: %w", err)
	}

	return nil
}

// CreateIssueInput is the input for creating an issue.
type CreateIssueInput struct {
	Owner  string   `json:"owner"`
	Repo   string   `json:"repo"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels,omitempty"`
}

// CreateIssue creates a new issue.
func (c *Client) CreateIssue(ctx context.Context, input CreateIssueInput) error {
	path := fmt.Sprintf("/repos/%s/%s/issues", input.Owner, input.Repo)

	payload := map[string]any{
		"title": input.Title,
		"body":  input.Body,
	}

	if len(input.Labels) > 0 {
		payload["labels"] = input.Labels
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling issue: %w", err)
	}

	if err := c.post(ctx, path, body); err != nil {
		return fmt.Errorf("creating issue: %w", err)
	}

	return nil
}

// EditIssueInput is the input for editing an issue or PR.
type EditIssueInput struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Title  string `json:"title,omitempty"`
	Body   string `json:"body,omitempty"`
	State  string `json:"state,omitempty"` // open or closed
}

// EditIssue edits an issue or PR.
func (c *Client) EditIssue(ctx context.Context, input EditIssueInput) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", input.Owner, input.Repo, input.Number)

	payload := make(map[string]any)

	if input.Title != "" {
		payload["title"] = input.Title
	}

	if input.Body != "" {
		payload["body"] = input.Body
	}

	if input.State != "" {
		payload["state"] = input.State
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling edit: %w", err)
	}

	if err := c.patch(ctx, path, body); err != nil {
		return fmt.Errorf("editing issue: %w", err)
	}

	return nil
}

// MergePRInput is the input for merging a PR.
type MergePRInput struct {
	Owner       string `json:"owner"`
	Repo        string `json:"repo"`
	Number      int    `json:"number"`
	MergeMethod string `json:"merge_method,omitempty"` // merge, squash, or rebase
}

// MergePR merges a pull request.
func (c *Client) MergePR(ctx context.Context, input MergePRInput) error {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", input.Owner, input.Repo, input.Number)

	method := input.MergeMethod
	if method == "" {
		method = "merge"
	}

	body, err := json.Marshal(map[string]any{"merge_method": method})
	if err != nil {
		return fmt.Errorf("marshaling merge: %w", err)
	}

	if err := c.put(ctx, path, body); err != nil {
		return fmt.Errorf("merging PR: %w", err)
	}

	return nil
}

// RequestReviewersInput is the input for requesting reviewers.
type RequestReviewersInput struct {
	Owner     string   `json:"owner"`
	Repo      string   `json:"repo"`
	Number    int      `json:"number"`
	Reviewers []string `json:"reviewers"`
}

// RequestReviewers requests reviewers on a PR.
func (c *Client) RequestReviewers(ctx context.Context, input RequestReviewersInput) error {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/requested_reviewers", input.Owner, input.Repo, input.Number)

	body, err := json.Marshal(map[string]any{"reviewers": input.Reviewers})
	if err != nil {
		return fmt.Errorf("marshaling reviewers: %w", err)
	}

	if err := c.post(ctx, path, body); err != nil {
		return fmt.Errorf("requesting reviewers: %w", err)
	}

	return nil
}
