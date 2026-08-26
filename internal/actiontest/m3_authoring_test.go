package actiontest_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestM3Authoring_WritableWorkspaceIsExact(t *testing.T) {
	repo, head := newM3AuthoringRepo(t)
	configPath, workflowPath, evidencePath := writeM3AuthoringInputs(t, repo, head, "")
	binary := dutoAIBinary(t)

	stdout, stderr, code := runDutoAI(t, binary, []string{
		"plan", "--config", configPath, "--control-evidence", evidencePath, "--format", "json", workflowPath,
	}, nil)
	if code != 0 {
		t.Fatalf("missing M3 authoring behavior: exact local writable workspace was rejected: %s", stderr)
	}

	if count := strings.Count(stdout, `"access":"write"`); count != 1 {
		t.Fatalf("missing M3 authoring behavior: effective plan write-workspace count = %d, want 1\n%s", count, stdout)
	}
}

func TestM3Authoring_SecondWritableWorkspaceIsRejected(t *testing.T) {
	repo, head := newM3AuthoringRepo(t)
	configYAML := m3AuthoringConfig(t, repo)
	configYAML = strings.Replace(configYAML,
		"  source:\n    root: "+repo+"\n    access: write",
		"  source:\n    root: "+repo+"\n    access: write\n  other:\n    root: "+t.TempDir()+"\n    access: write",
		1,
	)
	configPath := writeConfigFile(t, configYAML)
	workflowPath := writeWorkflowFile(t, m3TrustMutationWorkflow)
	evidencePath := writeEvidenceFile(t, m3AuthoringLocalEvidence(head))

	_, stderr, code := runDutoAI(t, dutoAIBinary(t), []string{
		"plan", "--config", configPath, "--control-evidence", evidencePath, workflowPath,
	}, nil)
	if code == 0 || (!strings.Contains(stderr, "workspace") && !strings.Contains(stderr, "invalid_value")) {
		t.Fatalf("missing M3 authoring behavior: second writable workspace must be rejected before construction\ncode=%d stderr=%s", code, stderr)
	}
}

func TestM3Authoring_DeniedContextHasZeroSideEffects(t *testing.T) {
	repo, head := newM3AuthoringRepo(t)
	configPath := writeConfigFile(t, m3AuthoringConfig(t, repo))
	workflowPath := writeWorkflowFile(t, m3TrustMutationWorkflow)
	evidencePath := writeEvidenceFile(t, m3GithubEvidence("pull_request", "9999", "refs/pull/7/merge", head))
	before := snapshotM3AuthoringRepo(t, repo)

	_, stderr, code := runDutoAI(t, dutoAIBinary(t), []string{
		"run", "--config", configPath, "--control-evidence", evidencePath, workflowPath,
	}, nil)
	if code == 0 || (!strings.Contains(stderr, "forked_pr") && !strings.Contains(stderr, "denied")) {
		t.Fatalf("missing M3 authoring behavior: forked PR mutation must be denied before construction\ncode=%d stderr=%s", code, stderr)
	}

	assertM3AuthoringRepoUnchanged(t, repo, before)
}

