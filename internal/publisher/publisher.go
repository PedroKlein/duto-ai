package publisher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
)

const (
	DispositionApplied   = "applied"
	DispositionUnchanged = "unchanged"
	DispositionRejected  = "rejected"
	DispositionConflict  = "conflict"
)

var (
	ErrRejected = errors.New("publisher request rejected")
	ErrConflict = errors.New("publisher resource conflict")
)

type Operation struct {
	RequestID       string
	CorrelationKey  string
	Kind            string
	RepositoryOwner string
	RepositoryName  string
	OriginKind      string
	OriginNumber    int
	BaseRef         string
	BaseSHA         string
	SourceCommit    string
	TargetRef       string
	ReplyBody       string
	PRTitle         string
	PRBody          string
	PayloadSHA256   string
	SourceBundle    string
}

type RemoteState struct {
	Disposition string
	Resource    string
}

type Remote interface {
	Preflight(context.Context, Operation) (RemoteState, error)
	Apply(context.Context, Operation) (string, error)
}

type OperationReceipt struct {
	RequestID   string `json:"request_id"`
	Kind        string `json:"kind"`
	Disposition string `json:"disposition"`
	Resource    string `json:"resource"`
}

type Receipt struct {
	Version        int                `json:"version"`
	PublisherRunID string             `json:"publisher_run_id"`
	BundleSHA256   string             `json:"bundle_sha256"`
	PlanSHA256     string             `json:"plan_sha256"`
	PolicySHA256   string             `json:"policy_sha256"`
	RepositoryID   string             `json:"repository_id"`
	OperationSet   string             `json:"operation_set"`
	Disposition    string             `json:"disposition"`
	Operations     []OperationReceipt `json:"operations"`
}

func (v *Verified) Repository() (owner, repository string) {
	if v == nil || len(v.operations) == 0 {
		return "", ""
	}

	return v.operations[0].RepositoryOwner, v.operations[0].RepositoryName
}

func (v *Verified) Publish(ctx context.Context, remote Remote) (*Receipt, error) {
	if v == nil || remote == nil {
		return nil, ErrRejected
	}

	receipt := v.receipt()

	states, err := v.preflight(ctx, remote, receipt)
	if err != nil {
		return receipt, err
	}

	if allUnchanged(states) {
		receipt.Disposition = DispositionUnchanged
		for index, operation := range v.operations {
			receipt.Operations[index] = operationReceipt(operation, states[index])
		}

		return receipt, nil
	}

	if err := v.apply(ctx, remote, receipt, states); err != nil {
		return receipt, err
	}

	receipt.Disposition = DispositionApplied

	return receipt, nil
}

func (v *Verified) preflight(ctx context.Context, remote Remote, receipt *Receipt) ([]RemoteState, error) {
	states := make([]RemoteState, len(v.operations))
	for index, operation := range v.operations {
		state, err := remote.Preflight(ctx, operation)
		if err != nil {
			receipt.Disposition = DispositionRejected
			return nil, fmtRemoteError("preflighting remote operation", err)
		}

		if state.Disposition != "" && state.Disposition != DispositionUnchanged && state.Disposition != DispositionConflict {
			return nil, ErrRejected
		}

		states[index] = state
		if state.Disposition == DispositionConflict {
			receipt.Disposition = DispositionConflict
			receipt.Operations[index] = operationReceipt(operation, state)

			return nil, ErrConflict
		}
	}

	return states, nil
}

func (v *Verified) apply(ctx context.Context, remote Remote, receipt *Receipt, states []RemoteState) error {
	for index, operation := range v.operations {
		if states[index].Disposition == DispositionUnchanged {
			receipt.Operations[index] = operationReceipt(operation, states[index])
			continue
		}

		resource, err := remote.Apply(ctx, operation)
		if err == nil {
			receipt.Operations[index] = OperationReceipt{RequestID: operation.RequestID, Kind: operation.Kind, Disposition: DispositionApplied, Resource: resource}
			continue
		}

		reconciled, reconcileErr := remote.Preflight(ctx, operation)
		if reconcileErr == nil && reconciled.Disposition == DispositionUnchanged {
			receipt.Operations[index] = OperationReceipt{RequestID: operation.RequestID, Kind: operation.Kind, Disposition: DispositionApplied, Resource: reconciled.Resource}
			continue
		}

		receipt.Disposition = DispositionConflict

		return fmtRemoteError("applying remote operation", err)
	}

	return nil
}

func allUnchanged(states []RemoteState) bool {
	return !slices.ContainsFunc(states, func(state RemoteState) bool {
		return state.Disposition != DispositionUnchanged
	})
}

func operationReceipt(operation Operation, state RemoteState) OperationReceipt {
	return OperationReceipt{RequestID: operation.RequestID, Kind: operation.Kind, Disposition: state.Disposition, Resource: state.Resource}
}

func Marker(operation Operation) string {
	correlation := sha256.Sum256([]byte(operation.CorrelationKey))

	return "<!-- duto:m3:" + hex.EncodeToString(correlation[:]) + ":" + operation.PayloadSHA256 + " -->"
}

func MarkerPrefix(operation Operation) string {
	correlation := sha256.Sum256([]byte(operation.CorrelationKey))

	return "<!-- duto:m3:" + hex.EncodeToString(correlation[:]) + ":"
}

func fmtRemoteError(action string, err error) error {
	if errors.Is(err, ErrRejected) {
		return fmt.Errorf("%s: %w", action, ErrRejected)
	}

	return fmt.Errorf("%s: %w", action, ErrConflict)
}
