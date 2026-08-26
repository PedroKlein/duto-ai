package publisher_test

import (
	"context"
	"errors"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/publisher"
	"github.com/PedroKlein/duto-ai/internal/safeoutput"
)

// spyRemote records call ordering so tests can prove preflight runs before apply.
type spyRemote struct {
	calls        []string
	preflight    map[string]publisher.RemoteState
	preflightErr map[string]error
	applyErr     map[string]error
}

func newSpyRemote() *spyRemote {
	return &spyRemote{
		preflight:    map[string]publisher.RemoteState{},
		preflightErr: map[string]error{},
		applyErr:     map[string]error{},
	}
}

func (s *spyRemote) Preflight(_ context.Context, operation publisher.Operation) (publisher.RemoteState, error) {
	s.calls = append(s.calls, "preflight:"+operation.RequestID)
	if err := s.preflightErr[operation.RequestID]; err != nil {
		return publisher.RemoteState{}, err
	}

	return s.preflight[operation.RequestID], nil
}

func (s *spyRemote) Apply(_ context.Context, operation publisher.Operation) (string, error) {
	s.calls = append(s.calls, "apply:"+operation.RequestID)
	if err := s.applyErr[operation.RequestID]; err != nil {
		return "", err
	}

	return "https://example.invalid/resource/" + operation.RequestID, nil
}

func replyOperation(requestID string) publisher.Operation {
	return publisher.Operation{
		RequestID: requestID, CorrelationKey: "issue-42", Kind: safeoutput.KindReply,
		RepositoryOwner: "example-owner", RepositoryName: "example-repository",
		OriginKind: "issue", OriginNumber: 42, ReplyBody: "hello", PayloadSHA256: "payload-" + requestID,
	}
}

func verifiedReply(requestID string) *publisher.Verified {
	return publisher.NewVerifiedForTest(safeoutput.ConversationReply, []publisher.Operation{replyOperation(requestID)})
}

func TestPublish_RejectsNilRemoteBeforeAnyCall(t *testing.T) {
	t.Parallel()

	if _, err := verifiedReply("a").Publish(context.Background(), nil); !errors.Is(err, publisher.ErrRejected) {
		t.Fatalf("Publish(nil remote) error = %v, want ErrRejected", err)
	}
}

func TestPublish_PreflightPrecedesApply(t *testing.T) {
	t.Parallel()

	remote := newSpyRemote()

	receipt, err := verifiedReply("a").Publish(context.Background(), remote)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	if len(remote.calls) != 2 || remote.calls[0] != "preflight:a" || remote.calls[1] != "apply:a" {
		t.Fatalf("call order = %v, want preflight before apply", remote.calls)
	}

	if receipt.Disposition != publisher.DispositionApplied || receipt.Operations[0].Disposition != publisher.DispositionApplied {
		t.Fatalf("receipt = %#v, want applied", receipt)
	}
}

func TestPublish_AllUnchangedSkipsApply(t *testing.T) {
	t.Parallel()

	remote := newSpyRemote()
	remote.preflight["a"] = publisher.RemoteState{Disposition: publisher.DispositionUnchanged, Resource: "existing"}

	receipt, err := verifiedReply("a").Publish(context.Background(), remote)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	for _, call := range remote.calls {
		if call == "apply:a" {
			t.Fatalf("apply called for unchanged operation: %v", remote.calls)
		}
	}

	if receipt.Disposition != publisher.DispositionUnchanged || receipt.Operations[0].Resource != "existing" {
		t.Fatalf("receipt = %#v, want unchanged with existing resource", receipt)
	}
}

func TestPublish_ConflictAtPreflightStopsBeforeApply(t *testing.T) {
	t.Parallel()

	remote := newSpyRemote()
	remote.preflight["a"] = publisher.RemoteState{Disposition: publisher.DispositionConflict, Resource: "other"}

	receipt, err := verifiedReply("a").Publish(context.Background(), remote)
	if !errors.Is(err, publisher.ErrConflict) {
		t.Fatalf("Publish() error = %v, want ErrConflict", err)
	}

	for _, call := range remote.calls {
		if call == "apply:a" {
			t.Fatalf("apply called after conflict: %v", remote.calls)
		}
	}

	if receipt.Disposition != publisher.DispositionConflict {
		t.Fatalf("receipt disposition = %q, want conflict", receipt.Disposition)
	}
}

func TestPublish_PreflightErrorRejectsWithoutApply(t *testing.T) {
	t.Parallel()

	remote := newSpyRemote()
	remote.preflightErr["a"] = publisher.ErrRejected

	receipt, err := verifiedReply("a").Publish(context.Background(), remote)
	if !errors.Is(err, publisher.ErrRejected) {
		t.Fatalf("Publish() error = %v, want ErrRejected", err)
	}

	if len(remote.calls) != 1 || remote.calls[0] != "preflight:a" {
		t.Fatalf("call order = %v, want a single preflight", remote.calls)
	}

	if receipt.Disposition != publisher.DispositionRejected {
		t.Fatalf("receipt disposition = %q, want rejected", receipt.Disposition)
	}
}

func TestPublish_ApplyErrorReconcilesToUnchanged(t *testing.T) {
	t.Parallel()

	// First preflight absent, apply fails, reconcile preflight returns unchanged.
	staged := &reconcileRemote{apply: publisher.ErrConflict, reconcile: publisher.RemoteState{Disposition: publisher.DispositionUnchanged, Resource: "reconciled"}}

	receipt, err := verifiedReply("a").Publish(context.Background(), staged)
	if err != nil {
		t.Fatalf("Publish() error = %v, want reconciled success", err)
	}

	if receipt.Disposition != publisher.DispositionApplied || receipt.Operations[0].Resource != "reconciled" {
		t.Fatalf("receipt = %#v, want applied via reconcile", receipt)
	}
}

// reconcileRemote returns an absent preflight, a failing apply, then an
// unchanged reconcile preflight, exercising the apply-error reconcile path.
type reconcileRemote struct {
	preflights int
	apply      error
	reconcile  publisher.RemoteState
}

func (r *reconcileRemote) Preflight(context.Context, publisher.Operation) (publisher.RemoteState, error) {
	r.preflights++
	if r.preflights == 1 {
		return publisher.RemoteState{}, nil
	}

	return r.reconcile, nil
}

func (r *reconcileRemote) Apply(context.Context, publisher.Operation) (string, error) {
	return "", r.apply
}
