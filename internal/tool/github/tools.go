package github

import (
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

type BoundSubjectArgs struct{}

func RegisterAll(registry *dtool.Registry, client *Client) error {
	if registry == nil || client == nil {
		return ErrInvalidPolicy
	}

	tools := []struct {
		name   string
		create func() (tool.Tool, error)
	}{
		{"github.read.issue", func() (tool.Tool, error) { return newReadIssueTool(client) }},
		{"github.read.pr", func() (tool.Tool, error) { return newReadPRTool(client) }},
		{"github.read.diff", func() (tool.Tool, error) { return newReadDiffTool(client) }},
		{"github.read.changed-files", func() (tool.Tool, error) { return newChangedFilesTool(client) }},
		{"github.read.comments", func() (tool.Tool, error) { return newCommentsTool(client) }},
		{"github.read.reviews", func() (tool.Tool, error) { return newReviewsTool(client) }},
		{"github.read.checks", func() (tool.Tool, error) { return newChecksTool(client) }},
		{"github.read.search-issues", func() (tool.Tool, error) { return newSearchIssuesTool(client) }},
	}

	for _, current := range tools {
		if _, selected := client.limits[current.name]; !selected {
			continue
		}

		created, err := current.create()
		if err != nil {
			return fmt.Errorf("creating tool %s: %w", current.name, err)
		}

		registry.Register(current.name, created)
	}

	return nil
}

func newReadIssueTool(client *Client) (tool.Tool, error) {
	return functiontool.New[BoundSubjectArgs, *ReadIssueOutput](
		functiontool.Config{Name: "github.read.issue", Description: "Read metadata for the runtime-bound issue."},
		func(ctx agent.Context, _ BoundSubjectArgs) (*ReadIssueOutput, error) { return client.ReadIssue(ctx) },
	)
}

func newReadPRTool(client *Client) (tool.Tool, error) {
	return functiontool.New[BoundSubjectArgs, *ReadPROutput](
		functiontool.Config{Name: "github.read.pr", Description: "Read metadata for the runtime-bound pull request."},
		func(ctx agent.Context, _ BoundSubjectArgs) (*ReadPROutput, error) { return client.ReadPR(ctx) },
	)
}

func newReadDiffTool(client *Client) (tool.Tool, error) {
	return functiontool.New[BoundSubjectArgs, *DiffResult](
		functiontool.Config{Name: "github.read.diff", Description: "Read the diff for the runtime-bound pull request."},
		func(ctx agent.Context, _ BoundSubjectArgs) (*DiffResult, error) { return client.ReadDiff(ctx) },
	)
}

func newChangedFilesTool(client *Client) (tool.Tool, error) {
	return functiontool.New[BoundSubjectArgs, *ChangedFilesResult](
		functiontool.Config{Name: "github.read.changed-files", Description: "Read changed files for the runtime-bound pull request."},
		func(ctx agent.Context, _ BoundSubjectArgs) (*ChangedFilesResult, error) {
			return client.ListChangedFiles(ctx)
		},
	)
}

func newCommentsTool(client *Client) (tool.Tool, error) {
	return functiontool.New[BoundSubjectArgs, *CommentsResult](
		functiontool.Config{Name: "github.read.comments", Description: "Read comments for the runtime-bound issue or pull request."},
		func(ctx agent.Context, _ BoundSubjectArgs) (*CommentsResult, error) { return client.ReadComments(ctx) },
	)
}

func newReviewsTool(client *Client) (tool.Tool, error) {
	return functiontool.New[BoundSubjectArgs, *ReviewsResult](
		functiontool.Config{Name: "github.read.reviews", Description: "Read reviews for the runtime-bound pull request."},
		func(ctx agent.Context, _ BoundSubjectArgs) (*ReviewsResult, error) { return client.ReadReviews(ctx) },
	)
}

func newChecksTool(client *Client) (tool.Tool, error) {
	return functiontool.New[BoundSubjectArgs, *ChecksResult](
		functiontool.Config{Name: "github.read.checks", Description: "Read checks for the runtime-bound revision."},
		func(ctx agent.Context, _ BoundSubjectArgs) (*ChecksResult, error) { return client.ReadChecks(ctx) },
	)
}

func newSearchIssuesTool(client *Client) (tool.Tool, error) {
	return functiontool.New[SearchIssuesInput, *SearchIssuesResult](
		functiontool.Config{Name: "github.read.search-issues", Description: "Search issues and pull requests within the runtime-bound repository."},
		func(ctx agent.Context, args SearchIssuesInput) (*SearchIssuesResult, error) {
			return client.SearchIssues(ctx, args)
		},
	)
}
