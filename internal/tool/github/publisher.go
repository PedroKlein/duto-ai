package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/PedroKlein/duto-ai/internal/publisher"
	"github.com/PedroKlein/duto-ai/internal/safeoutput"
)

const maxPublisherResponseBytes = 1 << 20

var errPublisherResponse = errors.New("GitHub publisher response")

type PublisherPolicy struct {
	BaseURL    string
	Token      string
	Owner      string
	Repository string
	HTTPClient *http.Client
}

type Publisher struct {
	baseURL    *url.URL
	token      string
	owner      string
	repository string
	httpClient *http.Client
}

func NewPublisher(policy PublisherPolicy) (*Publisher, error) {
	baseURL, err := parseBaseURL(policy.BaseURL)
	if err != nil || policy.Token == "" || !repositoryName.MatchString(policy.Owner) || !repositoryName.MatchString(policy.Repository) {
		return nil, ErrInvalidPolicy
	}

	return &Publisher{
		baseURL: baseURL, token: policy.Token, owner: policy.Owner, repository: policy.Repository,
		httpClient: redirectRejectingClient(policy.HTTPClient),
	}, nil
}

func (p *Publisher) Preflight(ctx context.Context, operation publisher.Operation) (publisher.RemoteState, error) {
	if p == nil || operation.RepositoryOwner != p.owner || operation.RepositoryName != p.repository {
		return publisher.RemoteState{}, publisher.ErrRejected
	}

	switch operation.Kind {
	case safeoutput.KindReply:
		return p.preflightReply(ctx, operation)
	case safeoutput.KindBranch:
		return p.preflightBranch(ctx, operation)
	case safeoutput.KindDraftPR:
		return p.preflightPR(ctx, operation)
	default:
		return publisher.RemoteState{}, publisher.ErrRejected
	}
}

func (p *Publisher) Apply(ctx context.Context, operation publisher.Operation) (string, error) {
	if p == nil || operation.RepositoryOwner != p.owner || operation.RepositoryName != p.repository {
		return "", publisher.ErrRejected
	}

	switch operation.Kind {
	case safeoutput.KindReply:
		return p.createReply(ctx, operation)
	case safeoutput.KindBranch:
		return operation.TargetRef, p.pushBranch(ctx, operation)
	case safeoutput.KindDraftPR:
		return p.createDraftPR(ctx, operation)
	default:
		return "", publisher.ErrRejected
	}
}

func (p *Publisher) preflightReply(ctx context.Context, operation publisher.Operation) (publisher.RemoteState, error) {
	if operation.OriginKind != "issue" && operation.OriginKind != "pull_request" || operation.OriginNumber <= 0 {
		return publisher.RemoteState{}, publisher.ErrRejected
	}

	var subject struct {
		State string `json:"state"`
	}
	if _, err := p.request(ctx, http.MethodGet, p.repositoryPath("/issues/"+strconv.Itoa(operation.OriginNumber)), nil, &subject); err != nil {
		return publisher.RemoteState{}, err
	}

	if subject.State != "open" {
		return publisher.RemoteState{}, publisher.ErrRejected
	}

	var comments []struct {
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
	}

	path := p.repositoryPath("/issues/" + strconv.Itoa(operation.OriginNumber) + "/comments?per_page=100")
	if _, err := p.request(ctx, http.MethodGet, path, nil, &comments); err != nil {
		return publisher.RemoteState{}, err
	}

	marker := publisher.Marker(operation)
	prefix := publisher.MarkerPrefix(operation)

	for _, comment := range comments {
		if strings.Contains(comment.Body, marker) {
			return publisher.RemoteState{Disposition: publisher.DispositionUnchanged, Resource: comment.HTMLURL}, nil
		}

		if strings.Contains(comment.Body, prefix) {
			return publisher.RemoteState{Disposition: publisher.DispositionConflict, Resource: comment.HTMLURL}, nil
		}
	}

	return publisher.RemoteState{}, nil
}

func (p *Publisher) createReply(ctx context.Context, operation publisher.Operation) (string, error) {
	body := operation.ReplyBody + "\n\n" + publisher.Marker(operation)
	request := struct {
		Body string `json:"body"`
	}{Body: body}

	var response struct {
		HTMLURL string `json:"html_url"`
	}

	path := p.repositoryPath("/issues/" + strconv.Itoa(operation.OriginNumber) + "/comments")
	if _, err := p.request(ctx, http.MethodPost, path, request, &response); err != nil {
		return "", err
	}

	if response.HTMLURL == "" {
		return "", errPublisherResponse
	}

	return response.HTMLURL, nil
}

