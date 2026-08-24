//go:build integration

package actiontest_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	dutoTestScenarioSetDigest  = "a9f4458fed938d5332fd75c5331d8dbedbc3c79a9905089cbf920e432afaca0a"
	dutoTestScenarioConfigRel  = ".github/ai-workflows/config-m2.yaml"
	dutoTestHostedWorkflowRel  = ".github/workflows/ai-scenarios.yaml"
	dutoTestHostedWorkflowName = "duto M2 ${{ inputs.correlation_id }}"
)

type dutoTestScenarioOwnership struct {
	name  string
	owner string
}

type dutoTestM2Case struct {
	name              string
	workflowRel       string
	promptRel         string
	skillRel          string
	expectedTypedData func(headSHA string) map[string]any
}

type dutoTestHostedWorkflow struct {
	RunName string                          `yaml:"run-name"`
	On      dutoTestHostedWorkflowOn        `yaml:"on"`
	Jobs    map[string]dutoTestHostedJobDef `yaml:"jobs"`
}

type dutoTestHostedWorkflowOn struct {
	WorkflowDispatch dutoTestHostedDispatch `yaml:"workflow_dispatch"`
}

type dutoTestHostedDispatch struct {
	Inputs map[string]dutoTestHostedDispatchInput `yaml:"inputs"`
}

type dutoTestHostedDispatchInput struct {
	Required bool `yaml:"required"`
}

type dutoTestHostedJobDef struct {
	Permissions map[string]string `yaml:"permissions"`
	Strategy    struct {
		Matrix struct {
			Include []dutoTestHostedMatrixRow `yaml:"include"`
		} `yaml:"matrix"`
	} `yaml:"strategy"`
	Steps []dutoTestHostedStep `yaml:"steps"`
}

type dutoTestHostedMatrixRow struct {
	Scenario string `yaml:"scenario"`
	Workflow string `yaml:"workflow"`
}

type dutoTestHostedStep struct {
	Uses string `yaml:"uses"`
	Run  string `yaml:"run"`
}

