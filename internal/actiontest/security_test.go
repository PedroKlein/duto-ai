package actiontest_test

import (
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	securityRequiredPermissionsEnv = "DUTO_ACTION_REQUIRED_PERMISSIONS"
	securityTokenCanary            = "TOKEN_CANARY_SECURITY"
)

type securityPermissionCase struct {
	name            string
	workflowFixture string
	want            []string
}

type securityTrustCase struct {
	name            string
	eventName       string
	eventFixture    string
	workflowFixture string
	actor           string
	actorID         string
	ref             string
	githubSHA       string
	eventRewrite    func(content, headSHA string) string
	errorFragments  []string
}

func TestPermissions_FixtureCatalog(t *testing.T) {
	cases := permissionScopeCases()

	got := make([]string, 0, len(cases))
	for _, tc := range cases {
		got = append(got, tc.name)
	}

	want := []string{
		"contents_only",
		"pull_requests_only",
		"issues_only",
		"checks_only",
		"all_read_scopes",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("missing permission policy behavior: fixture table must contain exact independent scope-set cases\nwant: %v\ngot:  %v", want, got)
	}
}

func TestPermissions_RequiredScopeSetEquality(t *testing.T) {
	for _, tc := range permissionScopeCases() {
		t.Run(tc.name, func(t *testing.T) {
			inv := setupSecurityPrepareInvocation(t, tc.workflowFixture, "event-workflow-dispatch.json", nil)

			result := runSecurityPrepare(t, inv, workflowDispatchEnv(inv.headSHA))
			if result.exitCode != 0 {
				t.Fatalf("missing permission policy behavior: %s must admit and export exact required read scopes\nexit=%d\nstderr:\n%s", tc.name, result.exitCode, result.stderr)
			}

			exports := readGitHubEnvFile(t, inv.githubEnvPath)

			raw := strings.TrimSpace(exports[securityRequiredPermissionsEnv])
			if raw == "" {
				t.Fatalf("missing permission policy behavior: %s must export %s with exact required read scopes\nexports=%v", tc.name, securityRequiredPermissionsEnv, exports)
			}

			got := parsePermissionSet(raw)

			want := append([]string(nil), tc.want...)
			sort.Strings(want)

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("missing permission policy behavior: %s required scope set mismatch (exact equality required)\nwant: %v\ngot:  %v\nraw:  %q", tc.name, want, got, raw)
			}
		})
	}
}

func TestPermissions_ProtectedBoundary403SingleRequestNoFallback(t *testing.T) {
	var (
		requestCount atomic.Int64
		withAuth     atomic.Int64
		withoutAuth  atomic.Int64
	)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)

		if strings.TrimSpace(r.Header.Get("Authorization")) == "" {
			withoutAuth.Add(1)
		} else {
			withAuth.Add(1)
		}

		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer api.Close()

	inv := setupSecurityPrepareInvocation(t, "workflow-permissions-issues.yaml", "event-workflow-dispatch.json", nil)
	env := workflowDispatchEnv(inv.headSHA)
	env["GITHUB_API_URL"] = api.URL
	env["GITHUB_SERVER_URL"] = api.URL
	env["GITHUB_TOKEN"] = securityTokenCanary

	result := runSecurityPrepare(t, inv, env)
	if result.exitCode == 0 {
		t.Fatalf("missing permission policy behavior: protected API 403 must fail closed with no fallback")
	}

	if got := requestCount.Load(); got != 1 {
		t.Fatalf("missing permission policy behavior: protected API 403 must perform exactly one request with no retry or anonymous fallback\nwant requests: 1\ngot requests:  %d\nstderr:\n%s", got, result.stderr)
	}

	if withoutAuth.Load() != 0 {
		t.Fatalf("missing permission policy behavior: protected API 403 must not fall back to an anonymous request\nanonymous requests: %d", withoutAuth.Load())
	}

	if withAuth.Load() != 1 {
		t.Fatalf("missing permission policy behavior: protected API 403 must use one authenticated request\nauthenticated requests: %d", withAuth.Load())
	}
}

func TestTrust_FixtureCatalog(t *testing.T) {
	cases := trustCases()

	got := make([]string, 0, len(cases))
	for _, tc := range cases {
		got = append(got, tc.name)
	}

	want := []string{
		"fork_pull_request",
		"unknown_issue_comment",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("missing trust policy behavior: fixture table must contain exact fork/unknown cases\nwant: %v\ngot:  %v", want, got)
	}
}

