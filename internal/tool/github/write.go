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

	payload := map[string]any{
		"labels": input.Labels,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling labels: %w", err)
	}

	err = c.post(ctx, path, body)
	if err != nil {
		return fmt.Errorf("adding labels: %w", err)
	}

	return nil
}
