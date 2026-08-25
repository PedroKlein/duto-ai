package files_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/tool/files"
)

func TestM3Authoring_AtomicWriteAndRollback(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, "docs", "report.md")
	if err := os.WriteFile(path, []byte("before\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	authoring, err := files.NewAuthoring(root, []string{"docs/"}, 2, 1024, 2048)
	if err != nil {
		t.Fatalf("NewAuthoring() error = %v", err)
	}
	defer authoring.Close()

	result, err := authoring.Write(t.Context(), files.WriteArgs{Path: "docs/report.md", Content: "after\n"})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if result.Status != "applied" || result.Path != "docs/report.md" || result.Size != len("after\n") || result.SHA256 == "" {
		t.Fatalf("Write() result = %#v", result)
	}

	assertFile(t, path, "after\n", 0o640)
	assertNoWriteTemporary(t, filepath.Join(root, "docs"))

	unchanged, err := authoring.Write(t.Context(), files.WriteArgs{Path: "docs/report.md", Content: "after\n"})
	if err != nil || unchanged.Status != "unchanged" {
		t.Fatalf("unchanged Write() = %#v, %v", unchanged, err)
	}

	if err := authoring.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	assertFile(t, path, "before\n", 0o640)
}

func TestM3Authoring_AtomicWriteRejectsSymlinksAndTraversal(t *testing.T) {
	root := t.TempDir()

	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(outside, filepath.Join(root, "linked.txt")); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "linked-dir")); err != nil {
		t.Fatal(err)
	}

	authoring, err := files.NewAuthoring(root, []string{"linked.txt", "linked-dir/file.txt", "safe.txt"}, 3, 1024, 2048)
	if err != nil {
		t.Fatal(err)
	}
	defer authoring.Close()

	for _, path := range []string{"linked.txt", "linked-dir/file.txt", "../outside.txt", "/tmp/absolute", ".git/config"} {
		if _, err := authoring.Write(t.Context(), files.WriteArgs{Path: path, Content: "changed\n"}); !errors.Is(err, files.ErrWriteNotAllowed) {
			t.Errorf("Write(%q) error = %v, want ErrWriteNotAllowed", path, err)
		}
	}

	assertFile(t, outside, "outside\n", 0o600)
	assertNoWriteTemporary(t, root)
}

func TestM3Authoring_SymlinkSwapNeverEscapesRoot(t *testing.T) {
	root := t.TempDir()

	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}

	authoring, err := files.NewAuthoring(root, []string{"docs/report.md"}, 1, 1024, 2048)
	if err != nil {
		t.Fatal(err)
	}
	defer authoring.Close()

	stop := make(chan struct{})

	var swaps sync.WaitGroup
	swaps.Go(func() {
		path := filepath.Join(root, "docs", "report.md")

		for {
			select {
			case <-stop:
				return
			default:
				_ = os.Remove(path)
				_ = os.Symlink(outside, path)
				_ = os.Remove(path)
				_ = os.WriteFile(path, []byte("local\n"), 0o600)
			}
		}
	})

	for range 100 {
		_, _ = authoring.Write(t.Context(), files.WriteArgs{Path: "docs/report.md", Content: "safe\n"})
	}

	close(stop)
	swaps.Wait()

	assertFile(t, outside, "outside\n", 0o600)
}

func TestM3Authoring_AtomicWriteEnforcesRunLimits(t *testing.T) {
	root := t.TempDir()

	authoring, err := files.NewAuthoring(root, []string{"one.txt", "two.txt"}, 1, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer authoring.Close()

	if _, err := authoring.Write(t.Context(), files.WriteArgs{Path: "one.txt", Content: "12345"}); !errors.Is(err, files.ErrWriteNotAllowed) {
		t.Fatalf("oversized Write() error = %v", err)
	}

	if _, err := authoring.Write(t.Context(), files.WriteArgs{Path: "one.txt", Content: "1234"}); err != nil {
		t.Fatalf("first Write() error = %v", err)
	}

	if _, err := authoring.Write(t.Context(), files.WriteArgs{Path: "two.txt", Content: "x"}); !errors.Is(err, files.ErrWriteLimit) {
		t.Fatalf("second Write() error = %v, want ErrWriteLimit", err)
	}
}

func assertFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != content {
		t.Fatalf("%s content = %q, want %q", path, data, content)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != mode {
		t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), mode)
	}
}

func assertNoWriteTemporary(t *testing.T, root string) {
	t.Helper()

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".duto-write-") {
			t.Fatalf("temporary file remains: %s", entry.Name())
		}
	}
}
