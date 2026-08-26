package actiontest_test

import (
	"crypto/sha256"
	"fmt"
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
		"## Use focused authoring and staged publication (M3)",
		"duto-ai publish",
		"files.write",
		"git.write.commit",
		"safe-output.branch",
		"permission-profile",
		"Direct remote mode",
	} {
		if !strings.Contains(readme, fragment) {
			t.Fatalf("missing docs M3 reference behavior: README must document %q", fragment)
		}
	}

	for _, fragment := range []string{
		"Fork PR or unknown trust contexts stay transitively read-only",
		"keep the full typed result and runtime evidence runner-local",
		"is not a sandbox",
		"## M3 authoring and publisher trust contract",
		"before reading `GITHUB_TOKEN`",
	} {
		if !strings.Contains(security, fragment) {
			t.Fatalf("missing docs Action reference behavior: SECURITY.md must document %q", fragment)
		}
	}
}

func TestDocs_MilestoneStatus(t *testing.T) {
	t.Parallel()

	const (
		sealedADR009Digest = "f4f9383040599afe51550bd86ed82a91d8f8515d15656e2b1482a771fdf18901"
		sealedADR011Digest = "afa17f69bae20850c3fdfca8196c122997b63545b1e3d5866b2987133904b95e"
	)

	root := repoRoot(t)
	readme := readDocFile(t, "README.md")
	architecture := readDocFile(t, filepath.Join("docs", "ARCHITECTURE.md"))
	adr008 := readDocFile(t, filepath.Join("docs", "adr", "008-product-center-and-delivery-layers.md"))
	adr009Path := filepath.Join(root, "docs", "adr", "009-one-shot-github-action.md")

	adr009, err := os.ReadFile(adr009Path)
	if err != nil {
		t.Fatalf("docs setup failure: read ADR 009: %v", err)
	}

	if got := fmt.Sprintf("%x", sha256.Sum256(adr009)); got != sealedADR009Digest {
		t.Errorf("sealed M2 contract changed: ADR 009 SHA-256 = %s, want %s", got, sealedADR009Digest)
	}

	adr011, err := os.ReadFile(filepath.Join(root, "docs", "adr", "011-m3-focused-authoring-contract.md"))
	if err != nil {
		t.Fatalf("missing M3 contract: %v", err)
	}

	if got := fmt.Sprintf("%x", sha256.Sum256(adr011)); got != sealedADR011Digest {
		t.Errorf("sealed M3 contract changed: ADR 011 SHA-256 = %s, want %s", got, sealedADR011Digest)
	}

	completionPath := filepath.Join(root, "docs", "adr", "010-m2-delivery-completion.md")

	completionBytes, err := os.ReadFile(completionPath)
	if err != nil {
		t.Errorf("missing M2 completion record: read docs/adr/010-m2-delivery-completion.md: %v", err)
	}

	completion := string(completionBytes)

	checks := []struct {
		name     string
		content  string
		fragment string
	}{
		{name: "README completion link", content: readme, fragment: "[ADR 010](docs/adr/010-m2-delivery-completion.md)"},
		{name: "README M3 status", content: readme, fragment: "| M3, shipped |"},
		{name: "README M3 contract", content: readme, fragment: "[ADR 011](docs/adr/011-m3-focused-authoring-contract.md)"},
		{name: "architecture M3 contract", content: architecture, fragment: "[ADR 011](adr/011-m3-focused-authoring-contract.md)"},
		{name: "architecture M3 status", content: architecture, fragment: "Focused M3 is shipped"},
		{name: "ADR 008 status", content: adr008, fragment: "- **Status:** Accepted; M1 and M2 shipped"},
		{name: "ADR 008 completion link", content: adr008, fragment: "[ADR 010](010-m2-delivery-completion.md)"},
		{name: "completion status", content: completion, fragment: "- **Status:** Accepted; M2 shipped"},
		{name: "completion Action revision", content: completion, fragment: "`462e48601658765e96448a147fbcd029f034e329`"},
		{name: "completion binary release", content: completion, fragment: "`v0.3.1`"},
		{name: "completion sealed digest", content: completion, fragment: sealedADR009Digest},
		{name: "completion M3 entry", content: completion, fragment: "M3 is the next unimplemented milestone"},
	}
	for _, check := range checks {
		if !strings.Contains(check.content, check.fragment) {
			t.Errorf("stale or missing M2 status documentation: %s must contain %q", check.name, check.fragment)
		}
	}

	if strings.Contains(adr008, "M2, M3, optional persistence, and durable-host behavior remain unimplemented") {
		t.Errorf("stale M2 status documentation: ADR 008 still says M2 is unimplemented")
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
		filepath.Join("docs", "adr", "010-m2-delivery-completion.md"),
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
