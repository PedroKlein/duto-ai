package actiontest_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestActionEvidence_RedactedUploadTree(t *testing.T) {
	inv := setupProjectorInvocation(t, "result-failed.json")

	sourceCanaryPresent := false

	sourceCanaryBytes, err := os.ReadFile(filepath.Join(inv.runtimeEvidenceDir, "result.json"))
	if err != nil {
		t.Fatalf("action evidence setup failure: read source result fixture: %v", err)
	}

	for _, canary := range projectionCanaries(t) {
		if strings.Contains(string(sourceCanaryBytes), canary) {
			sourceCanaryPresent = true
			break
		}
	}

	if !sourceCanaryPresent {
		t.Fatalf("action evidence setup failure: source fixtures must contain at least one canary marker to prove leak detection")
	}

	result := runProjector(t, inv, nil)
	if result.exitCode != 0 {
		t.Fatalf("missing Action evidence behavior: action/project.sh must emit a redacted upload tree\nexit=%d\nstderr:\n%s", result.exitCode, result.stderr)
	}

	outputs := readGitHubEnvFile(t, inv.githubOutputPath)

	evidencePath := outputs["evidence-path"]
	if strings.TrimSpace(evidencePath) == "" {
		t.Fatalf("missing Action evidence behavior: evidence-path output must be set\noutputs=%v", outputs)
	}

	entries, err := os.ReadDir(evidencePath)
	if err != nil {
		t.Fatalf("missing Action evidence behavior: evidence-path directory %s must exist: %v", evidencePath, err)
	}

	gotFiles := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("missing Action evidence behavior: upload tree must contain files only, found directory %q", entry.Name())
		}

		gotFiles = append(gotFiles, entry.Name())
	}

	sort.Strings(gotFiles)

	wantFiles := []string{"events.jsonl", "manifest.json", "receipt.json", "summary.md"}
	if !reflect.DeepEqual(gotFiles, wantFiles) {
		t.Fatalf("missing Action evidence behavior: upload tree must contain exactly the redacted four-file allowlist\nwant: %v\ngot:  %v", wantFiles, gotFiles)
	}

	if _, err := os.Stat(filepath.Join(evidencePath, "result.json")); err == nil {
		t.Fatalf("missing Action evidence behavior: upload tree must not include contentful runtime result.json")
	}

	canaries := projectionCanaries(t)

	for _, fileName := range gotFiles {
		content, err := os.ReadFile(filepath.Join(evidencePath, fileName))
		if err != nil {
			t.Fatalf("action evidence setup failure: read evidence file %s: %v", fileName, err)
		}

		assertNoCanaryLeak(t, "Action evidence file "+fileName, content, canaries)
	}
}
