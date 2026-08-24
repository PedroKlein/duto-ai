package actiontest_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const (
	defaultRepositoryOwner = "example-org"
	defaultRepositoryName  = "demo-repo"
	defaultRepositoryID    = "9007199254740993"
	defaultWorkflowSHA     = "ffffffffffffffffffffffffffffffffffffffff"
	defaultRunID           = "1234567890"
	defaultPullRequestRef  = "refs/pull/52/merge"
	defaultMainRef         = "refs/heads/main"
	defaultMergeSHA        = "dddddddddddddddddddddddddddddddddddddddd"
)

type eventMappingSuccessCase struct {
	name            string
	eventName       string
	eventFixture    string
	workflowFixture string
	actor           string
	actorID         string
	ref             string
	githubSHA       string
	eventRewrite    func(content, headSHA string) string
	expected        func(headSHA string) map[string]any
}

type eventMappingFailureCase struct {
	name            string
	eventName       string
	eventFixture    string
	workflowFixture string
	actor           string
	actorID         string
	ref             string
	githubSHA       string
	eventRewrite    func(content, headSHA string) string
	stderrFragments []string
}

type prepareInvocation struct {
	workspace      string
	headSHA        string
	githubEnvPath  string
	githubOutPath  string
	eventPath      string
	workflowRel    string
	configRel      string
	runnerTempPath string
}

func TestEventMapping_FixtureCatalog(t *testing.T) {
	successCases := eventMappingSuccessCases()
	failureCases := eventMappingFailureCases()

	got := make([]string, 0, len(successCases)+len(failureCases))
	for _, tc := range successCases {
		got = append(got, tc.name)
	}

	for _, tc := range failureCases {
		got = append(got, tc.name)
	}

	want := []string{
		"workflow_dispatch",
		"schedule",
		"push",
		"pull_request",
		"issues",
		"issue_comment_on_pull_request",
		"pull_request_fork",
		"declared_subset_filtering",
		"unknown_event",
		"push_stale_revision",
		"pull_request_repository_contradiction",
		"unknown_declared_input",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("missing event mapping behavior: fixture table must contain the exact supported/negative case set\nwant: %v\ngot:  %v", want, got)
	}
}

func TestEventMapping_ClosedHostProjection(t *testing.T) {
	for _, tc := range eventMappingSuccessCases() {
		t.Run(tc.name, func(t *testing.T) {
			inv := setupPrepareInvocation(t, tc.workflowFixture, tc.eventFixture, tc.eventRewrite)

			githubSHA := inv.headSHA
			if tc.githubSHA != "" {
				githubSHA = tc.githubSHA
			}

			result := runPrepareForCase(t, inv, map[string]string{
				"GITHUB_EVENT_NAME":       tc.eventName,
				"GITHUB_REPOSITORY":       defaultRepositoryOwner + "/" + defaultRepositoryName,
				"GITHUB_REPOSITORY_OWNER": defaultRepositoryOwner,
				"GITHUB_REPOSITORY_ID":    defaultRepositoryID,
				"GITHUB_ACTOR":            tc.actor,
				"GITHUB_ACTOR_ID":         tc.actorID,
				"GITHUB_SHA":              githubSHA,
				"GITHUB_REF":              tc.ref,
				"GITHUB_WORKFLOW_SHA":     defaultWorkflowSHA,
				"GITHUB_RUN_ID":           defaultRunID,
			})

			if result.exitCode != 0 {
				t.Fatalf("missing event mapping behavior: %s must project closed host inputs\nexit=%d\nstderr:\n%s", tc.name, result.exitCode, result.stderr)
			}

			if strings.TrimSpace(result.stdout) != "" {
				t.Fatalf("missing event mapping behavior: %s prepare step must keep stdout empty, got %q", tc.name, result.stdout)
			}

			exports := readGitHubEnvFile(t, inv.githubEnvPath)

			inputsPath, ok := exports["DUTO_ACTION_INPUTS_FILE"]
			if !ok || strings.TrimSpace(inputsPath) == "" {
				t.Fatalf("missing event mapping behavior: %s must export DUTO_ACTION_INPUTS_FILE via GITHUB_ENV\nexports=%v", tc.name, exports)
			}

			mapped := readJSONMap(t, inputsPath)

			want := tc.expected(inv.headSHA)
			if !reflect.DeepEqual(mapped, want) {
				t.Fatalf("missing event mapping behavior: %s must emit exact projected JSON\nwant: %#v\ngot:  %#v", tc.name, want, mapped)
			}

			rawMapped, err := os.ReadFile(inputsPath)
			if err != nil {
				t.Fatalf("event mapping setup failure: read mapped inputs: %v", err)
			}

			if strings.Contains(string(rawMapped), "HOSTILE_CANARY") {
				t.Fatalf("missing event mapping behavior: %s projected inputs leaked hostile canary content\njson:\n%s", tc.name, string(rawMapped))
			}
		})
	}
}

