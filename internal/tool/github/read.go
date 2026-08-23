package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type ReadIssueOutput struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Author string   `json:"author"`
	State  string   `json:"state"`
	Labels []string `json:"labels"`
}

type ReadPROutput struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Author string   `json:"author"`
	State  string   `json:"state"`
	Labels []string `json:"labels"`
	Base   string   `json:"base"`
	Head   string   `json:"head"`
}

type DiffResult struct {
	Diff string `json:"diff"`
}

type ChangedFile struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"`
}

type ChangedFilesResult struct {
	Files     []ChangedFile `json:"files"`
	Truncated bool          `json:"truncated"`
}

type Comment struct {
	Author string `json:"author"`
	Body   string `json:"body"`
}

type CommentsResult struct {
	Comments  []Comment `json:"comments"`
	Truncated bool      `json:"truncated"`
}

type Review struct {
	Author string `json:"author"`
	State  string `json:"state"`
	Body   string `json:"body"`
}

type ReviewsResult struct {
	Reviews   []Review `json:"reviews"`
	Truncated bool     `json:"truncated"`
}

type CheckRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

type ChecksResult struct {
	Checks    []CheckRun `json:"checks"`
	Truncated bool       `json:"truncated"`
}

type SearchIssuesInput struct {
	Query string `json:"query"`
}

type SearchIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Author string `json:"author"`
}

type SearchIssuesResult struct {
	Issues    []SearchIssue `json:"issues"`
	Truncated bool          `json:"truncated"`
}

