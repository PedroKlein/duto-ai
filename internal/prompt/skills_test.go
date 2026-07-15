package prompt_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/prompt"
)

func TestSkillsRegistry_Discover(t *testing.T) {
	dir := t.TempDir()

	// Create skill files.
	writeSkillFile(t, dir, "code-review.md", "# Code Review Skill")
	writeSkillFile(t, dir, "security-audit.md", "# Security Audit Skill")
	writeSkillFile(t, dir, "not-a-skill.txt", "ignored")

	reg := prompt.NewSkillsRegistryFromDir(dir)
	names := reg.Names()

	sort.Strings(names)

	if len(names) != 2 {
		t.Fatalf("expected 2 skills, got %d: %v", len(names), names)
	}

	if names[0] != "code-review" || names[1] != "security-audit" {
		t.Errorf("names = %v, want [code-review security-audit]", names)
	}
}

func TestSkillsRegistry_Resolve_ByName(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "review.md", "# Review")

	reg := prompt.NewSkillsRegistryFromDir(dir)
	path := reg.Resolve("review")

	want := filepath.Join(dir, "review.md")
	if path != want {
		t.Errorf("Resolve(review) = %q, want %q", path, want)
	}
}

func TestSkillsRegistry_Resolve_ExactPathPriority(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "review.md", "# Review")

	exactPath := filepath.Join(dir, "review.md")

	reg := prompt.NewSkillsRegistryFromDir(dir)
	path := reg.Resolve(exactPath)

	if path != exactPath {
		t.Errorf("Resolve(exact path) = %q, want %q", path, exactPath)
	}
}

func TestSkillsRegistry_Resolve_Fallback(t *testing.T) {
	dir := t.TempDir() // empty directory

	reg := prompt.NewSkillsRegistryFromDir(dir)
	path := reg.Resolve("unknown-skill")

	// Should return the conventional fallback path.
	want := filepath.Join(prompt.DefaultSkillsDir, "unknown-skill.md")
	if path != want {
		t.Errorf("Resolve(unknown) = %q, want %q", path, want)
	}
}

func TestSkillsRegistry_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	reg := prompt.NewSkillsRegistryFromDir(dir)
	names := reg.Names()

	if len(names) != 0 {
		t.Errorf("expected 0 skills in empty dir, got %d", len(names))
	}
}

func TestSkillsRegistry_NonexistentDirectory(t *testing.T) {
	reg := prompt.NewSkillsRegistryFromDir("/nonexistent/path")
	names := reg.Names()

	if len(names) != 0 {
		t.Errorf("expected 0 skills for nonexistent dir, got %d", len(names))
	}
}

func writeSkillFile(t *testing.T, dir, name, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