func TestEventMapping_RejectsUnsupportedOrInvalidEvidence(t *testing.T) {
	for _, tc := range eventMappingFailureCases() {
		t.Run(tc.name, func(t *testing.T) {
			inv := setupPrepareInvocation(t, tc.workflowFixture, tc.eventFixture, tc.eventRewrite)

			githubSHA := inv.headSHA
			if tc.githubSHA != "" {
				githubSHA = tc.githubSHA
			}

			result := runPrepareForCase(t, inv, map[string]string{
				"GITHUB_EVENT_NAME":       tc.eventName,
				"GITHUB_REPOSITORY":       defaultRepositoryOwner + "/" + defaultRepositoryName,
				"GITHUB_REPOSITORY_OWNER": defaultRepositoryOwner,
				"GITHUB_REPOSITORY_ID":    defaultRepositoryID,
				"GITHUB_ACTOR":            tc.actor,
				"GITHUB_ACTOR_ID":         tc.actorID,
				"GITHUB_SHA":              githubSHA,
				"GITHUB_REF":              tc.ref,
				"GITHUB_WORKFLOW_SHA":     defaultWorkflowSHA,
				"GITHUB_RUN_ID":           defaultRunID,
			})

			if result.exitCode == 0 {
				t.Fatalf("missing event mapping behavior: %s must reject unsupported or untrusted evidence", tc.name)
			}

			for _, fragment := range tc.stderrFragments {
				if !containsAll(result.stderr, fragment) {
					t.Fatalf("missing event mapping behavior: %s rejection must mention %q\nstderr:\n%s", tc.name, fragment, result.stderr)
				}
			}

			exports := readGitHubEnvFile(t, inv.githubEnvPath)
			if path := strings.TrimSpace(exports["DUTO_ACTION_INPUTS_FILE"]); path != "" {
				t.Fatalf("missing event mapping behavior: %s must not export DUTO_ACTION_INPUTS_FILE on rejection\nexports=%v", tc.name, exports)
			}
		})
	}
}