func TestTrust_ForkUnknownRejectProcessCapableBeforeRuntime(t *testing.T) {
	for _, tc := range trustCases() {
		t.Run(tc.name, func(t *testing.T) {
			inv := setupSecurityPrepareInvocation(t, tc.workflowFixture, tc.eventFixture, tc.eventRewrite)

			result := runSecurityPrepare(t, inv, trustCaseEnv(tc, inv.headSHA))

			runtimeStarts := 0
			if result.exitCode == 0 {
				_, _, runtimeStarts, _ = runSecurityRuntime(t, inv, nil)
			}

			if result.exitCode == 0 {
				t.Fatalf("missing trust policy behavior: %s must reject process-capable plans in fork/unknown contexts before runtime construction", tc.name)
			}

			for _, fragment := range tc.errorFragments {
				if !containsAll(result.stderr, fragment) {
					t.Fatalf("missing trust policy behavior: %s rejection must mention %q\nstderr:\n%s", tc.name, fragment, result.stderr)
				}
			}

			if runtimeStarts != 0 {
				t.Fatalf("missing trust policy behavior: %s must start zero provider/model/process boundaries on fork/unknown process-capable rejection\nruntime starts: %d", tc.name, runtimeStarts)
			}
		})
	}
}

func TestTrust_ForkUnknownTransitiveReadOnlyAllowsRuntime(t *testing.T) {
	for _, tc := range trustCases() {
		t.Run(tc.name, func(t *testing.T) {
			inv := setupSecurityPrepareInvocation(t, "workflow-read-only.yaml", tc.eventFixture, tc.eventRewrite)

			result := runSecurityPrepare(t, inv, trustCaseEnv(tc, inv.headSHA))
			if result.exitCode != 0 {
				t.Fatalf("missing trust policy behavior: %s must admit transitively read-only plans in fork/unknown contexts\nexit=%d\nstderr:\n%s", tc.name, result.exitCode, result.stderr)
			}

			runResult, _, runtimeStarts, _ := runSecurityRuntime(t, inv, map[string]string{"GITHUB_TOKEN": securityTokenCanary})
			if runResult.exitCode != 0 {
				t.Fatalf("missing trust policy behavior: %s admitted read-only plan must run through runtime boundary\nexit=%d\nstderr:\n%s", tc.name, runResult.exitCode, runResult.stderr)
			}

			if runtimeStarts != 1 {
				t.Fatalf("missing trust policy behavior: %s admitted read-only plan must start runtime exactly once\nwant starts: 1\ngot starts:  %d", tc.name, runtimeStarts)
			}
		})
	}
}

func TestToken_RuntimeExposureMatchesGitHubReadNeed(t *testing.T) {
	tests := []struct {
		name            string
		workflowFixture string
		expectToken     bool
	}{
		{
			name:            "no_github_read_tools",
			workflowFixture: "workflow-permissions-contents.yaml",
			expectToken:     false,
		},
		{
			name:            "github_read_tools_selected",
			workflowFixture: "workflow-permissions-issues.yaml",
			expectToken:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inv := setupSecurityPrepareInvocation(t, tc.workflowFixture, "event-workflow-dispatch.json", nil)

			prepareResult := runSecurityPrepare(t, inv, workflowDispatchEnv(inv.headSHA))
			if prepareResult.exitCode != 0 {
				t.Fatalf("missing token policy behavior: %s must admit before runtime token-scoping checks\nexit=%d\nstderr:\n%s", tc.name, prepareResult.exitCode, prepareResult.stderr)
			}

			runResult, runtimeEnv, runtimeStarts, evidenceDir := runSecurityRuntime(t, inv, map[string]string{"GITHUB_TOKEN": securityTokenCanary, "GH_TOKEN": securityTokenCanary})
			if runResult.exitCode != 0 {
				t.Fatalf("missing token policy behavior: %s must execute runtime boundary\nexit=%d\nstderr:\n%s", tc.name, runResult.exitCode, runResult.stderr)
			}

			if runtimeStarts != 1 {
				t.Fatalf("missing token policy behavior: %s must invoke runtime exactly once\nwant starts: 1\ngot starts:  %d", tc.name, runtimeStarts)
			}

			hasToken := strings.Contains(runtimeEnv, "GITHUB_TOKEN="+securityTokenCanary) || strings.Contains(runtimeEnv, "GH_TOKEN="+securityTokenCanary)
			if hasToken != tc.expectToken {
				t.Fatalf("missing token policy behavior: %s token scope mismatch\nexpect token in child env: %t\nactual token in child env: %t", tc.name, tc.expectToken, hasToken)
			}

			for _, channel := range []struct {
				name    string
				content string
			}{
				{name: "run stdout", content: runResult.stdout},
				{name: "run stderr", content: runResult.stderr},
			} {
				if strings.Contains(channel.content, securityTokenCanary) {
					t.Fatalf("missing token policy behavior: %s leaked token canary in %s", tc.name, channel.name)
				}
			}

			assertCanariesAbsentInDir(t, evidenceDir, []string{securityTokenCanary})
		})
	}
}

