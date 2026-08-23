//go:build darwin || linux

package git_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/PedroKlein/duto-ai/internal/tool/git"
)

func TestGitLog_CancellationTerminatesProcessGroup(t *testing.T) {
	root := initRepo(t)
	bin := t.TempDir()
	pidFile := filepath.Join(t.TempDir(), "child.pid")

	script := fmt.Sprintf("#!/bin/sh\n/bin/sleep 30 &\necho $! > %s\nwait\n", strconv.Quote(pidFile))
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", bin)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		_, err := git.GitLog(ctx, testPolicy(root), git.LogArgs{Count: 1})
		done <- err
	}()

	pid := waitForPID(t, pidFile)

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("GitLog() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("GitLog() did not return after cancellation")
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("child process %d survived cancellation", pid)
}

func waitForPID(t *testing.T, name string) int {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(name)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr != nil {
				t.Fatal(convErr)
			}

			return pid
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("fake Git process did not start")

	return 0
}