func TestDutoTestM2Scenarios(t *testing.T) {
	dutoTestDir := strings.TrimSpace(os.Getenv("DUTO_TEST_DIR"))
	if dutoTestDir == "" {
		t.Skip("skipping M2 cross-repository scenario assertions: DUTO_TEST_DIR is required")
	}

	assertDutoTestScenarioSetIdentity(t, dutoTestDir)
	assertDutoTestHostedWorkflowContract(t, dutoTestDir)

	cases := []dutoTestM2Case{
		{
			name:        "template-prompt-file",
			workflowRel: ".github/ai-workflows/scenarios/template-prompt-file.yaml",
			promptRel:   ".github/ai-workflows/prompts/review-pr.md",
			skillRel:    ".github/ai-workflows/skills/code-review.md",
			expectedTypedData: func(_ string) map[string]any {
				return map[string]any{
					"event-name":       "pull_request",
					"repository-owner": "example-org",
					"repository-name":  "demo-repo",
					"subject-kind":     "pull_request",
					"subject-number":   json.Number("52"),
					"actor":            "triggering-user",
				}
			},
		},
		{
			name:        "template-variables",
			workflowRel: ".github/ai-workflows/scenarios/template-variables.yaml",
			expectedTypedData: func(headSHA string) map[string]any {
				return map[string]any{
					"event-name":       "pull_request",
					"repository-owner": "example-org",
					"repository-name":  "demo-repo",
					"actor":            "triggering-user",
					"ref":              defaultPullRequestRef,
					"revision":         headSHA,
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verifyDutoTestM2Scenario(t, dutoTestDir, tc)
		})
	}
}

func verifyDutoTestM2Scenario(t *testing.T, dutoTestDir string, tc dutoTestM2Case) {
	t.Helper()

	issues := make([]string, 0)

	workflowPath := filepath.Join(dutoTestDir, tc.workflowRel)
	workflowContent, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("missing M2 scenario migration behavior: read workflow %q: %v", tc.workflowRel, err)
	}

	configPath := filepath.Join(dutoTestDir, dutoTestScenarioConfigRel)
	if err := assertRegularFile(configPath); err != nil {
		issues = append(issues, fmt.Sprintf("real config path %q is unresolved: %v", dutoTestScenarioConfigRel, err))
	}

	if tc.promptRel != "" {
		promptPath := filepath.Join(dutoTestDir, tc.promptRel)
		if err := assertRegularFile(promptPath); err != nil {
			issues = append(issues, fmt.Sprintf("real prompt path %q is unresolved: %v", tc.promptRel, err))
		} else {
			promptContent, readErr := os.ReadFile(promptPath)
			if readErr != nil {
				issues = append(issues, fmt.Sprintf("read prompt %q: %v", tc.promptRel, readErr))
			} else {
				issues = append(issues, forbiddenMarkerIssues(fmt.Sprintf("prompt %q", tc.promptRel), string(promptContent))...)
			}
		}
	}

	if tc.skillRel != "" {
		skillPath := filepath.Join(dutoTestDir, tc.skillRel)
		if err := assertRegularFile(skillPath); err != nil {
			issues = append(issues, fmt.Sprintf("real skill path %q is unresolved: %v", tc.skillRel, err))
		} else {
			skillContent, readErr := os.ReadFile(skillPath)
			if readErr != nil {
				issues = append(issues, fmt.Sprintf("read skill %q: %v", tc.skillRel, readErr))
			} else {
				issues = append(issues, forbiddenMarkerIssues(fmt.Sprintf("skill %q", tc.skillRel), string(skillContent))...)
			}
		}
	}

	issues = append(issues, forbiddenMarkerIssues(fmt.Sprintf("workflow %q", tc.workflowRel), string(workflowContent))...)

	inv := setupDutoTestPrepareInvocation(t, dutoTestDir, tc.workflowRel, dutoTestScenarioConfigRel)
	prepareResult := runSecurityPrepare(t, inv, dutoTestPullRequestEnv())
	if prepareResult.exitCode != 0 {
		issues = append(issues, fmt.Sprintf("local Action prepare step must admit and map typed host values for %s\nexit=%d\nstderr:\n%s", tc.name, prepareResult.exitCode, prepareResult.stderr))
	} else {
		exports := readGitHubEnvFile(t, inv.githubEnvPath)
		inputsPath := strings.TrimSpace(exports["DUTO_ACTION_INPUTS_FILE"])
		if inputsPath == "" {
			issues = append(issues, fmt.Sprintf("prepare step did not export DUTO_ACTION_INPUTS_FILE (exit=%d)\nstdout:\n%s\nstderr:\n%s", prepareResult.exitCode, prepareResult.stdout, prepareResult.stderr))
		} else {
			mapped := readJSONMap(t, inputsPath)
			for key, want := range tc.expectedTypedData(inv.headSHA) {
				got, ok := mapped[key]
				if !ok {
					issues = append(issues, fmt.Sprintf("typed host value %q is missing from mapped inputs", key))
					continue
				}
				if !reflect.DeepEqual(got, want) {
					issues = append(issues, fmt.Sprintf("typed host value %q mismatch: got %#v, want %#v", key, got, want))
				}
			}

			rawMapped, readErr := os.ReadFile(inputsPath)
			if readErr != nil {
				issues = append(issues, fmt.Sprintf("read mapped input file %q: %v", inputsPath, readErr))
			} else {
				issues = append(issues, rawHostContentIssues("mapped inputs JSON", string(rawMapped))...)
			}

			runtimeResult, traceLine := runDutoTestRuntimeBoundary(t, inv, exports)
			if runtimeResult.exitCode != 0 {
				issues = append(issues, fmt.Sprintf("local Action runtime boundary must execute fake model for %s\nexit=%d\nstderr:\n%s", tc.name, runtimeResult.exitCode, runtimeResult.stderr))
			} else {
				for _, fragment := range []string{"run", "--format", "json", "--inputs", "--evidence-directory", filepath.Join(dutoTestDir, tc.workflowRel), filepath.Join(dutoTestDir, dutoTestScenarioConfigRel)} {
					if !strings.Contains(traceLine, fragment) {
						issues = append(issues, fmt.Sprintf("runtime invocation is missing %q in fake-model trace: %s", fragment, traceLine))
					}
				}
			}
		}
	}

	if len(issues) > 0 {
		t.Fatalf("missing M2 scenario migration behavior for %s:\n- %s", tc.name, strings.Join(issues, "\n- "))
	}
}

