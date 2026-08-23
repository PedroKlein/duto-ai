package tool_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

func TestGuard_DebitsAllApplicableLimitsAtomicallyWithoutRefunds(t *testing.T) {
	guards, err := dtool.NewGuards(
		dtool.RuntimeLimit{MaxCalls: 20, Timeout: time.Minute},
		map[string]dtool.ScopeLimit{
			"inspect": {
				Names:    []string{"files.read"},
				MaxCalls: 10,
				Timeout:  30 * time.Second,
				Tools: map[string]dtool.ToolLimit{
					"files.read": {MaxCalls: 5, Timeout: 5 * time.Second, MaxRequestBytes: 64, MaxResultBytes: 128},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("NewGuards() error = %v", err)
	}

	guard := guards["inspect"]

	var (
		admitted atomic.Int64
		wait     sync.WaitGroup
	)
	for range 32 {
		wait.Go(func() {
			if guard.Before("files.read") == nil {
				admitted.Add(1)
			}
		})
	}

	wait.Wait()

	if admitted.Load() != 5 {
		t.Fatalf("admitted calls = %d, want 5", admitted.Load())
	}

	if err := guard.Before("files.read"); !errors.Is(err, dtool.ErrToolCallLimit) {
		t.Fatalf("call after exhaustion error = %v", err)
	}
}

func TestGuard_RejectsUnknownAuthorityBeforeDebit(t *testing.T) {
	guards, err := dtool.NewGuards(
		dtool.RuntimeLimit{MaxCalls: 1, Timeout: time.Minute},
		map[string]dtool.ScopeLimit{"inspect": {Names: []string{"files.read"}, MaxCalls: 1, Timeout: time.Minute, Tools: map[string]dtool.ToolLimit{"files.read": {MaxCalls: 1, Timeout: time.Second}}}},
	)
	if err != nil {
		t.Fatalf("NewGuards() error = %v", err)
	}

	guard := guards["inspect"]
	if err := guard.Before("web.fetch"); !errors.Is(err, dtool.ErrToolNotAllowed) {
		t.Fatalf("unauthorized error = %v", err)
	}

	if err := guard.Before("files.read"); err != nil {
		t.Fatalf("authorized call after rejection = %v", err)
	}
}

func TestGuard_RunCounterIsSharedAcrossScopes(t *testing.T) {
	guards, err := dtool.NewGuards(
		dtool.RuntimeLimit{MaxCalls: 2, Timeout: time.Minute},
		map[string]dtool.ScopeLimit{
			"first":  {Names: []string{"files.read"}, MaxCalls: 2, Timeout: time.Minute, Tools: map[string]dtool.ToolLimit{"files.read": {MaxCalls: 2, Timeout: time.Second}}},
			"second": {Names: []string{"web.fetch"}, MaxCalls: 2, Timeout: time.Minute, Tools: map[string]dtool.ToolLimit{"web.fetch": {MaxCalls: 2, Timeout: time.Second}}},
		},
	)
	if err != nil {
		t.Fatalf("NewGuards() error = %v", err)
	}

	if err := guards["first"].Before("files.read"); err != nil {
		t.Fatalf("first call error = %v", err)
	}

	if err := guards["second"].Before("web.fetch"); err != nil {
		t.Fatalf("second call error = %v", err)
	}

	if err := guards["first"].Before("files.read"); !errors.Is(err, dtool.ErrToolCallLimit) {
		t.Fatalf("shared run limit error = %v", err)
	}
}

func TestGuard_ContextUsesEarliestDeadline(t *testing.T) {
	guards, err := dtool.NewGuards(
		dtool.RuntimeLimit{MaxCalls: 2, Timeout: time.Minute},
		map[string]dtool.ScopeLimit{"inspect": {Names: []string{"files.read"}, MaxCalls: 2, Timeout: 30 * time.Second, Tools: map[string]dtool.ToolLimit{"files.read": {MaxCalls: 2, Timeout: 5 * time.Second}}}},
	)
	if err != nil {
		t.Fatalf("NewGuards() error = %v", err)
	}

	caller, cancelCaller := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancelCaller()

	bounded, cancel, err := guards["inspect"].Context(caller, "files.read")
	if err != nil {
		t.Fatalf("Context() error = %v", err)
	}
	defer cancel()

	deadline, ok := bounded.Deadline()
	if !ok || time.Until(deadline) > 100*time.Millisecond {
		t.Fatalf("effective deadline = %v, ok = %v", deadline, ok)
	}
}
