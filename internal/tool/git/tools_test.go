package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
	"github.com/PedroKlein/duto-ai/internal/tool/git"
)

func TestRegisterAll(t *testing.T) {
	root := t.TempDir()
	reg := dtool.NewRegistry()

	if err := git.RegisterAll(reg, root); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	want := []string{"git.blame", "git.diff", "git.log", "git.show"}
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

	output, err := git.GitLog(root, git.LogArgs{Count: 5})
	if err != nil {
		t.Fatalf("GitLog: %v", err)
	}

	if !strings.Contains(output, "initial commit") {
		t.Errorf("log should contain 'initial commit', got:\n%s", output)
	}
}

func TestGitLog_PathFilter(t *testing.T) {
	root := initRepo(t)

	// Add a second file with a commit.
	writeAndCommit(t, root, "other.txt", "other content", "add other")

	output, err := git.GitLog(root, git.LogArgs{Count: 10, Path: "hello.txt"})
	if err != nil {
		t.Fatalf("GitLog: %v", err)
	}

	if strings.Contains(output, "add other") {
		t.Error("log with path filter should not contain commits for other files")
	}

	if !strings.Contains(output, "initial commit") {
		t.Error("log with path filter should contain the initial commit")
	}
}

func TestGitBlame(t *testing.T) {
	root := initRepo(t)

	output, err := git.GitBlame(root, git.BlameArgs{Path: "hello.txt"})
	if err != nil {
		t.Fatalf("GitBlame: %v", err)
	}

	if !strings.Contains(output, "hello world") {
		t.Errorf("blame should contain file content, got:\n%s", output)
	}
}

func TestGitBlame_LineRange(t *testing.T) {
	root := initRepo(t)
	// Add multi-line file.
	writeAndCommit(t, root, "multi.txt", "line1\nline2\nline3\nline4\n", "multi-line")

	output, err := git.GitBlame(root, git.BlameArgs{Path: "multi.txt", StartLine: 2, EndLine: 3})
	if err != nil {
		t.Fatalf("GitBlame: %v", err)
	}

	if !strings.Contains(output, "line2") {
		t.Error("blame should contain line2")
	}

	if strings.Contains(output, "line1") {
		t.Error("blame with line range should not contain line1")
	}
}

func TestGitBlame_MissingPath(t *testing.T) {
	root := initRepo(t)

	_, err := git.GitBlame(root, git.BlameArgs{})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestGitShow(t *testing.T) {
	root := initRepo(t)

	output, err := git.GitShow(root, git.ShowArgs{Ref: "HEAD"})
	if err != nil {
		t.Fatalf("GitShow: %v", err)
	}

	if !strings.Contains(output, "initial commit") {
		t.Errorf("show should contain commit message, got:\n%s", output)
	}
}

func TestGitShow_MissingRef(t *testing.T) {
	root := initRepo(t)

	_, err := git.GitShow(root, git.ShowArgs{})
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

	output, err := git.GitDiff(root, git.DiffArgs{})
	if err != nil {
		t.Fatalf("GitDiff: %v", err)
	}

	if !strings.Contains(output, "modified") {
		t.Errorf("diff should show modified content, got:\n%s", output)
	}
}

func TestGitDiff_WithRef(t *testing.T) {
	root := initRepo(t)
	writeAndCommit(t, root, "hello.txt", "changed", "second commit")

	output, err := git.GitDiff(root, git.DiffArgs{Ref: "HEAD~1"})
	if err != nil {
		t.Fatalf("GitDiff: %v", err)
	}

	if !strings.Contains(output, "changed") {
		t.Errorf("diff against HEAD~1 should show change, got:\n%s", output)
	}
}

// --- helpers ---

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