func (p *Publisher) preflightBranch(ctx context.Context, operation publisher.Operation) (publisher.RemoteState, error) {
	baseSHA, exists, err := p.readRef(ctx, operation.BaseRef)
	if err != nil {
		return publisher.RemoteState{}, err
	}

	if !exists || baseSHA != operation.BaseSHA {
		return publisher.RemoteState{}, publisher.ErrConflict
	}

	targetSHA, exists, err := p.readRef(ctx, operation.TargetRef)
	if err != nil {
		return publisher.RemoteState{}, err
	}

	if !exists {
		return publisher.RemoteState{}, nil
	}

	if targetSHA == operation.SourceCommit {
		return publisher.RemoteState{Disposition: publisher.DispositionUnchanged, Resource: operation.TargetRef}, nil
	}

	return publisher.RemoteState{Disposition: publisher.DispositionConflict, Resource: operation.TargetRef}, nil
}

func (p *Publisher) readRef(ctx context.Context, ref string) (sha string, exists bool, err error) {
	path := p.repositoryPath("/git/ref/" + url.PathEscape(strings.TrimPrefix(ref, "refs/")))

	var response struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}

	status, requestErr := p.request(ctx, http.MethodGet, path, nil, &response)
	if status == http.StatusNotFound {
		return "", false, nil
	}

	if requestErr != nil {
		return "", false, requestErr
	}

	return response.Object.SHA, response.Object.SHA != "", nil
}

func (p *Publisher) pushBranch(ctx context.Context, operation publisher.Operation) error {
	if operation.SourceBundle == "" || !strings.HasPrefix(operation.TargetRef, "refs/heads/duto/m3/") {
		return publisher.ErrRejected
	}

	temporary, err := os.MkdirTemp("", "duto-publisher-push-")
	if err != nil {
		return fmt.Errorf("creating publisher push repository: %w", err)
	}
	defer os.RemoveAll(temporary) //nolint:errcheck // temporary publisher repository contains no credential files

	if initErr := publisherGit(ctx, "", nil, "init", "--bare", temporary); initErr != nil {
		return initErr
	}

	extra := map[string]string{
		"GIT_CONFIG_COUNT":   "1",
		"GIT_CONFIG_KEY_0":   "http.extraHeader",
		"GIT_CONFIG_VALUE_0": gitAuthorizationHeader(p.token),
	}
	remote := p.gitRemoteURL()

	if fetchErr := publisherGit(ctx, temporary, extra, "fetch", "--no-tags", "--no-write-fetch-head", remote, operation.BaseRef+":refs/duto/base"); fetchErr != nil {
		return fetchErr
	}

	base, err := publisherGitOutput(ctx, temporary, nil, "rev-parse", "refs/duto/base")
	if err != nil || strings.TrimSpace(string(base)) != operation.BaseSHA {
		return publisher.ErrConflict
	}

	if err := publisherGit(ctx, temporary, nil, "-c", "protocol.file.allow=always", "fetch", "--no-tags", "--no-write-fetch-head", operation.SourceBundle, "HEAD:refs/duto/source"); err != nil {
		return err
	}

	return publisherGit(ctx, temporary, extra, "push", remote, "refs/duto/source:"+operation.TargetRef)
}

