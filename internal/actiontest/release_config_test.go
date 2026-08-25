package actiontest_test

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

type releaseConfig struct {
	Archives []struct {
		Format  string   `yaml:"format"`
		Formats []string `yaml:"formats"`
		Files   []string `yaml:"files"`
	} `yaml:"archives"`
}

func TestReleaseConfig_BinaryOnlyArchives(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(repoRoot(t), ".goreleaser.yaml"))
	if err != nil {
		t.Fatalf("read GoReleaser config: %v", err)
	}

	var config releaseConfig
	if err := yaml.Unmarshal(content, &config); err != nil {
		t.Fatalf("parse GoReleaser config: %v", err)
	}

	if len(config.Archives) != 1 {
		t.Fatalf("release config must define exactly one archive")
	}

	archive := config.Archives[0]
	if archive.Format != "" || len(archive.Formats) != 1 || archive.Formats[0] != "tar.gz" {
		t.Fatalf("release archive must use the current tar.gz formats field")
	}

	if len(archive.Files) != 1 || archive.Files[0] != "none*" {
		t.Fatalf("release archives must package only the duto-ai binary")
	}
}