func assertDutoTestScenarioSetIdentity(t *testing.T, dutoTestDir string) {
	t.Helper()

	expected := []dutoTestScenarioOwnership{
		{name: "agent-skills", owner: "M1"},
		{name: "context-files", owner: "M1"},
		{name: "file-exploration", owner: "M1"},
		{name: "full-pipeline", owner: "M1"},
		{name: "git-history", owner: "M1"},
		{name: "iteration-limits", owner: "M1"},
		{name: "multi-model", owner: "M1"},
		{name: "no-tools", owner: "M1"},
		{name: "output-chain", owner: "M1"},
		{name: "parallel-fan-in", owner: "M1"},
		{name: "prompt-from-file", owner: "M1"},
		{name: "retry-transient", owner: "M1"},
		{name: "shell-exec", owner: "M1"},
		{name: "skills-injection", owner: "M1"},
		{name: "system-prompt", owner: "M1"},
		{name: "template-prompt-file", owner: "M2"},
		{name: "template-variables", owner: "M2"},
		{name: "web-fetch", owner: "M1"},
	}

	names, err := dutoTestScenarioNames(dutoTestDir)
	if err != nil {
		t.Fatalf("missing M2 scenario identity behavior: list scenario files: %v", err)
	}

	expectedNames := make([]string, 0, len(expected))
	expectedByName := make(map[string]dutoTestScenarioOwnership, len(expected))
	owners := map[string]int{}

	for _, item := range expected {
		expectedNames = append(expectedNames, item.name)
		expectedByName[item.name] = item
		owners[item.owner]++
	}

	sort.Strings(expectedNames)

	if !reflect.DeepEqual(names, expectedNames) {
		t.Fatalf("missing M2 scenario identity behavior: working-tree names mismatch\nwant: %v\ngot:  %v", expectedNames, names)
	}

	digest := sha256.Sum256([]byte(strings.Join(names, "\n") + "\n"))
	if got := hex.EncodeToString(digest[:]); got != dutoTestScenarioSetDigest {
		t.Fatalf("missing M2 scenario identity behavior: scenario digest mismatch\nwant: %s\ngot:  %s", dutoTestScenarioSetDigest, got)
	}

	if len(expected) != 18 || owners["M1"] != 16 || owners["M2"] != 2 || len(owners) != 2 {
		t.Fatalf("missing M2 scenario identity behavior: ownership mapping mismatch\nowners: %#v", owners)
	}

	for _, name := range names {
		item := expectedByName[name]
		if strings.TrimSpace(item.owner) == "" {
			t.Fatalf("missing M2 scenario identity behavior: scenario %q has no owner", name)
		}
	}
}

func dutoTestScenarioNames(dutoTestDir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(dutoTestDir, ".github", "ai-workflows", "scenarios"))
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.Name(), ".yaml"))
	}

	sort.Strings(names)
	for i := 1; i < len(names); i++ {
		if names[i-1] == names[i] {
			return nil, fmt.Errorf("duplicate scenario file %q", names[i])
		}
	}

	return names, nil
}

