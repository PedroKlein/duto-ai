package publisher_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/publisher"
	"github.com/PedroKlein/duto-ai/internal/safeoutput"
)

func fileRemoteFixture(t *testing.T) *publisher.FileRemote {
	t.Helper()

	remote, err := publisher.NewFileRemote(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("NewFileRemote() error = %v", err)
	}

	return remote
}

func TestFileRemote_ReplyIsIdempotentThenConflicts(t *testing.T) {
	t.Parallel()

	remote := fileRemoteFixture(t)
	operation := replyOperation("a")

	state, err := remote.Preflight(context.Background(), operation)
	if err != nil || state.Disposition != "" {
		t.Fatalf("first Preflight = %#v, err = %v, want absent", state, err)
	}

	if _, applyErr := remote.Apply(context.Background(), operation); applyErr != nil {
		t.Fatalf("Apply() error = %v", applyErr)
	}

	state, err = remote.Preflight(context.Background(), operation)
	if err != nil || state.Disposition != publisher.DispositionUnchanged {
		t.Fatalf("second Preflight = %#v, err = %v, want unchanged", state, err)
	}

	changed := operation
	changed.PayloadSHA256 = "different-payload"

	state, err = remote.Preflight(context.Background(), changed)
	if err != nil || state.Disposition != publisher.DispositionConflict {
		t.Fatalf("changed Preflight = %#v, err = %v, want conflict", state, err)
	}
}

func TestFileRemote_DraftPRRequiresAppliedBranch(t *testing.T) {
	t.Parallel()

	remote := fileRemoteFixture(t)
	sourceCommit := strings.Repeat("0", 40)

	pr := publisher.Operation{
		RequestID: "pr", CorrelationKey: "issue-42", Kind: safeoutput.KindDraftPR,
		RepositoryOwner: "example-owner", RepositoryName: "example-repository",
		TargetRef: "refs/heads/duto/m3/issue-42", SourceCommit: sourceCommit,
		PRTitle: "title", PayloadSHA256: "pr-payload",
	}

	if _, err := remote.Apply(context.Background(), pr); !errors.Is(err, publisher.ErrConflict) {
		t.Fatalf("draft PR before branch error = %v, want ErrConflict", err)
	}

	branch := publisher.Operation{
		RequestID: "branch", CorrelationKey: "issue-42", Kind: safeoutput.KindBranch,
		RepositoryOwner: "example-owner", RepositoryName: "example-repository",
		TargetRef: "refs/heads/duto/m3/issue-42", SourceCommit: sourceCommit,
	}
	if _, err := remote.Apply(context.Background(), branch); err != nil {
		t.Fatalf("branch Apply() error = %v", err)
	}

	if _, err := remote.Apply(context.Background(), pr); err != nil {
		t.Fatalf("draft PR after branch error = %v, want success", err)
	}

	state, err := remote.Preflight(context.Background(), pr)
	if err != nil || state.Disposition != publisher.DispositionUnchanged {
		t.Fatalf("draft PR Preflight = %#v, err = %v, want unchanged", state, err)
	}
}

func TestFileRemote_UnknownKindRejected(t *testing.T) {
	t.Parallel()

	remote := fileRemoteFixture(t)
	operation := publisher.Operation{RequestID: "x", Kind: "unknown.kind", CorrelationKey: "issue-42"}

	if _, err := remote.Preflight(context.Background(), operation); !errors.Is(err, publisher.ErrRejected) {
		t.Fatalf("Preflight(unknown kind) error = %v, want ErrRejected", err)
	}

	if _, err := remote.Apply(context.Background(), operation); !errors.Is(err, publisher.ErrRejected) {
		t.Fatalf("Apply(unknown kind) error = %v, want ErrRejected", err)
	}
}
