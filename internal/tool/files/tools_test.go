package files_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
	"github.com/PedroKlein/duto-ai/internal/tool/files"
)

func TestRegisterAll(t *testing.T) {
	root := t.TempDir()
	reg := dtool.NewRegistry()

	if err := files.RegisterAll(reg, files.Policy{
		Root: root,
		Limits: map[string]dtool.ToolLimit{
			"files.find": {MaxCalls: 1, Timeout: time.Second, MaxResultBytes: 1024},
			"files.grep": {MaxCalls: 1, Timeout: time.Second, MaxResultBytes: 1024},
			"files.read": {MaxCalls: 1, Timeout: time.Second, MaxResultBytes: 1024},
		},
	}); err != nil {
		t.Fatalf("RegisterAll failed: %v", err)
	}

	want := []string{"files.find", "files.grep", "files.read"}
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

func TestReadFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "hello.txt", "hello world")

	result, err := files.ReadFile(context.Background(), testPolicy(root, 2<<20), "hello.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Content != "hello world" {
		t.Errorf("content = %q, want %q", result.Content, "hello world")
	}

	if result.Truncated {
		t.Error("expected truncated=false for small file")
	}
}

func TestReadFile_Truncation(t *testing.T) {
	root := t.TempDir()
	bigContent := strings.Repeat("x", 1<<20+100)
	writeFile(t, root, "big.txt", bigContent)

	result, err := files.ReadFile(context.Background(), testPolicy(root, 2<<20), "big.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Truncated {
		t.Error("expected truncated=true for large file")
	}

	if len(result.Content) != 1<<20 {
		t.Errorf("content length = %d, want %d", len(result.Content), 1<<20)
	}
}

func TestReadFile_ContextAndResultLimit(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "data.txt", strings.Repeat("x", 256))

	result, err := files.ReadFile(context.Background(), testPolicy(root, 80), "data.txt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if !result.Truncated || len(result.Content) >= 256 {
		t.Fatalf("ReadFile() = content bytes %d, truncated %v", len(result.Content), result.Truncated)
	}

	assertResultFits(t, result, 80)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := files.ReadFile(ctx, testPolicy(root, 80), "data.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadFile() cancellation error = %v", err)
	}
}

func TestReadFile_PathTraversal(t *testing.T) {
	root := t.TempDir()

	_, err := files.ReadFile(context.Background(), testPolicy(root, 2<<20), "../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal")
	}

	if !errors.Is(err, files.ErrPathTraversal) {
		t.Errorf("expected ErrPathTraversal, got: %v", err)
	}
}

func TestReadFile_AbsolutePath(t *testing.T) {
	root := t.TempDir()

	_, err := files.ReadFile(context.Background(), testPolicy(root, 2<<20), "/etc/passwd")
	if err == nil {
		t.Fatal("expected error for absolute path")
	}

	if !errors.Is(err, files.ErrPathTraversal) {
		t.Errorf("expected ErrPathTraversal, got: %v", err)
	}
}

func TestReadFile_SymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "secret.txt", "secret")

	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "escape.txt")); err != nil {
		t.Fatal(err)
	}

	if _, err := files.ReadFile(context.Background(), testPolicy(root, 2<<20), "escape.txt"); err == nil {
		t.Fatal("ReadFile() error = nil for escaping symlink")
	}
}

func TestReadFile_Directory(t *testing.T) {
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := files.ReadFile(context.Background(), testPolicy(root, 2<<20), "subdir")
	if err == nil {
		t.Fatal("expected error for directory")
	}

	if !errors.Is(err, files.ErrIsDirectory) {
		t.Errorf("expected ErrIsDirectory, got: %v", err)
	}
}

func TestReadFile_NotFound(t *testing.T) {
	root := t.TempDir()

	_, err := files.ReadFile(context.Background(), testPolicy(root, 2<<20), "nonexistent.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestFindFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main")
	writeFile(t, root, "lib.go", "package lib")
	writeFile(t, root, "readme.md", "# Hello")

	result, err := files.FindFiles(context.Background(), testPolicy(root, 2<<20), "*.go", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Paths) != 2 {
		t.Errorf("expected 2 .go files, got %d: %v", len(result.Paths), result.Paths)
	}
}

func TestFindFiles_Subdirectory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "")
	writeFile(t, root, "sub/b.go", "")
	writeFile(t, root, "sub/c.txt", "")

	result, err := files.FindFiles(context.Background(), testPolicy(root, 2<<20), "*.go", "sub")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Paths) != 1 {
		t.Errorf("expected 1 file in sub/, got %d: %v", len(result.Paths), result.Paths)
	}
}