func assertDutoTestHostedWorkflowContract(t *testing.T, dutoTestDir string) {
	t.Helper()

	workflowPath := filepath.Join(dutoTestDir, dutoTestHostedWorkflowRel)
	content, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("missing hosted M2 workflow behavior: read workflow file: %v", err)
	}

	var workflow dutoTestHostedWorkflow
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		t.Fatalf("missing hosted M2 workflow behavior: parse workflow file: %v", err)
	}

	if workflow.RunName != dutoTestHostedWorkflowName {
		t.Fatalf("missing hosted M2 workflow behavior: run-name = %q, want %q", workflow.RunName, dutoTestHostedWorkflowName)
	}

	inputKeys := make([]string, 0, len(workflow.On.WorkflowDispatch.Inputs))
	for key := range workflow.On.WorkflowDispatch.Inputs {
		inputKeys = append(inputKeys, key)
	}
	sort.Strings(inputKeys)
	if !reflect.DeepEqual(inputKeys, []string{"correlation_id", "duto_version"}) {
		t.Fatalf("missing hosted M2 workflow behavior: workflow_dispatch inputs mismatch\nwant: %v\ngot:  %v", []string{"correlation_id", "duto_version"}, inputKeys)
	}

	for _, key := range inputKeys {
		if !workflow.On.WorkflowDispatch.Inputs[key].Required {
			t.Fatalf("missing hosted M2 workflow behavior: input %q must be required", key)
		}
	}

	if len(workflow.Jobs) != 1 {
		t.Fatalf("missing hosted M2 workflow behavior: expected exactly one job, got %d", len(workflow.Jobs))
	}

	var job dutoTestHostedJobDef
	for _, current := range workflow.Jobs {
		job = current
		break
	}

	if len(job.Permissions) == 0 {
		t.Fatalf("missing hosted M2 workflow behavior: permissions must be declared")
	}
	if _, ok := job.Permissions["contents"]; !ok {
		t.Fatalf("missing hosted M2 workflow behavior: contents permission must be declared")
	}
	for name, access := range job.Permissions {
		if access != "read" {
			t.Fatalf("missing hosted M2 workflow behavior: permission %q must be read-only, got %q", name, access)
		}
	}

	rows := job.Strategy.Matrix.Include
	if len(rows) != 2 {
		t.Fatalf("missing hosted M2 workflow behavior: expected exactly two matrix rows, got %d", len(rows))
	}

	gotRows := map[string]string{}
	for _, row := range rows {
		if strings.TrimSpace(row.Scenario) == "" || strings.TrimSpace(row.Workflow) == "" {
			t.Fatalf("missing hosted M2 workflow behavior: matrix row must include non-empty scenario and workflow: %#v", row)
		}
		gotRows[row.Scenario] = row.Workflow
	}

	wantRows := map[string]string{
		"template-prompt-file": ".github/ai-workflows/scenarios/template-prompt-file.yaml",
		"template-variables":   ".github/ai-workflows/scenarios/template-variables.yaml",
	}
	if !reflect.DeepEqual(gotRows, wantRows) {
		t.Fatalf("missing hosted M2 workflow behavior: matrix rows mismatch\nwant: %#v\ngot:  %#v", wantRows, gotRows)
	}

	shaRefPattern := regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`)
	legacyInstallerPattern := regexp.MustCompile(`releases/latest|/usr/local/bin|curl[^\n|]*\|\s*tar`)

	checkoutRef := ""
	actionRef := ""

	for _, step := range job.Steps {
		if legacyInstallerPattern.MatchString(step.Run) {
			t.Fatalf("missing hosted M2 workflow behavior: legacy installer content is forbidden in run step: %q", step.Run)
		}

		switch {
		case strings.HasPrefix(step.Uses, "actions/checkout@"):
			checkoutRef = step.Uses
		case strings.HasPrefix(step.Uses, "PedroKlein/duto-ai@"):
			actionRef = step.Uses
		}
	}

	if checkoutRef == "" {
		t.Fatalf("missing hosted M2 workflow behavior: checkout step is required")
	}
	if !shaRefPattern.MatchString(checkoutRef) {
		t.Fatalf("missing hosted M2 workflow behavior: checkout reference must be a full SHA, got %q", checkoutRef)
	}

	if actionRef == "" {
		t.Fatalf("missing hosted M2 workflow behavior: duto action step is required")
	}
	if !shaRefPattern.MatchString(actionRef) {
		t.Fatalf("missing hosted M2 workflow behavior: duto action reference must be a full SHA, got %q", actionRef)
	}
}

func setupDutoTestPrepareInvocation(t *testing.T, dutoTestDir, workflowRel, configRel string) prepareInvocation {
	t.Helper()

	headSHA := gitHeadSHA(t, dutoTestDir)
	eventPath := filepath.Join(t.TempDir(), "event.json")

	eventTemplate, err := os.ReadFile(eventsFixturePath(t, "pull_request.json"))
	if err != nil {
		t.Fatalf("M2 scenario setup failure: read pull_request fixture: %v", err)
	}

	rewritten := strings.ReplaceAll(string(eventTemplate), "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", headSHA)
	if err := os.WriteFile(eventPath, []byte(rewritten), 0o600); err != nil {
		t.Fatalf("M2 scenario setup failure: write pull_request fixture: %v", err)
	}

	githubEnvPath := filepath.Join(t.TempDir(), "github-env.txt")
	githubOutPath := filepath.Join(t.TempDir(), "github-output.txt")
	runnerTempPath := filepath.Join(t.TempDir(), "runner-temp")

	if err := os.MkdirAll(runnerTempPath, 0o755); err != nil {
		t.Fatalf("M2 scenario setup failure: create runner temp path: %v", err)
	}
	if err := os.WriteFile(githubEnvPath, nil, 0o600); err != nil {
		t.Fatalf("M2 scenario setup failure: create GITHUB_ENV: %v", err)
	}
	if err := os.WriteFile(githubOutPath, nil, 0o600); err != nil {
		t.Fatalf("M2 scenario setup failure: create GITHUB_OUTPUT: %v", err)
	}

	return prepareInvocation{
		workspace:      dutoTestDir,
		headSHA:        headSHA,
		githubEnvPath:  githubEnvPath,
		githubOutPath:  githubOutPath,
		eventPath:      eventPath,
		workflowRel:    workflowRel,
		configRel:      configRel,
		runnerTempPath: runnerTempPath,
	}
}

func dutoTestPullRequestEnv() map[string]string {
	return map[string]string{
		"GITHUB_EVENT_NAME":       "pull_request",
		"GITHUB_REPOSITORY":       defaultRepositoryOwner + "/" + defaultRepositoryName,
		"GITHUB_REPOSITORY_OWNER": defaultRepositoryOwner,
		"GITHUB_REPOSITORY_ID":    defaultRepositoryID,
		"GITHUB_ACTOR":            "triggering-user",
		"GITHUB_ACTOR_ID":         "9007199254740999",
		"GITHUB_SHA":              defaultMergeSHA,
		"GITHUB_REF":              defaultPullRequestRef,
		"GITHUB_WORKFLOW_SHA":     defaultWorkflowSHA,
		"GITHUB_RUN_ID":           defaultRunID,
	}
}

func runDutoTestRuntimeBoundary(t *testing.T, inv prepareInvocation, exports map[string]string) (commandResult, string) {
	t.Helper()

	fakeDir := t.TempDir()
	fakeBinary := filepath.Join(fakeDir, "duto-ai-fake")
	tracePath := filepath.Join(fakeDir, "fake-runtime-trace.txt")
	evidenceDir := filepath.Join(fakeDir, "runtime-evidence")

	fakeScript := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" > \"${DUTO_FAKE_TRACE_FILE:?}\"\nprintf '{\\\"status\\\":\\\"succeeded\\\",\\\"outcome\\\":\\\"completed\\\",\\\"run_id\\\":\\\"run-m2-scenario\\\"}\\n'\n"
	if err := os.WriteFile(fakeBinary, []byte(fakeScript), 0o755); err != nil {
		t.Fatalf("M2 scenario setup failure: write fake runtime binary: %v", err)
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
		"DUTO_ACTION_EVIDENCE_DIR":      evidenceDir,
		"DUTO_FAKE_TRACE_FILE":          tracePath,
	}
	maps.Copy(merged, exports)

	cmd.Env = securityEnvironment(merged)

	result := runCommand(t, cmd, nil)

	trace := ""
	traceContent, err := os.ReadFile(tracePath)
	if err == nil {
		trace = strings.TrimSpace(string(traceContent))
	}

	return result, trace
}

func gitHeadSHA(t *testing.T, directory string) string {
	t.Helper()

	cmd := exec.Command("git", "-C", directory, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("M2 scenario setup failure: git rev-parse HEAD in %s: %v", directory, err)
	}

	return strings.TrimSpace(string(output))
}

func assertRegularFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}

	return nil
}

func forbiddenMarkerIssues(location, content string) []string {
	issues := make([]string, 0)

	for _, marker := range hostTemplateMarkers() {
		if strings.Contains(content, marker) {
			issues = append(issues, fmt.Sprintf("%s still contains forbidden host marker %q", location, marker))
		}
	}

	return issues
}

func rawHostContentIssues(location, content string) []string {
	issues := make([]string, 0)
	for _, marker := range append(hostileCanaryMarkers(), hostTemplateMarkers()...) {
		if strings.Contains(content, marker) {
			issues = append(issues, fmt.Sprintf("%s leaked raw host content marker %q", location, marker))
		}
	}

	return issues
}

func hostTemplateMarkers() []string {
	return []string{
		strings.Join([]string{"{{ ", ".", "Env"}, ""),
		strings.Join([]string{"{{ ", ".", "Event"}, ""),
		strings.Join([]string{".", "Env", "."}, ""),
		strings.Join([]string{".", "Event", "."}, ""),
	}
}

func hostileCanaryMarkers() []string {
	return []string{
		strings.Join([]string{"HOSTILE", "_", "CANARY", "_", "PR", "_", "TITLE"}, ""),
		strings.Join([]string{"HOSTILE", "_", "CANARY", "_", "PR", "_", "BODY"}, ""),
	}
}
