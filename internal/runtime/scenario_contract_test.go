//go:build integration

package runtime_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScenarioForbiddenV02ExecutableMarkers(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	for _, removed := range []string{"action.yml", ".github/ai-workflows", "smoketest"} {
		if _, err := os.Stat(filepath.Join(root, removed)); !os.IsNotExist(err) {
			t.Errorf("superseded v0.2 path still exists: %s", removed)
		}
	}

	markers := []string{
		"ResolveNames(",
		"web.request",
		"github.post-review",
		"github.post-comment",
		"github.add-labels",
		"github.create-issue",
		"github.edit-issue",
		"github.merge-pr",
		"github.request-reviewers",
		"GITHUB_OUTPUT",
		"GITHUB_STEP_SUMMARY",
		"--dry-run",
		"--output-format",
		"--output-file",
		"context_files",
		"max_tokens",
	}
	for _, directory := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, marker := range markers {
				if strings.Contains(string(content), marker) {
					t.Errorf("%s contains forbidden executable marker %q", path, marker)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", directory, err)
		}
	}
}