func eventMappingSuccessCases() []eventMappingSuccessCase {
	return []eventMappingSuccessCase{
		{
			name:            "workflow_dispatch",
			eventName:       "workflow_dispatch",
			eventFixture:    "workflow_dispatch.json",
			workflowFixture: "workflow-all-inputs.yaml",
			actor:           "triggering-user",
			actorID:         "9007199254740999",
			ref:             defaultMainRef,
			expected: func(headSHA string) map[string]any {
				return universalMap("workflow_dispatch", "triggering-user", "9007199254740999", headSHA, defaultMainRef)
			},
		},
		{
			name:            "schedule",
			eventName:       "schedule",
			eventFixture:    "schedule.json",
			workflowFixture: "workflow-all-inputs.yaml",
			actor:           "scheduler-user",
			actorID:         "9007199254740998",
			ref:             defaultMainRef,
			expected: func(headSHA string) map[string]any {
				return universalMap("schedule", "scheduler-user", "9007199254740998", headSHA, defaultMainRef)
			},
		},
		{
			name:            "push",
			eventName:       "push",
			eventFixture:    "push.json",
			workflowFixture: "workflow-all-inputs.yaml",
			actor:           "pusher-user",
			actorID:         "9007199254740997",
			ref:             defaultMainRef,
			eventRewrite: func(content, headSHA string) string {
				return strings.ReplaceAll(content, "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", headSHA)
			},
			expected: func(headSHA string) map[string]any {
				return universalMap("push", "pusher-user", "9007199254740997", headSHA, defaultMainRef)
			},
		},
		{
			name:            "pull_request",
			eventName:       "pull_request",
			eventFixture:    "pull_request.json",
			workflowFixture: "workflow-all-inputs.yaml",
			actor:           "triggering-user",
			actorID:         "9007199254740999",
			ref:             defaultPullRequestRef,
			githubSHA:       defaultMergeSHA,
			eventRewrite: func(content, headSHA string) string {
				return strings.ReplaceAll(content, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", headSHA)
			},
			expected: func(headSHA string) map[string]any {
				mapped := universalMap("pull_request", "triggering-user", "9007199254740999", headSHA, defaultPullRequestRef)
				mapped["subject-kind"] = "pull_request"
				mapped["subject-number"] = json.Number("52")
				mapped["base-revision"] = headSHA
				mapped["head-revision"] = "cccccccccccccccccccccccccccccccccccccccc"
				mapped["base-repository-id"] = "9007199254740993"
				mapped["head-repository-id"] = "9007199254740995"
				mapped["fork"] = false

				return mapped
			},
		},
		{
			name:            "issues",
			eventName:       "issues",
			eventFixture:    "issues.json",
			workflowFixture: "workflow-all-inputs.yaml",
			actor:           "issue-user",
			actorID:         "9007199254740996",
			ref:             defaultMainRef,
			expected: func(headSHA string) map[string]any {
				mapped := universalMap("issues", "issue-user", "9007199254740996", headSHA, defaultMainRef)
				mapped["subject-kind"] = "issue"
				mapped["subject-number"] = json.Number("7")

				return mapped
			},
		},
		{
			name:            "issue_comment_on_pull_request",
			eventName:       "issue_comment",
			eventFixture:    "issue_comment.json",
			workflowFixture: "workflow-all-inputs.yaml",
			actor:           "comment-user",
			actorID:         "9007199254740994",
			ref:             defaultMainRef,
			expected: func(headSHA string) map[string]any {
				mapped := universalMap("issue_comment", "comment-user", "9007199254740994", headSHA, defaultMainRef)
				mapped["subject-kind"] = "pull_request"
				mapped["subject-number"] = json.Number("52")
				mapped["comment-id"] = "9007199254740123"

				return mapped
			},
		},
		{
			name:            "pull_request_fork",
			eventName:       "pull_request",
			eventFixture:    "pull_request_fork.json",
			workflowFixture: "workflow-all-inputs.yaml",
			actor:           "fork-user",
			actorID:         "9007199254740666",
			ref:             "refs/pull/62/merge",
			githubSHA:       defaultMergeSHA,
			eventRewrite: func(content, headSHA string) string {
				return strings.ReplaceAll(content, "abababababababababababababababababababab", headSHA)
			},
			expected: func(headSHA string) map[string]any {
				mapped := universalMap("pull_request", "fork-user", "9007199254740666", headSHA, "refs/pull/62/merge")
				mapped["subject-kind"] = "pull_request"
				mapped["subject-number"] = json.Number("62")
				mapped["base-revision"] = headSHA
				mapped["head-revision"] = "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
				mapped["base-repository-id"] = "9007199254740993"
				mapped["head-repository-id"] = "9007199254741888"
				mapped["fork"] = true

				return mapped
			},
		},
		{
			name:            "declared_subset_filtering",
			eventName:       "issue_comment",
			eventFixture:    "issue_comment.json",
			workflowFixture: "workflow-subset-inputs.yaml",
			actor:           "comment-user",
			actorID:         "9007199254740994",
			ref:             defaultMainRef,
			expected: func(headSHA string) map[string]any {
				return map[string]any{
					"event-name": "issue_comment",
					"revision":   headSHA,
					"comment-id": "9007199254740123",
				}
			},
		},
	}
}

func eventMappingFailureCases() []eventMappingFailureCase {
	return []eventMappingFailureCase{
		{
			name:            "unknown_event",
			eventName:       "workflow_run",
			eventFixture:    "unknown.json",
			workflowFixture: "workflow-all-inputs.yaml",
			actor:           "workflow-run-user",
			actorID:         "9007199254740111",
			ref:             defaultMainRef,
			stderrFragments: []string{"unsupported", "workflow_run"},
		},
		{
			name:            "push_stale_revision",
			eventName:       "push",
			eventFixture:    "push_stale.json",
			workflowFixture: "workflow-all-inputs.yaml",
			actor:           "stale-user",
			actorID:         "9007199254740222",
			ref:             defaultMainRef,
			stderrFragments: []string{"stale", "revision"},
		},
		{
			name:            "pull_request_repository_contradiction",
			eventName:       "pull_request",
			eventFixture:    "pull_request_contradiction.json",
			workflowFixture: "workflow-all-inputs.yaml",
			actor:           "contradiction-user",
			actorID:         "9007199254740333",
			ref:             "refs/pull/77/merge",
			githubSHA:       defaultMergeSHA,
			eventRewrite: func(content, headSHA string) string {
				return strings.ReplaceAll(content, "abababababababababababababababababababab", headSHA)
			},
			stderrFragments: []string{"contradict", "repository"},
		},
		{
			name:            "unknown_declared_input",
			eventName:       "workflow_dispatch",
			eventFixture:    "workflow_dispatch.json",
			workflowFixture: "workflow-unknown-input.yaml",
			actor:           "triggering-user",
			actorID:         "9007199254740999",
			ref:             defaultMainRef,
			stderrFragments: []string{"not-in-closed-map", "declared"},
		},
	}
}

func universalMap(eventName, actor, actorID, revision, ref string) map[string]any {
	return map[string]any{
		"event-name":        eventName,
		"repository-owner":  defaultRepositoryOwner,
		"repository-name":   defaultRepositoryName,
		"repository-id":     defaultRepositoryID,
		"actor":             actor,
		"actor-id":          actorID,
		"revision":          revision,
		"ref":               ref,
		"workflow-revision": defaultWorkflowSHA,
		"host-run-id":       defaultRunID,
	}
}

func setupPrepareInvocation(t *testing.T, workflowFixture, eventFixture string, rewrite func(content, headSHA string) string) prepareInvocation {
	t.Helper()

	workspace := t.TempDir()
	writeFixtureFile(t, eventsFixturePath(t, "config.yaml"), filepath.Join(workspace, "config.yaml"), 0o644)
	writeFixtureFile(t, eventsFixturePath(t, workflowFixture), filepath.Join(workspace, "workflow.yaml"), 0o644)

	headSHA := initWorkspaceRepository(t, workspace)

	eventBytes, err := os.ReadFile(eventsFixturePath(t, eventFixture))
	if err != nil {
		t.Fatalf("event mapping setup failure: read event fixture %s: %v", eventFixture, err)
	}

	content := string(eventBytes)
	if rewrite != nil {
		content = rewrite(content, headSHA)
	}

	eventPath := filepath.Join(workspace, "event.json")
	if err := os.WriteFile(eventPath, []byte(content), 0o644); err != nil {
		t.Fatalf("event mapping setup failure: write event fixture %s: %v", eventFixture, err)
	}

	githubEnvPath := filepath.Join(t.TempDir(), "github-env.txt")

	githubOutPath := filepath.Join(t.TempDir(), "github-output.txt")
	if err := os.WriteFile(githubEnvPath, nil, 0o600); err != nil {
		t.Fatalf("event mapping setup failure: create GITHUB_ENV: %v", err)
	}

	if err := os.WriteFile(githubOutPath, nil, 0o600); err != nil {
		t.Fatalf("event mapping setup failure: create GITHUB_OUTPUT: %v", err)
	}

	return prepareInvocation{
		workspace:      workspace,
		headSHA:        headSHA,
		githubEnvPath:  githubEnvPath,
		githubOutPath:  githubOutPath,
		eventPath:      eventPath,
		workflowRel:    "workflow.yaml",
		configRel:      "config.yaml",
		runnerTempPath: t.TempDir(),
	}
}

func runPrepareForCase(t *testing.T, inv prepareInvocation, extra map[string]string) commandResult {
	t.Helper()

	prepareScript := filepath.Join(repoRoot(t), "action", "prepare.sh")
	cmd := exec.Command("bash", prepareScript)
	cmd.Dir = repoRoot(t)

	env := append([]string{}, os.Environ()...)

	merged := map[string]string{
		"GITHUB_WORKSPACE":  inv.workspace,
		"INPUT_WORKFLOW":    inv.workflowRel,
		"INPUT_CONFIG":      inv.configRel,
		"GITHUB_EVENT_PATH": inv.eventPath,
		"RUNNER_TEMP":       inv.runnerTempPath,
		"GITHUB_ENV":        inv.githubEnvPath,
		"GITHUB_OUTPUT":     inv.githubOutPath,
	}
	maps.Copy(merged, extra)

	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		env = append(env, key+"="+merged[key])
	}

	cmd.Env = env

	return runCommand(t, cmd, nil)
}