func (p *Publisher) preflightPR(ctx context.Context, operation publisher.Operation) (publisher.RemoteState, error) {
	base := strings.TrimPrefix(operation.BaseRef, "refs/heads/")
	head := strings.TrimPrefix(operation.TargetRef, "refs/heads/")
	query := url.Values{"state": {"open"}, "head": {p.owner + ":" + head}, "base": {base}, "per_page": {"100"}}

	var pulls []struct {
		Title   string `json:"title"`
		Body    string `json:"body"`
		Draft   bool   `json:"draft"`
		HTMLURL string `json:"html_url"`
		Head    struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if _, err := p.request(ctx, http.MethodGet, p.repositoryPath("/pulls")+"?"+query.Encode(), nil, &pulls); err != nil {
		return publisher.RemoteState{}, err
	}

	marker := publisher.Marker(operation)

	prefix := publisher.MarkerPrefix(operation)
	for _, pull := range pulls {
		if pull.Draft && pull.Head.SHA == operation.SourceCommit && pull.Title == operation.PRTitle && strings.Contains(pull.Body, marker) {
			return publisher.RemoteState{Disposition: publisher.DispositionUnchanged, Resource: pull.HTMLURL}, nil
		}

		if strings.Contains(pull.Body, prefix) || pull.Head.SHA != "" {
			return publisher.RemoteState{Disposition: publisher.DispositionConflict, Resource: pull.HTMLURL}, nil
		}
	}

	return publisher.RemoteState{}, nil
}

func (p *Publisher) createDraftPR(ctx context.Context, operation publisher.Operation) (string, error) {
	request := struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Head  string `json:"head"`
		Base  string `json:"base"`
		Draft bool   `json:"draft"`
	}{
		Title: operation.PRTitle,
		Body:  operation.PRBody + "\n\n" + publisher.Marker(operation),
		Head:  strings.TrimPrefix(operation.TargetRef, "refs/heads/"),
		Base:  strings.TrimPrefix(operation.BaseRef, "refs/heads/"),
		Draft: true,
	}

	var response struct {
		HTMLURL string `json:"html_url"`
	}
	if _, err := p.request(ctx, http.MethodPost, p.repositoryPath("/pulls"), request, &response); err != nil {
		return "", err
	}

	if response.HTMLURL == "" {
		return "", errPublisherResponse
	}

	return response.HTMLURL, nil
}

func (p *Publisher) request(ctx context.Context, method, path string, body, output any) (int, error) {
	var input io.Reader = http.NoBody

	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("encoding GitHub publisher request: %w", err)
		}

		input = bytes.NewReader(encoded)
	}

	endpoint := strings.TrimRight(p.baseURL.String(), "/") + path

	request, err := http.NewRequestWithContext(ctx, method, endpoint, input)
	if err != nil {
		return 0, fmt.Errorf("creating GitHub publisher request: %w", err)
	}

	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+p.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	response, err := p.httpClient.Do(request)
	if err != nil {
		return 0, fmt.Errorf("executing GitHub publisher request: %w", err)
	}
	defer response.Body.Close() //nolint:errcheck // response body read result is authoritative

	data, err := io.ReadAll(io.LimitReader(response.Body, maxPublisherResponseBytes+1))
	if err != nil {
		return response.StatusCode, fmt.Errorf("reading GitHub publisher response: %w", err)
	}

	if len(data) > maxPublisherResponseBytes {
		return response.StatusCode, errPublisherResponse
	}

	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
		return response.StatusCode, publisher.ErrRejected
	}

	if response.StatusCode >= http.StatusBadRequest {
		return response.StatusCode, fmt.Errorf("%w: status %d", errPublisherResponse, response.StatusCode)
	}

	if output != nil && len(data) != 0 {
		if err := json.Unmarshal(data, output); err != nil {
			return response.StatusCode, fmt.Errorf("decoding GitHub publisher response: %w", err)
		}
	}

	return response.StatusCode, nil
}

func (p *Publisher) repositoryPath(resource string) string {
	return "/repos/" + url.PathEscape(p.owner) + "/" + url.PathEscape(p.repository) + resource
}

func gitAuthorizationHeader(token string) string {
	credential := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))

	return "Authorization: Basic " + credential
}

func (p *Publisher) gitRemoteURL() string {
	host := p.baseURL.Host
	if host == "api.github.com" {
		host = "github.com"
	}

	return p.baseURL.Scheme + "://" + host + "/" + url.PathEscape(p.owner) + "/" + url.PathEscape(p.repository) + ".git"
}

func publisherGit(ctx context.Context, directory string, extra map[string]string, args ...string) error {
	_, err := publisherGitOutput(ctx, directory, extra, args...)

	return err
}

func publisherGitOutput(ctx context.Context, directory string, extra map[string]string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", args...) //nolint:gosec // publisher owns every Git argv shape
	command.Dir = directory

	command.Env = []string{
		"PATH=" + os.Getenv("PATH"), "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_PAGER=cat", "GIT_TERMINAL_PROMPT=0", "GIT_PROTOCOL_FROM_USER=0",
	}
	for key, value := range extra {
		command.Env = append(command.Env, key+"="+value)
	}

	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("running fixed publisher Git command: %w", err)
	}

	if len(output) > maxPublisherResponseBytes {
		return nil, errPublisherResponse
	}

	return output, nil
}
