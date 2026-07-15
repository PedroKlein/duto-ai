package github

import (
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

// ReadIssueArgs is the input schema for the github.read-issue tool.
type ReadIssueArgs struct {
	Owner  string `json:"owner"`  // repository owner
	Repo   string `json:"repo"`   // repository name
	Number int    `json:"number"` // issue number
}

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

// ReadCommentsArgs is the input schema for the github.read-comments tool.
type ReadCommentsArgs struct {
	Owner  string `json:"owner"`  // repository owner
	Repo   string `json:"repo"`   // repository name
	Number int    `json:"number"` // issue or PR number
}

// ReadReviewsArgs is the input schema for the github.read-reviews tool.
type ReadReviewsArgs struct {
	Owner  string `json:"owner"`  // repository owner
	Repo   string `json:"repo"`   // repository name
	Number int    `json:"number"` // pull request number
}

// ReadChecksArgs is the input schema for the github.read-checks tool.
type ReadChecksArgs struct {
	Owner string `json:"owner"` // repository owner
	Repo  string `json:"repo"`  // repository name
	Ref   string `json:"ref"`   // branch, tag, or commit SHA
}

// SearchIssuesArgs is the input schema for the github.search-issues tool.
type SearchIssuesArgs struct {
	Query string `json:"query"` // GitHub search query string
}

// CreateIssueArgs is the input schema for the github.create-issue tool.
type CreateIssueArgs struct {
	Owner  string   `json:"owner"`            // repository owner
	Repo   string   `json:"repo"`             // repository name
	Title  string   `json:"title"`            // issue title
	Body   string   `json:"body"`             // issue body
	Labels []string `json:"labels,omitempty"` // labels to add
}

// EditIssueArgs is the input schema for the github.edit-issue tool.
type EditIssueArgs struct {
	Owner  string `json:"owner"`           // repository owner
	Repo   string `json:"repo"`            // repository name
	Number int    `json:"number"`          // issue or PR number
	Title  string `json:"title,omitempty"` // new title
	Body   string `json:"body,omitempty"`  // new body
	State  string `json:"state,omitempty"` // open or closed
}

// MergePRArgs is the input schema for the github.merge-pr tool.
type MergePRArgs struct {
	Owner       string `json:"owner"`                  // repository owner
	Repo        string `json:"repo"`                   // repository name
	Number      int    `json:"number"`                 // pull request number
	MergeMethod string `json:"merge_method,omitempty"` // merge, squash, or rebase
}

// RequestReviewersArgs is the input schema for the github.request-reviewers tool.
type RequestReviewersArgs struct {
	Owner     string   `json:"owner"`     // repository owner
	Repo      string   `json:"repo"`      // repository name
	Number    int      `json:"number"`    // pull request number
	Reviewers []string `json:"reviewers"` // usernames to request review from
}

// RegisterAll creates all GitHub tools and registers them in the tool registry.
func RegisterAll(reg *dtool.Registry, client *Client) error {
	tools := []struct {
		name   string
		create func() (tool.Tool, error)
	}{
		{"github.read-issue", func() (tool.Tool, error) { return newReadIssueTool(client) }},
		{"github.read-pr", func() (tool.Tool, error) { return newReadPRTool(client) }},
		{"github.read-diff", func() (tool.Tool, error) { return newReadDiffTool(client) }},
		{"github.list-changed-files", func() (tool.Tool, error) { return newListChangedFilesTool(client) }},
		{"github.read-comments", func() (tool.Tool, error) { return newReadCommentsTool(client) }},
		{"github.read-reviews", func() (tool.Tool, error) { return newReadReviewsTool(client) }},
		{"github.read-checks", func() (tool.Tool, error) { return newReadChecksTool(client) }},
		{"github.search-issues", func() (tool.Tool, error) { return newSearchIssuesTool(client) }},
		{"github.post-review", func() (tool.Tool, error) { return newPostReviewTool(client) }},
		{"github.post-comment", func() (tool.Tool, error) { return newPostCommentTool(client) }},
		{"github.add-labels", func() (tool.Tool, error) { return newAddLabelsTool(client) }},
		{"github.create-issue", func() (tool.Tool, error) { return newCreateIssueTool(client) }},
		{"github.edit-issue", func() (tool.Tool, error) { return newEditIssueTool(client) }},
		{"github.merge-pr", func() (tool.Tool, error) { return newMergePRTool(client) }},
		{"github.request-reviewers", func() (tool.Tool, error) { return newRequestReviewersTool(client) }},
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

func newReadIssueTool(client *Client) (tool.Tool, error) {
	return functiontool.New[ReadIssueArgs, *ReadIssueOutput](
		functiontool.Config{
			Name:        "github.read-issue",
			Description: "Read issue metadata including title, body, author, state, and labels by issue number",
		},
		func(ctx agent.Context, args ReadIssueArgs) (*ReadIssueOutput, error) {
			return client.ReadIssue(ctx, ReadIssueInput(args))
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

func newReadCommentsTool(client *Client) (tool.Tool, error) {
	return functiontool.New[ReadCommentsArgs, *CommentsResult](
		functiontool.Config{
			Name:        "github.read-comments",
			Description: "Read comments on an issue or pull request",
		},
		func(ctx agent.Context, args ReadCommentsArgs) (*CommentsResult, error) {
			return client.ReadComments(ctx, ReadCommentsInput(args))
		},
	)
}

func newReadReviewsTool(client *Client) (tool.Tool, error) {
	return functiontool.New[ReadReviewsArgs, *ReviewsResult](
		functiontool.Config{
			Name:        "github.read-reviews",
			Description: "Read reviews on a pull request with their state (APPROVED, CHANGES_REQUESTED, etc.)",
		},
		func(ctx agent.Context, args ReadReviewsArgs) (*ReviewsResult, error) {
			return client.ReadReviews(ctx, ReadReviewsInput(args))
		},
	)
}

func newReadChecksTool(client *Client) (tool.Tool, error) {
	return functiontool.New[ReadChecksArgs, *ChecksResult](
		functiontool.Config{
			Name:        "github.read-checks",
			Description: "Read CI check runs for a branch, tag, or commit SHA",
		},
		func(ctx agent.Context, args ReadChecksArgs) (*ChecksResult, error) {
			return client.ReadChecks(ctx, ReadChecksInput(args))
		},
	)
}

func newSearchIssuesTool(client *Client) (tool.Tool, error) {
	return functiontool.New[SearchIssuesArgs, *SearchIssuesResult](
		functiontool.Config{
			Name:        "github.search-issues",
			Description: "Search GitHub issues and pull requests using GitHub search query syntax",
		},
		func(ctx agent.Context, args SearchIssuesArgs) (*SearchIssuesResult, error) {
			return client.SearchIssues(ctx, SearchIssuesInput(args))
		},
	)
}

func newCreateIssueTool(client *Client) (tool.Tool, error) {
	return functiontool.New[CreateIssueArgs, *OKResult](
		functiontool.Config{
			Name:        "github.create-issue",
			Description: "Create a new issue in a repository",
		},
		func(ctx agent.Context, args CreateIssueArgs) (*OKResult, error) {
			err := client.CreateIssue(ctx, CreateIssueInput(args))
			if err != nil {
				return nil, err
			}

			return &OKResult{Success: true}, nil
		},
	)
}

func newEditIssueTool(client *Client) (tool.Tool, error) {
	return functiontool.New[EditIssueArgs, *OKResult](
		functiontool.Config{
			Name:        "github.edit-issue",
			Description: "Edit an existing issue or pull request (title, body, state)",
		},
		func(ctx agent.Context, args EditIssueArgs) (*OKResult, error) {
			err := client.EditIssue(ctx, EditIssueInput(args))
			if err != nil {
				return nil, err
			}

			return &OKResult{Success: true}, nil
		},
	)
}

func newMergePRTool(client *Client) (tool.Tool, error) {
	return functiontool.New[MergePRArgs, *OKResult](
		functiontool.Config{
			Name:        "github.merge-pr",
			Description: "Merge a pull request (supports merge, squash, or rebase method)",
		},
		func(ctx agent.Context, args MergePRArgs) (*OKResult, error) {
			err := client.MergePR(ctx, MergePRInput(args))
			if err != nil {
				return nil, err
			}

			return &OKResult{Success: true}, nil
		},
	)
}

func newRequestReviewersTool(client *Client) (tool.Tool, error) {
	return functiontool.New[RequestReviewersArgs, *OKResult](
		functiontool.Config{
			Name:        "github.request-reviewers",
			Description: "Request reviewers on a pull request",
		},
		func(ctx agent.Context, args RequestReviewersArgs) (*OKResult, error) {
			err := client.RequestReviewers(ctx, RequestReviewersInput(args))
			if err != nil {
				return nil, err
			}

			return &OKResult{Success: true}, nil
		},
	)
}
