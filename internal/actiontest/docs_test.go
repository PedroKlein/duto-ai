package actiontest_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestDocs_ActionReference(t *testing.T) {
	action := loadActionMetadata(t)
	readme := readDocFile(t, "README.md")
	security := readDocFile(t, "SECURITY.md")

	for _, fragment := range []string{
		"## Use the one-shot GitHub Action (M2)",
		"persist-credentials: false",
		"contents: read",
		"pull-requests: read",
		"issues: read",
		"checks: read",
		"evidence-retention-days",
		"result-path",
		"evidence-path",
		"full typed result and runtime evidence remain runner-local",
		"no writes, no SafeOutputs application, no durable state, no pause/resume, no cross-runner recovery, and no async replies",
	} {
		if !strings.Contains(readme, fragment) {
			t.Fatalf("missing docs Action reference behavior: README must document %q", fragment)
		}
	}

	for _, eventName := range []string{"workflow_dispatch", "schedule", "push", "pull_request", "issues", "issue_comment"} {
		if !strings.Contains(readme, "`"+eventName+"`") {
			t.Fatalf("missing docs Action reference behavior: README must list supported event %q", eventName)
		}
	}

	if !regexp.MustCompile(`(?m)^\s*uses:\s*actions/checkout@[0-9a-f]{40}\s*$`).MatchString(readme) {
		t.Fatalf("missing docs Action reference behavior: README workflow example must pin actions/checkout to a full SHA")
	}

	if !regexp.MustCompile(`(?m)^\s*uses:\s*PedroKlein/duto-ai@[0-9a-f]{40}\s*$`).MatchString(readme) {
		t.Fatalf("missing docs Action reference behavior: README workflow example must pin PedroKlein/duto-ai to a full SHA")
	}

	if !strings.Contains(readme, "uses: PedroKlein/duto-ai@462e48601658765e96448a147fbcd029f034e329") {
		t.Fatalf("missing docs Action reference behavior: README must pin the hosted-verified Action revision")
	}

	if !strings.Contains(readme, "version: v0.3.1") {
		t.Fatalf("missing docs Action reference behavior: README must use the hosted-verified binary release")
	}

	if !strings.Contains(readme, "| M2, shipped |") {
		t.Fatalf("missing docs Action reference behavior: README milestone table must mark M2 shipped")
	}

	if regexp.MustCompile(`(?m)^\s*uses:\s*[^@\s]+@v[0-9]+`).MatchString(readme) {
		t.Fatalf("missing docs Action reference behavior: README must not use moving major-tag Action references")
	}

	inputNames := sortedActionKeys(action.Inputs)
	for _, inputName := range inputNames {
		if !strings.Contains(readme, "`"+inputName+"`") {
			t.Fatalf("missing docs Action reference behavior: README must mention Action input %q", inputName)
		}
	}

	outputNames := sortedActionKeys(action.Outputs)
	for _, outputName := range outputNames {
		if !strings.Contains(readme, "`"+outputName+"`") {
			t.Fatalf("missing docs Action reference behavior: README must mention Action output %q", outputName)
		}
	}

	for _, fragment := range []string{
		"Fork PR or unknown trust contexts stay transitively read-only",
		"keep the full typed result and runtime evidence runner-local",
		"is not a sandbox",
	} {
		if !strings.Contains(security, fragment) {
			t.Fatalf("missing docs Action reference behavior: SECURITY.md must document %q", fragment)
		}
	}
}

func TestDocs_Examples(t *testing.T) {
	cliPath := buildCLI(t)
	root := repoRoot(t)

	configPath := filepath.Join(root, "testdata", "config.yaml")
	workflowPath := filepath.Join(root, "testdata", "workflow.yaml")

	validate := runCLI(t, cliPath, nil, "validate", "--format", "json", "--config", configPath, workflowPath)
	if validate.exitCode != 0 {
		t.Fatalf("missing docs examples behavior: README/DEVELOPMENT validation example must pass\nexit=%d\nstderr:\n%s", validate.exitCode, validate.stderr)
	}

	if !containsAll(validate.stdout, `"valid":true`) {
		t.Fatalf("missing docs examples behavior: validate example must produce JSON valid=true\nstdout:\n%s", validate.stdout)
	}

	plan := runCLI(t, cliPath, nil, "plan", "--format", "json", "--config", configPath, workflowPath)
	if plan.exitCode != 0 {
		t.Fatalf("missing docs examples behavior: README/DEVELOPMENT plan example must pass\nexit=%d\nstderr:\n%s", plan.exitCode, plan.stderr)
	}

	for _, fragment := range []string{`"workflow"`, `"steps"`, `"result"`} {
		if !strings.Contains(plan.stdout, fragment) {
			t.Fatalf("missing docs examples behavior: plan output must include %s\nstdout:\n%s", fragment, plan.stdout)
		}
	}
}

func TestDocs_Links(t *testing.T) {
	root := repoRoot(t)
	docFiles := []string{
		"README.md",
		"SECURITY.md",
		"CONTRIBUTING.md",
		filepath.Join("docs", "ARCHITECTURE.md"),
		filepath.Join("docs", "DEVELOPMENT.md"),
	}

	missing := make([]string, 0)

	for _, relPath := range docFiles {
		docPath := filepath.Join(root, relPath)

		contentBytes, err := os.ReadFile(docPath)
		if err != nil {
			t.Fatalf("docs link setup failure: read %s: %v", relPath, err)
		}

		content := string(contentBytes)
		for _, target := range markdownLinkTargets(content) {
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "#") {
				continue
			}

			target = strings.Split(target, "#")[0]
			target = strings.Split(target, "?")[0]
			target = strings.TrimSpace(target)

			if target == "" {
				continue
			}

			resolved := filepath.Clean(filepath.Join(filepath.Dir(docPath), target))
			if !strings.HasPrefix(resolved, root+string(os.PathSeparator)) && resolved != root {
				missing = append(missing, relPath+" -> "+target+" (outside repository)")
				continue
			}

			if _, err := os.Stat(resolved); err != nil {
				missing = append(missing, relPath+" -> "+target)
			}
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("missing docs link behavior: every relative Markdown link in README/security/development/contributing/architecture must resolve\n- %s", strings.Join(missing, "\n- "))
	}
}

func readDocFile(t *testing.T, relativePath string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(repoRoot(t), relativePath))
	if err != nil {
		t.Fatalf("docs setup failure: read %s: %v", relativePath, err)
	}

	return string(content)
}

func markdownLinkTargets(content string) []string {
	matches := regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`).FindAllStringSubmatch(content, -1)
	targets := make([]string, 0, len(matches))

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		targets = append(targets, strings.TrimSpace(match[1]))
	}

	return targets
}
