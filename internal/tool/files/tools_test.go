package files_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
	"github.com/PedroKlein/duto-ai/internal/tool/files"
)

func TestRegisterAll(t *testing.T) {
	root := t.TempDir()
	reg := dtool.NewRegistry()

	if err := files.RegisterAll(reg, root); err != nil {
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

	result, err := files.ReadFile(root, "hello.txt")
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

	result, err := files.ReadFile(root, "big.txt")
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

func TestReadFile_PathTraversal(t *testing.T) {
	root := t.TempDir()

	_, err := files.ReadFile(root, "../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal")
	}

	if !errors.Is(err, files.ErrPathTraversal) {
		t.Errorf("expected ErrPathTraversal, got: %v", err)
	}
}

func TestReadFile_AbsolutePath(t *testing.T) {
	root := t.TempDir()

	_, err := files.ReadFile(root, "/etc/passwd")
	if err == nil {
		t.Fatal("expected error for absolute path")
	}

	if !errors.Is(err, files.ErrPathTraversal) {
		t.Errorf("expected ErrPathTraversal, got: %v", err)
	}
}

func TestReadFile_Directory(t *testing.T) {
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := files.ReadFile(root, "subdir")
	if err == nil {
		t.Fatal("expected error for directory")
	}

	if !errors.Is(err, files.ErrIsDirectory) {
		t.Errorf("expected ErrIsDirectory, got: %v", err)
	}
}

func TestReadFile_NotFound(t *testing.T) {
	root := t.TempDir()

	_, err := files.ReadFile(root, "nonexistent.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestFindFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main")
	writeFile(t, root, "lib.go", "package lib")
	writeFile(t, root, "readme.md", "# Hello")

	result, err := files.FindFiles(root, "*.go", "")
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

	result, err := files.FindFiles(root, "*.go", "sub")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Paths) != 1 {
		t.Errorf("expected 1 file in sub/, got %d: %v", len(result.Paths), result.Paths)
	}
}

func TestFindFiles_PathTraversal(t *testing.T) {
	root := t.TempDir()

	_, err := files.FindFiles(root, "*.go", "../..")
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

	result, err := files.GrepFiles(root, "fmt\\.Println", "")
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

	result, err := files.GrepFiles(root, "bar", "data.txt")
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

	_, err := files.GrepFiles(root, "[invalid", "")
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}

	if !strings.Contains(err.Error(), "invalid regex") {
		t.Errorf("error should mention invalid regex, got: %v", err)
	}
}

func TestGrepFiles_PathTraversal(t *testing.T) {
	root := t.TempDir()

	_, err := files.GrepFiles(root, "pattern", "../../etc")
	if err == nil {
		t.Fatal("expected error for path traversal")
	}

	if !errors.Is(err, files.ErrPathTraversal) {
		t.Errorf("expected ErrPathTraversal, got: %v", err)
	}
}

// --- helpers ---

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