func TestM3Authoring_RepositoryAdmissionRejectsUnsafeState(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, string)
		config  func(*testing.T, string) string
		want    []string
	}{
		{
			name: "dirty worktree",
			prepare: func(t *testing.T, repo string) {
				t.Helper()
				writeM3AuthoringFile(t, repo, "docs/base.md", "dirty\n")
			},
			want: []string{"dirty", "clean", "worktree"},
		},
		{
			name: "dirty index",
			prepare: func(t *testing.T, repo string) {
				t.Helper()
				writeM3AuthoringFile(t, repo, "docs/base.md", "staged\n")
				runM3AuthoringGit(t, repo, "add", "--", "docs/base.md")
			},
			want: []string{"index", "staged", "clean"},
		},
		{
			name: "ignored selected path",
			prepare: func(t *testing.T, repo string) {
				t.Helper()
				writeM3AuthoringFile(t, repo, "ignored.log", "ignored\n")
			},
			config: func(t *testing.T, repo string) string {
				t.Helper()
				return strings.Replace(m3AuthoringConfig(t, repo), "allowed_paths: [cmd/, internal/, docs/, go.mod, go.sum]", "allowed_paths: [ignored.log]", 1)
			},
			want: []string{"ignored"},
		},
		{
			name: "submodule selected path",
			prepare: func(t *testing.T, repo string) {
				t.Helper()
				child, _ := newM3AuthoringRepo(t)
				runM3AuthoringGit(t, repo, "-c", "protocol.file.allow=always", "submodule", "add", child, "vendor/module")
				runM3AuthoringGit(t, repo, "commit", "-m", "add submodule")
			},
			config: func(t *testing.T, repo string) string {
				t.Helper()
				return strings.Replace(m3AuthoringConfig(t, repo), "allowed_paths: [cmd/, internal/, docs/, go.mod, go.sum]", "allowed_paths: [vendor/module/]", 1)
			},
			want: []string{"submodule"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo, head := newM3AuthoringRepo(t)
			test.prepare(t, repo)

			if test.name == "submodule selected path" {
				head = strings.TrimSpace(runM3AuthoringGit(t, repo, "rev-parse", "HEAD"))
			}

			configYAML := m3AuthoringConfig(t, repo)
			if test.config != nil {
				configYAML = test.config(t, repo)
			}

			configPath := writeConfigFile(t, configYAML)
			workflowPath := writeWorkflowFile(t, m3TrustMutationWorkflow)
			evidencePath := writeEvidenceFile(t, m3AuthoringLocalEvidence(head))
			before := snapshotM3AuthoringRepo(t, repo)

			_, stderr, code := runDutoAI(t, dutoAIBinary(t), []string{
				"run", "--config", configPath, "--control-evidence", evidencePath, workflowPath,
			}, nil)
			if code == 0 || !containsM3AuthoringTerm(stderr, test.want) {
				t.Fatalf("missing M3 authoring behavior: %s must be rejected at repository admission\ncode=%d stderr=%s", test.name, code, stderr)
			}

			assertM3AuthoringRepoUnchanged(t, repo, before)
		})
	}
}

func TestM3Authoring_CheckoutMismatchHasZeroSideEffects(t *testing.T) {
	repo, _ := newM3AuthoringRepo(t)
	configPath := writeConfigFile(t, m3AuthoringConfig(t, repo))
	workflowPath := writeWorkflowFile(t, m3TrustMutationWorkflow)
	evidencePath := writeEvidenceFile(t, m3AuthoringLocalEvidence("1111111111111111111111111111111111111111"))
	before := snapshotM3AuthoringRepo(t, repo)

	_, stderr, code := runDutoAI(t, dutoAIBinary(t), []string{
		"run", "--config", configPath, "--control-evidence", evidencePath, workflowPath,
	}, nil)
	if code == 0 || !containsM3AuthoringTerm(stderr, []string{"checkout", "base", "HEAD", "revision"}) {
		t.Fatalf("missing M3 authoring behavior: checkout mismatch must be rejected before construction\ncode=%d stderr=%s", code, stderr)
	}

	assertM3AuthoringRepoUnchanged(t, repo, before)
}

func TestM3Authoring_PlanExposesNoRemoteAuthority(t *testing.T) {
	repo, head := newM3AuthoringRepo(t)
	configPath, workflowPath, evidencePath := writeM3AuthoringInputs(t, repo, head, "")
	canary := "write-token-canary"

	stdout, stderr, code := runDutoAI(t, dutoAIBinary(t), []string{
		"plan", "--config", configPath, "--control-evidence", evidencePath, "--format", "json", workflowPath,
	}, map[string]string{"GITHUB_TOKEN": canary})
	if code != 0 {
		t.Fatalf("missing M3 authoring behavior: admitted plan failed: %s", stderr)
	}

	for _, forbidden := range []string{repo, canary, strings.Join([]string{"github", "pat", ""}, "_"), strings.Join([]string{"ghp", ""}, "_"), `"remote"`, `"endpoint"`} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("missing M3 authoring behavior: plan leaked remote authority %q\n%s", forbidden, stdout)
		}
	}

	for _, required := range []string{"files.write", "git.write.commit", "workspace.mutate", "git.mutate", "policy_sha256"} {
		if !strings.Contains(stdout, required) {
			t.Fatalf("missing M3 authoring behavior: plan omitted %q\n%s", required, stdout)
		}
	}
}