func readGitHubEnvFile(t *testing.T, path string) map[string]string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}
		}

		t.Fatalf("event mapping setup failure: read GITHUB_ENV %s: %v", path, err)
	}

	out := map[string]string{}

	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		out[parts[0]] = parts[1]
	}

	return out
}

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("event mapping setup failure: read JSON file %s: %v", path, err)
	}

	dec := json.NewDecoder(bytes.NewReader(content))
	dec.UseNumber()

	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("event mapping setup failure: decode JSON file %s: %v", path, err)
	}

	return out
}

func initWorkspaceRepository(t *testing.T, dir string) string {
	t.Helper()

	runGit := func(args ...string) {
		t.Helper()

		cmd := exec.Command("git", args...)
		cmd.Dir = dir

		var stderr bytes.Buffer

		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("event mapping setup failure: git %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
		}
	}

	runGit("init")
	runGit("config", "user.name", "Action Test")
	runGit("config", "user.email", "action-test@example.invalid")

	marker := filepath.Join(dir, ".head-marker")
	if err := os.WriteFile(marker, []byte("head"), 0o644); err != nil {
		t.Fatalf("event mapping setup failure: write git marker: %v", err)
	}

	runGit("add", ".head-marker")
	runGit("commit", "-m", "head")

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir

	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("event mapping setup failure: git rev-parse HEAD: %v", err)
	}

	return strings.TrimSpace(string(stdout))
}

func eventsFixturePath(t *testing.T, name string) string {
	t.Helper()

	return filepath.Join(repoRoot(t), "internal", "actiontest", "testdata", "events", name)
}