func TestFindFiles_DeterministicAndBounded(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"z.go", "a.go", "m.go"} {
		writeFile(t, root, name, "package example")
	}

	result, err := files.FindFiles(context.Background(), testPolicy(root, 40), "*.go", "")
	if err != nil {
		t.Fatalf("FindFiles() error = %v", err)
	}

	if !result.Truncated || len(result.Paths) == 0 || result.Paths[0] != "a.go" {
		t.Fatalf("FindFiles() = %#v", result)
	}

	assertResultFits(t, result, 40)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := files.FindFiles(ctx, testPolicy(root, 40), "*.go", ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("FindFiles() cancellation error = %v", err)
	}
}

func TestFindFiles_SkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "outside.go", "package outside")

	if err := os.Symlink(filepath.Join(outside, "outside.go"), filepath.Join(root, "escape.go")); err != nil {
		t.Fatal(err)
	}

	result, err := files.FindFiles(context.Background(), testPolicy(root, 2<<20), "*.go", "")
	if err != nil {
		t.Fatalf("FindFiles() error = %v", err)
	}

	if len(result.Paths) != 0 {
		t.Fatalf("FindFiles() exposed symlink: %v", result.Paths)
	}
}

func TestFindFiles_PathTraversal(t *testing.T) {
	root := t.TempDir()

	_, err := files.FindFiles(context.Background(), testPolicy(root, 2<<20), "*.go", "../..")
	if err == nil {
		t.Fatal("expected error for path traversal in dir")
	}

	if !errors.Is(err, files.ErrPathTraversal) {
		t.Errorf("expected ErrPathTraversal, got: %v", err)
	}
}

func TestGrepFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n")

	result, err := files.GrepFiles(context.Background(), testPolicy(root, 2<<20), "fmt\\.Println", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(result.Matches))
	}

	m := result.Matches[0]
	if m.File != "main.go" {
		t.Errorf("match file = %q, want %q", m.File, "main.go")
	}

	if m.Line != 4 {
		t.Errorf("match line = %d, want 4", m.Line)
	}
}

func TestGrepFiles_SingleFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "data.txt", "foo\nbar\nbaz\nbar again\n")

	result, err := files.GrepFiles(context.Background(), testPolicy(root, 2<<20), "bar", "data.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Matches) != 2 {
		t.Errorf("expected 2 matches, got %d", len(result.Matches))
	}
}

func TestGrepFiles_InvalidRegex(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "file.txt", "content")

	_, err := files.GrepFiles(context.Background(), testPolicy(root, 2<<20), "[invalid", "")
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}

	if !strings.Contains(err.Error(), "invalid regex") {
		t.Errorf("error should mention invalid regex, got: %v", err)
	}
}

func TestGrepFiles_DeterministicBoundedAndCancelled(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "z.txt", "match z\n")
	writeFile(t, root, "a.txt", "match a\nmatch b\n")

	result, err := files.GrepFiles(context.Background(), testPolicy(root, 100), "match", "")
	if err != nil {
		t.Fatalf("GrepFiles() error = %v", err)
	}

	if !result.Truncated || len(result.Matches) == 0 || result.Matches[0].File != "a.txt" {
		t.Fatalf("GrepFiles() = %#v", result)
	}

	assertResultFits(t, result, 100)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := files.GrepFiles(ctx, testPolicy(root, 100), "match", ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("GrepFiles() cancellation error = %v", err)
	}
}

func TestGrepFiles_RejectsSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "secret.txt", "match secret")

	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "escape.txt")); err != nil {
		t.Fatal(err)
	}

	if _, err := files.GrepFiles(context.Background(), testPolicy(root, 2<<20), "match", "escape.txt"); err == nil {
		t.Fatal("GrepFiles() error = nil for escaping symlink")
	}
}

func TestGrepFiles_PathTraversal(t *testing.T) {
	root := t.TempDir()

	_, err := files.GrepFiles(context.Background(), testPolicy(root, 2<<20), "pattern", "../../etc")
	if err == nil {
		t.Fatal("expected error for path traversal")
	}

	if !errors.Is(err, files.ErrPathTraversal) {
		t.Errorf("expected ErrPathTraversal, got: %v", err)
	}
}

// --- helpers ---

func assertResultFits(t *testing.T, result any, limit int) {
	t.Helper()

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	if len(encoded) > limit {
		t.Fatalf("encoded result bytes = %d, limit %d", len(encoded), limit)
	}
}

func testPolicy(root string, resultBytes int) files.Policy {
	limits := map[string]dtool.ToolLimit{
		"files.find": {MaxCalls: 10, Timeout: time.Second, MaxRequestBytes: 1024, MaxResultBytes: resultBytes},
		"files.grep": {MaxCalls: 10, Timeout: time.Second, MaxRequestBytes: 1024, MaxResultBytes: resultBytes},
		"files.read": {MaxCalls: 10, Timeout: time.Second, MaxRequestBytes: 1024, MaxResultBytes: resultBytes},
	}

	return files.Policy{Root: root, Limits: limits}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()

	path := filepath.Join(root, rel)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