func (c *Client) ReadIssue(ctx context.Context) (*ReadIssueOutput, error) {
	path := c.repositoryPath("/issues/" + strconv.Itoa(c.subject))

	data, err := c.get(ctx, "github.read.issue", path, "", nil, 0)
	if err != nil {
		return nil, fmt.Errorf("reading issue: %w", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing issue response: %w", err)
	}

	return &ReadIssueOutput{
		Title:  getString(raw, "title"),
		Body:   getString(raw, "body"),
		Author: nestedString(raw, "user", "login"),
		State:  getString(raw, "state"),
		Labels: labels(raw),
	}, nil
}

func (c *Client) ReadPR(ctx context.Context) (*ReadPROutput, error) {
	path := c.repositoryPath("/pulls/" + strconv.Itoa(c.subject))

	data, err := c.get(ctx, "github.read.pr", path, "", nil, 0)
	if err != nil {
		return nil, fmt.Errorf("reading pull request: %w", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing pull request response: %w", err)
	}

	return &ReadPROutput{
		Title:  getString(raw, "title"),
		Body:   getString(raw, "body"),
		Author: nestedString(raw, "user", "login"),
		State:  getString(raw, "state"),
		Labels: labels(raw),
		Base:   nestedString(raw, "base", "ref"),
		Head:   nestedString(raw, "head", "ref"),
	}, nil
}

func (c *Client) ReadDiff(ctx context.Context) (*DiffResult, error) {
	path := c.repositoryPath("/pulls/" + strconv.Itoa(c.subject))

	data, err := c.get(ctx, "github.read.diff", path, "application/vnd.github.v3.diff", nil, 0)
	if err != nil {
		return nil, fmt.Errorf("reading pull request diff: %w", err)
	}

	return &DiffResult{Diff: string(data)}, nil
}

func (c *Client) ListChangedFiles(ctx context.Context) (*ChangedFilesResult, error) {
	path := c.repositoryPath("/pulls/" + strconv.Itoa(c.subject) + "/files")

	items, truncated, err := c.pagedArray(ctx, "github.read.changed-files", path, nil)
	if err != nil {
		return nil, fmt.Errorf("listing changed files: %w", err)
	}

	files := make([]ChangedFile, len(items))
	for i, item := range items {
		if err := json.Unmarshal(item, &files[i]); err != nil {
			return nil, fmt.Errorf("parsing changed file: %w", err)
		}
	}

	return &ChangedFilesResult{Files: files, Truncated: truncated}, nil
}

func (c *Client) ReadComments(ctx context.Context) (*CommentsResult, error) {
	path := c.repositoryPath("/issues/" + strconv.Itoa(c.subject) + "/comments")

	items, truncated, err := c.pagedArray(ctx, "github.read.comments", path, nil)
	if err != nil {
		return nil, fmt.Errorf("reading comments: %w", err)
	}

	comments := make([]Comment, 0, len(items))
	for _, item := range items {
		var raw map[string]any
		if err := json.Unmarshal(item, &raw); err != nil {
			return nil, fmt.Errorf("parsing comment: %w", err)
		}

		comments = append(comments, Comment{Author: nestedString(raw, "user", "login"), Body: getString(raw, "body")})
	}

	return &CommentsResult{Comments: comments, Truncated: truncated}, nil
}

func (c *Client) ReadReviews(ctx context.Context) (*ReviewsResult, error) {
	path := c.repositoryPath("/pulls/" + strconv.Itoa(c.subject) + "/reviews")

	items, truncated, err := c.pagedArray(ctx, "github.read.reviews", path, nil)
	if err != nil {
		return nil, fmt.Errorf("reading reviews: %w", err)
	}

	reviews := make([]Review, 0, len(items))
	for _, item := range items {
		var raw map[string]any
		if err := json.Unmarshal(item, &raw); err != nil {
			return nil, fmt.Errorf("parsing review: %w", err)
		}

		reviews = append(reviews, Review{Author: nestedString(raw, "user", "login"), State: getString(raw, "state"), Body: getString(raw, "body")})
	}

	return &ReviewsResult{Reviews: reviews, Truncated: truncated}, nil
}

func (c *Client) ReadChecks(ctx context.Context) (*ChecksResult, error) {
	path := c.repositoryPath("/commits/" + url.PathEscape(c.ref) + "/check-runs")
	query := url.Values{}
	collected := make([]CheckRun, 0, c.maxResults)
	totalBytes := 0

	for page := 1; page <= c.maxPages && len(collected) < c.maxResults; page++ {
		perPage := min(100, c.maxResults-len(collected))

		query.Set("page", strconv.Itoa(page))
		query.Set("per_page", strconv.Itoa(perPage))

		remaining := c.limits["github.read.checks"].MaxResultBytes - totalBytes
		if remaining <= 0 {
			return &ChecksResult{Checks: collected, Truncated: true}, nil
		}

		data, err := c.get(ctx, "github.read.checks", path, "", query, remaining)
		if err != nil {
			return nil, fmt.Errorf("reading checks page: %w", err)
		}

		totalBytes += len(data)

		var response struct {
			Checks []CheckRun `json:"check_runs"`
		}
		if err := json.Unmarshal(data, &response); err != nil {
			return nil, fmt.Errorf("parsing checks: %w", err)
		}

		if len(response.Checks) > perPage {
			collected = append(collected, response.Checks[:perPage]...)
			return &ChecksResult{Checks: collected, Truncated: true}, nil
		}

		collected = append(collected, response.Checks...)
		if len(response.Checks) < perPage {
			return &ChecksResult{Checks: collected}, nil
		}
	}

	return &ChecksResult{Checks: collected, Truncated: true}, nil
}

func (c *Client) SearchIssues(ctx context.Context, input SearchIssuesInput) (*SearchIssuesResult, error) {
	queryText := strings.TrimSpace(input.Query)
	if queryText == "" || strings.Contains(strings.ToLower(queryText), "repo:") {
		return nil, ErrPolicyViolation
	}

	path := "/search/issues"
	query := url.Values{}
	collected := make([]SearchIssue, 0, c.maxResults)
	totalBytes := 0

	for page := 1; page <= c.maxPages && len(collected) < c.maxResults; page++ {
		perPage := min(100, c.maxResults-len(collected))
		query.Set("q", queryText+" repo:"+c.owner+"/"+c.repository)
		query.Set("page", strconv.Itoa(page))
		query.Set("per_page", strconv.Itoa(perPage))

		remaining := c.limits["github.read.search-issues"].MaxResultBytes - totalBytes
		if remaining <= 0 {
			return &SearchIssuesResult{Issues: collected, Truncated: true}, nil
		}

		data, err := c.get(ctx, "github.read.search-issues", path, "", query, remaining)
		if err != nil {
			return nil, fmt.Errorf("searching issues page: %w", err)
		}

		totalBytes += len(data)

		var response struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(data, &response); err != nil {
			return nil, fmt.Errorf("parsing issue search: %w", err)
		}

		pageItems := response.Items
		pageTruncated := false

		if len(pageItems) > perPage {
			pageItems = pageItems[:perPage]
			pageTruncated = true
		}

		for _, item := range pageItems {
			var raw struct {
				Number int    `json:"number"`
				Title  string `json:"title"`
				State  string `json:"state"`
				User   struct {
					Login string `json:"login"`
				} `json:"user"`
			}
			if err := json.Unmarshal(item, &raw); err != nil {
				return nil, fmt.Errorf("parsing issue search item: %w", err)
			}

			if raw.Number < 0 {
				return nil, fmt.Errorf("parsing issue number: %w", ErrPolicyViolation)
			}

			collected = append(collected, SearchIssue{Number: raw.Number, Title: raw.Title, State: raw.State, Author: raw.User.Login})
		}

		if pageTruncated {
			return &SearchIssuesResult{Issues: collected, Truncated: true}, nil
		}

		if len(response.Items) < perPage {
			return &SearchIssuesResult{Issues: collected}, nil
		}
	}

	return &SearchIssuesResult{Issues: collected, Truncated: true}, nil
}

func (c *Client) pagedArray(ctx context.Context, toolName, path string, query url.Values) ([]json.RawMessage, bool, error) {
	if query == nil {
		query = url.Values{}
	}

	items := make([]json.RawMessage, 0, c.maxResults)
	totalBytes := 0

	for page := 1; page <= c.maxPages && len(items) < c.maxResults; page++ {
		perPage := min(100, c.maxResults-len(items))

		query.Set("page", strconv.Itoa(page))
		query.Set("per_page", strconv.Itoa(perPage))

		remaining := c.limits[toolName].MaxResultBytes - totalBytes
		if remaining <= 0 {
			return items, true, nil
		}

		data, err := c.get(ctx, toolName, path, "", query, remaining)
		if err != nil {
			return nil, false, err
		}

		totalBytes += len(data)

		var pageItems []json.RawMessage
		if err := json.Unmarshal(data, &pageItems); err != nil {
			return nil, false, fmt.Errorf("parsing GitHub list response: %w", err)
		}

		if len(pageItems) > perPage {
			items = append(items, pageItems[:perPage]...)
			return items, true, nil
		}

		items = append(items, pageItems...)
		if len(pageItems) < perPage {
			return items, false, nil
		}
	}

	return items, true, nil
}

func getString(value map[string]any, key string) string {
	result, _ := value[key].(string)
	return result
}

func nestedString(value map[string]any, key, nested string) string {
	object, _ := value[key].(map[string]any)
	return getString(object, nested)
}

func labels(value map[string]any) []string {
	raw, _ := value["labels"].([]any)

	result := make([]string, 0, len(raw))
	for _, item := range raw {
		label, _ := item.(map[string]any)
		if name := getString(label, "name"); name != "" {
			result = append(result, name)
		}
	}

	return result
}