func TestToken_ContentCanaryAbsent(t *testing.T) {
	inv := setupSecurityPrepareInvocation(t, "workflow-permissions-contents.yaml", "event-workflow-dispatch.json", nil)
	prepareEnv := workflowDispatchEnv(inv.headSHA)
	prepareEnv["GITHUB_TOKEN"] = securityTokenCanary

	prepareResult := runSecurityPrepare(t, inv, prepareEnv)
	if prepareResult.exitCode != 0 {
		t.Fatalf("missing redaction behavior: prepare step must admit content-free no-read case before canary scan\nexit=%d\nstderr:\n%s", prepareResult.exitCode, prepareResult.stderr)
	}

	runResult, runtimeEnv, _, evidenceDir := runSecurityRuntime(t, inv, map[string]string{"GITHUB_TOKEN": securityTokenCanary, "GH_TOKEN": securityTokenCanary})
	if runResult.exitCode != 0 {
		t.Fatalf("missing redaction behavior: no-read runtime invocation must complete before canary scan\nexit=%d\nstderr:\n%s", runResult.exitCode, runResult.stderr)
	}

	canaries := securityCanaries(t)

	githubEnvBytes, err := os.ReadFile(inv.githubEnvPath)
	if err != nil {
		t.Fatalf("security setup failure: read GITHUB_ENV: %v", err)
	}

	githubOutputBytes, err := os.ReadFile(inv.githubOutPath)
	if err != nil {
		t.Fatalf("security setup failure: read GITHUB_OUTPUT: %v", err)
	}

	channels := []struct {
		name    string
		content string
	}{
		{name: "prepare stdout", content: prepareResult.stdout},
		{name: "prepare stderr", content: prepareResult.stderr},
		{name: "run stdout", content: runResult.stdout},
		{name: "run stderr", content: runResult.stderr},
		{name: "GITHUB_ENV", content: string(githubEnvBytes)},
		{name: "GITHUB_OUTPUT", content: string(githubOutputBytes)},
		{name: "runtime env", content: runtimeEnv},
	}

	for _, channel := range channels {
		assertCanariesAbsentInText(t, channel.name, channel.content, canaries)
	}

	assertCanariesAbsentInDir(t, evidenceDir, canaries)
}

func permissionScopeCases() []securityPermissionCase {
	return []securityPermissionCase{
		{name: "contents_only", workflowFixture: "workflow-permissions-contents.yaml", want: []string{"contents"}},
		{name: "pull_requests_only", workflowFixture: "workflow-permissions-pull-requests.yaml", want: []string{"contents", "pull-requests"}},
		{name: "issues_only", workflowFixture: "workflow-permissions-issues.yaml", want: []string{"contents", "issues"}},
		{name: "checks_only", workflowFixture: "workflow-permissions-checks.yaml", want: []string{"checks", "contents"}},
		{name: "all_read_scopes", workflowFixture: "workflow-permissions-all.yaml", want: []string{"checks", "contents", "issues", "pull-requests"}},
	}
}

func trustCases() []securityTrustCase {
	return []securityTrustCase{
		{
			name:            "fork_pull_request",
			eventName:       "pull_request",
			eventFixture:    "event-pull-request-fork.json",
			workflowFixture: "workflow-process-capable.yaml",
			actor:           "fork-user",
			actorID:         "9007199254740666",
			ref:             "refs/pull/62/merge",
			githubSHA:       defaultMergeSHA,
			eventRewrite: func(content, headSHA string) string {
				return strings.ReplaceAll(content, "abababababababababababababababababababab", headSHA)
			},
			errorFragments: []string{"fork", "read-only"},
		},
		{
			name:            "unknown_issue_comment",
			eventName:       "issue_comment",
			eventFixture:    "event-issue-comment.json",
			workflowFixture: "workflow-process-capable.yaml",
			actor:           "comment-user",
			actorID:         "9007199254740994",
			ref:             defaultMainRef,
			errorFragments:  []string{"unknown", "read-only"},
		},
	}
}

