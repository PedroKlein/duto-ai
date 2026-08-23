package git_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
	"github.com/PedroKlein/duto-ai/internal/tool/git"
)

func TestRegisterAll(t *testing.T) {
	root := t.TempDir()
	reg := dtool.NewRegistry()

	if err := git.RegisterAll(reg, testPolicy(root)); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	want := []string{"git.read.blame", "git.read.diff", "git.read.log", "git.read.show"}
	got := reg.Names()

	if len(got) != len(want) {
		t.Fatalf("registered %d tools, want %d: %v", len(got), len(want), got)
	}

	for i, name := range want {
		if got[i] != name {
			t.Errorf("tool[%d] = %q, want %q", i, got[i], name)
		}
	}
}

func TestGitLog(t *testing.T) {
	root := initRepo(t)

	result, err := git.GitLog(context.Background(), testPolicy(root), git.LogArgs{Count: 5})
	if err != nil {
		t.Fatalf("GitLog: %v", err)
	}

	if !strings.Contains(result.Output, "initial commit") {
		t.Errorf("log should contain 'initial commit', got:\n%s", result.Output)
	}
}

func TestGitLog_UsesFixedFormatCountAndResultLimit(t *testing.T) {
	root := initRepo(t)
	writeAndCommit(t, root, "second.txt", "second", "second commit")

	policy := testPolicy(root)
	policy.MaxLogCount = 1
	policy.Limits["git.read.log"] = dtool.ToolLimit{MaxCalls: 10, Timeout: time.Second, MaxRequestBytes: 1024, MaxResultBytes: 80}

	result, err := git.GitLog(context.Background(), policy, git.LogArgs{Count: 100})
	if err != nil {
		t.Fatalf("GitLog() error = %v", err)
	}

	if !result.Truncated || strings.Count(strings.TrimSpace(result.Output), "\n") > 0 {
		t.Fatalf("GitLog() = %#v", result)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	if len(encoded) > 80 {
		t.Fatalf("encoded result bytes = %d", len(encoded))
	}
}

func TestGitLog_RejectsPathTraversal(t *testing.T) {
	root := initRepo(t)

	_, err := git.GitLog(context.Background(), testPolicy(root), git.LogArgs{Path: "../outside"})
	if !errors.Is(err, git.ErrPathTraversal) {
		t.Fatalf("GitLog() error = %v", err)
	}
}

func TestGitLog_PathFilter(t *testing.T) {
	root := initRepo(t)
	writeAndCommit(t, root, "other.txt", "other content", "add other")

	result, err := git.GitLog(context.Background(), testPolicy(root), git.LogArgs{Count: 10, Path: "hello.txt"})
	if err != nil {
		t.Fatalf("GitLog: %v", err)
	}

	if strings.Contains(result.Output, "add other") {
		t.Error("log with path filter should not contain commits for other files")
	}

	if !strings.Contains(result.Output, "initial commit") {
		t.Error("log with path filter should contain the initial commit")
	}
}

func TestGitBlame(t *testing.T) {
	root := initRepo(t)

	result, err := git.GitBlame(context.Background(), testPolicy(root), git.BlameArgs{Path: "hello.txt"})
	if err != nil {
		t.Fatalf("GitBlame: %v", err)
	}

	if !strings.Contains(result.Output, "hello world") {
		t.Errorf("blame should contain file content, got:\n%s", result.Output)
	}
}

func TestGitBlame_LineRange(t *testing.T) {
	root := initRepo(t)
	writeAndCommit(t, root, "multi.txt", "line1\nline2\nline3\nline4\n", "multi-line")

	result, err := git.GitBlame(context.Background(), testPolicy(root), git.BlameArgs{Path: "multi.txt", StartLine: 2, EndLine: 3})
	if err != nil {
		t.Fatalf("GitBlame: %v", err)
	}

	if !strings.Contains(result.Output, "line2") {
		t.Error("blame should contain line2")
	}

	if strings.Contains(result.Output, "line1") {
		t.Error("blame with line range should not contain line1")
	}
}

func TestGitBlame_RejectsSymlinkEscape(t *testing.T) {
	root := initRepo(t)
	outside := t.TempDir()

	outsidePath := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsidePath, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(outsidePath, filepath.Join(root, "escape.txt")); err != nil {
		t.Fatal(err)
	}

	if _, err := git.GitBlame(context.Background(), testPolicy(root), git.BlameArgs{Path: "escape.txt"}); err == nil {
		t.Fatal("GitBlame() error = nil for escaping symlink")
	}
}

func TestGitBlame_MissingPath(t *testing.T) {
	root := initRepo(t)

	_, err := git.GitBlame(context.Background(), testPolicy(root), git.BlameArgs{})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestGitShow(t *testing.T) {
	root := initRepo(t)

	output, err := git.GitShow(context.Background(), testPolicy(root), git.ShowArgs{Ref: "HEAD"})
	if err != nil {
		t.Fatalf("GitShow: %v", err)
	}

	if !strings.Contains(output.Output, "initial commit") {
		t.Errorf("show should contain commit message, got:\n%s", output.Output)
	}
}

func TestGitShow_RejectsUntrustedRef(t *testing.T) {
	root := initRepo(t)

	_, err := git.GitShow(context.Background(), testPolicy(root), git.ShowArgs{Ref: "refs/heads/other"})
	if !errors.Is(err, git.ErrRefNotAllowed) {
		t.Fatalf("GitShow() error = %v", err)
	}
}

func TestGitShow_MissingRef(t *testing.T) {
	root := initRepo(t)

	_, err := git.GitShow(context.Background(), testPolicy(root), git.ShowArgs{})
	if err == nil {
		t.Fatal("expected error for empty ref")
	}
}

func TestGitDiff(t *testing.T) {
	root := initRepo(t)

	// Modify a file without committing.
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := git.GitDiff(context.Background(), testPolicy(root), git.DiffArgs{})
	if err != nil {
		t.Fatalf("GitDiff: %v", err)
	}

	if !strings.Contains(output.Output, "modified") {
		t.Errorf("diff should show modified content, got:\n%s", output.Output)
	}
}

func TestGitDiff_RequiresTrustedWorkingTreeAndRef(t *testing.T) {
	root := initRepo(t)
	policy := testPolicy(root)
	policy.AllowWorkingTree = false

	if _, err := git.GitDiff(context.Background(), policy, git.DiffArgs{}); !errors.Is(err, git.ErrWorkingTreeNotAllowed) {
		t.Fatalf("GitDiff() working tree error = %v", err)
	}

	policy.AllowWorkingTree = true
	if _, err := git.GitDiff(context.Background(), policy, git.DiffArgs{Ref: "other"}); !errors.Is(err, git.ErrRefNotAllowed) {
		t.Fatalf("GitDiff() ref error = %v", err)
	}
}

func TestGitDiff_WithRef(t *testing.T) {
	root := initRepo(t)
	writeAndCommit(t, root, "hello.txt", "changed", "second commit")

	output, err := git.GitDiff(context.Background(), testPolicy(root), git.DiffArgs{Ref: "HEAD~1"})
	if err != nil {
		t.Fatalf("GitDiff: %v", err)
	}

	if !strings.Contains(output.Output, "changed") {
		t.Errorf("diff against HEAD~1 should show change, got:\n%s", output.Output)
	}
}

// --- helpers ---

func testPolicy(root string) git.Policy {
	return git.Policy{
		Root:             root,
		Refs:             []string{"HEAD", "HEAD~1"},
		AllowWorkingTree: true,
		MaxLogCount:      20,
		Limits: map[string]dtool.ToolLimit{
			"git.read.blame": {MaxCalls: 10, Timeout: time.Second, MaxRequestBytes: 1024, MaxResultBytes: 1 << 20},
			"git.read.diff":  {MaxCalls: 10, Timeout: time.Second, MaxRequestBytes: 1024, MaxResultBytes: 1 << 20},
			"git.read.log":   {MaxCalls: 10, Timeout: time.Second, MaxRequestBytes: 1024, MaxResultBytes: 1 << 20},
			"git.read.show":  {MaxCalls: 10, Timeout: time.Second, MaxRequestBytes: 1024, MaxResultBytes: 1 << 20},
		},
	}
}

func initRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@test.com")
	runGit(t, root, "config", "user.name", "Test")

	writeAndCommit(t, root, "hello.txt", "hello world", "initial commit")

	return root
}

func writeAndCommit(t *testing.T, root, file, content, msg string) {
	t.Helper()

	path := filepath.Join(root, file)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	runGit(t, root, "add", file)
	runGit(t, root, "commit", "-m", msg)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, string(out), err)
	}
}
