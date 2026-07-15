package logging_test

import (
	"os"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/logging"
)

func TestIsGitHubActions(t *testing.T) {
	t.Run("false when env not set", func(t *testing.T) {
		t.Setenv("GITHUB_ACTIONS", "")

		if logging.IsGitHubActions() {
			t.Error("IsGitHubActions() = true, want false")
		}
	})

	t.Run("true when env is true", func(t *testing.T) {
		t.Setenv("GITHUB_ACTIONS", "true")

		if !logging.IsGitHubActions() {
			t.Error("IsGitHubActions() = false, want true")
		}
	})

	t.Run("false for other values", func(t *testing.T) {
		t.Setenv("GITHUB_ACTIONS", "1")

		if logging.IsGitHubActions() {
			t.Error("IsGitHubActions() = true for '1', want false")
		}
	})
}

func TestGHAGroup_NoOpOutsideGHA(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "")

	// Capture stdout to verify no output.
	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w

	logging.GHAGroup("test")
	logging.GHAEndGroup()

	os.Stdout = oldStdout

	w.Close()

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)

	r.Close()

	if n > 0 {
		t.Errorf("GHAGroup/GHAEndGroup produced output outside GHA: %q", string(buf[:n]))
	}
}

func TestGHAGroup_EmitsInsideGHA(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")

	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w

	logging.GHAGroup("Step: gather")
	logging.GHAEndGroup()

	os.Stdout = oldStdout

	w.Close()

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)

	r.Close()

	output := string(buf[:n])

	if output == "" {
		t.Fatal("GHAGroup/GHAEndGroup produced no output inside GHA")
	}

	if !contains(output, "::group::Step: gather") {
		t.Errorf("missing ::group:: in output: %q", output)
	}

	if !contains(output, "::endgroup::") {
		t.Errorf("missing ::endgroup:: in output: %q", output)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, sub string) bool {
	for i := range len(s) - len(sub) + 1 {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}

	return false
}