func setupSecurityPrepareInvocation(t *testing.T, workflowFixture, eventFixture string, rewrite func(content, headSHA string) string) prepareInvocation {
	t.Helper()

	workspace := t.TempDir()
	writeFixtureFile(t, securityFixturePath(t, "config.yaml"), filepath.Join(workspace, "config.yaml"), 0o644)
	writeFixtureFile(t, securityFixturePath(t, workflowFixture), filepath.Join(workspace, "workflow.yaml"), 0o644)

	headSHA := initWorkspaceRepository(t, workspace)

	eventBytes, err := os.ReadFile(securityFixturePath(t, eventFixture))
	if err != nil {
		t.Fatalf("security setup failure: read event fixture %s: %v", eventFixture, err)
	}

	content := string(eventBytes)
	if rewrite != nil {
		content = rewrite(content, headSHA)
	}

	eventPath := filepath.Join(workspace, "event.json")
	if err := os.WriteFile(eventPath, []byte(content), 0o644); err != nil {
		t.Fatalf("security setup failure: write event fixture %s: %v", eventFixture, err)
	}

	githubEnvPath := filepath.Join(t.TempDir(), "github-env.txt")
	githubOutPath := filepath.Join(t.TempDir(), "github-output.txt")
	runnerTempPath := filepath.Join(t.TempDir(), "runner-temp")

	if err := os.MkdirAll(runnerTempPath, 0o755); err != nil {
		t.Fatalf("security setup failure: create runner temp path: %v", err)
	}

	if err := os.WriteFile(githubEnvPath, nil, 0o600); err != nil {
		t.Fatalf("security setup failure: create GITHUB_ENV: %v", err)
	}

	if err := os.WriteFile(githubOutPath, nil, 0o600); err != nil {
		t.Fatalf("security setup failure: create GITHUB_OUTPUT: %v", err)
	}

	return prepareInvocation{
		workspace:      workspace,
		headSHA:        headSHA,
		githubEnvPath:  githubEnvPath,
		githubOutPath:  githubOutPath,
		eventPath:      eventPath,
		workflowRel:    "workflow.yaml",
		configRel:      "config.yaml",
		runnerTempPath: runnerTempPath,
	}
}

func runSecurityPrepare(t *testing.T, inv prepareInvocation, extra map[string]string) commandResult {
	t.Helper()

	prepareScript := filepath.Join(repoRoot(t), "action", "prepare.sh")
	cmd := exec.Command("bash", prepareScript)
	cmd.Dir = repoRoot(t)

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

	cmd.Env = securityEnvironment(merged)

	return runCommand(t, cmd, nil)
}

func runSecurityRuntime(t *testing.T, inv prepareInvocation, extra map[string]string) (commandResult, string, int, string) {
	t.Helper()

	exports := readGitHubEnvFile(t, inv.githubEnvPath)

	inputsPath := strings.TrimSpace(exports["DUTO_ACTION_INPUTS_FILE"])
	if inputsPath == "" {
		t.Fatalf("security setup failure: DUTO_ACTION_INPUTS_FILE must be exported before runtime invocation\nexports=%v", exports)
	}

	fakeDir := t.TempDir()
	fakeBinary := filepath.Join(fakeDir, "duto-ai-fake")
	runtimeEnvPath := filepath.Join(fakeDir, "runtime-env.txt")
	runtimeStartsPath := filepath.Join(fakeDir, "runtime-starts.txt")
	evidenceDir := filepath.Join(fakeDir, "runtime-evidence")

	fakeScript := "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'start\\n' >> \"${DUTO_FAKE_RUNTIME_STARTS_FILE}\"\nenv | LC_ALL=C sort > \"${DUTO_FAKE_RUNTIME_ENV_FILE}\"\nprintf '{\"status\":\"succeeded\",\"outcome\":\"completed\",\"run_id\":\"run-security-canary\"}\\n'\n"
	if err := os.WriteFile(fakeBinary, []byte(fakeScript), 0o755); err != nil {
		t.Fatalf("security setup failure: write fake runtime binary: %v", err)
	}

	runScript := filepath.Join(repoRoot(t), "action", "run.sh")
	cmd := exec.Command("bash", runScript, fakeBinary)
	cmd.Dir = repoRoot(t)

	merged := map[string]string{
		"GITHUB_WORKSPACE":              inv.workspace,
		"RUNNER_TEMP":                   inv.runnerTempPath,
		"INPUT_WORKFLOW":                inv.workflowRel,
		"INPUT_CONFIG":                  inv.configRel,
		"INPUT_VERSION":                 "v0.2.2",
		"INPUT_EVIDENCE_RETENTION_DAYS": "7",
		"DUTO_ACTION_INPUTS_FILE":       inputsPath,
		"DUTO_ACTION_EVIDENCE_DIR":      evidenceDir,
		"DUTO_FAKE_RUNTIME_ENV_FILE":    runtimeEnvPath,
		"DUTO_FAKE_RUNTIME_STARTS_FILE": runtimeStartsPath,
	}

	maps.Copy(merged, exports)
	maps.Copy(merged, extra)

	cmd.Env = securityEnvironment(merged)

	result := runCommand(t, cmd, nil)

	runtimeEnv := ""

	runtimeEnvBytes, err := os.ReadFile(runtimeEnvPath)
	if err == nil {
		runtimeEnv = string(runtimeEnvBytes)
	}

	runtimeStarts := 0

	startsBytes, err := os.ReadFile(runtimeStartsPath)
	if err == nil {
		for line := range strings.SplitSeq(strings.TrimSpace(string(startsBytes)), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}

			runtimeStarts++
		}
	}

	return result, runtimeEnv, runtimeStarts, evidenceDir
}

