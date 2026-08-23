//go:build darwin || linux

package shell_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/PedroKlein/duto-ai/internal/tool/shell"
)

func TestRun_CancellationTerminatesDescendants(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	policy := testPolicy(t, `/bin/sleep 30 & printf '%s' "$!" > "$1"; wait`, pidFile)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		_, err := shell.Run(ctx, policy)
		done <- err
	}()

	pid := waitForPID(t, pidFile)

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.NewTimer(3 * time.Second).C:
		t.Fatal("Run() did not return after cancellation")
	}

	waitForExit(t, pid)
}

func TestRun_TimeoutTerminatesDescendants(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	policy := testPolicy(t, `/bin/sleep 30 & printf '%s' "$!" > "$1"; wait`, pidFile)
	policy.Limit.Timeout = 500 * time.Millisecond
	done := make(chan error, 1)

	go func() {
		_, err := shell.Run(context.Background(), policy)
		done <- err
	}()

	pid := waitForPID(t, pidFile)

	select {
	case err := <-done:
		if !errors.Is(err, shell.ErrTimeout) {
			t.Fatalf("Run() error = %v, want ErrTimeout", err)
		}
	case <-time.NewTimer(3 * time.Second).C:
		t.Fatal("Run() did not return after timeout")
	}

	waitForExit(t, pid)
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

	t.Fatal("child process did not start")

	return 0
}

func waitForExit(t *testing.T, pid int) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("child process %d survived cancellation", pid)
}
