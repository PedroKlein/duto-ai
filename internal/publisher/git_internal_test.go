package publisher

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyAuthoredBundleAcceptsShallowPublisherCheckout(t *testing.T) {
	t.Parallel()

	repository := filepath.Join(t.TempDir(), "source")
	runPublisherGitTest(t, "", "init", repository)
	runPublisherGitTest(t, repository, "config", "user.name", "Duto Test")
	runPublisherGitTest(t, repository, "config", "user.email", "duto@example.invalid")

	path := filepath.Join(repository, "proof.txt")
	if err := os.WriteFile(path, []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runPublisherGitTest(t, repository, "add", "--", "proof.txt")
	runPublisherGitTest(t, repository, "commit", "-m", "base")
	base := runPublisherGitTest(t, repository, "rev-parse", "HEAD")
	runPublisherGitTest(t, repository, "branch", "base", base)

	if err := os.WriteFile(path, []byte("authored\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runPublisherGitTest(t, repository, "commit", "-am", "authored")
	source := runPublisherGitTest(t, repository, "rev-parse", "HEAD")

	bundle := filepath.Join(t.TempDir(), "authored.bundle")
	runPublisherGitTest(t, repository, "bundle", "create", bundle, "HEAD", "^"+base)

	checkout := filepath.Join(t.TempDir(), "checkout")
	runPublisherGitTest(t, "", "clone", "--depth=1", "--branch", "base", "file://"+repository, checkout)

	if _, err := os.Stat(filepath.Join(checkout, ".git", "shallow")); err != nil {
		t.Fatalf("test checkout is not shallow: %v", err)
	}

	if err := verifyAuthoredBundle(checkout, bundle, base, source); err != nil {
		t.Fatalf("verifyAuthoredBundle() error = %v", err)
	}
}

func runPublisherGitTest(t *testing.T, directory string, args ...string) string {
	t.Helper()

	command := exec.Command("git", args...)
	command.Dir = directory

	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}

	return strings.TrimSpace(string(output))
}