func workflowDispatchEnv(headSHA string) map[string]string {
	return map[string]string{
		"GITHUB_EVENT_NAME":       "workflow_dispatch",
		"GITHUB_REPOSITORY":       defaultRepositoryOwner + "/" + defaultRepositoryName,
		"GITHUB_REPOSITORY_OWNER": defaultRepositoryOwner,
		"GITHUB_REPOSITORY_ID":    defaultRepositoryID,
		"GITHUB_ACTOR":            "triggering-user",
		"GITHUB_ACTOR_ID":         "9007199254740999",
		"GITHUB_SHA":              headSHA,
		"GITHUB_REF":              defaultMainRef,
		"GITHUB_WORKFLOW_SHA":     defaultWorkflowSHA,
		"GITHUB_RUN_ID":           defaultRunID,
	}
}

func trustCaseEnv(tc securityTrustCase, headSHA string) map[string]string {
	sha := headSHA
	if tc.githubSHA != "" {
		sha = tc.githubSHA
	}

	return map[string]string{
		"GITHUB_EVENT_NAME":       tc.eventName,
		"GITHUB_REPOSITORY":       defaultRepositoryOwner + "/" + defaultRepositoryName,
		"GITHUB_REPOSITORY_OWNER": defaultRepositoryOwner,
		"GITHUB_REPOSITORY_ID":    defaultRepositoryID,
		"GITHUB_ACTOR":            tc.actor,
		"GITHUB_ACTOR_ID":         tc.actorID,
		"GITHUB_SHA":              sha,
		"GITHUB_REF":              tc.ref,
		"GITHUB_WORKFLOW_SHA":     defaultWorkflowSHA,
		"GITHUB_RUN_ID":           defaultRunID,
	}
}

func securityEnvironment(extra map[string]string) []string {
	merged := map[string]string{
		"PATH": os.Getenv("PATH"),
		"HOME": os.Getenv("HOME"),
	}

	if tmp := os.Getenv("TMPDIR"); strings.TrimSpace(tmp) != "" {
		merged["TMPDIR"] = tmp
	}

	maps.Copy(merged, extra)

	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, fmt.Sprintf("%s=%s", key, merged[key]))
	}

	return env
}

func parsePermissionSet(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', ' ', '\t', '\n', '\r':
			return true
		default:
			return false
		}
	})

	set := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		set[part] = struct{}{}
	}

	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}

	sort.Strings(out)

	return out
}

func securityCanaries(t *testing.T) []string {
	t.Helper()

	content, err := os.ReadFile(securityFixturePath(t, "canaries.txt"))
	if err != nil {
		t.Fatalf("security setup failure: read canary fixture: %v", err)
	}

	canaries := make([]string, 0)

	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		canaries = append(canaries, line)
	}

	if len(canaries) == 0 {
		t.Fatalf("security setup failure: canary fixture must contain at least one marker")
	}

	return canaries
}

func assertCanariesAbsentInText(t *testing.T, location, content string, canaries []string) {
	t.Helper()

	for _, canary := range canaries {
		if strings.Contains(content, canary) {
			t.Fatalf("missing redaction behavior: %s leaked canary %q", location, canary)
		}
	}
}

func assertCanariesAbsentInDir(t *testing.T, dir string, canaries []string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}

		t.Fatalf("security setup failure: read evidence directory %s: %v", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		path := filepath.Join(dir, entry.Name())

		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("security setup failure: read file %s: %v", path, err)
		}

		assertCanariesAbsentInText(t, path, string(content), canaries)
	}
}

func securityFixturePath(t *testing.T, name string) string {
	t.Helper()

	return filepath.Join(repoRoot(t), "internal", "actiontest", "testdata", "security", name)
}
