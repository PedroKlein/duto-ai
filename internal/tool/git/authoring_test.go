package git_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/tool/files"
	gittool "github.com/PedroKlein/duto-ai/internal/tool/git"
)

func TestM3Authoring_CommitStagesOnlyWrittenPathsAndPreservesRepositoryAuthority(t *testing.T) {
	repo, base := newAuthoringRepository(t)
	beforeRemote := gitAuthoringCommand(t, repo, "remote", "-v")
	beforeConfig := gitAuthoringCommand(t, repo, "config", "--local", "--null", "--list")
	beforeTags := gitAuthoringCommand(t, repo, "show-ref", "--tags")

	authoring, writer := newBoundAuthoring(t, repo, base, t.TempDir())
	defer writer.Close()

	if _, err := writer.Write(t.Context(), files.WriteArgs{Path: "docs/report.md", Content: "report\n"}); err != nil {
		t.Fatal(err)
	}

	if _, err := writer.Write(t.Context(), files.WriteArgs{Path: "new.txt", Content: "new\n"}); err != nil {
		t.Fatal(err)
	}

	result, err := authoring.Commit(t.Context(), gittool.CommitArgs{Paths: []string{"new.txt", "docs/report.md"}, Message: "Add report"})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	if result.Status != "applied" || result.Commit == base || result.Tree == "" || !reflect.DeepEqual(result.Paths, []string{"docs/report.md", "new.txt"}) {
		t.Fatalf("Commit() result = %#v", result)
	}

	if parent := strings.TrimSpace(gitAuthoringCommand(t, repo, "rev-parse", "HEAD^")); parent != base {
		t.Fatalf("commit parent = %s, want %s", parent, base)
	}

	if changed := strings.Fields(gitAuthoringCommand(t, repo, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD")); !reflect.DeepEqual(changed, []string{"docs/report.md", "new.txt"}) {
		t.Fatalf("changed paths = %v", changed)
	}

	if status := gitAuthoringCommand(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("repository is dirty: %q", status)
	}

	if got := gitAuthoringCommand(t, repo, "remote", "-v"); got != beforeRemote {
		t.Fatal("remotes changed")
	}

	if got := gitAuthoringCommand(t, repo, "config", "--local", "--null", "--list"); got != beforeConfig {
		t.Fatal("Git config changed")
	}

	if got := gitAuthoringCommand(t, repo, "show-ref", "--tags"); got != beforeTags {
		t.Fatal("tags changed")
	}

	if verifyErr := authoring.Verify(t.Context()); verifyErr != nil {
		t.Fatalf("Verify() error = %v", verifyErr)
	}

	gitAuthoringCommand(t, repo, "reset", "--hard", base)

	second, secondWriter := newBoundAuthoring(t, repo, base, t.TempDir())
	defer secondWriter.Close()

	if _, writeErr := secondWriter.Write(t.Context(), files.WriteArgs{Path: "docs/report.md", Content: "report\n"}); writeErr != nil {
		t.Fatal(writeErr)
	}

	if _, writeErr := secondWriter.Write(t.Context(), files.WriteArgs{Path: "new.txt", Content: "new\n"}); writeErr != nil {
		t.Fatal(writeErr)
	}

	secondResult, err := second.Commit(t.Context(), gittool.CommitArgs{Paths: []string{"docs/report.md", "new.txt"}, Message: "Add report"})
	if err != nil {
		t.Fatalf("second Commit() error = %v", err)
	}

	if secondResult.Commit != result.Commit {
		t.Fatalf("deterministic commit = %s, want %s", secondResult.Commit, result.Commit)
	}
}

func TestM3Authoring_CommitRejectsUnwrittenAndUnrelatedPaths(t *testing.T) {
	repo, base := newAuthoringRepository(t)

	authoring, writer := newBoundAuthoring(t, repo, base, t.TempDir())
	defer writer.Close()

	if _, err := writer.Write(t.Context(), files.WriteArgs{Path: "docs/report.md", Content: "report\n"}); err != nil {
		t.Fatal(err)
	}

	if _, err := authoring.Commit(t.Context(), gittool.CommitArgs{Paths: []string{"new.txt"}, Message: "Bad commit"}); err == nil {
		t.Fatal("Commit() accepted an unwritten path")
	}

	if err := os.WriteFile(filepath.Join(repo, "unrelated.txt"), []byte("unrelated\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := authoring.Commit(t.Context(), gittool.CommitArgs{Paths: []string{"docs/report.md"}, Message: "Bad commit"}); err == nil {
		t.Fatal("Commit() accepted an unrelated worktree change")
	}

	if head := strings.TrimSpace(gitAuthoringCommand(t, repo, "rev-parse", "HEAD")); head != base {
		t.Fatalf("HEAD = %s, want base %s", head, base)
	}
}

func TestM3Authoring_RecoveryArtifactsPrecedeCleanup(t *testing.T) {
	repo, base := newAuthoringRepository(t)
	evidence := t.TempDir()

	authoring, writer := newBoundAuthoring(t, repo, base, evidence)
	defer writer.Close()

	if _, err := writer.Write(t.Context(), files.WriteArgs{Path: "docs/report.md", Content: "after\n"}); err != nil {
		t.Fatal(err)
	}

	if _, err := writer.Write(t.Context(), files.WriteArgs{Path: "new.txt", Content: "new\n"}); err != nil {
		t.Fatal(err)
	}

	if err := authoring.Recover(t.Context(), "execution"); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}

	metadataPath := filepath.Join(evidence, "recovery", "metadata.json")
	patchPath := filepath.Join(evidence, "recovery", "changes.patch")

	metadata, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}

	patch, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatal(err)
	}

	var decoded struct {
		Ordering []string `json:"ordering"`
		Changes  []struct {
			Path string
		} `json:"changes"`
	}
	if decodeErr := json.Unmarshal(metadata, &decoded); decodeErr != nil {
		t.Fatal(decodeErr)
	}

	if !reflect.DeepEqual(decoded.Ordering, []string{"metadata_closed", "patch_closed", "cleanup_started"}) {
		t.Fatalf("ordering = %v", decoded.Ordering)
	}

	if !strings.Contains(string(patch), "docs/report.md") || !strings.Contains(string(patch), "new.txt") {
		t.Fatalf("recovery patch omitted authored paths:\n%s", patch)
	}

	if got := gitAuthoringCommand(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != "" {
		t.Fatalf("repository not cleaned after recovery: %q", got)
	}

	if head := strings.TrimSpace(gitAuthoringCommand(t, repo, "rev-parse", "HEAD")); head != base {
		t.Fatalf("HEAD = %s, want %s", head, base)
	}

	content, err := os.ReadFile(filepath.Join(repo, "docs", "report.md"))
	if err != nil || string(content) != "base\n" {
		t.Fatalf("base content = %q, %v", content, err)
	}

	if _, err := os.Stat(filepath.Join(repo, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new file remains after recovery: %v", err)
	}
}

func TestM3Authoring_RecoveryAfterCommitRestoresBase(t *testing.T) {
	repo, base := newAuthoringRepository(t)
	evidence := t.TempDir()

	authoring, writer := newBoundAuthoring(t, repo, base, evidence)
	defer writer.Close()

	if _, err := writer.Write(t.Context(), files.WriteArgs{Path: "docs/report.md", Content: "after\n"}); err != nil {
		t.Fatal(err)
	}

	if _, err := authoring.Commit(t.Context(), gittool.CommitArgs{Paths: []string{"docs/report.md"}, Message: "Update report"}); err != nil {
		t.Fatal(err)
	}

	if err := authoring.Recover(t.Context(), "execution"); err != nil {
		t.Fatal(err)
	}

	if head := strings.TrimSpace(gitAuthoringCommand(t, repo, "rev-parse", "HEAD")); head != base {
		t.Fatalf("HEAD = %s, want %s", head, base)
	}

	patch, err := os.ReadFile(filepath.Join(evidence, "recovery", "changes.patch"))
	if err != nil || !strings.Contains(string(patch), "docs/report.md") {
		t.Fatalf("recovery patch = %q, %v", patch, err)
	}
}

func newBoundAuthoring(t *testing.T, repo, base, evidence string) (*gittool.Authoring, *files.Authoring) {
	t.Helper()

	policy := gittool.AuthoringPolicy{
		Root: repo, AllowedPaths: []string{"docs/", "new.txt"}, MaxChangedFiles: 4,
		MaxCommitMessageBytes: 1024, MaxRecoveryBytes: 1 << 20,
		AuthorName: "Duto Automation", AuthorEmail: "duto@example.invalid",
		BaseRef: "refs/heads/main", BaseSHA: base, EvidenceDirectory: evidence,
	}

	authoring, err := gittool.NewAuthoring(t.Context(), policy)
	if err != nil {
		t.Fatalf("NewAuthoring() error = %v", err)
	}

	writer, err := files.NewAuthoring(repo, policy.AllowedPaths, policy.MaxChangedFiles, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}

	if err := authoring.BindWriter(writer); err != nil {
		t.Fatal(err)
	}

	return authoring, writer
}

func newAuthoringRepository(t *testing.T) (string, string) {
	t.Helper()
	repo := t.TempDir()
	gitAuthoringCommand(t, repo, "init", "-b", "main")
	gitAuthoringCommand(t, repo, "config", "user.name", "Example Author")
	gitAuthoringCommand(t, repo, "config", "user.email", "author@example.invalid")

	if err := os.Mkdir(filepath.Join(repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repo, "docs", "report.md"), []byte("base\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	gitAuthoringCommand(t, repo, "add", "--", "docs/report.md")
	gitAuthoringCommand(t, repo, "commit", "-m", "initial")
	gitAuthoringCommand(t, repo, "tag", "baseline")

	return repo, strings.TrimSpace(gitAuthoringCommand(t, repo, "rev-parse", "HEAD"))
}

func gitAuthoringCommand(t *testing.T, repo string, args ...string) string {
	t.Helper()

	command := exec.Command("git", append([]string{"-C", repo}, args...)...)

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}

	return string(output)
}
