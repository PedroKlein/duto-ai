package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/PedroKlein/duto-ai/internal/safeoutput"
)

type FileRemote struct {
	path string
}

type fileRemoteState struct {
	Replies  map[string]fileReply `json:"replies"`
	Branches map[string]string    `json:"branches"`
	PRs      map[string]filePR    `json:"pull_requests"`
}

type fileReply struct {
	PayloadSHA256 string `json:"payload_sha256"`
	Resource      string `json:"resource"`
}

type filePR struct {
	SourceCommit string `json:"source_commit"`
	Title        string `json:"title"`
	BodySHA256   string `json:"body_sha256"`
	Resource     string `json:"resource"`
}

func NewFileRemote(path string) (*FileRemote, error) {
	if path == "" {
		return nil, ErrRejected
	}

	return &FileRemote{path: path}, nil
}

func (r *FileRemote) Preflight(ctx context.Context, operation Operation) (RemoteState, error) {
	if err := ctx.Err(); err != nil {
		return RemoteState{}, fmt.Errorf("preflighting file remote: %w", err)
	}

	state, err := r.load()
	if err != nil {
		return RemoteState{}, err
	}

	switch operation.Kind {
	case safeoutput.KindReply:
		value, exists := state.Replies[operation.CorrelationKey]
		if !exists {
			return RemoteState{}, nil
		}

		if value.PayloadSHA256 == operation.PayloadSHA256 {
			return RemoteState{Disposition: DispositionUnchanged, Resource: value.Resource}, nil
		}

		return RemoteState{Disposition: DispositionConflict, Resource: value.Resource}, nil
	case safeoutput.KindBranch:
		value, exists := state.Branches[operation.TargetRef]
		if !exists {
			return RemoteState{}, nil
		}

		if value == operation.SourceCommit {
			return RemoteState{Disposition: DispositionUnchanged, Resource: operation.TargetRef}, nil
		}

		return RemoteState{Disposition: DispositionConflict, Resource: operation.TargetRef}, nil
	case safeoutput.KindDraftPR:
		value, exists := state.PRs[operation.TargetRef]
		if !exists {
			return RemoteState{}, nil
		}

		if value.SourceCommit == operation.SourceCommit && value.Title == operation.PRTitle && value.BodySHA256 == operation.PayloadSHA256 {
			return RemoteState{Disposition: DispositionUnchanged, Resource: value.Resource}, nil
		}

		return RemoteState{Disposition: DispositionConflict, Resource: value.Resource}, nil
	default:
		return RemoteState{}, ErrRejected
	}
}

func (r *FileRemote) Apply(ctx context.Context, operation Operation) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("applying file remote: %w", err)
	}

	state, err := r.load()
	if err != nil {
		return "", err
	}

	var resource string

	switch operation.Kind {
	case safeoutput.KindReply:
		resource = fmt.Sprintf("https://github.com/%s/%s/issues/%d#issuecomment-1", operation.RepositoryOwner, operation.RepositoryName, operation.OriginNumber)
		state.Replies[operation.CorrelationKey] = fileReply{PayloadSHA256: operation.PayloadSHA256, Resource: resource}
	case safeoutput.KindBranch:
		resource = operation.TargetRef
		state.Branches[operation.TargetRef] = operation.SourceCommit
	case safeoutput.KindDraftPR:
		if state.Branches[operation.TargetRef] != operation.SourceCommit {
			return "", ErrConflict
		}

		resource = fmt.Sprintf("https://github.com/%s/%s/pull/1", operation.RepositoryOwner, operation.RepositoryName)
		state.PRs[operation.TargetRef] = filePR{SourceCommit: operation.SourceCommit, Title: operation.PRTitle, BodySHA256: operation.PayloadSHA256, Resource: resource}
	default:
		return "", ErrRejected
	}

	if err := r.store(state); err != nil {
		return "", err
	}

	return resource, nil
}

func (r *FileRemote) load() (fileRemoteState, error) {
	state := fileRemoteState{Replies: make(map[string]fileReply), Branches: make(map[string]string), PRs: make(map[string]filePR)}

	data, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}

	if err != nil {
		return fileRemoteState{}, fmt.Errorf("reading file remote state: %w", err)
	}

	if err := json.Unmarshal(data, &state); err != nil {
		return fileRemoteState{}, fmt.Errorf("decoding file remote state: %w", err)
	}

	if state.Replies == nil || state.Branches == nil || state.PRs == nil {
		return fileRemoteState{}, ErrRejected
	}

	return state, nil
}

func (r *FileRemote) store(state fileRemoteState) error {
	encoded, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encoding file remote state: %w", err)
	}

	mkdirErr := os.MkdirAll(filepath.Dir(r.path), 0o700)
	if mkdirErr != nil {
		return fmt.Errorf("creating file remote directory: %w", mkdirErr)
	}

	temporary, err := os.CreateTemp(filepath.Dir(r.path), ".duto-publisher-state-")
	if err != nil {
		return fmt.Errorf("creating file remote state: %w", err)
	}

	name := temporary.Name()
	defer os.Remove(name) //nolint:errcheck // deterministic adapter state cleanup is best effort

	chmodErr := temporary.Chmod(0o600)
	if chmodErr != nil {
		_ = temporary.Close()
		return fmt.Errorf("setting file remote state mode: %w", chmodErr)
	}

	_, writeErr := temporary.Write(append(encoded, '\n'))
	if writeErr != nil {
		_ = temporary.Close()
		return fmt.Errorf("writing file remote state: %w", writeErr)
	}

	syncErr := temporary.Sync()
	if syncErr != nil {
		_ = temporary.Close()
		return fmt.Errorf("syncing file remote state: %w", syncErr)
	}

	if closeErr := temporary.Close(); closeErr != nil {
		return fmt.Errorf("closing file remote state: %w", closeErr)
	}

	if renameErr := os.Rename(name, r.path); renameErr != nil {
		return fmt.Errorf("renaming file remote state: %w", renameErr)
	}

	return nil
}