type m3AuthoringRepoSnapshot struct {
	head    string
	status  string
	refs    string
	remotes string
	config  string
}

func snapshotM3AuthoringRepo(t *testing.T, repo string) m3AuthoringRepoSnapshot {
	t.Helper()

	return m3AuthoringRepoSnapshot{
		head:    runM3AuthoringGit(t, repo, "rev-parse", "HEAD"),
		status:  runM3AuthoringGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all"),
		refs:    runM3AuthoringGit(t, repo, "for-each-ref", "--format=%(refname)%00%(objectname)"),
		remotes: runM3AuthoringGit(t, repo, "remote", "-v"),
		config:  runM3AuthoringGit(t, repo, "config", "--local", "--null", "--list"),
	}
}

func assertM3AuthoringRepoUnchanged(t *testing.T, repo string, before m3AuthoringRepoSnapshot) {
	t.Helper()

	after := snapshotM3AuthoringRepo(t, repo)
	if after != before {
		t.Fatalf("repository changed across denied authoring\nbefore: %#v\nafter: %#v", before, after)
	}
}

func writeM3AuthoringInputs(t *testing.T, repo, head, configYAML string) (string, string, string) {
	t.Helper()

	if configYAML == "" {
		configYAML = m3AuthoringConfig(t, repo)
	}

	return writeConfigFile(t, configYAML), writeWorkflowFile(t, m3TrustMutationWorkflow), writeEvidenceFile(t, m3AuthoringLocalEvidence(head))
}

func m3AuthoringConfig(t *testing.T, repo string) string {
	t.Helper()

	configYAML := strings.Replace(m3TrustMinimalConfig, "/tmp/m3-trust-test-workspace", repo, 1)

	return strings.Replace(configYAML, "/tmp/m3-trust-test-evidence", filepath.Join(t.TempDir(), "evidence"), 1)
}

func m3AuthoringLocalEvidence(head string) map[string]any {
	document := m3LocalEvidence()
	document["checkout"].(map[string]any)["sha"] = head

	return document
}

func newM3AuthoringRepo(t *testing.T) (string, string) {
	t.Helper()
	repo := t.TempDir()
	runM3AuthoringGit(t, repo, "init", "-b", "main")
	runM3AuthoringGit(t, repo, "config", "user.name", "Example Author")
	runM3AuthoringGit(t, repo, "config", "user.email", "author@example.invalid")
	writeM3AuthoringFile(t, repo, ".gitignore", "ignored.log\n")
	writeM3AuthoringFile(t, repo, "docs/base.md", "base\n")
	runM3AuthoringGit(t, repo, "add", "--", ".gitignore", "docs/base.md")
	runM3AuthoringGit(t, repo, "commit", "-m", "initial")

	return repo, strings.TrimSpace(runM3AuthoringGit(t, repo, "rev-parse", "HEAD"))
}

func writeM3AuthoringFile(t *testing.T, repo, relative, contents string) {
	t.Helper()

	path := filepath.Join(repo, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent: %v", err)
	}

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func runM3AuthoringGit(t *testing.T, repo string, args ...string) string {
	t.Helper()

	command := exec.Command("git", append([]string{"-C", repo}, args...)...)

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}

	return string(output)
}

func containsM3AuthoringTerm(value string, terms []string) bool {
	value = strings.ToLower(value)
	for _, term := range terms {
		if strings.Contains(value, strings.ToLower(term)) {
			return true
		}
	}

	return false
}

func TestM3Authoring_FixtureJSONIsClosed(t *testing.T) {
	encoded, err := json.Marshal(m3AuthoringLocalEvidence("1111111111111111111111111111111111111111"))
	if err != nil || !json.Valid(encoded) {
		t.Fatalf("authoring evidence fixture is invalid: %v", err)
	}

	if strings.Contains(string(encoded), fmt.Sprintf("%c", os.PathSeparator)+"Users"+fmt.Sprintf("%c", os.PathSeparator)) {
		t.Fatal("authoring evidence fixture contains a workstation path")
	}
}
