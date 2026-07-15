package github

import (
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

// ReadPRArgs is the input schema for the github.read-pr tool.
type ReadPRArgs struct {
	Owner  string `json:"owner"`  // repository owner
	Repo   string `json:"repo"`   // repository name
	Number int    `json:"number"` // pull request number
}

// ReadDiffArgs is the input schema for the github.read-diff tool.
type ReadDiffArgs struct {
	Owner  string `json:"owner"`  // repository owner
	Repo   string `json:"repo"`   // repository name
	Number int    `json:"number"` // pull request number
}

// ListChangedFilesArgs is the input schema for the github.list-changed-files tool.
type ListChangedFilesArgs struct {
	Owner  string `json:"owner"`  // repository owner
	Repo   string `json:"repo"`   // repository name
	Number int    `json:"number"` // pull request number
}

// PostReviewArgs is the input schema for the github.post-review tool.
type PostReviewArgs struct {
	Owner    string          `json:"owner"`              // repository owner
	Repo     string          `json:"repo"`               // repository name
	Number   int             `json:"number"`             // pull request number
	Body     string          `json:"body"`               // review body text
	Event    string          `json:"event"`              // review event: COMMENT, APPROVE, or REQUEST_CHANGES
	Comments []ReviewComment `json:"comments,omitempty"` // inline review comments
}

// PostCommentArgs is the input schema for the github.post-comment tool.
type PostCommentArgs struct {
	Owner  string `json:"owner"`  // repository owner
	Repo   string `json:"repo"`   // repository name
	Number int    `json:"number"` // issue or PR number
	Body   string `json:"body"`   // comment body text
}

// AddLabelsArgs is the input schema for the github.add-labels tool.
type AddLabelsArgs struct {
	Owner  string   `json:"owner"`  // repository owner
	Repo   string   `json:"repo"`   // repository name
	Number int      `json:"number"` // issue or PR number
	Labels []string `json:"labels"` // labels to add
}

// DiffResult wraps the diff text output.
type DiffResult struct {
	Diff string `json:"diff"`
}

// ChangedFilesResult wraps the list of changed files (ADK requires struct output).
type ChangedFilesResult struct {
	Files []ChangedFile `json:"files"`
}

// OKResult is returned by write tools on success.
type OKResult struct {
	Success bool `json:"success"`
}

// RegisterAll creates all GitHub tools and registers them in the tool registry.
func RegisterAll(reg *dtool.Registry, client *Client) error {
	tools := []struct {
		name   string
		create func() (tool.Tool, error)
	}{
		{"github.read-pr", func() (tool.Tool, error) { return newReadPRTool(client) }},
		{"github.read-diff", func() (tool.Tool, error) { return newReadDiffTool(client) }},
		{"github.list-changed-files", func() (tool.Tool, error) { return newListChangedFilesTool(client) }},
		{"github.post-review", func() (tool.Tool, error) { return newPostReviewTool(client) }},
		{"github.post-comment", func() (tool.Tool, error) { return newPostCommentTool(client) }},
		{"github.add-labels", func() (tool.Tool, error) { return newAddLabelsTool(client) }},
	}

	for _, t := range tools {
		adkTool, err := t.create()
		if err != nil {
			return fmt.Errorf("creating tool %s: %w", t.name, err)
		}

		reg.Register(t.name, adkTool)
	}

	return nil
}

func newReadPRTool(client *Client) (tool.Tool, error) {
	return functiontool.New[ReadPRArgs, *ReadPROutput](
		functiontool.Config{
			Name:        "github.read-pr",
			Description: "Read pull request metadata including title, body, author, state, labels, and branches",
		},
		func(ctx agent.Context, args ReadPRArgs) (*ReadPROutput, error) {
			return client.ReadPR(ctx, ReadPRInput(args))
		},
	)
}

func newReadDiffTool(client *Client) (tool.Tool, error) {
	return functiontool.New[ReadDiffArgs, *DiffResult](
		functiontool.Config{
			Name:        "github.read-diff",
			Description: "Read the unified diff of a pull request",
		},
		func(ctx agent.Context, args ReadDiffArgs) (*DiffResult, error) {
			diff, err := client.ReadDiff(ctx, ReadPRInput(args))
			if err != nil {
				return nil, err
			}

			return &DiffResult{Diff: diff}, nil
		},
	)
}

func newListChangedFilesTool(client *Client) (tool.Tool, error) {
	return functiontool.New[ListChangedFilesArgs, *ChangedFilesResult](
		functiontool.Config{
			Name:        "github.list-changed-files",
			Description: "List files changed in a pull request with additions, deletions, and patch info",
		},
		func(ctx agent.Context, args ListChangedFilesArgs) (*ChangedFilesResult, error) {
			files, err := client.ListChangedFiles(ctx, ReadPRInput(args))
			if err != nil {
				return nil, err
			}

			return &ChangedFilesResult{Files: files}, nil
		},
	)
}

func newPostReviewTool(client *Client) (tool.Tool, error) {
	return functiontool.New[PostReviewArgs, *OKResult](
		functiontool.Config{
			Name:        "github.post-review",
			Description: "Post a review on a pull request with optional inline comments",
		},
		func(ctx agent.Context, args PostReviewArgs) (*OKResult, error) {
			err := client.PostReview(ctx, PostReviewInput(args))
			if err != nil {
				return nil, err
			}

			return &OKResult{Success: true}, nil
		},
	)
}

func newPostCommentTool(client *Client) (tool.Tool, error) {
	return functiontool.New[PostCommentArgs, *OKResult](
		functiontool.Config{
			Name:        "github.post-comment",
			Description: "Post a comment on an issue or pull request",
		},
		func(ctx agent.Context, args PostCommentArgs) (*OKResult, error) {
			err := client.PostComment(ctx, PostCommentInput(args))
			if err != nil {
				return nil, err
			}

			return &OKResult{Success: true}, nil
		},
	)
}

func newAddLabelsTool(client *Client) (tool.Tool, error) {
	return functiontool.New[AddLabelsArgs, *OKResult](
		functiontool.Config{
			Name:        "github.add-labels",
			Description: "Add labels to an issue or pull request",
		},
		func(ctx agent.Context, args AddLabelsArgs) (*OKResult, error) {
			err := client.AddLabels(ctx, AddLabelsInput(args))
			if err != nil {
				return nil, err
			}

			return &OKResult{Success: true}, nil
		},
	)
}
